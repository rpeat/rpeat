package rpeat

import (
	//"context"
	"crypto/sha256"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"path/filepath"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"fmt"
	"github.com/fsnotify/fsnotify"
	"html/template"
	"net"
	"reflect"
	"sync"
	"time"

	"math/rand"
	"os"
	"os/signal"
	"syscall"
	//"github.com/davecgh/go-spew/spew"
)

func Init() {
	rand.Seed(time.Now().Unix())
	initServerLogging(os.Stderr)
	initConnectionLogging(os.Stderr)
	initUpdatesLogging(os.Stderr)
}

func StartServer(server ServerConfig) {

	home := server.HOME
	host := server.HOST
	port := server.PORT
	useHttps := server.Https
	if useHttps && server.TLS == nil {
		ServerLogger.Fatal("TLS requires Cert and Key to be defined")
	}

	addr := fmt.Sprintf("%s:%s", host, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	fmt.Printf(`

                     (\/) (°,,,,°) (\/)

                        www.rpeat.io 
                          @rpeatio


           All rights reserved. Copyright Lemnica, Corp

        rpeat® is a registered trademark of Lemnica, Corp.

`)
	pidfile := createPidFile(home, port, os.Getpid())
	CreateIcons(filepath.Join(home, "assets"))
	CreateSounds(filepath.Join(home, "assets"))

	fmt.Printf("\nrpeat® server time is %s\n", server.Now().String())
	if !useHttps {
		fmt.Printf(WarningColor, "\n\n!!!! Critical !!!!! Enable TLS for security, rpeat® server is currently running without encryption enabled\n\n")
		fmt.Printf("Starting rpeat® server on http://%s\n\n", listener.Addr().String())
	} else {
		fmt.Printf("Starting rpeat® server on https://%s\n\n", listener.Addr().String())
	}

	sd := &ServerData{}
	sd.pidfile = pidfile
	_, sd.jve = ValidateJobs(server.JobsFiles, server.ConfigFile, server.AuthFile, false)

	server.startJobs(sd)
	if server.Heartbeat {
		go server.startServerHeartbeat(time.Duration(60 * time.Minute))
	}

	ServerLogger.Println("All jobs started - starting services")

	jobsHandler := httptransport.NewServer(
		makeJobsEndpoint(sd.svc),
		decodeJobsRequest,
		httptransport.EncodeJSONResponse,
	)
	jobsStatusHandler := httptransport.NewServer(
		makeJobsStatusEndpoint(sd.svc),
		decodeJobsRequest,
		httptransport.EncodeJSONResponse,
	)
	infoHandler := httptransport.NewServer(
		makeInfoEndpoint(sd.svc),
		decodeKRequest,
		encodeInfoResponse,
	)
	dependenciesHandler := httptransport.NewServer(
		makeDependenciesEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	logHandler := httptransport.NewServer(
		makeLogEndpoint(sd.svc),
		decodeLogRequest,
		encodeResponse,
	)
	serverInfoHandler := httptransport.NewServer(
		makeServerInfoEndpoint(sd.svc),
		decodeKRequest,
		encodeServerInfoResponse,
	)
	serverRestartHandler := httptransport.NewServer(
		makeServerRestartEndpoint(sd.svc),
		decodeKRequest,
		encodeServerInfoResponse,
	)

	// Controls
	startHandler := httptransport.NewServer(
		makeStartEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	stopHandler := httptransport.NewServer(
		makeStopEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	restartHandler := httptransport.NewServer(
		makeRestartEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	holdHandler := httptransport.NewServer(
		makeHoldEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	statusHandler := httptransport.NewServer(
		makeStatusEndpoint(sd.svc),
		decodeKRequest,
		encodeResponse,
	)
	/// optional TLS using pure go
	/// https://gist.github.com/denji/12b3a568f092ab951456
	mx := mux.NewRouter()

	/* assets */
	mx.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir(server.HOME+"/assets/"))))

	/* API */
	mx.Handle("/api/serverinfo", serverInfoHandler)
	mx.Handle("/api/serverrestart", serverRestartHandler)

	mx.Handle("/api/jobs", jobsHandler)
	mx.Handle("/api/jobs/status", jobsStatusHandler)
	mx.Handle("/api/info", infoHandler)
	mx.Handle("/api/dependencies", dependenciesHandler)
	mx.Handle("/api/log", logHandler)
	mx.Handle("/api/start", startHandler)
	mx.Handle("/api/stop", stopHandler)
	mx.Handle("/api/restart", restartHandler)
	mx.Handle("/api/hold", holdHandler)
	mx.Handle("/api/status", statusHandler)

	mx.HandleFunc("/api/log/{ext}/{jobid}/{runid}", func(w http.ResponseWriter, r *http.Request) {

		vars := mux.Vars(r)
		ext := vars["ext"]
		jobid := vars["jobid"]
		runid := vars["runid"]
		download := r.URL.Query().Get("download")

		job, ok := sd.jobs.getJob(jobid) // strictly checking functionality
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		user, _, _ := r.BasicAuth()
		if !job.hasPermission(user, "log") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		stdoutFile, stderrFile := job.getLogs(runid)
		var logfile string
		switch ext {
		case "out":
			logfile = stdoutFile
		case "err":
			logfile = stderrFile
		}
		ServerLogger.Printf("[download log files] %s-%s.std%s", jobid, runid, ext)

		if download == "true" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-%s.std%s", jobid, runid, ext))
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		http.ServeFile(w, r, logfile)
	})

	/* Job Page */
	mx.HandleFunc("/job/{jobuuid}", func(w http.ResponseWriter, r *http.Request) {

		vars := mux.Vars(r)
		jobuuid := vars["jobuuid"]

		funs := template.FuncMap{"getControls": GetControls,
			"DateEnvEval":      DateEnvEval,
			"getElapsed":       GetElapsed,
			"getDependencies":  func(job Job) template.HTML { return GetDependencies(job, sd) },
			"slugify":          slugify,
			"historyBars":      HistoryBars,
			"stringify":        Stringify,
			"stringifyWithSep": StringifyWithSep,
			"stringifyHTML":    func(s string) template.HTML { h := template.HTML(Stringify(s)); return h },
			"stringifyJson": func(v interface{}) template.JS {
				a, _ := json.MarshalIndent(v, "", "    ")
				return template.JS(a)
			}}

		t, err := template.New("JobViewHTML").Funcs(funs).Parse(JobViewHTML)

		/// move all of template creation to a external func
		themePath := filepath.Join(server.ThemeDir, server.Theme)
		cssFiles, _ := ioutil.ReadDir(themePath)
		var themeCSS string
		for _, cssfile := range cssFiles {
			cssfileFullPath, err := filepath.Abs(filepath.Join(themePath, cssfile.Name()))
			if err != nil {
				ServerLogger.Println("issue sourcing theme css file:", err.Error())
			}
			thiscss, err := ioutil.ReadFile(cssfileFullPath)
			if err != nil {
				ServerLogger.Println("failed read of theme css file:", err.Error())
			}
			themeCSS = fmt.Sprintf("%s\n%s", themeCSS, thiscss)
		}
		CSS := fmt.Sprintf("%s\n%s", ClientCSS, themeCSS)
		t.New("ClientHeaderHTML").Parse(ClientHeaderHTML)
		t.New("ClientCSS").Parse(CSS)
		t.New("ClientJS").Parse(ClientJS)
		t.New("ClientWS").Parse(ClientWS)
		t.New("JobView").Parse(JobView)

		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		//jobp, ok := sd.jobs[jobuuid]
		jobp, ok := sd.jobs.getJob(jobuuid) // strictly checking functionality
		if !ok {
			ServerLogger.Printf("Job not found - redirect to 404")
			return
		}
		evaluatedCmd(jobp, false, "")
		evaluatedCmd(jobp, true, "")
		job := jobp

		//job.getDependencyGraph(sd).Print(true,0)

		user, _, _ := r.BasicAuth()
		authorized := make(map[string]map[string]bool, 1)
		actions := []string{"hold", "start", "stop", "restart", "update", "info", "log"}

		perms := make(map[string]bool)
		for _, action := range actions {
			perms[action] = job.hasPermission(user, action)
		}
		authorized[job.JobUUID.String()] = perms

		type JobView struct {
			Base       template.URL
			Config     ServerConfig
			Job        *Job
			UUID       string
			Protocol   string
			Authorized map[string]map[string]bool
		}

		err = t.Execute(w, JobView{Config: server, Job: job, Authorized: authorized, UUID: job.JobUUID.String(), Base: ""})
		if err != nil {
			ServerLogger.Printf("JobView Parse Error: %s", err.Error())
			return
		}
	})

	/* Dashboards */
	mx.HandleFunc("/{group}", func(w http.ResponseWriter, r *http.Request) {
		dashboardHandler(w, r, sd, server)
	})
	mx.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		dashboardHandler(w, r, sd, server)
	})

	mx.HandleFunc("/api/updates", func(w http.ResponseWriter, r *http.Request) {
		// Support for multiple clients - and multiple hosts with single master
		//         https://dev.to/danielkun/go-asynchronous-and-safe-real-time-broadcasting-using-channels-and-websockets-4g5d
		//         https://github.com/sirfilip/webchat/blob/master/main.go#L603
		//         https://github.com/gorilla/websocket/blob/master/examples/chat/client.go
		//         https://brandur.org/live-reload
		// FIXME: can quickly overload server if (wsClientPool.watch?) if pages are reloaded too fast

		conn, err := upgrader.Upgrade(w, r, nil) // error ignored for sake of simplicity
		if err != nil {
			ConnectionLogger.Println(err)
			return
		}

		user, _ := GetUserFromAuth(r)
		// wsClient user and permissions
		authorizedJobs := make([]string, 0)
		for _, v := range sd.jobs {
			if user == v.User || stringInSlice(user, v.Admin) {
				authorizedJobs = append(authorizedJobs, v.JobUUID.String())
			}
		}
		wsClient := &wSClient{conn: conn, pool: sd.wsClientPool, updates: make(chan *JobUpdate, 10), user: user, authorizedJobs: authorizedJobs}
		sd.wsClientPool.register <- wsClient
		go wsClient.watch()
		go wsClient.heartbeat()

	})

	// pushUpdates:
	//   heartbeat
	//   send updated

	srv := &http.Server{
		Handler: authenticateUser(mx, sd.users, server.ID),
		Addr:    listener.Addr().String(),
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	if useHttps {
		ServerLogger.Fatal("ServeTLS:", srv.ServeTLS(listener, server.TLS.Cert, server.TLS.Key))
	} else {
		ServerLogger.Fatal("Serve:", srv.Serve(listener))
	}

}

func Shutdown(jobs jobMap) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		ServerLogger.Printf("SHUTDOWN check for %s:%s", job.JobUUID, job.Name)
		wg.Add(1)
		go func(job *Job, wg *sync.WaitGroup) {
			time.Sleep(time.Second)
			defer wg.Done()

			if job.JobState == JRunning || job.JobState == JRetrying {
				if job.ShutdownCmd != "" {
					ServerLogger.Printf("Shutting down %s:%s", job.JobUUID, job.Name)
					shutdownJob(job, JStopped)
				} else {
					ServerLogger.Printf("Stopping %s:%s", job.JobUUID, job.Name)
					stopJob(job, JStopped)
				}
				time.Sleep(time.Second)
			} else {
				ServerLogger.Printf("Holding %s:%s", job.JobUUID, job.Name)
				if job.TryLock() {
					job.Unlock()
					job.setHold(true)
				}
			}
			return
		}(job, &wg)
	}
	wg.Wait()
	ServerLogger.Printf("SHUTDOWN complete. Exiting.\n\n")
}

func (server ServerConfig) startServerHeartbeat(d time.Duration) {

	serverkey := fmt.Sprintf("%x", sha256.Sum256([]byte(server.ID)))
	servername := server.Name

	hb := HeartbeatParams{ServerName: servername, ServerKey: serverkey}
	key, ok := getApiKey()
	if !ok {
		ServerLogger.Printf("RPEAT_API_KEY environment variable not set - rpeat.io alerts will not work")
		return // should be in gui?
	}
	hb.ApiKey = key
	timer := time.NewTimer(d)
	for {
		select {
		case <-timer.C:
			hb.Next = time.Now().Add(d).Unix() + 15

			ServerLogger.Printf("sending heartbeat to %s/rpeat-heartbeat", server.ApiEndpoint)
			server.rpeatioHeartbeat(hb)

			timer.Reset(d)
		}
	}
}

func (server ServerConfig) startJobs(sd *ServerData) {

	home := server.HOME
	serverkey := fmt.Sprintf("%x", sha256.Sum256([]byte(server.ID)))
	config := server.JobsFiles
	reloadjobs := server.Clean
	keephistory := server.KeepHistory
	maxhistory := server.MaxHistory

	ServerLogger.Printf("Loading Jobs configuration file: %s", config[0])
	sjobs, jve := LoadConfig2(home, config, server, reloadjobs, keephistory, maxhistory, server.Name, serverkey, server.ApiKey, server.Logging)
	jobs := sjobs.Jobs
	job_order := sjobs.JobOrder
	groups := sjobs.Groups
	group_order := sjobs.GroupOrder

	if jve != nil {
		//      ServerLogger.Fatal(err) // FIXME: should we error out or disable/hold job with error/warning
	}

	var users []AuthUser

	if server.AuthFile != "" {
		users, _ = LoadAuth(server.AuthFile)
		// FIXME: add auth error check
	}

	depEvt := make(chan *depEvt, MAX_JOBS)
	//ServerLogger.Printf("Initializing JobUpdate Channel")
	sd.updates = make(chan *JobUpdate, MAX_JOBS)

	dClientPool := NewDependencyClientPool()
	sd.dClientPool = dClientPool
	go dClientPool.Monitor(depEvt)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// might need a wait group here
	for _, job := range jobs {
		go func(job *Job, dClientPool *DependencyClientPool) {
			if len(job.Dependency) > 0 {
				for di := range job.Dependency {
					if job.Dependency[di].Dependencies != nil {
						dClient := dClientPool.AddClient(job, job.Dependency[di])
						go dClient.watch()
					}
				}
			}
			//log.Printf("finished adding dependencies for %s:%s", job.Name, job.JobUUID)
		}(job, dClientPool)
	}
	stopAll := make(map[uuid.UUID]chan bool)

	var wg sync.WaitGroup
	for _, job := range jobs {
		if job.Disabled {
			continue
		}
		msg := make(chan *Signal)
		job.MsgC = msg

		stopc := make(chan bool)
		stopAll[job.JobUUID] = stopc

		wg.Add(1)
		go RegisterJob(job, sd.updates, depEvt, stopc, &wg)
		ServerLogger.Printf("Initializing rpeat® for %s [%s]", job.JobUUID, job.Name)
	}
	wg.Wait()

	unwatch := make(chan bool)

	ServerLogger.Println("starting signal watcher")
	go func(sigCh chan os.Signal, jobs jobMap, home, port string, unwatch <-chan bool) {
		ServerLogger.Println("registering signal watcher")
		select {
		case interrupt, _ := <-sigCh:
			fmt.Println()
			ServerLogger.Printf("[ %s ] caught - shutting jobs down", interrupt)
		case <-unwatch:
			ServerLogger.Println("unwatching signal watcher")
			return
		}
		fmt.Println()
		// TODO: add in confirmation at command line or gui
		Shutdown(jobs)
		for _, s := range stopAll {
			s <- true
		}
		removePidFile(home, port)
		time.Sleep(time.Second * 2)
		os.Exit(0)
	}(sigCh, jobs, home, server.PORT, unwatch)

	// watch for Chmod (touch) on pid file to reload
	go func(c *ServerConfig, sd *ServerData) {
		for {
			watchFile(sd.pidfile, fsnotify.Chmod)
			server.reloadJobs(sd)
		}
	}(&server, sd)

	wsClientPool := NewWSClientPool()
	go wsClientPool.watch(sd.updates)

	sd.svc = &service{Jobs: jobs, JobOrder: job_order, ServerConfig: server, Config: config[0], Home: home, sd: sd}
	sd.jobs = jobs
	sd.jobNameUUID = sjobs.JobNameUUID
	sd.job_order = job_order
	sd.group_order = group_order
	sd.groups = groups
	sd.depEvt = depEvt
	sd.users = users
	sd.wsClientPool = wsClientPool
	sd.dClientPool = dClientPool
	sd.stopAll = stopAll
	sd.unwatch = unwatch
	sd.shutdownJobs = func() {
		for u, s := range stopAll {
			ServerLogger.Printf("shutting down server %s (may block if job is currently running)", u)
			s <- true
		}
		ServerLogger.Printf(WarningColor, "ALL JOBS STOPPED")
	}
}

func (server *ServerConfig) reloadJobs(sd *ServerData) {
	home := server.HOME
	config := server.JobsFiles
	serverkey := fmt.Sprintf("%x", sha256.Sum256([]byte(server.ID)))
	reloadjobs := server.Clean
	keephistory := server.KeepHistory
	maxhistory := server.MaxHistory

	ServerLogger.Printf("[reloadJobs] loading %d Job configuration files: %v", len(config), config)
	sjobs, _ := LoadConfig2(home, config, *server, reloadjobs, keephistory, maxhistory, server.Name, serverkey, server.ApiKey, server.Logging)

	var wg sync.WaitGroup

	jobs := sjobs.Jobs
	newJobsInCurrentJobs := StringsInSlice(sjobs.JobOrder, sd.job_order, false)
	for _, id := range newJobsInCurrentJobs {
		job := sd.jobs[id]
		if !jobs[id].areEqualSpec(job) {
			wg.Add(1)
			go func(id string, job *Job, sd *ServerData, wg *sync.WaitGroup) {
				defer wg.Done()
				ServerLogger.Printf("beginning Job update for %s (%s)", job.Name, job.JobUUID)
				job.runlock.Lock()
				defer job.runlock.Unlock()
				defer func() {
					job.Updating = false
					d, next := NextCronStart(job.cronStartArray)
					job.setNextStart(next)
					job.resetTimer(d)
					ServerLogger.Printf("Next job %s <%s> scheduled for %s [%d] (starts in %s)\n", job.Name, job.JobUUID, next, next.Unix(), d.Round(time.Second))
					ServerLogger.Printf("completed Job update for %s [%s]", job.Name, job.JobUUID)
					job.sendUpdateClient() // only update clients
				}()

				ServerLogger.Printf("acquired runlock for %s (%s)", job.Name, job.JobUUID)
				job.Updating = true
				job.sendUpdateClient()
				job.Timezone = jobs[id].Timezone
				job.Calendar = jobs[id].Calendar
				job.CalendarDirs = jobs[id].CalendarDirs
				job.Rollback = jobs[id].Rollback
				job.RequireCal = jobs[id].RequireCal
				job.CronStart = jobs[id].CronStart
				job.CronStartArray = jobs[id].CronStartArray
				job.StartTime = jobs[id].StartTime
				job.StartDay = jobs[id].StartDay
				job.CronEnd = jobs[id].CronEnd
				job.CronEndArray = jobs[id].CronEndArray
				job.CronRestart = jobs[id].CronRestart
				job.Dependency = jobs[id].Dependency
				job.parseJob(true) // converts Cron* strings to Cron objects

				job.Name = jobs[id].Name
				job.Description = jobs[id].Description
				job.Comment = jobs[id].Comment
				job.Tags = jobs[id].Tags
				job.Group = jobs[id].Group
				job.Inherits = jobs[id].Inherits
				job.Hold = jobs[id].Hold
				job.Disabled = jobs[id].Disabled // this is not possible?
				job.Hidden = jobs[id].Hidden
				job.Cmd = jobs[id].Cmd
				job.ShutdownCmd = jobs[id].ShutdownCmd
				job.ShutdownSig = jobs[id].ShutdownSig
				job.Env = jobs[id].Env
				job.DateEnv = jobs[id].DateEnv
				job.AlertActions = jobs[id].AlertActions
				job.Jobs = jobs[id].Jobs
				job.JobsControl = jobs[id].JobsControl
				job.Retry = jobs[id].Retry
				job.RetryWait = jobs[id].RetryWait
				job.MaxDuration = jobs[id].MaxDuration
				job.TmpDir = jobs[id].TmpDir
				job.Logging = jobs[id].Logging
				job.Host = jobs[id].Host
				job.User = jobs[id].User
				job.Permissions = jobs[id].Permissions
				job.Admin = jobs[id].Admin

				// TODO: send message
				sd.jobs[id] = job
			}(id, job, sd, &wg)
		} else {
			ServerLogger.Printf("[reloadJobs] %s (%s) has no change and will not be updated", job.Name, job.JobUUID)
			// TODO: send message
		}
	}
	newJobsNotInCurrentJobs := StringsInSlice(sjobs.JobOrder, sd.job_order, true)
	ServerLogger.Printf("job.Add() newJobsNotInCurrentJobs: %v", newJobsNotInCurrentJobs)
	for _, id := range newJobsNotInCurrentJobs {
		sd.jobs.Add(jobs[id], sd.dClientPool, sd.updates, sd.depEvt, sd.stopAll)
		// TODO: send message
	}
	currentJobsNotInNewJobs := StringsInSlice(sd.job_order, sjobs.JobOrder, true)
	ServerLogger.Printf("job.Remove() currentJobsNotInNewJobs: %v", currentJobsNotInNewJobs)
	for _, id := range currentJobsNotInNewJobs {
		sd.jobs.Remove(sd.jobs[id], sd.dClientPool, sd.stopAll)
		// TODO: send message
	}
	sd.job_order = sjobs.JobOrder
	sd.group_order = sjobs.GroupOrder
	sd.groups = sjobs.Groups
	go func(wg *sync.WaitGroup) {
		wg.Wait()
		ServerLogger.Printf("ALL JOBS UPDATED")
		// TODO: send message
	}(&wg)

}

// this needs to be (jobs jobMap) Add(job *Job, ...)
func (jobs jobMap) Add(job *Job, dClientPool *DependencyClientPool, updates chan *JobUpdate, depEvt chan *depEvt, stopAll map[uuid.UUID]chan bool) {

	go func(job *Job, dClientPool *DependencyClientPool) {
		if len(job.Dependency) > 0 {
			for di := range job.Dependency {
				if job.Dependency[di].Dependencies != nil {
					dClient := dClientPool.AddClient(job, job.Dependency[di])
					go dClient.watch()
				}
			}
		}
		ServerLogger.Printf("finished adding dependencies for %s:%s", job.Name, job.JobUUID)
	}(job, dClientPool)

	var wg sync.WaitGroup
	stopc := make(chan bool)
	stopAll[job.JobUUID] = stopc

	msg := make(chan *Signal)
	job.MsgC = msg

	wg.Add(1)
	go RegisterJob(job, updates, depEvt, stopc, &wg)
	ServerLogger.Printf("starting RegisterJob %s [JobId: \"%s\"]", job.Name, job.JobUUID)
	wg.Wait()
	jobs[job.JobUUID.String()] = job
}
func (jobs jobMap) Remove(job *Job, dClientPool *DependencyClientPool, stopAll map[uuid.UUID]chan bool) {
	// cancel dependencies in pool
	dClientPool.RemoveAllClients(job)

	// cancel DependencyClient.watch TODO

	// cancel job
	stopAll[job.JobUUID] <- true
	close(stopAll[job.JobUUID])
	delete(stopAll, job.JobUUID)

	// remove job from jobs map
	delete(jobs, job.JobUUID.String())
}

func (x *Job) areEqualSpec(y *Job) bool {
	if !reflect.DeepEqual(x.Calendar, y.Calendar) {
		ServerLogger.Printf("Calendar has been updated")
		return false
	}
	if !reflect.DeepEqual(x.CalendarDirs, y.CalendarDirs) {
		ServerLogger.Printf("CalendarDirs has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Rollback, y.Rollback) {
		ServerLogger.Printf("Rollback has been updated")
		return false
	}
	if !reflect.DeepEqual(x.RequireCal, y.RequireCal) {
		ServerLogger.Printf("RequireCal has been updated")
		return false
	}
	if isNilAndNot(x.CronStart, y.CronStart) {
		return false
	}
	if (x.CronStart != nil && y.CronStart != nil) && *(x.CronStart) != *(y.CronStart) {
		ServerLogger.Printf("CronStart has been updated")
		return false
	}
	if !reflect.DeepEqual(x.CronStartArray, y.CronStartArray) {
		ServerLogger.Printf("CronStartArray has been updated")
		return false
	}
	if !reflect.DeepEqual(x.StartTime, y.StartTime) {
		ServerLogger.Printf("StartTime has been updated")
		return false
	}
	if !reflect.DeepEqual(x.StartDay, y.StartDay) {
		ServerLogger.Printf("StartDay has been updated")
		return false
	}
	if !reflect.DeepEqual(x.CronEndArray, y.CronEndArray) {
		ServerLogger.Printf("CronEndArray has been updated")
		return false
	}
	if !reflect.DeepEqual(x.CronRestart, y.CronRestart) {
		ServerLogger.Printf("CronRestart has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Inherits, y.Inherits) {
		ServerLogger.Printf("Inherits has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Name, y.Name) {
		ServerLogger.Printf("Name has been updated x:%s, y:%s", x.Name, y.Name)
		return false
	}
	if !reflect.DeepEqual(x.Description, y.Description) {
		ServerLogger.Printf("Description has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Comment, y.Comment) {
		ServerLogger.Printf("Comment has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Tags, y.Tags) {
		ServerLogger.Printf("Tags has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Group, y.Group) {
		ServerLogger.Printf("Group has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Env, y.Env) {
		ServerLogger.Printf("Env has been updated")
		return false
	}
	if !reflect.DeepEqual(x.DateEnv, y.DateEnv) {
		ServerLogger.Printf("DateEnv has been updated")
		return false
	}
	if x.Hold != y.Hold {
		ServerLogger.Printf("Hold has been updated")
		return false
	}
	if x.Retry != y.Retry {
		ServerLogger.Printf("Retry has been updated")
		return false
	}
	if x.RetryWait != y.RetryWait {
		ServerLogger.Printf("RetryWait has been updated")
		return false
	}
	if x.MaxDuration != y.MaxDuration {
		ServerLogger.Printf("MaxDuration has been updated")
		return false
	}
	if x.TmpDir != y.TmpDir {
		ServerLogger.Printf("TmpDir has been updated")
		return false
	}
	ServerLogger.Printf("checking logging purge %s <-> %s", x.Logging.Purge, y.Logging.Purge)
	if x.Logging.StdoutFile != y.Logging.StdoutFile || x.Logging.StderrFile != y.Logging.StderrFile || x.Logging.Append != y.Logging.Append || x.Logging.Purge != y.Logging.Purge {
		ServerLogger.Printf("Logging has been updated")
		return false
	}
	if x.Host != y.Host {
		ServerLogger.Printf("Host has been updated")
		return false
	}
	if x.User != y.User {
		ServerLogger.Printf("User has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Permissions, y.Permissions) {
		ServerLogger.Printf("Permissions has been updated")
		return false
	}
	if !reflect.DeepEqual(x.Admin, y.Admin) {
		ServerLogger.Printf("Admin has been updated")
		return false
	}
	if *(x.Cmd) != *(y.Cmd) {
		ServerLogger.Printf("Cmd has been updated x:%s, y:%s", *x.Cmd, *y.Cmd)
		return false
	}
	if x.ShutdownSig != y.ShutdownSig {
		ServerLogger.Printf("ShutdownSig has been updated")
		return false
	}
	if x.ShutdownCmd != y.ShutdownCmd {
		ServerLogger.Printf("ShutdownCmd has been updated")
		return false
	}
	//if (x.CronStart == nil && y.CronStart != nil) || (x.CronStart != nil && y.CronStart == nil) {
	return true
}

func isNilAndNot(x, y interface{}) bool {
	return (x == nil && y != nil) || (x != nil && y == nil)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request, sd *ServerData, server ServerConfig) {

	vars := mux.Vars(r)

	funs := template.FuncMap{"getControls": GetControls,
		"getElapsed":      GetElapsed,
		"getDependencies": func(job Job) template.HTML { return GetDependencies(job, sd) },
		"slugify":         slugify,
		"stringifyHTML":   func(s string) template.HTML { h := template.HTML(Stringify(s)); return h },
	}

	t, err := template.New("JobsHTML").Funcs(funs).Parse(JobsHTML)

	themePath := filepath.Join(server.ThemeDir, server.Theme)
	cssFiles, _ := ioutil.ReadDir(themePath)
	var themeCSS string
	for _, cssfile := range cssFiles {
		cssfileFullPath, err := filepath.Abs(filepath.Join(themePath, cssfile.Name()))
		if err != nil {
			ServerLogger.Println("issue sourcing theme css file:", err.Error())
		}
		thiscss, err := ioutil.ReadFile(cssfileFullPath)
		if err != nil {
			ServerLogger.Println("failed read of theme css file:", err.Error())
		}
		themeCSS = fmt.Sprintf("%s\n%s", themeCSS, thiscss)
	}
	CSS := fmt.Sprintf("%s\n%s", ClientCSS, themeCSS)
	t.New("ClientHeaderHTML").Parse(ClientHeaderHTML)
	t.New("ClientCSS").Parse(CSS)
	t.New("ClientJS").Parse(ClientJS)
	t.New("ClientWS").Parse(ClientWS)
	t.New("JobsTable").Parse(JobsTable)

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	user, _, _ := r.BasicAuth()

	type Dashboard struct {
		Base       template.URL
		Config     ServerConfig
		Jobs       map[string]jobMapStatic `json:"jobs"`
		JobOrder   map[string][]string     `json:"joborder"`
		GroupOrder []string                `json:"grouporder"`
		Authorized map[string]map[string]bool
	}

	// FIXME: this is similar code that is now in API AllJobs and should be functionalized
	// remove jobs from job_order not permissioned for current authenticated user
	job_order := make([]string, len(sd.job_order))
	copy(job_order, sd.job_order)
	for i := 0; i < len(job_order); i++ {
		v := sd.jobs[job_order[i]]
		if user != v.User && !stringInSlice(user, v.Admin) {
			job_order = append(job_order[:i], job_order[i+1:]...)
		}
	}
	jobs_static := make(jobMapStatic, 0)
	authorized := make(map[string]map[string]bool, 0)
	actions := []string{"hold", "start", "stop", "restart", "info"}
	for i := 0; i < len(job_order); i++ {
		v := sd.jobs[job_order[i]]
		jobs_static[job_order[i]] = v.copyJob()
		perms := make(map[string]bool)
		for _, action := range actions {
			perms[action] = v.hasPermission(user, action)
		}
		authorized[job_order[i]] = perms
	}

	reqGroup, passedGroup := vars["group"]
	group_order := make([]string, 0)

	groupedJobs := make(map[string]jobMapStatic)
	groupedOrder := make(map[string][]string)
	for _, group := range sd.group_order {
		if passedGroup && reqGroup != group {
			continue
		}
		group_order = append(group_order, group)
		j := make(jobMapStatic)
		var o []string
		for _, uuid := range sd.groups[group] {
			uuidString := uuid.String()
			if job, ok := jobs_static[uuidString]; ok {
				j[uuidString] = job.copyJob()
				o = append(o, uuidString)
			}
		}
		groupedJobs[group] = j
		groupedOrder[group] = o
	}
	err = t.Execute(w, Dashboard{Config: server, Jobs: groupedJobs, JobOrder: groupedOrder, GroupOrder: group_order, Authorized: authorized, Base: ""})
	if err != nil {
		ServerLogger.Printf("Dashboard Handler Error: %s", err.Error())
		return
	}
}
