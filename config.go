// https://stackoverflow.com/questions/48050945/how-to-unmarshal-json-into-durations

package rpeat

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"html"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

var GITCOMMIT, BUILDDATE, VERSION string
var template_ = "template"
var TEMPLATE = &template_

var RPEAT_API_KEY string

func getApiKey() (string, bool) {
	if RPEAT_API_KEY == "" {
		return "", false
	}
	return RPEAT_API_KEY, true
}
func loadApiKey(apiKeyFile string) (string, error) {
	var tok string
	var ok bool

	tok, ok = os.LookupEnv("RPEAT_API_KEY")
	if !ok {
		fmt.Printf("attempting to load api key from %s\n", apiKeyFile)
		fh, err := os.Open(apiKeyFile)
		if err != nil {
			fmt.Println(err)
			return tok, err
		}
		key := make([]byte, 44)
		_, err = fh.Read(key)
		if err != nil {
			fmt.Println(err)
			return tok, err
		}
		tok = string(key)
	}
	RPEAT_API_KEY = tok
	return tok, nil
}

type ServerStatus struct {
	njobs  int      // total
	rjobs  int      // running
	hjobs  int      // stopped or held
	sjobs  int      // succeeded
	rtjobs int      // retrying
	ondeck []string // uuids of next k jobs
	failed []string // uuids of failed jobs

	pendingupdates []string // uuids of jobs reloading
	started        int64    // unix timestamp
	updated        int64    // unix timestamp
}

// main server data object
type ServerData struct {
	svc          *service
	jobs         jobMap
	jobNameUUID  map[string]uuid.UUID
	job_order    []string
	group_order  []string
	groups       map[string][]uuid.UUID
	depEvt       chan *depEvt
	users        []AuthUser
	updates      chan *JobUpdate
	wsClientPool *wSClientPool
	dClientPool  *DependencyClientPool
	stopAll      map[uuid.UUID]chan bool
	unwatch      chan bool
	pidfile      string
	shutdownJobs func()
	jve          map[uuid.UUID]JobValidationExceptions
}

// find job by UUID or slug
func (jobs jobMap) getJob(id string) (*Job, bool) {
	var job *Job
	var ok bool
	if job, ok = jobs[id]; true { // run through all slugs just to check
		for _, j := range jobs {
			if slugify(j.Name) == id {
				job = j
				ok = true
				break
			}
		}
	}
	return job, ok
}
func slugify(s string) string {
	return strings.ToLower(strings.Replace(s, " ", "-", -1))
}

var Slugify = slugify

type ServerConfig struct {
	GITCOMMIT        string `json:"-" xml:"-"`
	BUILDDATE        string
	VERSION          string
	ID               string
	KEY              string
	HOME             string
	HOST             string
	PORT             string
	Https            bool
	TLS              *TLS   `json:"TLS,omitempty" xml:"TLS,omitempty"`
	ApiKey           string `json:"ApiKey,omitempty" xml:"ApiKey,omitempty"`
	ApiEndpoint      string `json:"ApiEndpoint" xml:"ApiEndpoint"`
	JobsFiles        []string
	Name             string
	ConfigFile       string
	AuthFile         string
	Owner            string
	Admin            []string
	Permissions      Permission
	Origin           int64 `json:"Origin,omitempty" xml:"Origin,omitempty"`
	Timezone         string
	Shell            string     `json:"Shell" xml:"Shell"`
	Clean            bool       `json:"Clean,omitempty" xml:"Clean,omitempty"`
	KeepHistory      bool       `json:"KeepHistory,omitempty" xml:"KeepHistory,omitempty"`
	MaxHistory       int        `json:"MaxHistory,omitempty" xml:"MaxHistory,omitempty"`
	Heartbeat        bool       `json:"Heartbeat" xml:"Heartbeat"`
	CalendarDirs     []string   `json:"CalendarDirs,omitempty"`
	Theme            string     `json:"Theme,omitempty" xml:"Theme,omitempty"`
	ThemeDir         string     `json:"ThemeDir,omitempty" xml:"ThemeDir,omitempty"`
	UseRelativePaths bool       `json:"UseRelativePaths,omiyempty" xml:"UseRelativePaths,omitempty"`
	TempDir          string     `json:"TmpDir" xml:"TmpDir"`
	Logging          JobLogging `json:"JobLogging" xml:"JobLogging"`
	Jobs             []Job      `json:"-"`
}

func (k ServerConfig) Abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	p = filepath.Join(k.HOME, p)
	return p
}

func (k ServerConfig) Now() time.Time {
	loc, err := time.LoadLocation(k.Timezone)
	if err != nil {
		ServerLogger.Fatal(err)
	}
	now := time.Now().In(loc)
	if k.Origin != 0 {
		now = time.Unix(k.Origin, 0).In(loc)
	}
	return now
}

func ReadServerConfig(b *bytes.Buffer) (jobs jobMap, order []string) {
	var sjobs ServerJobs
	sjobs.Unserialize(b)
	jobs, order = parseJobs(&sjobs)
	return
}

func (sjobs ServerJobs) WriteServerConfig(b *bytes.Buffer) {
	sjobs.Serialize(b)
}

func LoadServerConfig(config string, save bool) (ServerConfig, error) {
	if exists, err := FileExists(config); !exists {
		ServerLogger.Fatal(err)
	}
	jsonFile, err := os.Open(config)

	byteval, _ := ioutil.ReadAll(jsonFile)
	jsonFile.Close()

	var sconf ServerConfig
	// defaults
	sconf.KeepHistory = true
	sconf.MaxHistory = 10
	sconf.Timezone = "UTC"
	err = json.Unmarshal(byteval, &sconf)
	if err != nil {
		ServerLogger.Fatal("error encountered while loading rpeat config file: ", err.Error())
	}
	if !sconf.UseRelativePaths {
		sconf.HOME, _ = filepath.Abs(sconf.HOME)
		sconf.ConfigFile = sconf.Abs(config)
		if sconf.TLS != nil {
			sconf.TLS.Cert = sconf.Abs(sconf.TLS.Cert)
			sconf.TLS.Key = sconf.Abs(sconf.TLS.Key)
		}
	}
	var jobsFilesFullPath []string
	for _, jobfile := range sconf.JobsFiles {
		fullpath := jobfile
		if !sconf.UseRelativePaths {
			fullpath = sconf.Abs(jobfile)
		}
		jobsFilesFullPath = append(jobsFilesFullPath, fullpath)
	}
	sconf.JobsFiles = jobsFilesFullPath
	if !sconf.UseRelativePaths {
		sconf.ConfigFile = sconf.Abs(sconf.ConfigFile)
		sconf.AuthFile = sconf.Abs(sconf.AuthFile)
	}

	if sconf.KEY == "" {
		sconf.KEY = uuid.New().String()
	}
	if sconf.ID == "" {
		sconf.ID = uuid.New().String()
	}
	if sconf.ApiEndpoint == "" { // FIXME: this should be easily updated through an evironment variable instead of hardcoded!
		sconf.ApiEndpoint = "https://api-internal.rpeat.io/"
	}

	if save {
		ch, _ := os.Create(config)
		defer ch.Close()

		enc := json.NewEncoder(ch)
		enc.SetIndent("", "    ")
		enc.SetEscapeHTML(false) // disable unicode coercion
		enc.Encode(sconf)
	}
	sconf.ApiKey, _ = loadApiKey(sconf.HOME + "/key")

	return sconf, err
}

func xmlJobSpecLoader(config string) (specs []JobSpec, err error) {
	//ServerLogger.Printf("[LoadJobSpec] Using XML")

	conf, err := os.Open(config)

	byteval, _ := ioutil.ReadAll(conf)
	conf.Close()

	var xmljobs Jobs
	err = xml.Unmarshal(replaceEntities(byteval), &xmljobs)
	if err != nil {
		ServerLogger.Fatal("error encountered while loading '", config, "' file into spec ", err.Error())
	}
	specs = xmljobs.Jobs
	return
}
func jsonJobSpecLoader(config string) (specs []JobSpec, err error) {

	conf, err := os.Open(config)

	byteval, _ := ioutil.ReadAll(conf)
	conf.Close()

	err = json.Unmarshal(byteval, &specs)
	if err != nil {
		// check err type: if UnmarshalTypeError it may be that Jobs file is missing arrays structure or something else
		ServerLogger.Printf("[LoadJobSpec] configuration error encountered, aborting: %s", err)
		if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
			var jobspecarray []JobSpec
			if typeErr.Type == reflect.TypeOf(jobspecarray) {
				ServerLogger.Printf("[ rpeat® jobs file parse error (json) ]  job specification requires an array of Jobs i.e. [ Job, Job, ... ]: Make sure your job(s) are wrapped in a json array ")
			} else {
				ServerLogger.Printf("[ rpeat® jobs file parse error (json) ]  found error at %d reading %s", typeErr.Offset, typeErr.Field)
			}
		} else if syntaxErr, ok := err.(*json.SyntaxError); ok {
			parseErrors := map[string]string{"\":\" after object key:value pair": "check for missing comma on previous line or unmatched quote issue",
				"\"' after object key:value pair":                                  "possible missing comma from prior key:value entry",
				"invalid character '{' after array element":                        "possible missing comma before next job or job parameter",
				"invalid character '}' looking for beginning of object key string": "trailing comma at end of individual job object parameters",
				"invalid character ']' looking for beginning of value":             "trailing comma at end of jobs array",
				"invalid character '":                                              "possible missing quotes",
				"'' looking for beginning of object key string":                    "possible issue with illegal single quote instead of double",
				"'' looking for beginning of value":                                "issue with value being single quoted instead of double",
			}
			var _ = parseErrors
			offset := syntaxErr.Offset
			linesparsed := strings.Count(string(byteval[0:offset]), "\n")
			lines := strings.Split(string(byteval), "\n")
			var pbytes, lineoffset int
			tbytes := 0
			for l := range lines {
				pbytes = tbytes
				tbytes = tbytes + len(lines[l]) + 1
				if tbytes > int(offset) {
					lineoffset = int(offset) - pbytes
					break
				}
			}
			jerr := fmt.Sprintf("[ rpeat jobs file parse error (json) ]:\n\n%s\n\n", err.Error())
			for e, helper := range parseErrors {
				if strings.Contains(err.Error(), e) {
					jerr = fmt.Sprintf("%s  ** suggestion: %s **\n\n", jerr, helper)
				}
			}
			jerr = fmt.Sprintf("%snear lines: %d-%d\n\n", jerr, linesparsed, linesparsed+1) // add character offset on line OR markup using ASCII color OR add byte[offset-3:offset+3] context
			for i := int(math.Max(0, float64(linesparsed-5))); i < int(math.Min(float64(len(lines)-1), float64(linesparsed+5))); i++ {
				if i == linesparsed || i == linesparsed-1 {
					jerr = fmt.Sprintf("%s->% 4d\t%s\n", jerr, i+1, lines[i])
				} else {
					if i == linesparsed+1 {
						jerr = fmt.Sprintf("%s      \t%s%s\n", jerr, strings.Repeat(" ", lineoffset-1), "|")
						jerr = fmt.Sprintf("%s      \t%s\n", jerr, strings.Repeat("^", lineoffset))
					}
					jerr = fmt.Sprintf("%s  % 4d\t%s\n", jerr, i+1, lines[i])
				}
			}
			jerr = fmt.Sprintln(jerr)
			fmt.Printf("%s", jerr)
		} else {
			ServerLogger.Fatal("[rpeat] unknown error occured while loading jobs json file", err.Error())
		}
		ServerLogger.Fatal()
	}
	return
}

type JobsFile struct {
	Config string
	Jobs   []Job
	Specs  []JobSpec
	Groups map[string][]uuid.UUID
	Xml    bool
}
type LoadTemplatesOutput struct {
	templates   map[string]JobSpec
	allJobNames []string
	err         error
}

func LoadTemplates(configs []string) LoadTemplatesOutput {
	var jobNames []string
	var specs []JobSpec
	var err error

	//isXML := false
	for _, config := range configs {
		var s []JobSpec
		if config[len(config)-3:] == "xml" { // FIXME: This should use proper test for filetype
			//isXML = true
			s, _ = xmlJobSpecLoader(config)
		} else {
			s, _ = jsonJobSpecLoader(config)
		}
		for i := 0; i < len(s); i++ {
			s[i].src = config
		}
		specs = append(specs, s...)
	}

	var jve JobValidationExceptions

	var njobs int
	for i := range specs {
		if specs[i].isTemplate() {
			specs[i].Disabled = true
		}
		ljobs := 0
		if !specs[i].Disabled {
			ljobs = len(specs[i].Jobs)
			jobNames = append(jobNames, specs[i].Name)
			jobNames = append(jobNames, specs[i].JobUUID.String())
		}
		njobs = njobs + 1 + ljobs
	}

	// build template map
	// { "Type": "template", "Name": "Template_A", "CronStart": "* 9 * * 1-5", "Env":{"A":5} }
	// { "Type": "template", Inherits: "Template_A", "Name": "Template_A2", "Env":{"A":7} }
	job_templates := make(map[string]JobSpec)
	for i := range specs {
		if specs[i].isTemplate() {
			if prev, ok := job_templates[specs[i].Name]; ok {
				verr := ValidationError{Exception: Template, Msg: fmt.Sprintf("Template (%d) '%s' skipped in (%s) - previously defined in (%s)", i, specs[i].Name, specs[i].src, prev.src)}
				jve.AddError(verr)
				err = jve
				continue
			}
			job_templates[specs[i].Name] = specs[i]
		}
	}

	templates := make(map[string]JobSpec)
	for tname, _ := range job_templates {
		inh := []string{tname}
		inherits := job_templates[tname].Inherits
		for {
			if inherits != nil {
				//inh = append(inh, *inherits)
				inh = append([]string{*inherits}, inh...)
				inherits = job_templates[*inherits].Inherits
			} else {
				break
			}
		}
		tmpl := JobSpec{Name: tname}
		//for i := len(inh)-1; i >= 0; i-- {
		for i := 0; i < len(inh); i++ {
			tmpl.copyTemplate(job_templates[inh[i]])
		}
		tmpl.InheritanceChain = inh
		templates[tname] = tmpl
	}
	return LoadTemplatesOutput{templates: templates, allJobNames: jobNames, err: err}

}
func LoadJobSpec(config string, maxhistory int, templates map[string]JobSpec, sconf ServerConfig, servername string, serverkey string, apikey string, logging JobLogging) ([]Job, []JobSpec, map[string][]uuid.UUID, bool, error) {

	var specs []JobSpec
	var err error

	isXML := false
	if config[len(config)-3:] == "xml" { // FIXME: This should use proper test for filetype
		isXML = true
		specs, err = xmlJobSpecLoader(config)
	} else {
		specs, err = jsonJobSpecLoader(config)
	}
	for i := 0; i < len(specs); i++ {
		specs[i].src = config
	}

	groups := make(map[string][]uuid.UUID)

	var njobs int
	for i := range specs {
		if specs[i].isTemplate() {
			specs[i].Disabled = true
		}
		ljobs := 0
		if !specs[i].Disabled {
			ljobs = len(specs[i].Jobs)
		}
		njobs = njobs + 1 + ljobs
	}

	jobs := make([]Job, njobs)

	j := 0
	var currentUUID uuid.UUID

	for i := range specs {
		// specs are top-level jobs which may contain .Jobs themselves (max one-level nesting)
		// i iterates over top-level
		// j iterates over top-level PLUS Jobs
		jobs[j] = Job{Disabled: true, Timezone: sconf.Timezone, MaxHistory: sconf.MaxHistory, Shell: sconf.Shell, ServerName: sconf.Name, ServerKey: serverkey, src: config, apiKey: sconf.ApiKey}

		// copy inherited template (if any) into new Job[j]
		if specs[i].Inherits != nil && !specs[i].isTemplate() {
			tmpspec := JobSpec{Name: specs[i].Name}
			env := make(EnvList, 0)
			tmpspec.Env = &env
			dateenv := make(EnvList, 0)
			tmpspec.DateEnv = &dateenv
			if tmpl, ok := templates[*(specs[i].Inherits)]; ok {
				tmpspec.copyTemplate(tmpl)
				jobs[j].copyJobSpec(&tmpspec)
				jobs[j].InheritanceChain = tmpl.InheritanceChain
			} else {
				msg := fmt.Sprintf("Template '%s' Not Found for '%s'", *(specs[i].Inherits), specs[i].Name)
				jobs[j].jve.AddWarning(ValidationWarning{Msg: msg, Exception: Template})
			}
			jobs[j].LocalEnv = EnvList([]string{})
			jobs[j].LocalDateEnv = EnvList([]string{})
		}

		// copy user defined specs into Job[j]
		jobs[j].copyJobSpec(&specs[i])
		if logging.Purge != "" {
			if specs[j].Logging == nil {
				ServerLogger.Printf("adding log rotation to %s", jobs[j].JobUUID)
				jobs[j].Logging.Purge = logging.Purge
			}
		}
		parentJob := jobs[j].copyJob()
		currentUUID = jobs[j].JobUUID
		specs[i].JobUUID = currentUUID
		if jobs[j].Disabled {
			continue
		}
		groups[jobs[j].Group[0]] = append(groups[jobs[j].Group[0]], jobs[j].JobUUID)
		if specs[i].Jobs != nil { // CONTROLLER
			p := j
			//log.Printf("processing Jobs for %s [%s]", specs[i].Name, specs[i].JobUUID)
			uuids := make([]string, 0, len(specs[i].Jobs))
			var uuids_enabled []string
			specJobs := specs[i].Jobs
			depends := "@depends"
			trigger := "running" // first job starts when controller starts
			control_dependencies_success := make(map[string]string)
			control_dependencies_stopped := make(map[string]string)
			control_dependencies_failed := make(map[string]string)
			delay := "300ms"
			if specs[i].JobsControl.Delay != "" {
				delay = specs[i].JobsControl.Delay
			}
			jobs[j].Group = append(parentJob.Group, parentJob.Name)
			//maxFailures := specs[i].JobsControl.MaxFailures
			maxFailures := 1

			joj := 0
			for s := range specJobs { // JOJ
				j++
				//log.Printf(InfoColor, fmt.Sprintf("(Constructing Jobs) i=%d j=%d",i,j))
				jobs[j].copyJobSpec(&specJobs[s]) //, job_templates)
				if specJobs[s].Disabled {
					uuids = append(uuids, jobs[j].JobUUID.String())
					continue
				}
				uuids_enabled = append(uuids_enabled, jobs[j].JobUUID.String())
				joj++
				depUUID := currentUUID
				currentUUID = jobs[j].JobUUID
				specJobs[s].JobUUID = currentUUID
				//log.Printf("building Dependency for Jobs %s->%s", specs[i].Name, specJobs[s].Name)
				jobs[j].Type = JOJ
				jobs[j].Group = append(parentJob.Group, parentJob.Name)
				jobs[j].Timezone = parentJob.Timezone
				jobs[j]._location = parentJob._location
				groups[parentJob.Group[0]] = append(groups[jobs[j].Group[0]], jobs[j].JobUUID)
				jobs[j].JobsControl = parentJob.JobsControl
				//log.Printf("Name:%s UUID:%s Group:%v", jobs[j].Name, jobs[j].JobUUID, jobs[j].Group)
				jobs[j].Dependency = []Dependency{
					// start job if prior job in Jobs succeeds && controller is running IFF JobsControl.nfailures < N
					Dependency{Dependencies: map[string]string{depUUID.String(): trigger, parentJob.JobUUID.String(): "running"},
						Action: "start", Condition: "all", Delay: delay},

					// stop job if controlling Job is stopped
					Dependency{Dependencies: map[string]string{parentJob.JobUUID.String(): "stopped"},
						Action: "stop", Condition: "all", Delay: "100ms"},

					// reset back to ready state if controlling Job success
					Dependency{Dependencies: map[string]string{parentJob.JobUUID.String(): "success|failed"},
						Action: "ready", Condition: "all", Delay: "1s"},

					// reset back to ready state if controlling Job is reset to ready - by unhold
					Dependency{Dependencies: map[string]string{parentJob.JobUUID.String(): "ready"},
						Action: "ready", Condition: "all", Delay: "1s"},
				}
				// TODO
				// Parallelism can be supported by using make(chan bool, MaxConcurrent) and each job after Dependency
				// trigger attempts to write to chan, blocking if MaxConcurrent jobs are running already.  Global
				// rpeat setting (ServerConfig or system) can set MaxConcurrent=min(GMaxConcurrent, MaxConcurrent)
				// after job completes it pull from chan to make room for next job.  This also allows for jobs
				// to run independent of ordering success
				jobs[j].CronStart = &depends
				control_dependencies_success[currentUUID.String()] = "success" //need to count failures
				control_dependencies_stopped[currentUUID.String()] = "stopped"
				control_dependencies_failed[currentUUID.String()] = "failed"

				// inherit certain fields from parent FIXME: should be a function
				if jobs[j].Permissions == nil {
					jobs[j].Permissions = parentJob.Permissions
				}
				if jobs[j].User == "" {
					jobs[j].User = parentJob.User
				}
				if jobs[j].Env == nil {
					jobs[j].Env = parentJob.Env
					// should be able to override Env as well as update ?
				}
				if jobs[j].DateEnv == nil {
					jobs[j].DateEnv = parentJob.DateEnv
					// should be able to override Env as well as update ?
				}
				uuids = append(uuids, jobs[j].JobUUID.String())
				//trigger = "success"
				trigger = "success"
			}

			// add UUID back to configuration file object: FIXME: test doing aways with userspec and utilizing json/xml tags more carefully in JobSpec
			//           if len(userspec) > 0 {
			//               var jobuuid []map[string]interface{}
			//               for s,j := range userspec[i]["Jobs"].([]interface{}) {
			//                   j.(map[string]interface{})["JobUUID"] = uuids[s]
			//                   specs[i].Jobs[s].JobUUID = uuid.MustParse(uuids[s])
			//                   jobuuid = append(jobuuid, j.(map[string]interface{}))
			//               }
			//               userspec[i]["Jobs"] = jobuuid
			//           } //else {
			for s := range specs[i].Jobs {
				specs[i].Jobs[s].JobUUID = uuid.MustParse(uuids[s])
			}
			//}

			// add final Jobs dependences to top-level Job
			ctldependencies := []Dependency{
				// when all Jobs succeed, Job completes with completed_success
				Dependency{Dependencies: control_dependencies_success,
					Action: "completed_success", Condition: "all", Delay: "100ms"},
				// when any Jobs are stopped, Job completes with completed_stopped
				Dependency{Dependencies: control_dependencies_stopped,
					Action: "completed_stopped", Condition: "any", Delay: "100ms", N: 1},
				// any jobs fail, Jop fails
				Dependency{Dependencies: control_dependencies_failed,
					Action: "completed_failed", Condition: "any", Delay: "100ms", N: maxFailures},
			}
			jobs[p].Dependency = ctldependencies
		}
		j++
	}

	return jobs, specs, groups, isXML, err
}

func (job *JobSpec) copyTemplate(spec JobSpec) {
	// never copy Name, Description, Jobs, JobsControl
	if spec.Tags != nil {
		job.Tags = spec.Tags
	}
	if job.Group == nil {
		if spec.Group == nil {
			job.Group = []string{job.Name}
		} else {
			job.Group = spec.Group
		}
	} else if spec.Group != nil {
		job.Group = spec.Group
	}
	job.Type = TEMPLATE
	job.Disabled = true // all Templates are disabled by design
	job.Inherits = spec.Inherits
	if job.Inherits != nil {
		job.InheritanceChain = append(job.InheritanceChain, *job.Inherits)
	}
	if spec.Cmd != nil {
		job.Cmd = spec.Cmd
	}
	job.ShutdownCmd = spec.ShutdownCmd
	job.ShutdownSig = spec.ShutdownSig
	job.Shell = spec.Shell
	if spec.Env != nil {
		if job.Env == nil {
			job.Env = spec.Env
		} else {
			for _, kv := range *spec.Env {
				*job.Env = append(*job.Env, kv)
			}
		}
	}
	if spec.DateEnv != nil {
		if job.DateEnv == nil {
			job.DateEnv = spec.DateEnv
		} else {
			for _, kv := range *spec.DateEnv {
				*job.DateEnv = append(*job.DateEnv, kv)
			}
		}
	}
	if spec.AlertActions != nil {
		job.AlertActions = spec.AlertActions
	}
	if spec.Timezone != nil {
		job.Timezone = spec.Timezone
	}
	if spec.Calendar != nil {
		job.Calendar = spec.Calendar
	}
	if spec.CalendarDirs != nil {
		job.CalendarDirs = spec.CalendarDirs
	}
	job.Rollback = spec.Rollback
	job.RequireCal = spec.RequireCal
	if spec.StartDay != nil {
		job.StartDay = spec.StartDay
	}
	if spec.StartTime != nil {
		job.StartTime = spec.StartTime
	}
	if spec.EndDay != nil {
		job.EndDay = spec.EndDay
	}
	if spec.EndTime != nil {
		job.EndTime = spec.EndTime
	}
	if spec.CronStart != nil {
		job.CronStart = spec.CronStart
	}
	if spec.CronEnd != nil {
		job.CronEnd = spec.CronEnd
	}
	if spec.CronRestart != nil {
		job.CronRestart = spec.CronRestart
	}
	if spec.Jitter != nil {
		job.Jitter = spec.Jitter
	}
	if spec.StartRule != nil {
		job.StartRule = spec.StartRule
	}
	if spec.Dependency != nil {
		job.Dependency = spec.Dependency
	}
	if spec.Artifacts != nil {
		job.Artifacts = spec.Artifacts
	}
	if spec.Hold != nil {
		job.Hold = spec.Hold
	}
	if spec.HoldDuration != nil {
		job.HoldDuration = spec.HoldDuration
	}
	if spec.Retry != nil {
		job.Retry = spec.Retry
	}
	if spec.RetryWait != nil {
		job.RetryWait = spec.RetryWait
	}
	if spec.RetryReset != nil {
		job.RetryReset = spec.RetryReset
	}
	if spec.MissedReset != nil {
		job.MissedReset = spec.MissedReset
	}
	if spec.Reset != nil {
		job.Reset = spec.Reset
	}
	if spec.MaxRuntime != nil {
		job.MaxRuntime = spec.MaxRuntime
	}
	if spec.MinRuntime != nil {
		job.MinRuntime = spec.MinRuntime
	}
	if spec.MaxDuration != nil {
		job.MaxDuration = spec.MaxDuration
	}
	if spec.Logging != nil {
		job.Logging = spec.Logging
	}
	if spec.Host != nil {
		job.Host = spec.Host
	}
	if spec.User != nil {
		job.User = spec.User
	}
	if spec.Permissions != nil {
		job.Permissions = spec.Permissions
	}
	if spec.Admin != nil {
		job.Admin = spec.Admin
	}
	job.src = spec.src
}
func (job *Job) CopyJobSpec(spec *JobSpec) {
	job.copyJobSpec(spec)
}
func (job *Job) copyJobSpec(spec *JobSpec) {
	job.Name = spec.Name
	job.Description = spec.Description
	job.Comment = spec.Comment
	switch spec.JobUUID.String() {
	case "00000000-0000-0000-0000-000000000000":
		spec.JobUUID = uuid.New()
		job.JobUUID = spec.JobUUID
	default:
		job.JobUUID = spec.JobUUID
	}
	job.Type = spec.Type
	if spec.Tags != nil {
		job.Tags = *spec.Tags
	}
	if job.Group == nil {
		if spec.Group == nil {
			job.Group = []string{""}
		} else {
			job.Group = spec.Group
		}
	} else if spec.Group != nil {
		job.Group = spec.Group
	}
	job.Disabled = spec.Disabled
	job.Inherits = spec.Inherits
	// DO NOT UPDATE InheritanceChain as this is taken care of in Templates
	if spec.Cmd != nil {
		job.Cmd = spec.Cmd
	}
	job.ShutdownCmd = spec.ShutdownCmd
	job.ShutdownSig = spec.ShutdownSig
	job.Jobs = spec.Jobs
	if job.Jobs != nil {
		job.Type = CONTROLLER
		job.Jobs = nil // may be a leak?
	}
	for i := range job.Jobs {
		job.Jobs[i].Group = []string{job.Name}
	}
	job.JobsControl = spec.JobsControl
	if spec.JobsControl == nil {
		job.JobsControl = &JobsControl{}
	}
	if spec.Shell != nil {
		job.Shell = *spec.Shell
	}
	if spec.Env != nil {
		if job.Env != nil {
			for _, kv := range *spec.Env {
				job.Env = append(job.Env, kv)
			}
		} else {
			job.Env = *spec.Env
		}
		job.LocalEnv = *spec.Env
	}
	if spec.DateEnv != nil {
		if job.DateEnv != nil {
			for _, kv := range *spec.DateEnv {
				job.DateEnv = append(job.DateEnv, kv)
			}
		} else {
			job.DateEnv = *spec.DateEnv
		}
		job.LocalDateEnv = *spec.DateEnv
	}
	if spec.AlertActions != nil {
		job.AlertActions = *spec.AlertActions
	}
	if spec.Timezone != nil {
		job.Timezone = *spec.Timezone
	}
	if spec.Calendar != nil {
		job.Calendar = *spec.Calendar
	}
	if spec.Rollback != nil {
		job.Rollback = *spec.Rollback
	}
	if spec.Rollback != nil {
		job.RequireCal = *spec.RequireCal
	}
	if spec.CronStart != nil {
		//job.CronStart = spec.CronStart
		job.CronStartArray = *spec.CronStart
	}
	//if spec.CronStartArray != nil {
	//    job.CronStartArray = *spec.CronStartArray
	//}
	if spec.CronEnd != nil {
		//job.CronEnd = spec.CronEnd
		job.CronEndArray = *spec.CronEnd
	}
	//if spec.CronEndArray != nil {
	//    job.CronEndArray = *spec.CronEndArray
	//}
	if spec.CronRestart != nil {
		job.CronRestart = spec.CronRestart
	}
	if spec.Jitter != nil {
		job.Jitter = *spec.Jitter
	}
	if spec.StartDay != nil {
		job.StartDay = *spec.StartDay
	}
	if spec.StartTime != nil {
		job.StartTime = *spec.StartTime
	}
	if spec.EndDay != nil {
		job.EndDay = *spec.EndDay
	}
	if spec.EndTime != nil {
		job.EndTime = *spec.EndTime
	}
	if spec.StartRule != nil {
		job.StartRule = *spec.StartRule
	}
	if spec.Dependency != nil {
		job.Dependency = spec.Dependency
	}
	if spec.Artifacts != nil {
		job.Artifacts = *spec.Artifacts
	}
	for i := range job.Dependency {
		if job.Dependency[i].Condition == "" {
			job.Dependency[i].Condition = "all"
		}
	}
	if spec.Hold != nil {
		job.Hold = *spec.Hold
	}
	if spec.HoldDuration != nil {
		job.HoldDuration = *spec.HoldDuration
	}
	if spec.Retry != nil {
		job.Retry = *spec.Retry
	}
	if spec.RetryWait != nil {
		job.RetryWait = *spec.RetryWait
	}
	if spec.RetryReset != nil {
		job.RetryReset = *spec.RetryReset
	}
	if spec.MissedReset != nil {
		job.MissedReset = *spec.MissedReset
	}
	if spec.Reset != nil {
		job.Reset = spec.Reset
	}
	if spec.MaxRuntime != nil {
		job.MaxRuntime = *spec.MaxRuntime
	}
	if spec.MinRuntime != nil {
		job.MinRuntime = *spec.MinRuntime
	}
	if spec.MaxDuration != nil {
		job.MaxDuration = *spec.MaxDuration
	}
	if spec.Logging != nil {
		job.Logging = *spec.Logging
	}
	if spec.CalendarDirs != nil {
		job.CalendarDirs = *spec.CalendarDirs
		for d := range job.CalendarDirs {
			job.CalendarDirs[d], _ = filepath.Abs(job.CalendarDirs[d])
		}
	}
	if spec.Host != nil {
		job.Host = *spec.Host
	}
	if spec.User != nil {
		job.User = *spec.User
	}
	if spec.Permissions != nil {
		job.Permissions = *spec.Permissions
	}
	if spec.Admin != nil {
		job.Admin = *spec.Admin
	}
	job.LoadLocation()
	job.History = make([]JobHistory, 10)
	job.src = spec.src
}

// deepcopy job to avoid copying hidden fields and shallow for read-only
func (job *Job) copyJob() Job {
	if job.TryLock() {
		defer job.Unlock()
	}

	jobc := Job{}

	jobc.Name = job.Name
	jobc.Description = job.Description
	jobc.Comment = job.Comment
	jobc.JobUUID = job.JobUUID
	jobc.RunUUID = job.RunUUID
	jobc.Type = job.Type
	jobc.Tags = make([]string, len(job.Tags))
	copy(jobc.Tags, job.Tags)
	jobc.Group = job.Group
	jobc.Disabled = job.Disabled
	jobc.Inherits = job.Inherits
	// DO NOT UPDATE InheritanceChain as this is taken care of in Templates
	jobc.Cmd = job.Cmd
	jobc.ShutdownCmd = job.ShutdownCmd
	jobc.ShutdownSig = job.ShutdownSig
	jobc.Shell = job.Shell
	jobc.Env = make([]string, len(job.Env))
	copy(jobc.Env, job.Env)
	jobc.DateEnv = make([]string, len(job.DateEnv))
	copy(jobc.DateEnv, job.DateEnv)
	jobc.Timezone = job.Timezone
	jobc.Calendar = job.Calendar
	jobc.Rollback = job.Rollback
	jobc.CronStart = job.CronStart
	jobc.CronStartArray = make([]string, len(job.CronStartArray))
	copy(jobc.CronStartArray, job.CronStartArray)
	jobc.CronEnd = job.CronEnd
	jobc.CronEndArray = make([]string, len(job.CronEndArray))
	copy(jobc.CronEndArray, job.CronEndArray)
	jobc.CronRestart = job.CronRestart
	jobc.Jitter = job.Jitter
	jobc.StartDay = job.StartDay
	jobc.StartTime = job.StartTime
	jobc.EndDay = job.EndDay
	jobc.EndTime = job.EndTime
	jobc.StartRule = job.StartRule
	jobc.Dependency = job.Dependency
	jobc.Hold = job.Hold
	jobc.HoldDuration = job.HoldDuration
	jobc.Retry = job.Retry
	jobc.RetryWait = job.RetryWait
	jobc.RetryReset = job.RetryReset
	jobc.MissedReset = job.MissedReset
	jobc.MaxRuntime = job.MaxRuntime
	jobc.MinRuntime = job.MinRuntime
	jobc.MaxDuration = job.MaxDuration
	jobc.Logging = job.Logging
	jobc.CalendarDirs = job.CalendarDirs
	jobc.Host = job.Host
	jobc.User = job.User
	jobc.Admin = job.Admin
	jobc.History = job.History
	jobc._location = job._location
	jobc.src = job.src

	jobc.ServerName = job.ServerName
	jobc.ServerKey = job.ServerKey
	jobc.Pid = job.Pid
	jobc.Unscheduled = job.Unscheduled
	jobc.Restarting = job.Restarting
	jobc.IsRunning = job.IsRunning
	jobc.Failed = job.Failed
	jobc.ExitCode = job.ExitCode
	jobc.JobState = job.JobState
	jobc.JobStateString = job.JobStateString
	jobc.PrevJobState = job.PrevJobState
	jobc.PrevJobStateString = job.PrevJobStateString
	jobc.RetryAttempt = job.RetryAttempt
	jobc.PrevStart = job.PrevStart
	jobc.PrevStop = job.PrevStop
	jobc.Started = job.Started
	jobc.Elapsed = job.Elapsed
	jobc.ElapsedUNIX = job.ElapsedUNIX
	jobc.NextStart = job.NextStart

	jobc.Updating = job.Updating
	jobc.MaxHistory = job.MaxHistory
	jobc.StartedUNIX = job.StartedUNIX
	jobc.PrevStopUNIX = job.PrevStopUNIX
	jobc.NextStartUNIX = job.NextStartUNIX
	jobc.StdoutFile = make([]string, len(job.StdoutFile))
	copy(jobc.StdoutFile, job.StdoutFile)
	jobc.StderrFile = make([]string, len(job.StderrFile))
	copy(jobc.StderrFile, job.StderrFile)

	return jobc
}

func (job *Job) LoadLocation() {
	var tzerr error
	job._location, tzerr = time.LoadLocation(job.Timezone)
	if tzerr != nil {
		//ServerLogger.Printf("unrecognized Timezone %s, setting to UTC",job.Timezone)
		job._location = time.UTC
	}
}

// xml <-> json conversion
func ConvertJobsFile(jobsFile string, asHtml, asXml bool) {
	templ := make(map[string]JobSpec)
	var logging JobLogging
	_, specs, _, isXML, _ := LoadJobSpec(jobsFile, 0, templ, ServerConfig{}, "", "", "", logging)
	if (isXML || asHtml) && !asXml {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "    ")
		enc.SetEscapeHTML(false) // disable unicode coercion
		enc.Encode(specs)
	} else {
		xmlJobs := Jobs{Jobs: specs, XMLName: xml.Name{Space: "", Local: "Jobs"}}
		x, err := xml.MarshalIndent(xmlJobs, "", "  ")
		if err != nil {
			panic(err)
		}
		xmls := string(x)
		xmls = html.UnescapeString(xmls)
		fmt.Println(xmls)
	}
}

//	func LoadConfig(home, config string, clean, keephistory bool, maxhistory int, servername string, serverkey string, apikey string, logging JobLogging) (ServerJobs, error) {
//		return LoadConfig2(home, []string{config}, clean, keephistory, maxhistory, servername, serverkey, apikey, logging)
//	}
func LoadConfig2(home string, jobsFiles []string, sconf ServerConfig, clean, keephistory bool, maxhistory int, servername string, serverkey string, apikey string, logging JobLogging) (ServerJobs, error) {

	var err error
	var alljobs []Job
	allgroups := make(map[string][]uuid.UUID)

	//templates, err := LoadTemplates(jobsFiles)
	tmplOut := LoadTemplates(jobsFiles)
	templates := tmplOut.templates
	err = tmplOut.err
	if err != nil {
		ServerLogger.Printf("error loading templates")
		return ServerJobs{}, err
	}

	for _, jobsFile := range jobsFiles {
		jobs, specs, groups, isXML, _ := LoadJobSpec(jobsFile, sconf.MaxHistory, templates, sconf, sconf.Name, serverkey, sconf.ApiKey, sconf.Logging)

		alljobs = append(alljobs, jobs...)

		if isXML {
			xmlJobs := Jobs{Jobs: specs, XMLName: xml.Name{Space: "", Local: "Jobs"}}
			x, err := xml.MarshalIndent(xmlJobs, "", "  ")
			if err != nil {
				panic(err)
			}
			err = ioutil.WriteFile(jobsFile, replaceHTML(x), 0644)
			if err != nil {
				panic(err)
			}
		} else {
			// save original spec back to file with (new?) UUID
			ch, _ := os.Create(jobsFile)
			defer ch.Close()
			enc := json.NewEncoder(ch)
			enc.SetIndent("", "    ")
			enc.SetEscapeHTML(false) // disable unicode coercion
			//enc.Encode(userspec)
			enc.Encode(specs)
		}

		for group, jobids := range groups {
			allgroups[group] = append(allgroups[group], jobids...)
		}
	}

	// reload prior job state if enabled
	for i, job := range alljobs {
		rj := fmt.Sprintf("%s/.%s.rj", home, job.JobUUID)
		_, err := os.Stat(rj)
		if err == nil {
			if sconf.Clean {
				if sconf.KeepHistory {
					ServerLogger.Printf("loading job history for %s", job.JobUUID)
					tmpjob, err := LoadJobState(rj, true)
					if err != nil {
						ServerLogger.Printf("failed to load prior state from %s, skipping history", rj)
						continue
					}
					alljobs[i].History = tmpjob.History
					//alljobs[i].FullHistory = tmpjob.FullHistory
				}
				continue
			} else {
				ServerLogger.Printf("loading prior job state for %s", job.JobUUID)
				tmpjob, err := LoadJobState(rj, true)
				if err != nil {
					ServerLogger.Printf("failed to load prior state from %s, skipping history", rj)
					continue
				}
				// alljobs[i] = tmpjob
				alljobs[i].History = tmpjob.History
				alljobs[i].JobState = tmpjob.JobState
			}
			if err != nil {
				ServerLogger.Fatal("failed to load prior state from", rj)
			}
			if alljobs[i].JobState == JRunning || alljobs[i].JobState == JRetrying {
				alljobs[i].setHold(true)
				alljobs[i].setJobState(JUnknown)
				ServerLogger.Printf("job [%s] in inconsistent state!", alljobs[i].JobUUID)
			}
		}
	}
	sjobs := NewJobMap(home, alljobs)
	sjobs.Groups = allgroups

	var b bytes.Buffer
	sjobs.WriteServerConfig(&b)
	ioutil.WriteFile(".rpeat/rpeat", b.Bytes(), os.FileMode(0644)) // this is not needed and should be moved elsewhere until implemented
	//job_map, job_order := ReadServerConfig(&b)
	//return job_map, job_order, err
	//return sjobs.Jobs, sjobs.JobOrder, err
	return sjobs, err
}

func parseJobs(sjobs *ServerJobs) (jobMap, []string) {

	jobs := sjobs.Jobs
	job_order := sjobs.JobOrder

	meta := KMeta{Uuid: uuid.New().String(), Instantiated: time.Now()}

	for _, job := range jobs {
		job.parseJob(true)
		job.JobMeta = meta

	}
	return jobs, job_order
}

func (spec *JobSpec) parseJob(createDir bool) error {
	job := Job{}
	job.copyJobSpec(spec)
	err := job.parseJob(createDir)
	return err
}
func (job *Job) ParseJob() error {
	return job.parseJob(true)
}
func (job *Job) parseJob(createDir bool) error {

	var err error

	if job.Host == "" {
		hostname, _ := os.Hostname()
		job.Host = hostname
	}

	job.LoadLocation()

	if job.StartTime != "" {
		cronStartArray, cronstring := ParseDayAndTime(job.StartDay, job.StartTime, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
		job.cronStartArray = cronStartArray
		job.CronStart = &cronstring
	} else {
		if job.CronStartArray != nil {
			var cronstring []string
			job.cronStart = NullCron()
			if job.CronStartArray[0] == "@depends" {
				job.cronStart = DependentCron()
			}
			job.cronStart.array = true
			var cronArray []Cron
			for _, c := range job.CronStartArray {
				cron, err := ParseCron(c, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
				if err != nil {
					cErr := err.(CronError)
					cErr.Schedule = CronStart
					return cErr
				}
				cronstring = append(cronstring, fmt.Sprintf("%s", c))
				cronArray = append(cronArray, cron)
			}
			job.cronStartArray = cronArray
			CronStart := strings.Join(cronstring, ",")
			job.CronStart = &CronStart
		} else {
			if job.CronStart == nil {
				job.cronStart = NullCron()
			} else {
				job.cronStart, err = ParseCron(*job.CronStart, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
				if err != nil {
					cErr := err.(CronError)
					cErr.Schedule = CronStart
					return cErr
				}
			}
			job.cronStartArray = []Cron{job.cronStart}
		}
	}
	if job.EndTime != "" {
		cronEndArray, cronstring := ParseDayAndTime(job.EndDay, job.EndTime, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
		job.cronEndArray = cronEndArray
		job.CronEnd = &cronstring
	} else {
		if job.CronEndArray != nil {
			var cronstring []string
			job.cronEnd = NullCron()
			job.cronEnd.array = true
			var cronArray []Cron
			for _, c := range job.CronEndArray {
				cron, err := ParseCron(c, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
				if err != nil {
					cErr := err.(CronError)
					cErr.Schedule = CronEnd
					return cErr
				}
				cronstring = append(cronstring, fmt.Sprintf("%s", c))
				cronArray = append(cronArray, cron)
			}
			job.cronEndArray = cronArray
			CronEnd := strings.Join(cronstring, ",")
			job.CronEnd = &CronEnd
		} else {
			if job.CronEnd == nil {
				job.cronEnd = NullCron()
			} else {
				job.cronEnd, err = ParseCron(*job.CronEnd, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
				if err != nil {
					cErr := err.(CronError)
					cErr.Schedule = CronEnd
					return cErr
				}
			}
			job.cronEndArray = []Cron{job.cronEnd}
		}
	}
	if job.CronRestart != nil {
		job.cronRestart, err = ParseCron(*job.CronRestart, job.Timezone, job.Calendar, job.CalendarDirs, job.Rollback, job.RequireCal, job.Jitter)
		if err != nil {
			return err
		}
	}
	job.startRule = job.getStartRule()
	job.JobState = JReady
	if job.Hold == true {
		job.JobState = JHold
	}
	job.JobStateString = job.JobState.String()
	if job.TmpDir == "" {
		job.TmpDir = filepath.Join(os.TempDir(), "rpeat")
	}
	//job.Logging.Logs = make([]JobLog,0)
	if job.Logging.Purge == "" { // never remove
		job.Logging.purge = time.Duration(math.MaxInt64)
	} else {
		purge, err := time.ParseDuration(job.Logging.Purge)
		if err != nil {
			ServerLogger.Printf("error parsing Logging.Purge duration: %s", err)
			purge = time.Duration(math.MaxInt64)
			return err
		}
		job.Logging.purge = purge
	}
	job.Logging.l = time.NewTimer(math.MaxInt64) // no trigger set until job runs

	if createDir {
		os.Mkdir(job.TmpDir, os.FileMode(0770))
	}
	return err

}

func NewJobMap(home string, ojobs []Job) ServerJobs {

	var jobs []Job
	for _, j := range ojobs {
		if j.JobUUID.String() != "00000000-0000-0000-0000-000000000000" {
			jobs = append(jobs, j)
		}
	}

	jobs_map := make(jobMap, len(jobs))
	jobNameUUID := make(map[string]uuid.UUID)
	var job_uuid, job_order, group_order []string

	meta := KMeta{HOME: home, Uuid: uuid.New().String(), Instantiated: time.Now()}

	for i, job := range jobs {
		jobs[i].JobUUID = job.JobUUID
		job_uuid = append(job_uuid, jobs[i].JobUUID.String()) // need this to persist regardless even if Disabled
		if job.Disabled {
			continue
		}

		jobs[i].parseJob(true)

		jobs[i].JobMeta = meta

		job_order = append(job_order, jobs[i].JobUUID.String())
		if !stringInSlice(jobs[i].Group[0], group_order) {
			group_order = append(group_order, jobs[i].Group[0])
		}
		jobs_map[jobs[i].JobUUID.String()] = &jobs[i]
		jobNameUUID[jobs[i].JobUUID.String()] = jobs[i].JobUUID
		jobNameUUID[jobs[i].Name] = jobs[i].JobUUID
	}
	sjobs := ServerJobs{Jobs: jobs_map, JobOrder: job_order, JobUUID: job_uuid, GroupOrder: group_order, JobNameUUID: jobNameUUID}
	return sjobs
}
