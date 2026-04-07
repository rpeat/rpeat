package rpeat

// Custom struct tags
// https://itnext.io/creating-your-own-struct-field-tags-in-go-c6c86727eff

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	//"runtime"
)

var _JOJ string = "JOJ"
var JOJ = &_JOJ
var _CONTROLLER string = "CONTROLLER"
var CONTROLLER = &_CONTROLLER

type onStart int

const (
	RestartJob onStart = 1
	StartJob   onStart = 2
	NoStartJob onStart = 3
)

type StartRule struct {
	Kill       bool
	Start      bool
	Concurrent bool
}
type Env map[string]string
type EnvPair string
type EnvList []string

type Reason struct {
	Action    string `json:"Action,omitempty"`
	Comment   string `json:"Comment"`
	HeldUntil string `json:"HeldUntil"`
	User      string `json:"User,omitempty"`
	Timestamp int64  `json:"Timestamp,omitempty"`
}
type Reset struct {
	Retry  string `json:"Failed,omitempty"`
	Missed string `json:"Missed,omitempty"`
	Hold   string `json:"Hold,omitempty"`
}

const (
	MAX_JOBS = 1000
)

type JState int

const (
	JRunning       JState = iota + 1100
	JHold                 //1101
	JStopped              //1102
	JStarted              //1103
	JFailed               //1104
	JReady                //1105
	JRetrying             //1106
	JRetryWait            //1107
	JRetryFailed          //1108
	JSuccess              //1109
	JEnd                  //1110
	JRestart              //1111
	JReset                //1112
	JStopping             //1113
	JUnknown              //1114
	JContingent           //1115
	JWarning              //1116
	JWarning2             //1117
	JWarning3             //1118
	JConfigWarning        //1119
	JConfigError          //1120
	JMissedWarning        //1121
	JMissedError          //1122
	JDepWarning           //1123
	JDepRetry             //1124
	JDepFailed            //1125
	JManual               //1126
	JManualSuccess        //1127
	JUpdating             //1128
	JUpdated              //1129
)

func (jstate JState) String() string {
	names := [...]string{
		"running",
		"onhold",
		"stopped",
		"started",
		"failed",
		"ready",
		"retrying",
		"retrywait",
		"retryfailed",
		"success",
		"end",
		"restart",
		"reset",
		"stopping",
		"unknown",
		"contingent",
		"warning",
		"warning2",
		"warning3",
		"configwarning",
		"configerror",
		"missedwarning",
		"missed",
		"depwarning",
		"depretry",
		"depfailed",
		"manual",
		"manualsuccess",
		"updating",
		"updated",
	}
	if jstate < JRunning || jstate > JManualSuccess {
		return "Unknown"
	}
	return names[jstate-1100]
}

var StringToJState = map[string]JState{"JFailed": JFailed, "JSuccess": JSuccess, "JWarning": JWarning, "JWarning2": JWarning2, "JWarning3": JWarning3}

func (jstate JState) ColorizedString() string {
	var s string
	switch jstate {
	case JSuccess:
		s = fmt.Sprintf(Green, jstate.String())
	case JFailed:
		s = fmt.Sprintf(WarningColor, jstate.String())
	default:
		s = jstate.String()
	}
	return s
}

func (j JState) MarshalText() ([]byte, error) {
	return []byte(j.String()), nil
}

type KConfig interface {
	Now() time.Time
}

// JobSpec is the primary rpeat configuration for an individual job. At a minimum, only a Cmd needs to be specified.
//
type JobSpec struct {
	// job definition

	// name of job
	Name string `json:"Name" xml:"Name"`

	// optional description of job
	Description string `json:"Description,omitempty" xml:"Description,omitempty"`

	// optional comment
	Comment string `json:"Comment,omitempty" xml:"Comment,omitempty"`

	// type of job. If "Template" this job can be used in "Inherits" of other jobs
	Type *string `json:"Type,omitempty" xml:"Type,omitempty"`

	// user defined tags if desired. Used for future search in gui and api
	Tags *[]string `json:"Tags,omitempty" xml:"Tags,omitempty"`

	// display group - similar to Tags, but designed for GUI
	Group []string `json:"Group,omitempty" xml:"Group,omitempty"`

	// Name of defined Template for job to inherit from
	//
	// Jobs may only inherit from a single Template, but Templates can and often do
	// inherit from other Templates.  This chain is visible in the (R.O.) InheritanceChain
	Inherits         *string  `json:"Inherits,omitempty"`
	InheritanceChain []string `json:"-" xml:"-"` // read only

	// Hold controls if jobs is held on start up
	// Disabled=true removes job from schedule and view, but remains in configuration file
	// Hidden=true removes job from view, but it is still scheduled
	Hold     *bool `json:"Hold,omitempty"`
	Disabled bool  `json:"Disabled,omitempty" xml:"Disabled,omitempty"`
	Hidden   bool  `json:"Hidden,omitempty" xml:"Hidden,omitempty"`

	// Commands defining what runs on trigger
	//   - Shell (not implemented) controls what shell to run
	//
	//   - Cmd is string run by scheduler.  Like cron, if bash-style behavior
	//     is required it should be of form:
	//       Cmd: "/bin/sh -c echo 'hello'"
	//         -- or --
	//       <Cmd>/bin/sh -c echo 'hello'</Cmd>
	//
	//     In almost all cases this must be set or your job will fail. Shells enable
	//     most all path settings, binaries, etc.
	//
	//
	//   - ShutdownCmd is used to terminate a job when required, possibly gracefully (api or external command)
	//   - ShutdownSig is used to terminate a job when required by sending a signal to process (i.e. Control-C (SIGINT) or (SIGKILL))
	Shell       *string `json:"Shell,omitempty"`
	Cmd         *string `json:"Cmd,omitempty"`
	ShutdownCmd string  `json:"ShutdownCmd,omitempty" xml:"ShutdownCmd,omitempty"`
	ShutdownSig string  `json:"ShutdownSig,omitempty" xml:"ShutdownSig,omitempty"`

	// Environment Variables
	//
	//   Env defined execution environment variables that are resolved at runtime
	//   and follow shell conventions.  The resolved values are visible in GUI and
	//   accessible via the API
	//
	//   DateEnv defines variable in execution environment using special date-style
	//   rules - including accounting for timezones and calendar modifications if
	//   defined.  Dates may be shifted forward and back using special conventions
	//
	//   e.g.
	//        YR=CCYY        => $YR=2006
	//        MDY=MM/DD/CCYY => $MDY=01/02/2006
	//   Add example for TZ and cal and shifts
	Env     *EnvList `json:"Env,omitempty"`
	DateEnv *EnvList `json:"DateEnv,omitempty"`

	// ExitState is defined as 2=JWarning 3=JFailed to allow error codes to be used
	// to internally trigger JobStates
	ExitState ExitState `json:"ExitState,omitempy"`

	// AlertActions are a generalized mechanism to send json alerts from
	// rpeat-server to an arbitrary endpoint as specified in AlertAction (see also)
	AlertActions *AlertActions `json:"AlertActions,omitempty"`

	// UpdateEvents are external notifications send to API endpoint :TBA

	// Job of Jobs
	//
	// Jobs is an array of jobs designed to run sequentially from single parent
	// trigger.  Parent or child stop or failure will terminate all jobs, and all
	// jobs of parent job will cause parent to succeed.  This is an automatically
	// constructed Dependency ruleset
	//
	// JobsControl makes available additional controls and is required if Jobs
	// is defined.
	//
	// WARNING:
	// This feature is still experimental and will likely change in future versions.
	Jobs        []JobSpec    `json:"Jobs,omitempty" xml:"Jobs,omitempty"`
	JobsControl *JobsControl `json:"JobsControl,omitempty" xml:"JobsControl,omitempty"`

	// Calendar and Timezone controls
	//
	// All jobs use a defined timezone (implied or explicit) to control
	// trigger based on defined cron specifications.  If Timezone is
	// not defined (i.e. implied), it is automatically set to UTC.
	// Calendar offers a mechnism to condition triggers based on a valid
	// set of days - e.g. excluding US holidays.  A variety of Calendar
	// definitions are builtin, and custom calenders are easy to create
	// if required.
	//
	// Timezone is zoneinfo derived, and should look like America/Chicago or
	// Asia/Tokyo.  Shortnames are supported but are ambiguous and
	// resolved relative to the local timezone. Full names should be used instead.
	// For local timezone of server, use "Timezone":"Local"
	//
	// Calendar is the name of the calendar to be used, and accessible in
	// one of the CalendatDirs paths. If Calendar is not specified, all days
	// are considered available. Calendars act as a filter, e.g. M-F cron schedule
	// using a Calendar where a holiday falls on a particular Monday, the Monday trigger
	// would be ignored.
	//
	// Rollback determines if trigger should occur the first valid calendar day before the date
	// the trigger would normally be run, or if it should roll forward.  There are
	// only a few cases where rollback makes sense, for instance to ensure a job
	// is run on the last business day of the month.
	//
	// RequireCal ensures that all trigger dates calculated have coverage in the Calendar
	// file specified.  If set to false, a lack of dates is interpretted as no special treatment.
	// This is useful if a special calendar is defined which may not extend to all future dates
	// but is critical for historic reasons.
	Timezone     *string   `json:"Timezone,omitempty"`
	Calendar     *string   `json:"Calendar,omitempty"`
	CalendarDirs *[]string `json:"CalendarDirs,omitempty"`
	Rollback     *bool     `json:"Rollback,omitempty"`
	RequireCal   *bool     `json:"RequireCal,omitempty"`

	// Start, Stop and Restart triggers and concurrency rule
	//
	// CronStart, CronEnd and CronRestart are all cron-style arrays to specify when a
	// job is triggered.  If more than one rule is given for a particular trigger,
	// all cron rules will be evaluated to determine which is next. This
	// allows for disjoint rules to be combined which would otherwise be impossible.
	//
	// e.g.
	//   CronStart: ["0 8 * * 3"]                   ## every Wednesday at 08:00:00
	//   CronStart: ["05 30 16 * * 5"]              ## every Friday at 16:30:05
	//   CronStart: ["0 8 * * 3","05 30 16 * * 5"]  ## every Wednesday at 08:00:00 and Friday at 16:30:05
	//
	// all stardard cron-style shortcuts are available and *-style specification allows
	// 5 (minute resolutio) or 6 (second resolution)
	//
	// StartDay, StartTime, EndDay, and EndTime offer a more human reasable option
	// for specifying triggers
	//
	// Start: time when a job is supposed to start
	// End: time when a job will be stopped forcibly (via kill or ShutdownCmd) if still running
	// Restart: similar to End, but will then immediately restart job
	//
	// All options may be unspecified, in which case CronStart is set to @manual
	//
	// Dependencies to control Start, End and Restart are specified using the Dependency parameter
	// which is described elsewhere.
	//
	// Contingent jobs are ones that include a specified Dependency, but
	// define a CronStart which triggers the job. The dependency is not used to trigger.
	//
	// StartRule controls concurrency - allowing for either NoStart (ignore next trigger if running),
	// Start (start new job, leaving current running - NOT YET IMPLEMENTED) or Restart (trigger end
	// of running jobs and start a new job)
	//
	// Jitter: ADD uniform (max) random seconds to CronStart
	CronStart   *[]string `json:"CronStart,omitempty" xml:"CronStart,omitempty"`
	CronEnd     *[]string `json:"CronEnd,omitempty" xml:"CronEnd,omitempty"`
	CronRestart *string   `json:"CronRestart,omitempty"`
	StartDay    *string   `json:"StartDay,omitempty"`
	StartTime   *string   `json:"StartTime,omitempty"`
	EndDay      *string   `json:"EndDay,omitempty"`
	EndTime     *string   `json:"EndTime,omitempty"`
	StartRule   *string   `json:"StartRule,omitempty"`
	Jitter      *int      `json:"Jitter,omitempty"`

	// Control parameters for runtime exceptions
	//
	// Retry is number of retries before permanently failing a job
	// RetryWait defines durations between failure and retry attempts
	// RetryReset defines duration between final failure and reseting job (defaults to never)
	// MaxDuration defines maximum duration of job before it is terminated (via kill or ShutdownCmd)
	//
	// RetryWait, RetryReset and MaxDuration accept strings available to time.ParseDuration
	//
	// RetryWait may be a comma-delimted string to different durations for each retry, with final duration used
	// for any remaining tries.
	//
	// MinRuntime, MaxRuntime will signal a JWarning if below or above time, respectively (NOT YET IMPLEMENTED)
	Retry       *int    `json:"Retry,omitempty"`
	RetryWait   *string `json:"RetryWait,omitempty"`   // e.g. "11s", "3m" or "30s,5m,60m" - used in time.ParseDuration
	RetryReset  *string `json:"RetryReset,omitempty"`  // how long before a failed job is re-enabled "e.g. "12h" or "0s" - used in time.ParseDuration"
	MaxDuration *string `json:"MaxDuration,omitempty"` // used in time.ParseDuration"
	MinRuntime  *string `json:"MinRuntime,omitempty"`
	MaxRuntime  *string `json:"MaxRuntime,omitempty"`

	// Reset job after JFailed, JMissedWarning
	MissedReset *string `json:"MissedReset,omitempty"`
	Reset       *Reset  `json:"Reset,omitempty"`

	// HoldDuration is the duration a job is held before being released. This
	// facilites temporary holds expressed like RetryWait. If empty, job is held
	// indefinitely
	HoldDuration *string `json:"HoldDuration"`

	// Dependency offers a simple yet powerful mechanism to condition
	// triggers based on one or more Jobs defined within a server. If specified
	// in conjunction with CronStart, will result in a contingency that must be
	// satisfied before allowing job to run.
	//
	// See Dependency for more detail
	Dependency []Dependency `json:"Dependency,omitempty" xml:"Dependency,omitempty"`

	// Artifacts are results of jobs which can be used to trigger an action
	// such as copying to a new location.
	Artifacts *Artifacts `json:"Artifacts,omitempty" xml:"Artifacts,omitempty"`

	// Logging
	// All stdout and stderr from a Cmd are captured and saved to
	// a temporary file used by logs.  If the StdoutFile and/or StderrFile
	// are set, jobs output is also logged to those files.
	// TmpDir controls location of temp files, if not set defaults to system temp
	// as determined by os.TempFile (... will need to better document this)
	//
	// See JobLogging for more details of log controls
	TmpDir  string      `json:"TmpDir,omitempty" xml:"TmpDir,omitempty"`
	Logging *JobLogging `json:"Logging,omitempty"`

	// Access controls
	//
	// Host defines which machine the job is to run on - for use in HA/Distributed version
	// if undefined, it is set to host of server process
	//
	// User is the owner or the job - granted permission by default. Only one allowed
	//
	// Permissions allows for granular control of actions and views - defaults to least priviledge
	// based on a list of authorized users as defined by Authentication process.
	// Admin allows for ??
	Host        *string     `json:"Host,omitempty"`
	User        *string     `json:"User,omitempty"`
	Permissions *Permission `json:"Permissions,omitempty"`
	Admin       *[]string   `json:"Admin,omitempty" xml:"Admin,omitempty"`

	//JobUUID uuid.UUID  `json:"JobUUID---INTERNAL-DO-NOT-EDIT" xml:"JobUUID---INTERNAL-DO-NOT-EDIT"`// exported to be able to write to file DO NOT EDIT
	// JobUUID is an internal value associated with a job once run for the first time. It should not be
	// edited as it links a unique job with its history and potentially with Dependencies
	JobUUID uuid.UUID `json:"JobUUID,omitempty" xml:"JobUUID,omitempty"` // exported to be able to write to file DO NOT EDIT

	src string
}

type Jobs struct {
	XMLName xml.Name  `xml:"Jobs"`
	Jobs    []JobSpec `xml:"JobSpec"`
	Delay   *string
}
type JobsControl struct {
	Delay         string
	MaxConcurrent int
	MaxFailures   int

	nfailures int
	lock      sync.Mutex
}

func (jobs *Jobs) Length() int {
	return len(jobs.Jobs)
}
func (jobs *Jobs) HasJobs() bool {
	if len(jobs.Jobs) > 0 {
		return true
	} else {
		return false
	}
}

type Job struct {
	// This is the name that may be used to reference this job in either
	// updates to .jobs file or to reference in downsteam Dependency fields
	// Note that while Name must be unique within a server instance, the
	// JobUUID is the reference that is used internally for all processes
	Name        string `json:"Name"`
	Description string `json:"Description"`
	// JobUUID and RunUUID are immutable for the duration of the process
	// or events run. These are automatically assigned and should not be
	// modified or deleted as all associated history for jobs would be lost
	// in the process.
	JobUUID          uuid.UUID
	RunUUID          uuid.UUID
	Type             *string  `json:"Type,omitempty"`
	Tags             []string `json:"Tags,omitempty"`
	JobMeta          KMeta    `json:"JobMeta,omitempty"`
	Disabled         bool     `json:"Disabled,omitempty"`
	Hidden           bool     `json:"Hidden,omitempty"`
	Inherits         *string  `json:"Inherits,omitempty"`
	InheritanceChain []string `json:"-"`

	Comment         string       `json:"Comment,omitempty"`
	Cmd             *string      `json:"Cmd,omitempty"`
	CmdEval         string       `json:"CmdEval,omitempty"`
	ShutdownCmd     string       `json:"ShutdownCmd,omitempty"`
	ShutdownCmdEval string       `json:"ShutdownCmdEval,omitempty"`
	ShutdownSig     string       `json:"ShutdownSig,omitempty"`
	Jobs            []JobSpec    `json:"Jobs,omitempty"`
	JobsControl     *JobsControl `json:"JobsControl,omitempty"`
	Shell           string       `json:"Shell,omitempty"`
	Env             EnvList
	DateEnv         EnvList
	LocalEnv        EnvList
	LocalDateEnv    EnvList

	ExitState    ExitState    `json:"ExitState,omitempy"`
	AlertActions AlertActions `json:"AlertActions,omitempty"`

	Timezone       string   `json:"Timezone,omitempty"`
	Calendar       string   `json:"Calendar,omitempty"`
	CalendarDirs   []string `json:"CalendarDirs,omitempty"`
	Rollback       bool     `json:"Rollback,omitempty"`
	RequireCal     bool     `json:"RequireCal,omitempty"`
	StartDay       string   `json:"StartDay,omitempty"`
	StartTime      string   `json:"StartTime,omitempty"`
	EndDay         string   `json:"EndDay,omitempty"`
	EndTime        string   `json:"EndTime,omitempty"`
	CronStart      *string  `json:"CronStart,omitempty"` // cron format "2,3 9-15 * * *"
	CronStartArray []string `json:"CronStartArray,omitempty"`
	CronEnd        *string  `json:"CronEnd,omitempty"`
	CronEndArray   []string `json:"CronEndArray,omitempty"`
	CronRestart    *string  `json:"CronRestart,omitempty"`
	StartRule      string   `json:"StartRule,omitempty"` // "Restart", "Start", "NoStart"
	Jitter         int      `json:"Jitter,omitempty"`
	//RRStart RRule `json:"RRStart"`        //  RRULE or ISO8601
	//RREnd RRule `json:"RREnd"`
	//RRRestart RRule `json:"RRRestart"`

	Dependency []Dependency `json:"Dependency,omitempty"`
	Artifacts  Artifacts    `json:"Artifacts,omitempty"`

	Retry       int    `json:"Retry,omitempty"`
	RetryWait   string `json:"RetryWait,omitempty"`   // "e.g. "11s" or "3m" - used in time.ParseDuration"
	RetryReset  string `json:"RetryReset,omitempty"`  // "e.g. "11s" or "3m" - used in time.ParseDuration"
	MaxDuration string `json:"MaxDuration,omitempty"` // used in time.ParseDuration"
	MinRuntime  string `json:"MinRuntime,omitempty"`
	MaxRuntime  string `json:"MaxRuntime,omitempty"`

	MissedReset  string `json:"MissedReset,omitempty"`
	Reset        *Reset `json:"Reset,omitempty"`
	HoldDuration string `json:"HoldDuration"`
	holdDuration string `json:"-"`
	Reason       Reason `json:"Reason"`

	//Repeat time.Duration
	TmpDir         string     `json:"TmpDir,omitempty"`
	Logging        JobLogging `json:"Logging,omitempty"`
	stdout, stderr bool

	Host        string     `json:"Host,omitempty"`
	User        string     `json:"User,omitempty"`
	Permissions Permission `json:"Permissions,omitempty"`
	Group       []string   `json:"Group,omitempty"`
	Admin       []string   `json:"Admin,omitempty"`
	//Concurrent bool
	MsgC chan *Signal `json:"-"`
	Ctl  chan *Ctl    `json:"-"`

	// status variables for API
	ServerName         string       `json:"-"`
	ServerKey          string       `json:"-"`
	Pid                int          `json:"Pid,omitempty"`
	Unscheduled        bool         `json:"Unscheduled,omitempty"`
	Restarting         bool         `json:"Restarting,omitempty"`
	IsRunning          bool         `json:"IsRunning,omitempty"`
	Hold               bool         `json:"Hold,omitempty"`
	Failed             bool         `json:"Failed,omitempty"`
	ExitCode           int          `json:"ExitCode,omitempty"`
	JobState           JState       `json:"JobState,omitempty"`
	JobStateString     string       `json:"JobStateString,omitempty"`
	PrevJobState       JState       `json:"PrevJobState,omitempty"`
	PrevJobStateString string       `json:"PrevJobStateString,omitempty"`
	RetryAttempt       int          `json:"RetryAttempt,omitempty"`
	PrevStart          string       `json:"PrevStart,omitempty"`
	PrevStop           string       `json:"PrevStop,omitempty"`
	Started            string       `json:"Started,omitempty"`
	Elapsed            string       `json:"Elapsed,omitempty"`
	ElapsedUNIX        int64        `json:"ElapsedUNIX,omitempty"`
	NextStart          string       `json:"NextStart,omitempty"`
	Updating           bool         `json:"Updating,omitempty"`
	MaxHistory         int          `json:"MaxHistory,omitempty"`
	History            []JobHistory `json:"History,omitempty"`
	//FullHistory []JobHistory `json:"-"`
	StartedUNIX   int64 `json:"StartedUNIX,omitempty"`
	PrevStopUNIX   int64 `json:"PrevStopUNIX,omitempty"`
	NextStartUNIX int64 `json:"NextStartUNIX,omitempty"`
	StdoutFile    []string
	StderrFile    []string

	// private members
	lock           sync.Mutex       `json:"-"`
	runlock        sync.Mutex       `json:"-"`
	status         chan int         `json:"-"`
	signal         chan int         `json:"-"`
	pid            int              `json:"-"`
	proc           *os.Process      `json:"-"`
	procState      *os.ProcessState `json:"-"`
	_location      *time.Location   `json:"-"`
	cronStart      Cron             `json:"-"`
	cronStartArray []Cron           `json:"-"`
	cronEnd        Cron             `json:"-"`
	cronEndArray   []Cron           `json:"-"`
	cronRestart    Cron             `json:"-"`
	startRule      StartRule        `json:"-"`
	//lastRun time.Time `json:"-"`
	prevStart time.Time       `json:"-"`
	prevStop  time.Time       `json:"-"`
	elapsed   time.Duration   `json:"-"`
	modified  int64           `json:"-"`
	updates   chan *JobUpdate `json:"-"`
	jobstatus chan *JobStatus `json:"-"`
	state     chan *depEvt    `json:"-"`
	Version   string          `json:"Version"`
	src       string
	authKey   string
	apiKey    string
	jve       JobValidationExceptions
	t         *time.Timer `json:"-"`
	te        *time.Timer `json:"-"`
	th        *time.Timer `json:"-"` // hold reset timer
}

type ExitStateMap map[int]JState
type ExitState []string

type JobLog struct {
	PrevStop time.Time
	LogFiles []string
}
type LogStats struct {
	Size  int64
	Lines int64
}

// JobLogging allows for specifying additional files for standard output and
// error logging, controls whether files are appended to across runs, and allows
// for log rotation schedules to be specified in time.Duration format
type JobLogging struct {
	StdoutFile string `json:"StdoutFile,omitempty" xml:"StdoutFile,omitempty"`
	StderrFile string `json:"StderrFile,omitempty" xml:"StderrFile,omitempty"`
	Append     bool   `json:"Append,omitempty" xml:"Append,omitempty"`
	// Remove files after duration specified by Purge using a timer
	// or FIFO queue with keeping history less than or equal to Max
	Purge string `json:"Purge,omitempty" xml:"Purge,omitempty"`
	Max   int    `json:"Max,omitempty" xml:"Max,omitempty"`
	//Perm os.FileMode
	stdoutFile  string
	stderrFile  string
	Logs        []JobLog `json:"Logs,omitempty" xml:"Logs,omitempty"`
	StdoutStats LogStats `json:"Stats,omitempty" xml:"State,omitempty"`
	StderrStats LogStats `json:"Stats,omitempty" xml:"State,omitempty"`
	purge       time.Duration
	l           *time.Timer
}

// JobStats tracks execution statistics
type JobStats struct {
	Elapsed                                                 int64
	Triggers, Manual, Stopped, Success, Failed, RetryFailed int
	Memory                                                  int64
	CPU                                                     float64
}
type JobHistory struct {
	RunUUID        string
	ExitCode       int
	CmdEval        string
	JobStateString string
	JobStateAbb    string
	RetryAttempt   int
	Start          string
	StartUNIX      int64
	Stop           string
	Elapsed        string
	Stdout         string
	Stderr         string
	Unscheduled    bool
	Reason         Reason
}

func (job *Job) addHistory() {
	var kabb string
	switch job.JobState {
	case JSuccess:
		kabb = "S"
	case JFailed:
		kabb = "F"
	case JRetryFailed:
		kabb = "R"
	case JEnd:
		kabb = "E"
	case JStopped:
		kabb = "s"
	case JWarning:
		kabb = "W"
	case JHold:
		kabb = "H"
	}
	jh := JobHistory{
		RunUUID:        job.RunUUID.String(),
		ExitCode:       job.ExitCode,
		JobStateString: job.JobStateString,
		JobStateAbb:    kabb,
		RetryAttempt:   job.RetryAttempt,
		Start:          job.Started,
		StartUNIX:      job.StartedUNIX,
		Stop:           job.PrevStop,
		Elapsed:        job.Elapsed,
		Stdout:         job.Logging.stdoutFile,
		Stderr:         job.Logging.stderrFile,
		CmdEval:        job.CmdEval,
		Unscheduled:    job.Unscheduled,
		Reason:         job.Reason,
		//CronStart:job.CronStart,
		//CronEnd:job.CronEnd,
		//CronRestart:job.CronRestart,
	}
	//job.FullHistory = append([]JobHistory{jh}, job.FullHistory...)
	job.History = append([]JobHistory{jh}, job.History...)
	//job.History = append(job.History, jh)
	if len(job.History) == job.MaxHistory+1 {
		//job.History = job.History[1:]
		job.History = job.History[:len(job.History)-1]
	}
}
func (h JobHistory) isNull() bool {
	return h.RunUUID == "00000000-0000-0000-0000-000000000000"
}
/* TODO: getHistory */
func (job *Job) getLogs(runid string) (stdout string, stderr string) {
	ServerLogger.Printf("[ getLogging for %s ] %s", runid, job.JobUUID)
	if runid == job.RunUUID.String() {
		stdout = job.Logging.stdoutFile
		stderr = job.Logging.stderrFile
	} else {
		for _, h := range job.History {
			if h.RunUUID == runid {
				stdout = h.Stdout
				stderr = h.Stderr
				break
			}
		}
	}
	return
}

type jobMap map[string]*Job
type jobMapStatic map[string]Job             // used for serialization to client
type jobStatusMapStatic map[string]JobStatus // used for serialization to client

type ServerJobs struct {
	Jobs        jobMap
	JobOrder    []string
	JobUUID     []string
	JobNameUUID map[string]uuid.UUID
	Groups      map[string][]uuid.UUID
	GroupOrder  []string
	lock        sync.Mutex
}

func (sjobs *ServerJobs) Lock() {
	sjobs.lock.Lock()
}
func (sjobs *ServerJobs) Unlock() {
	sjobs.lock.Unlock()
}

type KMeta struct {
	Uuid         string
	Instantiated time.Time
	HOME         string
}

func (job *Job) Validate() {

}
func (job *Job) SaveSnapshot(compress bool) {
	jobfile := fmt.Sprintf("%s/.%s.rj", job.JobMeta.HOME, job.JobUUID)
	path := filepath.Dir(jobfile)
	err := os.MkdirAll(path, os.FileMode(0700))
	if err != nil {
		ServerLogger.Fatal("error saving job", err.Error())
	}
	ServerLogger.Printf("Saving job to %s", jobfile)

	var buf bytes.Buffer
	if compress {
		job.SerializeGZ(&buf)
	} else {
		job.Serialize(&buf)
	}
	ioutil.WriteFile(jobfile, buf.Bytes(), os.FileMode(0600))
}
func (job *Job) SerializeGZ(buf *bytes.Buffer) {
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(job)
	if err != nil {
		ServerLogger.Fatal("unable to serialize job ", job.JobUUID, err.Error())
	}
	var gzb bytes.Buffer
	gz := gzip.NewWriter(&gzb)
	gz.Write(buf.Bytes())
	gz.Close()
	*buf = gzb
}

func (job *Job) Serialize(buf *bytes.Buffer) {
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(job)
	if err != nil {
		ServerLogger.Fatal("unable to serialize job ", job.JobUUID, err.Error())
	}
}
func (job *Job) UnserializeGZ(buf *bytes.Buffer) {
	gzb, err := gzip.NewReader(buf)
	if err != nil {
		ServerLogger.Fatal("unable to uncompress server jobs map ", err.Error())
	}
	defer gzb.Close()
	decoder := gob.NewDecoder(gzb)
	err = decoder.Decode(job)
	if err != nil {
		ServerLogger.Fatal("unable to unserialize server jobs map ", err.Error())
	}
	job.parseJob(true)
}
func (job *Job) Unserialize(buf *bytes.Buffer) {
	decoder := gob.NewDecoder(buf)
	err := decoder.Decode(job)
	if err != nil {
		ServerLogger.Fatal("unable to unserialize server jobs map ", err.Error())
	}
	job.parseJob(true)
}
func LoadJobState(jobfile string, compress bool) (job Job, err error) {
	f, err := os.Open(jobfile)
	b, _ := ioutil.ReadAll(f)
	buf := bytes.NewBuffer(b)
	f.Close()

	if compress {
		job.UnserializeGZ(buf)
	} else {
		job.Unserialize(buf)
	}
	return
}

func (sjobs *ServerJobs) Serialize(buf *bytes.Buffer) {
	sjobs.Lock()
	s := time.Now()
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(&sjobs)
	if err != nil {
		ServerLogger.Fatal("unable to serialize server jobs ", err.Error())
	}
	sjobs.Unlock()
	ServerLogger.Printf("Elapsed time to serialize server jobs: %s", time.Now().Sub(s))
}
func (sjobs *ServerJobs) Unserialize(buf *bytes.Buffer) {
	s := time.Now()
	decoder := gob.NewDecoder(buf)
	err := decoder.Decode(sjobs)
	if err != nil {
		ServerLogger.Fatal("Unable to unserialize server jobs ", err.Error())
	}
	ServerLogger.Printf("Elapsed time to unserialize server jobs: %s", time.Now().Sub(s))
}

func (jobm jobMap) Serialize(buf *bytes.Buffer) {
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(&jobm)
	if err != nil {
		ServerLogger.Fatal("Unable to serialize job map ", err.Error())
	}
}
func (jobm *jobMap) Unserialize(buf *bytes.Buffer) {
	decoder := gob.NewDecoder(buf)
	err := decoder.Decode(jobm)
	if err != nil {
		ServerLogger.Fatal("Unable to unserialize job map ", err.Error())
	}
}

func (job *Job) Lock() {
	job.lock.Lock()
}
func (job *Job) Unlock() {
	job.lock.Unlock()
}
func (job *JobSpec) isTemplate() bool {
	if job.Type == nil {
		return false
	}
	jobtype := strings.ToUpper(*job.Type)
	return jobtype == "TEMPLATE"
}
func (job *Job) isTemplate() bool {
	if job.Type == nil {
		return false
	}
	jobtype := strings.ToUpper(*job.Type)
	return jobtype == "TEMPLATE"
}
func (job *Job) isJOJ() bool {
	if job.Type == nil {
		return false
	}
	jobtype := strings.ToUpper(*job.Type)
	return jobtype == "JOJ"
}
func (job *Job) isController() bool {
	if job.Type == nil {
		return false
	}
	jobtype := strings.ToUpper(*job.Type)
	return jobtype == "CONTROLLER"
}

/* might be useful to have a per job mutex to sync access */
/* setter functions need to push job.JobUUID to ws: producer channel monitored by go routine */
type JobUpdate struct {
	Type     string
	Uuid     string
	Modified int64 // UNIX
	Tzoffset int
	Tzname   string
	Job      JobUpdateParams
}
type JobUpdateParams struct {
	Name               *string
	JobUUID            *uuid.UUID
	RunUUID            *uuid.UUID
	CronStart          **string
	CronEnd            **string
	CronRestart        **string
	Timezone           *string
	Calendar           *string
	Rollback           *bool
	RequireCal         *bool
	PrevStart          *string
	PrevStop           *string
	PrevStopUNIX        *int64
	Elapsed            *string
	Started            *string
	StartedUNIX        *int64
	NextStart          *string
	Hold               *bool
	JobState           *JState
	JobStateString     *string
	PrevJobState       *JState
	PrevJobStateString *string
	Updating           *bool
	Retry              *int
	RetryAttempt       *int
	Pid                *int
	Unscheduled        *bool
	Reason             *Reason
	Controls           *[]string
	History            *[]JobHistory
}
type JobStatus struct {
	Type     string
	Uuid     string
	Modified int64 // UNIX
	Tzoffset int
	Tzname   string
	Job      JobStatusParams
}
type JobStatusParams struct {
	Name           string
	JobUUID        uuid.UUID
	RunUUID        uuid.UUID
	PrevStop       string
	PrevStopUNIX   int64
	Elapsed        string
	ElapsedUNIX    int64
	Started        string
	StartedUNIX    int64
	NextStartUNIX  int64
	NextStart      string
	JobState       JState
	JobStateString string
	Updating       bool
}

func (job *Job) availableControls() []string {
	//job.lock.Lock()
	//defer job.lock.Unlock()

	controls := []string{"info"}

	if job.Type == JOJ {
		if job.JobState == JRunning {
			controls = []string{"stop", "info"}
		}
	} else {
		switch job.JobState {
		case JHold, JMissedWarning, JMissedError, JWarning, JWarning2, JWarning3, JDepWarning:
			if job.getHold() {
				controls = []string{"hold", "info"}
			} else {
				controls = []string{"hold", "start", "info"}
			}
		case JRetryWait, JDepRetry:
			controls = []string{"stop", "start"}
		case JRunning:
			controls = []string{"stop", "restart", "info"}
		case JStopped, JFailed, JRetryFailed, JDepFailed:
			controls = []string{"hold", "info"}
		case JReady, JSuccess, JManualSuccess, JEnd:
			controls = []string{"start", "hold", "info"}
		}
	}
	return controls
}

// Extracts subset of Job struct to provide back only
// relevent fields required for updates to client
func (job *Job) updateParams() *JobUpdateParams {

	controls := job.availableControls()

	params := JobUpdateParams{
		Name:               &job.Name,
		JobUUID:            &job.JobUUID,
		RunUUID:            &job.RunUUID,
		CronStart:          &job.CronStart,
		CronEnd:            &job.CronEnd,
		CronRestart:        &job.CronRestart,
		Timezone:           &job.Timezone,
		Calendar:           &job.Calendar,
		Rollback:           &job.Rollback,
		RequireCal:         &job.RequireCal,
		PrevStart:          &job.PrevStart,
		PrevStop:           &job.PrevStop,
		PrevStopUNIX:       &job.PrevStopUNIX,
		Elapsed:            &job.Elapsed,
		Started:            &job.Started,
		StartedUNIX:        &job.StartedUNIX,
		NextStart:          &job.NextStart,
		Hold:               &job.Hold,
		JobState:           &job.JobState,
		JobStateString:     &job.JobStateString,
		PrevJobState:       &job.PrevJobState,
		PrevJobStateString: &job.PrevJobStateString,
		Updating:           &job.Updating,
		Retry:              &job.Retry,
		RetryAttempt:       &job.RetryAttempt,
		Pid:                &job.Pid,
		Unscheduled:        &job.Unscheduled,
		Reason:             &job.Reason,
		Controls:           &controls,
		History:            &job.History,
	}

	return &params
}
func (job *Job) statusParams() *JobStatusParams {
	params := JobStatusParams{
		Name:           job.Name,
		JobUUID:        job.JobUUID,
		RunUUID:        job.RunUUID,
		PrevStop:       job.PrevStop,
		Elapsed:        job.Elapsed,
		ElapsedUNIX:    job.ElapsedSeconds(),
		Started:        job.Started,
		StartedUNIX:    job.StartedUNIX,
		NextStart:      job.NextStart,
		NextStartUNIX:  job.NextStartUNIX,
		JobState:       job.JobState,
		JobStateString: job.JobStateString,
		Updating:       job.Updating,
	}
	return &params
}
func (job *Job) sendUpdateClient() {
	tzname, tzoffset := time.Now().Zone()
	// websocket clients
	job.updates <- &JobUpdate{Uuid: job.JobUUID.String(), Modified: job.modified, Job: *job.updateParams(), Tzoffset: tzoffset, Tzname: tzname}
}
func (job *Job) sendUpdate() {

	//_, filename, line, _ := runtime.Caller(1)
	//ServerLogger.Printf("[sendUpdate] %s:%d", filename, line)

	tzname, tzoffset := time.Now().Zone()
	// websocket clients
	job.updates <- &JobUpdate{Uuid: job.JobUUID.String(), Modified: job.modified, Job: *job.updateParams(), Tzoffset: tzoffset, Tzname: tzname}
	// dependency clients
	job.state <- &depEvt{JobUUID: job.JobUUID, Name: job.Name, JobState: job.JobState}
	// alert API
	if job.HasAlerts() {
		go job.sendAlert()
	}
}
func (job *Job) setRetryAttempt(k int) {
	job.Lock()
	defer job.Unlock()
	job.RetryAttempt = k
	job.modified = time.Now().Unix()
}

func (job *Job) isValidJobStateChange(s JState) bool {
	var isValid bool
	switch s {
	case JRestart:
		if job.JobState == JRunning {
			isValid = true
		}
	case JRunning, JManual: // Start
		if job.JobState == JStopped ||
			job.JobState == JSuccess ||
			job.JobState == JManualSuccess ||
			job.JobState == JReady ||
			job.JobState == JWarning ||
			job.JobState == JWarning2 ||
			job.JobState == JWarning3 ||
			job.JobState == JRetrying ||
			job.JobState == JRetryWait ||
			job.JobState == JHold ||
			job.JobState == JContingent ||
			job.JobState == JEnd ||
			job.JobState == JFailed ||
			job.JobState == JRestart ||
			job.JobState == JRetryFailed ||
			job.JobState == JDepWarning ||
			job.JobState == JDepRetry ||
			job.JobState == JDepFailed ||
			job.JobState == JMissedError ||
			job.JobState == JMissedWarning {
			isValid = true
		}
	case JStopping:
		if job.JobState == JRunning || job.JobState == JRetrying {
			isValid = true
		}
	case JStopped, JSuccess, JEnd, JManualSuccess: // Stop, Success, End
		if job.JobState == JStopping || job.JobState == JRunning || job.JobState == JManual || job.JobState == JRetrying || job.JobState == JRetryWait || job.JobState == JRestart ||
			job.JobState == JReady || job.JobState == JSuccess || job.JobState == JEnd || job.JobState == JManualSuccess {
			isValid = true
		}
	case JFailed:
		if job.JobState == JReady || job.JobState == JRunning || job.JobState == JStopping || job.JobState == JRetryFailed || job.JobState == JRetryWait {
			// RetryWait is here to catch fork/exec failures with retries as they are not controlled well enough yet
			isValid = true
		}
	case JRetryFailed:
		if job.JobState == JRetrying || job.JobState == JRunning || job.JobState == JFailed || job.JobState == JStopping {
			isValid = true
		}
	case JRetrying, JRetryWait:
		if job.JobState == JFailed || job.JobState == JRetryFailed || job.JobState == JRetryWait {
			isValid = true
		}
	case JHold, JReady, JContingent:
		if job.JobState == JHold ||
			job.JobState == JReady ||
			job.JobState == JContingent ||
			job.JobState == JRetryFailed ||
			job.JobState == JFailed ||
			job.JobState == JSuccess ||
			job.JobState == JManualSuccess ||
			job.JobState == JEnd ||
			job.JobState == JStopped ||
			job.JobState == JStopping ||
			job.JobState == JWarning ||
			job.JobState == JWarning2 ||
			job.JobState == JWarning3 ||
			job.JobState == JMissedError ||
			job.JobState == JMissedWarning ||
			job.JobState == JDepWarning ||
			job.JobState == JDepFailed ||
			job.JobState == JUnknown {
			isValid = true
		}
	case JReset, JMissedError, JMissedWarning, JDepWarning, JDepRetry, JDepFailed:
		isValid = true
	case JUnknown:
		if job.JobState == JRunning || job.JobState == JRetrying {
			isValid = true
		}
	default:
		isValid = false
	}
	return isValid
}

func (job *Job) setJobState(s JState) error {
	job.Lock()
	defer job.Unlock()

	if s == job.JobState {
		return nil
	}
	if !job.isValidJobStateChange(s) { // FIXME: how to alert client that action is unavailable at present
		ServerLogger.Println(fmt.Sprintf("setJobState [%s] (%s => %s): %t", job.JobUUID, job.JobState, s, job.isValidJobStateChange(s)))
		return errors.New("unable to set job state")
	}

	job.PrevJobState = job.JobState
	job.PrevJobStateString = job.JobStateString

	job.JobState = s
	job.JobStateString = s.String()
	job.modified = time.Now().Unix()
	switch job.JobState {
	case JFailed:
		ServerLogger.Printf("checking for RetryReset: %s", job.RetryReset)
		if job.RetryReset != "" {
			// should retry allow for immediate execution like missed does?
			d, _ := time.ParseDuration(job.RetryReset)
			time.AfterFunc(d, func() { job.setHold(false); job.setJobState(JReady); job.sendUpdate() })
		}
		job.addHistory()
	case JMissedWarning:
		ServerLogger.Printf("checking for MissedReset: %s", job.MissedReset)
        //_, filename, line, _ := runtime.Caller(1)
        //ServerLogger.Printf("[error] %s:%d", filename, line)
		if job.MissedReset != "" {
			if !job.Hold && job.JobState != JRetrying {
				d, _ := time.ParseDuration(job.MissedReset)
				if d < 0 {
					// special case if negative duration, trigger job
					time.AfterFunc(d, func() { job.setHold(false); job.resetTimer(time.Second * 0) })
				} else {
					time.AfterFunc(d, func() { job.setHold(false) })
				}
			}
		} else {
			// in the case of MissedWarning jobs, never set them to be held
			//job.setHold(true)
		}
		job.addHistory()
	case JHold:
		ServerLogger.Printf("checking for HoldDuration: %s", job.HoldDuration)
		if job.HoldDuration != "" || job.holdDuration != "" {
			if job.holdDuration == "" {
				job.holdDuration = job.HoldDuration
			}
			d, _ := time.ParseDuration(job.holdDuration)
			ServerLogger.Printf("setting hold release on %s for %s", job.JobUUID, d)
			time.AfterFunc(d, func() {
				ServerLogger.Printf("releasing hold on %s", job.JobUUID)
				job.setHold(false)
				job.setJobState(JReady)
				job.sendUpdate()
			})
		} else {
			ServerLogger.Printf("no hold release on", job.JobUUID)
		}
		job.addHistory()
	case JSuccess, JManualSuccess, JEnd, JRetryFailed, JDepWarning, JDepFailed, JStopped:
		job.addHistory()
	}
	job.SaveSnapshot(true)
	ServerLogger.Printf("State Change JobUUID:%s %s => %s @ %d", job.JobUUID, job.PrevJobState, job.JobState, job.modified)
	return nil
}
func (job *Job) WaitForTrigger(stop <-chan bool) error {
	ServerLogger.Printf(InfoColor, fmt.Sprintf("Waiting for Trigger %s:%s (%s)", job.JobUUID, job.Name, job.JobState))
	ticker := time.NewTicker(time.Second * 15)
    defer ticker.Stop()
	lastTick := time.Now().Unix()
	for {
		select {
		case <-ticker.C:
			now := time.Now().Unix()
			if now-lastTick > 30 {
				// Skip setting the job as missed here
				// TODO: might want to add some logging here
				//job.setJobState(JMissedWarning)
				return nil
			}
			lastTick = now
		case <-job.t.C:
			if !job.Hold && job.JobState == JRetrying {
				job.setJobState(JRetrying)
			}
			return nil
		case <-stop:
			if job.IsRunning {
				ServerLogger.Printf("STOPPED %s:%s", job.JobUUID, job.Name)
				job.setHold(true)
				job.setJobState(JStopped)
			}
			return errors.New("job stopped")
		}
	}
	return nil
}
func (job *Job) resetTimer(d time.Duration) {
	job.Lock()
	defer job.Unlock()
	if !job.Unscheduled {
		job.Reason = Reason{}
	}
	job.t.Reset(d)
	job.modified = time.Now().Unix()
}
func (job *Job) getHold() bool {
	hold := job.Hold
	return hold
}
func (job *Job) setHold(h bool) {
	job.Hold = h
	job.modified = time.Now().Unix()
}
func (job *Job) setContingent(h bool) {
	job.Hold = h
	job.cronStart.Contingent = true
	job.modified = time.Now().Unix()
}
func (job *Job) setNextStart(next time.Time) {
	job.Lock()
	defer job.Unlock()

	loc, _ := time.LoadLocation(job.Timezone)

	//var ns_notz string
	//if job.cronStartArray[0].IsNull() || job.cronStartArray[0].isDependent() {
	if (job.isCronNull() || job.isCronDependent()) && !job.isEvery() {
		job.NextStart = "@manual"
		if job.isCronDependent() {
			job.NextStart = "@depends"
		}
		job.NextStartUNIX = math.MaxInt64
	} else {
		job.NextStart = next.In(loc).Format("2006-01-02 15:04:05")
		job.NextStartUNIX = next.Unix()
		//ns_notz = next.Format("2006-01-02 15:04:05")
	}
	job.modified = time.Now().Unix()
	UpdatesLogger.Printf("[%s @ %d] NextStart updated to %s", job.JobUUID, job.modified, job.NextStart)
}
func (job *Job) setPid(pid int) {
	job.Lock()
	defer job.Unlock()

	job.pid = pid
}

/* getter functions can operate on copy */
func (job *Job) getPid() int {
	job.Lock()
	defer job.Unlock()

	return job.pid
}
func (job *Job) getProc() *os.Process {
	job.Lock()
	defer job.Unlock()

	return job.proc
}
func (job *Job) getRetryWait(retry int) time.Duration {
	job.Lock()
	defer job.Unlock()

	waitTimes := strings.Split(job.RetryWait, ",")
	wait := waitTimes[len(waitTimes)-1]
	if retry < len(waitTimes) {
		wait = waitTimes[retry-1]
	}
	d, _ := time.ParseDuration(wait)
	return d
}
func (job *Job) getMaxDuration() time.Duration {
	job.Lock()
	defer job.Unlock()

	d, _ := time.ParseDuration(job.MaxDuration)
	return d
}
func (job *Job) onNextStart() onStart {
	var onstart onStart
	switch job.StartRule {
	case "Restart":
		onstart = RestartJob
	case "Start":
		onstart = StartJob
	case "NoStart":
		onstart = NoStartJob
	default:
		onstart = onStart(0)
	}
	return onstart
}
func (job *Job) getStartRule() StartRule {
	var startrule StartRule
	onstart := job.onNextStart()
	switch onstart {
	case RestartJob: // kill, start
		startrule = StartRule{Kill: true, Start: true, Concurrent: false}
	case StartJob: // continue, start
		startrule = StartRule{Kill: false, Start: true, Concurrent: true}
	case NoStartJob: // continue, no start
		startrule = StartRule{Kill: false, Start: false, Concurrent: false}
	}
	return startrule
}

type CronType int

const (
	CronStart CronType = iota + 1
	CronEnd
	CronRestart
)

func (ct CronType) String() string {
	names := [...]string{"CronStart", "CronEnd", "CronRestart"}
	return names[ct-1]
}

func (job *Job) currentHost() bool { // should check
	isTargetHost := false
	hostname, _ := os.Hostname()
	if job.Host == hostname {
		isTargetHost = true
		return isTargetHost
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		ServerLogger.Printf("error on currentHost() for %s: %s", job.JobUUID, err.Error())
	}
	// handle err
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		// handle err
		if err != nil {
			ServerLogger.Printf("error on currentHost() for %s: %s", job.JobUUID, err.Error())
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if job.Host == ip.String() {
				isTargetHost = true
			}
		}
	}
	return isTargetHost
}
