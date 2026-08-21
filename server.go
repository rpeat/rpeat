package rpeat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	//"github.com/google/uuid"
	"github.com/go-kit/kit/endpoint"
	"time"
	//"github.com/davecgh/go-spew/spew"
)

type Service interface {
	AllJobs(string, []string) (jobsResponse, error) // FIXME: add this in
	AllStatus(string, []string) (jobsStatusResponse, error)
	Info(string, string) (*Job, error)
	Dependencies(string, string) (DependencyGraphs, error)
	ServerInfo(string) (*ServerConfig, error)
	ServerRestart(string) (*ServerConfig, error)

	// Controls
	Start(string, string, string) (*controlResponse, error)
	Stop(string, string, string) (*controlResponse, error)
	Hold(string, string, string, string) (*controlResponse, error)
	Restart(string, string, string) (*controlResponse, error)
	Log(string, string, string, bool, bool, int, int64) (*LogOutput, error)

	Resume(string, string) (*JobUpdateParams, error) // not implemented and may never be
	Status(string, string) (*JobUpdateParams, error)
}
type service struct {
	Jobs         jobMap
	ServerConfig ServerConfig
	Config       string
	Home         string
	JobOrder     []string
	sd           *ServerData
}

func (k service) ServerShutdown() error {
	return nil
}
func (k service) ServerRestart(user string) (*ServerConfig, error) {
	ServerLogger.Printf("\tSERVER RESTART\tuser:%s", user)
	sconf := k.ServerConfig
	if permitted := sconf.hasPermission(user, "restart"); !permitted {
		return &sconf, ErrEmpty
	}
	ServerLogger.Printf(WarningColor, "sleeping 1s before restart")
	time.Sleep(time.Second)
	ServerLogger.Printf(WarningColor, "restarting")
	sconf.reloadJobs(k.sd)
	return &sconf, nil
}

// request all jobs, filtered by reqgroups, user/admin authentication, permission
func (k service) AllJobs(user string, reqgroups []string) (jobsResponse, error) {
	ServerLogger.Printf("\tALLSTATUS\tuser:%s", user)
	job_order := make([]string, len(k.sd.job_order))
	copy(job_order, k.sd.job_order)
	for i := 0; i < len(job_order); i++ {
		v := k.Jobs[job_order[i]]
		if user != v.User && !stringInSlice(user, v.Admin) {
			job_order = append(job_order[:i], job_order[i+1:]...)
		}
	}
	jobs_static := make(map[string]Job, 0)
	authorized := make(map[string]map[string]bool, 0)
	actions := []string{"hold", "start", "stop", "restart", "info"}
	for i := 0; i < len(job_order); i++ {
		v := k.Jobs[job_order[i]]
		jobs_static[job_order[i]] = v.copyJob()
		perms := make(map[string]bool)
		for _, action := range actions {
			perms[action] = v.hasPermission(user, action)
		}
		authorized[job_order[i]] = perms
	}
	var reqgroups_order []string
	groupedJobs := make(map[string]jobMapStatic)
	groupedOrder := make(map[string][]string)
	for group, uuids := range k.sd.groups {
		if (len(reqgroups) == 0) || stringInSlice(group, reqgroups) {
			j := make(jobMapStatic, 0)
			var o []string
			for _, uuid := range uuids {
				uuidString := uuid.String()
				if job, ok := jobs_static[uuidString]; ok {
					j[uuidString] = job.copyJob()
					o = append(o, uuidString)
				}
			}
			groupedJobs[group] = j
			groupedOrder[group] = o
			reqgroups_order = append(reqgroups_order, group)
		}
	}
	return jobsResponse{Jobs: groupedJobs, JobOrder: job_order, Groups: groupedOrder, GroupOrder: reqgroups_order, Authorized: authorized}, nil
}
func (k service) editBegin() {

}
func (k service) editEnd() {

}
func (k service) Dependencies(jobid string, user string) (DependencyGraphs, error) {
	ServerLogger.Printf("\tDEPENDENCIES\tJobUUID: %s\tuser:%s", jobid, user)
	jobs := k.Jobs
	if permitted := jobs[jobid].hasPermission(user, "dependencies"); !permitted {
		// FIXME
	}
	return jobs[jobid].getDependencyGraph(k.sd), nil
}

// Job Status
func (k service) Status(jobid string, user string) (*JobUpdateParams, error) {
	ServerLogger.Printf("\tSTATUS\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return &JobUpdateParams{}, errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "status"); !permitted {
		return &JobUpdateParams{}, errors.New("request denied: insufficient permissions")
	}
	return job.updateParams(), nil
}

// Job Status (All)
func (k service) AllStatus(user string, reqgroups []string) (jobsStatusResponse, error) {
	ServerLogger.Printf("\tALLSTATUS\tuser:%s", user)

	sconf := k.ServerConfig
	if permitted := sconf.hasPermission(user, "info"); !permitted {
		ServerLogger.Printf("[ACCESS DENIED] ServerInfo request from user:%s ", user)
		return jobsStatusResponse{}, ErrPermission
	}

	job_order := make([]string, len(k.sd.job_order))
	copy(job_order, k.sd.job_order)
	for i := 0; i < len(job_order); i++ {
		v := k.Jobs[job_order[i]]
		if user != v.User && !stringInSlice(user, v.Admin) {
			job_order = append(job_order[:i], job_order[i+1:]...)
		}
	}
	jobs_static := make(map[string]Job, 0)
	authorized := make(map[string]map[string]bool, 0)
	actions := []string{"hold", "start", "stop", "restart", "info", "status"}
	for i := 0; i < len(job_order); i++ {
		v := k.Jobs[job_order[i]]
		jobs_static[job_order[i]] = v.copyJob()
		perms := make(map[string]bool)
		for _, action := range actions {
			perms[action] = v.hasPermission(user, action)
		}
		authorized[job_order[i]] = perms
	}
	var reqgroups_order []string
	groupedJobs := make(map[string]jobMapStatic)
	groupedOrder := make(map[string][]string)
	for group, uuids := range k.sd.groups {
		if (len(reqgroups) == 0) || stringInSlice(group, reqgroups) {
			j := make(jobMapStatic, 0)
			var o []string
			for _, uuid := range uuids {
				uuidString := uuid.String()
				if job, ok := jobs_static[uuidString]; ok {
					j[uuidString] = job.copyJob()
					o = append(o, uuidString)
				}
			}
			groupedJobs[group] = j
			groupedOrder[group] = o
			reqgroups_order = append(reqgroups_order, group)
		}
	}
	jobs_status := make(map[string]JobStatusParams, len(jobs_static))
	for id, job := range jobs_static {
		jobs_status[id] = *job.statusParams()
	}
	requested := time.Now().Unix()
	return jobsStatusResponse{Jobs: jobs_status, JobOrder: job_order, Groups: groupedOrder, GroupOrder: reqgroups_order, Authorized: authorized, Requested: requested}, nil
}
func (k service) Info(jobid string, user string) (*Job, error) {
	ServerLogger.Printf("\tINFO\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs.getJob(jobid)
	if !ok {
		return job, errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "info"); !permitted {
		empty := new(Job)
		return empty, ErrEmpty
	}
	if ok {
		evaluatedCmd(job, false, "") // jit command evaluated given environment variables
		evaluatedCmd(job, true, "")  // jit shutdown command evaluated given environment variables
		return job, nil
	} else {
		return job, ErrEmpty
	}
}

type LogOutput struct {
	Stdout     string
	StdoutFile string
	Stderr     string
	StderrFile string
	AsOf       int64
}

func (k service) Log(jobid, runid, user string, stdout, stderr bool, lines int, lastmod int64) (*LogOutput, error) {
	ServerLogger.Printf("\tLOG\tJobUUID: %s\tuser:%s", jobid, user)
	logs := new(LogOutput)
	logs.AsOf = time.Now().Unix()
	if job, ok := k.Jobs[jobid]; ok {
		if permitted := job.hasPermission(user, "log"); !permitted {
			ServerLogger.Println("access to logs denied")
			return logs, ErrPermission
		}
		stdoutFile, stderrFile := job.getLogs(runid)
		//tmpdir := job.TmpDir
		if stdout {
			logs.StdoutFile = stdoutFile
			logs.Stdout = tailLog(stdoutFile, lines)
		}
		if stderr {
			logs.StderrFile = stderrFile
			logs.Stderr = tailLog(stderrFile, lines)
		}
		return logs, nil
	} else {
		return logs, ErrEmpty
	}
}
func (k service) ServerInfo(user string) (*ServerConfig, error) {
	ServerLogger.Printf("\tSERVER INFO\tuser:%s", user)
	sconf := k.ServerConfig
	if permitted := sconf.hasPermission(user, "info"); !permitted {
		ServerLogger.Printf("[ACCESS DENIED] ServerInfo request from user:%s ", user)
		empty := new(ServerConfig)
		return empty, ErrPermission
	}
	return &sconf, nil
}

// api endpoint behaviors Hold, Stop, Start, Restart
func (k service) Hold(jobid string, user string, comment, duration string) (*controlResponse, error) {
	ServerLogger.Printf("\tHOLD\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return &controlResponse{Status: "invalid jobid"}, errors.New("bad jobid")
	}
	if job.isJOJ() {
		return &controlResponse{Status: "job of jobs array does not support hold"}, errors.New("Hold is unavailable for jobs of job arrays")
	}
	if permitted := job.hasPermission(user, "hold"); !permitted {
		return &controlResponse{Status: "permission denied"}, ErrEmpty
	}
	isHold := job.Hold
	if isHold {
		job.Unscheduled = false
		job.setHold(false)
		job.Reason = Reason{Action: "unhold", Comment: comment, User: user, Timestamp: time.Now().Unix()}
		job.setRetryAttempt(0)
		job.setJobState(JReady)
	} else {
		if job.IsRunning {
			job.getProc().Kill() // FIXME: shouldn't be in Hold, rather chain call or disable
		}
		job.setHold(true)
		job.Reason = Reason{Action: "hold", Comment: comment, User: user, Timestamp: time.Now().Unix()}
		job.setJobState(JHold)
	}
	job.IsRunning = false
	job.ExitCode = 0
	job.Pid = 0
	job.sendUpdate()
	return &controlResponse{Status: "success"}, nil
}
func (k service) Start(jobid string, user string, comment string) (*controlResponse, error) {
	ServerLogger.Printf("\tSTART\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return &controlResponse{Status: "invalid jobid"}, errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "start"); !permitted {
		return &controlResponse{Status: "permission denied"}, ErrEmpty
	}
	if job.Hold {
		job.setHold(false)
		job.setRetryAttempt(0)
	}
	if !job.IsRunning {
		job.Unscheduled = true
		job.Reason = Reason{Action: "start", Comment: comment, User: user, Timestamp: time.Now().Unix()}
		job.resetTimer(time.Second * 0)
		//job.setJobState(JManual)
		//job.sendUpdate()
	} else {
		ServerLogger.Println("unable to start a running job: ignoring Start()")
	}
	return &controlResponse{Status: "success"}, nil
}
func (k service) Stop(jobid string, user string, comment string) (*controlResponse, error) {
	ServerLogger.Printf("\tSTOP\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return &controlResponse{Status: "invalid jobid"}, errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "stop"); !permitted {
		return &controlResponse{Status: "permission denied"}, ErrEmpty
	}
	job.Unscheduled = true
	job.Reason = Reason{Action: "stop", Comment: comment, User: user, Timestamp: time.Now().Unix()}
	if job.ShutdownCmd == "" {
		stopJob(job, JStopped)
	} else {
		shutdownJob(job, JStopped)
	}
	return &controlResponse{Status: "stopped"}, nil
}
func (k service) Restart(jobid string, user string, comment string) (*controlResponse, error) {
	ServerLogger.Printf("\tRESTART\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return &controlResponse{Status: "invalid jobid"}, errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "restart"); !permitted {
		return &controlResponse{Status: "permission denied"}, ErrEmpty
	}
	job.Unscheduled = true
	job.Reason = Reason{Action: "restart", Comment: comment, User: user, Timestamp: time.Now().Unix()}
	if job.ShutdownCmd == "" {
		stopJob(job, JEnd)
		time.Sleep(time.Second * 1)
		job.resetTimer(time.Second * 0)
	} else {
		shutdownJob(job, JRestart)
		time.Sleep(time.Second * 1)
		job.resetTimer(time.Second * 0)
	}
	return &controlResponse{Status: "success"}, nil
}
func (k service) Resume(jobid string, user string) (*JobUpdateParams, error) {
	ServerLogger.Printf("\tRESUME\tJobUUID: %s\tuser:%s", jobid, user)
	job, ok := k.Jobs[jobid]
	if !ok {
		return job.updateParams(), errors.New("bad jobid")
	}
	if permitted := job.hasPermission(user, "resume"); !permitted {
		return job.updateParams(), ErrPermission
	}
	resumeJob(job)
	return job.updateParams(), nil
}

// request/response structures
var ErrEmpty = errors.New("empty string")
var ErrPermission = errors.New("insufficient permission")

/* request and response structs - should be generic!*/
type jobsRequest struct {
	UserID string   `json:"userid"`
	Groups []string `json:"groups"`
	States []string `json:"states"`
	// Next is a string describing either a duration to filter within (e.g. 10m or 30s)
	// or list of upcoming jobs. If Next is negative this will return the most recent N
	// jobs
	Next string `json:"next"`
}
type jobsResponse struct {
	Jobs map[string]jobMapStatic `json:"jobs"`
	//Jobs interface{} `json:"jobs"`
	JobOrder   []string                   `json:"joborder"`
	Groups     map[string][]string        `json:"groups"`
	GroupOrder []string                   `json:"grouporder"`
	Authorized map[string]map[string]bool `json:"authorized"`
	Requested  int64                      `json:"requested"`
}
type jobsStatusResponse struct {
	Jobs map[string]JobStatusParams `json:"jobs"`
	//Jobs interface{} `json:"jobs"`
	JobOrder   []string                   `json:"joborder"`
	Groups     map[string][]string        `json:"groups"`
	GroupOrder []string                   `json:"grouporder"`
	Authorized map[string]map[string]bool `json:"authorized"`
	Requested  int64                      `json:"requested"`
}
type logRequest struct {
	JobID   string `json:"jobid"`
	RunID   string `json:"runid"`
	UserID  string `json:"userid"`
	Stdout  bool   `json:"stdout"`
	Stderr  bool   `json:"stderr"`
	Lines   int    `json:"lines"`
	LastMod int64  `json:"lastmod"`
}

type kRequest struct {
	JobID  string `json:"jobid"`
	RunID  string `json:"runid"`
	UserID string `json:"userid"`
}
type kResponse struct {
	Job interface{} `json:"job"`           // should be renamed to Resp or something as it is generic
	Err string      `json:"err,omitempty"` // errors don't define JSON marshaling
}

type serverConfigResponse struct {
	ServerConfig interface{} `json:"serverconfig"`
	Err          string      `json:"err,omitempty"` // errors don't define JSON marshaling
}
type controlResponse struct {
	Status string
}

// endpoints (info)
func makeJobsEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(jobsRequest)
		jobs, err := svc.AllJobs(req.UserID, req.Groups)
		if err != nil {
			ServerLogger.Printf("error requestion all jobs")
		}
		return jobs, nil
		//jobs, joborder, groups, grouporder, authorized := svc.AllJobs(req.UserID, req.Groups)
		//        return jobsResponse{Jobs:jobs,
		//                            JobOrder:joborder,
		//                            Groups:groups,
		//                            GroupOrder:grouporder,
		//                            Authorized:authorized}, nil
	}
}
func makeJobsStatusEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(jobsRequest)
		jobs, err := svc.AllStatus(req.UserID, req.Groups)
		if err != nil {
			ServerLogger.Printf("error requestion all jobs")
		}
		return jobs, nil
		//jobs, joborder, groups, grouporder, authorized := svc.AllJobs(req.UserID, req.Groups)
		//        return jobsResponse{Jobs:jobs,
		//                            JobOrder:joborder,
		//                            Groups:groups,
		//                            GroupOrder:grouporder,
		//                            Authorized:authorized}, nil
	}
}
func makeDependenciesEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		dep, err := svc.Dependencies(req.JobID, req.UserID)
		if err != nil {
			return kResponse{dep, err.Error()}, nil
		}
		return kResponse{dep, ""}, nil
	}
}
func makeInfoEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		job, err := svc.Info(req.JobID, req.UserID)
		if err != nil {
			//return kResponse{*job, err.Error()}, nil
			return kResponse{job.copyJob(), err.Error()}, nil
		}
		//return kResponse{*job, ""}, nil
		return kResponse{job.copyJob(), ""}, nil
	}
}
func makeLogEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(logRequest)
		logs, err := svc.Log(req.JobID, req.RunID, req.UserID, req.Stdout, req.Stderr, req.Lines, req.LastMod)
		if err != nil {
			return kResponse{*logs, err.Error()}, nil
		}
		return kResponse{*logs, ""}, nil
	}
}
func makeServerInfoEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		sconf, err := svc.ServerInfo(req.UserID)
		if err != nil {
			return serverConfigResponse{*sconf, err.Error()}, nil
		}
		return serverConfigResponse{*sconf, ""}, nil
	}
}
func makeServerRestartEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		sconf, err := svc.ServerRestart(req.UserID)
		if err != nil {
			return serverConfigResponse{*sconf, err.Error()}, nil
		}
		return serverConfigResponse{*sconf, ""}, nil
	}
}

// endpoints (controls)
func makeHoldEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		ctl, err := svc.Hold(req.JobID, req.UserID, "", "")
		if err != nil {
			return *ctl, nil
		}
		return *ctl, nil
	}
}
func makeStartEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		ctl, err := svc.Start(req.JobID, req.UserID, "")
		if err != nil {
			return *ctl, nil
		}
		return *ctl, nil
	}
}
func makeStopEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		ctl, err := svc.Stop(req.JobID, req.UserID, "")
		if err != nil {
			return *ctl, nil
		}
		return *ctl, nil
	}
}
func makeRestartEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		ctl, err := svc.Restart(req.JobID, req.UserID, "")
		if err != nil {
			return *ctl, nil
		}
		return *ctl, nil
	}
}
func makeStatusEndpoint(svc Service) endpoint.Endpoint {
	return func(_ context.Context, request interface{}) (interface{}, error) {
		req := request.(kRequest)
		job, err := svc.Status(req.JobID, req.UserID)
		if err != nil {
			return kResponse{*job, err.Error()}, nil
		}
		return kResponse{*job, ""}, nil
	}
}

// request decoders
func decodeJobsRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var request jobsRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		//ServerLogger.Printf("decodeJobsRequest err:%s",err)
		return nil, err
	}
	user, ok := GetUserFromAuth(r)
	if ok {
		request.UserID = user
	}
	return request, nil
}
func decodeLogRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var request logRequest
	user, ok := GetUserFromAuth(r)
	if ok {
		request.UserID = user
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, err
	}
	return request, nil
}
func decodeKRequest(_ context.Context, r *http.Request) (interface{}, error) {
	var request kRequest
	user, ok := GetUserFromAuth(r)
	if ok {
		request.UserID = user
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return nil, err
	}
	return request, nil
}

// response encoders
func encodeInfoResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	if response.(kResponse).Job.(Job).JobUUID.String() == "00000000-0000-0000-0000-000000000000" {
		empty := kResponse{}
		return json.NewEncoder(w).Encode(empty)
	}
	return json.NewEncoder(w).Encode(response)
}
func encodeServerInfoResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}
func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}
