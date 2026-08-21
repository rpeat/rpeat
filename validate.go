package rpeat

import (
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"
)

type ConfigException int

const (
	Parse ConfigException = iota
	DuplicateJob
	Template
	Exec
	EnvVar
	CmdVar
	Cmd
	DateEnvVar
	Schedule
	Calendar
	Timezone
	Permissions
	Dependencies
	Alerts
	Logs
)

func (ce ConfigException) String() string {
	names := [...]string{"Parse", "DuplicateJob", "Template", "Exec", "EnvVar", "CmdVar", "Cmd", "DateEnvVar", "Schedule", "Calendar", "Timezone", "Permissions", "Dependencies", "Alerts", "Logs"}
	return names[ce]
}

// MarshalText implements the encoding.TextMarshaler interface.
func (ce ConfigException) MarshalText() ([]byte, error) {
	return []byte(ce.String()), nil
}

// trigger JConfigWarning
type ValidationWarning struct {
	JobName   string
	Errno     int
	Msg       string
	Exception ConfigException
	Warn      error
}

func (e ValidationWarning) Error() string {
	var s string
	switch e.Exception {
	case EnvVar:
		s = fmt.Sprintf("%s: Variables requested but not defined: %s", e.Exception, e.Msg)
	default:
		s = e.Msg
	}
	return s
}

// trigger JConfigError
type ValidationError struct {
	JobName   string
	Errno     int
	Msg       string
	Exception ConfigException
	Err       error
}

func (e ValidationError) Error() string {
	var s string
	switch e.Exception {
	case EnvVar:
		s = fmt.Sprintf("%s: Env variables requested but not defined: %s", e.Exception, e.Msg)
	case CmdVar:
		s = fmt.Sprintf("%s: Env or DateEnv variables used in Cmd but not defined: %s", e.Exception, e.Msg)
	case Cmd:
		s = fmt.Sprintf("%s: %s", e.Exception, e.Msg)
	case DateEnvVar:
		s = fmt.Sprintf("%s: DateEnv requested but not defined: %s", e.Exception, e.Msg)
	case Exec:
		s = fmt.Sprintf("%s: %s", e.Exception, e.Msg)
	default:
		s = e.Msg
	}
	return s
}

type JobValidationExceptions struct {
	Warnings []ValidationWarning
	Errors   []ValidationError
	Sep      string
	JState   JState
	checked  bool
}

func (e *JobValidationExceptions) IsOk() bool {
	return (len(e.Warnings) == 0 && len(e.Errors) == 0)
}
func (job *Job) AnyExceptions(ce ConfigException) bool {
	e := job.jve
	if e.IsOk() {
		return false
	}
	for _, vw := range e.Warnings {
		if vw.Exception == ce {
			return true
		}
	}
	for _, ve := range e.Errors {
		if ve.Exception == ce {
			return true
		}
	}
	return false
}

// TODO: make a function to allow fo r
func (e *JobValidationExceptions) HasError() bool {
	return len(e.Errors) > 0
}
func (e *JobValidationExceptions) HasWarning() bool {
	return len(e.Warnings) > 0
}
func (e *JobValidationExceptions) AddWarning(v ValidationWarning) {
	e.Warnings = append(e.Warnings, v)
}
func (e *JobValidationExceptions) AddError(v ValidationError) {
	e.Errors = append(e.Errors, v)
}
func (e JobValidationExceptions) Error() string {
	var msgs []string
	for _, warn := range e.Warnings {
		msgs = append(msgs, warn.Msg)
	}
	for _, err := range e.Errors {
		msgs = append(msgs, err.Msg)
	}
	return strings.Join(msgs, e.Sep)
}
func appendJobValidationExceptions(j0, j1 JobValidationExceptions) JobValidationExceptions {
	var jve JobValidationExceptions
	jve.Warnings = append(jve.Warnings, j0.Warnings...)
	jve.Warnings = append(jve.Warnings, j1.Warnings...)
	jve.Errors = append(jve.Errors, j0.Errors...)
	jve.Errors = append(jve.Errors, j1.Errors...)
	jve.Sep = j0.Sep
	if j0.JState == JConfigError || j1.JState == JConfigError {
		jve.JState = JConfigError
	} else {
		jve.JState = JConfigWarning
	}
	return jve
}
func (e *JobValidationExceptions) Validated() bool {
	return e.checked
}

/*
Specific Validators
*/
type CmdException int

const (
	CmdMissing CmdException = iota
	ExecutableNotFound
)

type CmdError struct {
	Cmd       string
	Exception CmdException
}

func (e CmdException) String() string {
	names := [...]string{"CmdMissing", "ExecutableNotFound"}
	return names[e]
}
func (e CmdError) Error() string {
	var s string
	switch e.Exception {
	case CmdMissing:
		s = fmt.Sprintf("%s: 'Cmd' is missing or template inherited from is not available", e.Exception)
	case ExecutableNotFound:
		s = fmt.Sprintf("%s: Executable not found", e.Exception)
	default:
		s = e.Exception.String()
	}
	return s
}
func (job *Job) ValidateCmd() {
	if job.Cmd == nil {
		ce := CmdError{Exception: CmdMissing, Cmd: ""}
		job.jve.AddError(ValidationError{Exception: Cmd, Msg: ce.Error(), JobName: job.Name})
	} else {
		evo, _ := expandAllVars(job, []string{}, job.Cmd, false, "")
		if evo.execErr != nil {
			//ce := CmdError{Exception: ExecutableNotFound, Cmd: evo.execErr.Error() }
			job.jve.AddError(ValidationError{Exception: Cmd, Msg: evo.execErr.Error(), JobName: job.Name})
		}
	}
	return
}

// EXCEPTION: Env and DateEnv
type DateEnvException int

const (
	NoMagicDateVars DateEnvException = iota
	DuplicateMagicVar
	MissingShiftValue
	MissingShiftUnit
	ErrorInShiftValue
	UnknownShiftUnit
	UnknownCalendar
	CommaError // CC,CC ,,,+
)

type DateEnvError struct {
	MagicDate string
	DateField string
	UnitField string
	Calendar  string
	Exception DateEnvException
}

func (e DateEnvException) String() string {
	names := [...]string{"NoMagicDateVars", "DuplicateMagicVar", "MissingShiftValue", "MissingShiftUnit", "ErrorInShiftValue", "UnknownShiftUnit", "UnknownCalendar", "CommaError"}
	return names[e]
}
func (e DateEnvError) Error() string {
	var s string
	switch e.Exception {
	case UnknownShiftUnit:
	case ErrorInShiftValue:
		s = e.UnitField
	case DuplicateMagicVar:
		s = e.MagicDate
	case UnknownCalendar:
		s = e.Calendar
	default:
		s = e.Exception.String()
	}
	return s
}
func validateDateEnv(job *Job) {
}

// EXCEPTION: Logging
func validateLogging(job *Job) {}

// EXCEPTION: Alerts
func validateAlerts(job *Job) {}

// EXCEPTION: Dependencies
func validateDependencies(job *Job) {}

// EXCEPTION: Calendar
type CalendarException int

const (
	MissingCalendar CalendarException = iota
	MissingCalendarDirs
	MultipleCalendarsFound
	CalendarNotFound
	CalendarDirNotFound
	CalendarReadError // permission | format
	CalendarBadDates
	CalendarOutOfRange
)

type CalendarError struct {
	Calendar     string
	CalendarDirs []string
	Exception    CalendarException

	fileErr error
}

func (e CalendarException) String() string {
	names := [...]string{"MissingCalendar", "MissingCalendarDirs", "MultipleCalendarsFound", "CalendarNotFound", "CalendarDirNotFound", "CaldendarReadError", "FileDatesMalformed", "CalendarOutOfRange"}
	return names[e]
}
func (e CalendarError) Error() string {
	var s string
	switch e.Exception {
	case MissingCalendar:
		s = fmt.Sprintf("%s: has no defined calendar. Use \"ALL\" to specify all days are OK and remove this message", e.Exception)
	case MissingCalendarDirs:
		s = fmt.Sprintf("%s: \"%s\" set, but no calendar directories provided", e.Exception, e.Calendar)
	//case CalendarNotFound: // FIXME these should just be a general format call for any file issue
	//    s = fmt.Sprintf("%s: \"%s\" not found in %s (%s)",e.Exception,e.Calendar,Stringify(e.CalendarDirs),e.fileErr)
	case CalendarNotFound, CalendarDirNotFound:
		s = fmt.Sprintf("%s: %s", e.Exception, e.fileErr)
	default:
		s = e.Exception.String()
	}
	return s
}
func (job *Job) ValidateCalendar() {
	// check
	//   calendar not specified
	//   calendar dir not specified with calendar
	//   multiple calendars found
	//   specified calendar not found
	//   calendar dir not found
	//   calendar file read error
	//   calendar file out of range
	if job.Calendar == "ALL" {
		return
	}
	if job.Calendar == "" {
		if len(job.CalendarDirs) > 0 {
			ce := CalendarError{Exception: MissingCalendar, Calendar: job.Calendar}
			job.jve.AddWarning(ValidationWarning{Exception: Calendar, Msg: ce.Error(), JobName: job.Name})
		}
		return
	}
	if job.Calendar != "" && len(job.CalendarDirs) == 0 {
		ce := CalendarError{Exception: MissingCalendarDirs, Calendar: job.Calendar}
		job.jve.AddError(ValidationError{Exception: Calendar, Msg: ce.Error(), JobName: job.Name})
		return
	}
	// has calendar and calendar dir
	_, err := ReadCalendar(job.Calendar, job.CalendarDirs)
	if err != nil {
		job.jve.AddError(ValidationError{Exception: Calendar, Msg: err.(CalendarError).Error(), JobName: job.Name})
		return
	}
}

// EXCEPTION: Timezone
type TimezoneException int

const (
	MissingTimezone TimezoneException = iota
	InvalidTimezone
	AbbreviatedTimezone
)

type TimezoneError struct {
	Timezone  string
	Exception TimezoneException
}

func (e TimezoneException) String() string {
	names := [...]string{"MissingTimezone", "InvalidTimezone", "AbbreviatedTimezone"}
	return names[e]
}
func (e TimezoneError) Error() string {
	var s string
	switch e.Exception {
	case MissingTimezone:
		s = fmt.Sprintf("%s: Timezone should be set to valid IANA zone. Defaults to UTC", e.Exception)
	case InvalidTimezone:
		s = fmt.Sprintf("%s: %s is not a valid IANA timezone", e.Exception, e.Timezone)
	case AbbreviatedTimezone:
		s = fmt.Sprintf("%s: %s may be ambiguous and result in inconsistent timezones", e.Exception, e.Timezone)
	default:
		s = e.Exception.String()
	}
	return s
}
func (job *Job) ValidateTimezone() {
	tz := job.Timezone
	if tz == "" {
		tze := TimezoneError{Exception: MissingTimezone, Timezone: job.Timezone}
		job.jve.AddWarning(ValidationWarning{Exception: Timezone, Msg: tze.Error(), JobName: job.Name})
	} else {
		_, tzerr := time.LoadLocation(tz)
		if tzerr == nil {
			if len(tz) == 3 && tz != "GMT" && tz != "UTC" {
				tze := TimezoneError{Exception: AbbreviatedTimezone, Timezone: job.Timezone}
				job.jve.AddWarning(ValidationWarning{Exception: Timezone, Msg: tze.Error(), JobName: job.Name})
			}
		} else {
			tze := TimezoneError{Exception: InvalidTimezone, Timezone: job.Timezone}
			job.jve.AddError(ValidationError{Exception: Timezone, Msg: tze.Error(), JobName: job.Name})
		}
	}
}

// EXCEPTIONS: Permissions
type PermissionException int

const (
	UnknownUser PermissionException = iota
	UnknownAction
	NoPermissionsGranted
	NoUserSet
	NoAuthUserSet // this may be OK, as long as they are legit users or no users
	NoRestrictions
)

func (pe PermissionException) String() string {
	names := [...]string{"UnknownUser", "UnknownAction", "NoPermissionsGranted", "NoUserSet", "NoAuthUserSet", "NoRestrictions"}
	return names[pe]
}

type PermissionError struct {
	Exception PermissionException
	User      string
	Action    string
	isWarning bool
}

func (e PermissionError) Error() string {
	var s string
	switch e.Exception {
	case UnknownUser:
		s = fmt.Sprintf("%s: user '%s' not found in auth file", e.Exception, e.User)
	case UnknownAction:
		s = fmt.Sprintf("%s: '%s' is not a valid job action", e.Exception, e.Action)
	case NoPermissionsGranted:
		s = fmt.Sprintf("%s: jobs only accessible to user:'%s'", e.Exception, e.User)
	case NoUserSet:
		s = fmt.Sprintf("%s: jobs without a user may not be visible.", e.Exception)
	case NoAuthUserSet:
		s = fmt.Sprintf("%s: single user job.", e.Exception)
	case NoRestrictions:
		s = fmt.Sprintf("%s: unrestricted users (*) granted for '%s'", e.Exception, e.Action)
	}
	return s
}
func (job *Job) ValidatePermissions() PermissionError {
	user := job.User
	perms := job.Permissions
	//admin := job.Admin   TODO
	if user == "" {
		// NoUserSet
		pe := PermissionError{Exception: NoUserSet, isWarning: true}
		job.jve.AddError(ValidationError{Exception: Permissions, Msg: pe.Error(), JobName: job.Name})
	} else {
		// UnknownUser
	}
	// AuthUsers
	if len(perms) == 0 {
		// NoPermissionsGranted
		pe := PermissionError{Exception: NoPermissionsGranted, User: user}
		job.jve.AddWarning(ValidationWarning{Exception: Permissions, Msg: pe.Error(), JobName: job.Name})
	} else {
		// TODO: no returned actions being set yet
		for action, users := range perms {
			// UnknownAction
			//fmt.Printf("check action %s is valid (TODO)", action)
			if stringInSlice("*", users) {
				// NoRestrictions (Warning)
				pe := PermissionError{Exception: NoRestrictions, Action: action}
				job.jve.AddError(ValidationError{Exception: Permissions, Msg: pe.Error(), JobName: job.Name})
			}
			/* TODO: check that user is defined/available
			   for _, u := range users {
			       fmt.Printf("check user %s is known (TODO)\n", u)
			       // UnknownUser
			   }
			*/
		}
	}
	return PermissionError{}
}

type DependencyException int

const (
	MissingDependency DependencyException = iota
	CircularDependency
	DuplicateDependency
	InvalidDependencyTriggerState
	InvalidDependencyAction
	InvalidDependencyCondition
	InvalidDependencyDelay
	MissingDependencyDelay
	DependsWithoutDependency
)

func (de DependencyException) String() string {
	names := [...]string{"MissingDependency", "CircularDependency", "DuplicateDependency", "InvalidDependencyTriggerState", "InvalidDependencyAction", "InvalidDependencyCondition", "InvalidDependencyDelay", "MissingDependencyDelay", "DependsWithoutDependency"}
	return names[de]
}

type DependencyError struct {
	Exception DependencyException
	Name      string
	Value     string
	isWarning bool
}

func (e DependencyError) Error() string {
	var s string
	switch e.Exception {
	case CircularDependency:
		s = fmt.Sprintf("%s: \"%s\" cannot be a dependency of itself", e.Exception, e.Name)
	case MissingDependency:
		s = fmt.Sprintf("%s: Specified dependency \"%s\" not found. Verify name or JobUUID and ensure dependency job is enabled", e.Exception, e.Name)
	case InvalidDependencyTriggerState:
		s = fmt.Sprintf("%s: \"%s=%s\" unrecognized state [start,stop,restart,running,failed,success,ready]", e.Exception, e.Name, e.Value)
	case InvalidDependencyAction: // is fail legal?
		s = fmt.Sprintf("%s: unrecognized action \"%s\" [start,stop,restart,hold]", e.Exception, e.Value)
	case InvalidDependencyCondition:
		s = fmt.Sprintf("%s: unrecognized condition \"%s\"", e.Exception, e.Value)
	case InvalidDependencyDelay, MissingDependencyDelay:
		s = fmt.Sprintf("%s: %s", e.Exception, e.Value)
	case DependsWithoutDependency:
		s = fmt.Sprintf("%s: @depends \"%s\" without Dependency defined.", e.Exception, e.Name)
	default:
		s = e.Exception.String()
	}
	return s
}
func (job *Job) ValidateDependency(jobs map[string]*Job) {
	if job.Dependency == nil && job.isCronDependent() && !job.isTemplate() {
		de := DependencyError{Exception: DependsWithoutDependency, Name: job.Name}
		job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
		return
	}
	if job.Dependency != nil {
		for _, dep := range job.Dependency {
			for trigger, state := range dep.Dependencies {
				if j, ok := jobs[trigger]; ok {
					if j.JobUUID == job.JobUUID {
						de := DependencyError{Exception: CircularDependency, Name: trigger}
						job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
						return
					}
				} else {
					de := DependencyError{Exception: MissingDependency, Name: trigger}
					job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
				}
				for _, s := range strings.Split(state, "|") {
					if !stringInSlice(s, []string{"start", "stop", "restart", "running", "success", "end", "failed", "ready"}) {
						de := DependencyError{Exception: InvalidDependencyTriggerState, Name: trigger, Value: s}
						job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
					}
				}
			}
			if !stringInSlice(dep.Action, []string{"start", "stop", "hold", "restart"}) {
				de := DependencyError{Exception: InvalidDependencyAction, Value: dep.Action}
				job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
			}
			if !stringInSlice(dep.Condition, []string{"all", "any"}) {
				de := DependencyError{Exception: InvalidDependencyCondition, Value: dep.Condition}
				job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
			}
			if _, err := time.ParseDuration(dep.Delay); err != nil {
				if dep.Delay == "" {
					de := DependencyError{Exception: MissingDependencyDelay, Value: "no delay specified - defaulting to 0s"}
					job.jve.AddWarning(ValidationWarning{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
				} else {
					de := DependencyError{Exception: InvalidDependencyDelay, Value: err.Error()}
					job.jve.AddError(ValidationError{JobName: job.Name, Msg: de.Error(), Exception: Dependencies})
				}
			}
		}
	}
}

type AlertException int

const (
	NoAlerts             AlertException = iota // Warning
	InvalidAlertType                           // Error
	MissingAlertEndpoint                       // Error
	MissingAlertType                           // Error
)

func (ae AlertException) String() string {
	names := [...]string{"NoAlerts", "InvalidAlertType", "MissingAlertEndpoint", "MissingAlertType"}
	return names[ae]
}

type AlertError struct {
	Exception AlertException
	Action    string
	Type      string
	Endpoint  string
	isWarning bool
}

func (e AlertError) Error() string {
	var s string
	switch e.Exception {
	case NoAlerts:
		s = fmt.Sprintf("%s: job not set to alert on any condition", e.Exception)
	case InvalidAlertType:
		s = fmt.Sprintf("%s: %s not recognized as Alert Type", e.Exception, e.Type)
	case MissingAlertEndpoint:
		s = fmt.Sprintf("%s: no AlertActions.Endpoint or Alert.Endpoint found", e.Exception)
	case MissingAlertType:
		s = fmt.Sprintf("%s: no AlertActions.Type or Alert.Type found", e.Exception)
	}
	return s
}
func (job *Job) ValidateAlerts() {
	// FIXME: needs to handle Alert level override
	if !job.HasAlerts() {
		ae := AlertError{Exception: NoAlerts, isWarning: true}
		job.jve.AddWarning(ValidationWarning{JobName: job.Name, Msg: ae.Error(), Exception: Alerts})
	} else {
		alertType := job.AlertActions.Type
		if alertType != nil {
			if !stringInSlice(*alertType, []string{"rpeat", "smtp", "queue", "file", "db", "custom", "gmail", "office365"}) {
				var ae AlertError
				if *alertType == "" {
					ae = AlertError{Exception: MissingAlertType, Type: *alertType}
				} else {
					ae = AlertError{Exception: InvalidAlertType, Type: *alertType}
				}
				job.jve.AddWarning(ValidationWarning{JobName: job.Name, Msg: ae.Error(), Exception: Alerts})
			}
		}
		alertEndpoint := job.AlertActions.Endpoint
		if alertEndpoint != nil && *alertEndpoint == "" {
			if *alertType != "gmail" && *alertType != "office365" {
				ae := AlertError{Exception: MissingAlertEndpoint}
				job.jve.AddError(ValidationError{JobName: job.Name, Msg: ae.Error(), Exception: Alerts})
			}
		}
	}
}

// EXCEPTION: Logs
// EXCEPTION: Path
type ConfigValidationExceptions struct {
}

func ValidateConfig(configFile string) ConfigValidationExceptions {
	cve := ConfigValidationExceptions{}
	return cve
}

// Configuration exceptions are special errors (ValidationError) and
// warnings (ValidationWarning) captured during the configuration
// validation step accessible via rpeat-util tool.
// Exceptions are checked in 14 categories (ConfigExceptions),
// with each category in turn composed of various exceptions
// specific to each category.
//
// A Parse exception occurs when a configuration or jobs file(s) fails to parse,
// likely due to a syntax error, or when duplicate jobs are found.
//
// A Template error is either a parse error in a template or
// a warning if duplicate templates are defined with the same name
//
// Exec exceptions indicate the system command (used in Cmd) was not found or
// is not executable.
//
// CmdVar exception indicates an evironment variable used in Cmd is not
// defined or misspecified.
//
// Cmd exception is thrown when Cmd field is not defined or inherited.  This
// is not an error within a template, though if jobs inheriting from template
// do not define a Cmd it will become an error.
//
// EnvVar exceptions indicate an environment variable was used within
// the JobSpec.EnvVar parameter(s), but is not defined within the configuration
// or in the environment. Note that this may differ if environment used to
// validate is not the environment when rpeat-server is run.
//
// # DateEnvVar TBA
//
// Schedule exceptions indicate issues with the cron syntax
// IncorrectNumberOfFields: cron spec must be either 5 (minutes) or 6 fields,
// MalformedAt: @at requires a valid time,
// MalformedEvery: @every requires a duration string - e.g. 1m or 1h,
// UnrecognizedAt: indicates a shorthand @ is not defined as stated
//
// # Calendar exceptions
//
// o  MissingCalendar
// o  MissingCalendarDirs
// o  MultipleCalendarsFound
// o  CalendarNotFound
// o  CalendarDirNotFound
// o  CalendarReadError  // permission | format
//
// # Timezone exceptions
//
// o  MissingTimezone
// o  InvalidTimezone
// o  AbbreviatedTimezone
type JobValidationExceptionsMap map[uuid.UUID]JobValidationExceptions

func ValidateJobs(files []string, configFile, authFile string, verbose bool) (JobValidationExceptions, JobValidationExceptionsMap) {
	ServerLogger.Println("validating job files")
	var totaljobs, totaltemplates, totaldisabled int
	var nerr, nwarn int
	jve := JobValidationExceptions{}

	var serverkey, servername, apiKey string // FIXME: this needs to be extracted from LoadServerConfig(configFile, false)

	tmplOut := LoadTemplates(files)
	templates := tmplOut.templates
	//allJobNames := tmplOut.allJobNames

	job_templates := make(map[string]Job) // hash map to flag duplicate templates
	var allParseErrs []error
	var alljobs []*Job
	seenJobNames := make(map[string]bool)
	seenJobUUIDs := make(map[string]bool)

	var logging JobLogging
	for i, f := range files {
		if verbose {
			fmt.Println()
			fmt.Printf("validating (%d/%d): %s\n", i+1, len(files), f)
		}

		var ntemplates, ndisabled int
		ji := 0
		jobs, specs, _, _, err := LoadJobSpec(f, 1, templates, ServerConfig{}, servername, serverkey, apiKey, logging)
		if err != nil && verbose {
			fmt.Printf("  Job loading error encountered in %s\n", f)
		} else {
			for si, _ := range specs {
				if specs[si].isTemplate() {
					ntemplates++
					job := Job{}

					jobtmpl := Job{}
					tmpl := templates[specs[si].Name]
					jobtmpl.copyJobSpec(&tmpl)

					job.copyJobSpec(&specs[si])

					if prev, ok := job_templates[specs[si].Name]; ok {
						msg := fmt.Sprintf("Template '%s' (#%d in %s) skipped - previously defined in (%s)", specs[si].Name, si+1, specs[si].src, prev.src)
						jobtmpl.jve.AddWarning(ValidationWarning{JobName: specs[si].Name, Exception: Template, Msg: msg})
						if verbose {
							fmt.Printf(WarningColor, fmt.Sprintf("  %s\n", msg))
						}
					} else {
						job_templates[job.Name] = job.copyJob()
						err := jobtmpl.parseJob(false)
						if err != nil {
							if verbose {
								fmt.Printf("  Template parse error encountered in %s (%s)\n", jobs[i].Name, f)
							}
							jobtmpl.jve.AddError(ValidationError{JobName: jobs[i].Name, Msg: err.Error(), Exception: Parse})
						}
					}

					jobtmpl.ValidateTimezone()
					jobtmpl.ValidateCalendar()
					alljobs = append(alljobs, &jobtmpl)
					allParseErrs = append(allParseErrs, nil)
					jve = appendJobValidationExceptions(jve, jobtmpl.jve)

				} else {
					// non-template, regular jobs already processed for inheritance
					if specs[si].Disabled {
						ndisabled++
						continue
					}

					if _, ok := seenJobNames[jobs[ji].Name]; ok {
						msg := fmt.Sprintf("Duplicate job name '%s' found", jobs[ji].Name)
						jobs[ji].jve.AddError(ValidationError{Msg: msg, Exception: DuplicateJob})
					}
					if _, ok := seenJobUUIDs[jobs[ji].JobUUID.String()]; ok {
						msg := fmt.Sprintf("Duplicate job UUID '%s' found", jobs[ji].JobUUID.String())
						jobs[ji].jve.AddError(ValidationError{Msg: msg, Exception: DuplicateJob})
					}
					seenJobNames[jobs[ji].Name] = true
					seenJobUUIDs[jobs[ji].JobUUID.String()] = true

					parseErr := jobs[ji].parseJob(false)
					if parseErr != nil {
						if verbose {
							fmt.Printf(ErrorColor, fmt.Sprintf("  Job parse error encountered in %s (%s)\n", jobs[ji].Name, f))
						}
						exception := Parse
						_, ok := parseErr.(CronError)
						if ok {
							exception = Schedule
						}
						// FIXME: this should be set within parseJob call
						jobs[si].jve.AddError(ValidationError{JobName: jobs[ji].Name, Msg: parseErr.Error(), Exception: exception})
						jve.JState = JConfigError
					}
					jobs[ji].ValidateCmd()
					jobs[ji].ValidateTimezone()
					jobs[ji].ValidateCalendar()
					jobs[ji].ValidatePermissions()
					jobs[ji].ValidateAlerts()
					alljobs = append(alljobs, &jobs[ji])
					allParseErrs = append(allParseErrs, parseErr)
					ji++
				}
			}
			njobs := len(jobs)
			totaljobs = totaljobs + njobs
			totaltemplates = totaltemplates + ntemplates
			totaldisabled = totaldisabled + ndisabled
			if verbose {
				fmt.Printf("  %d total jobs found in %s\n", njobs-ntemplates, f)
				fmt.Printf("  %d of %d jobs are disabled in %s\n", ndisabled, njobs-ntemplates, f)
				fmt.Printf("  %d jobs checked %s\n", njobs-ntemplates-ndisabled, f)
				fmt.Printf("  %d templates checked in %s\n", ntemplates, f)
			}
		}
	}

	// convert alljobs slice into a map for validating any dependency
	jobmap := make(map[string]*Job)
	jobNameUUID := make(map[string]uuid.UUID)
	for _, job := range alljobs {
		jobmap[job.Name] = job
		jobmap[job.JobUUID.String()] = job
		jobNameUUID[job.Name] = job.JobUUID
		jobNameUUID[job.JobUUID.String()] = job.JobUUID
	}
	for _, job := range alljobs {
		job.ValidateDependency(jobmap)
	}

	if verbose {
		fmt.Println()
		fmt.Printf("Jobs:\n\n")
	}
	ji := 0
	for i, _ := range alljobs {
		if !alljobs[i].isTemplate() {

			// check Cron specs
			var cronErr CronError
			if allParseErrs[i] != nil {
				cronErr, _ = allParseErrs[i].(CronError)
			}

			// get CCYYMMDDhhmmss "asof" string for correct DateEnv
			var asofstart string
			if len(alljobs[i].CronStartArray) > 0 {
				if cronErr.Schedule != CronStart {
					_, next := NextCronStart(alljobs[i].cronStartArray)
					if !next.IsZero() {
						asofstart = next.In(alljobs[i]._location).Format("20060102150405")
					}
				}
			}
			evaluatedCmd(alljobs[i], false, asofstart)

			// check Env and DateEnv variables
			if verbose {
				ji++
				fmt.Println("  ---------------------------------------------------------------------------------------------------")
				fmt.Printf("  [%d of %d] \033[1m%s\033[0m [%s] (%s)\n", ji, totaljobs-totaltemplates-totaldisabled, alljobs[i].Name, alljobs[i].JobUUID, alljobs[i].src)
				//inherits := "N/A"
				inheritanceChain := ""
				group := "N/A"
				if alljobs[i].Inherits != nil {
					// TODO: inherits graph
					//inherits = (*alljobs[i].Inherits)
					inheritanceChain = StringifyWithSep(alljobs[i].InheritanceChain, " =>")
				}
				if len(alljobs[i].Group) > 0 {
					group = Stringify(alljobs[i].Group)
				}
				fmt.Printf("    \033[1;38;5;12mGroup:\033[0m [%s]\t\033[1;38;5;12mInherits:\033[0m [%s]\n", group, inheritanceChain)
				if alljobs[i].Cmd != nil {
					fmt.Printf("    \033[1;38;5;12mCmd:\033[0m\t%s\n", *alljobs[i].Cmd)
					fmt.Printf("    \033[1;38;5;12mCmdEval:\033[0m\t%s\n", alljobs[i].CmdEval)
				} else {
					fmt.Printf("    \033[1;38;5;12mCmd:\033[0m\t%s\n", "NOT DEFINED")
					fmt.Printf("    \033[1;38;5;12mCmdEval:\033[0m\t%s\n", "NOT DEFINED")
				}
				// TODO:
				// Variable overrides
				// Held
				// Retries
				// Alerts
				// Logging
			}

			if len(alljobs[i].CronStartArray) > 0 {
				if cronErr.Schedule == CronStart {
					if verbose {
						fmt.Printf(ErrorColor, "    Start:\t** FAILED ** (see below)\n")
					}
				} else {
					_, next := NextCronStart(alljobs[i].cronStartArray)
					if verbose {
						nextStart := next.In(alljobs[i]._location).Format(time.RFC1123)
						if next.IsZero() {
							nextStart = "TBD (Dependency Triggered)"
						}
						fmt.Printf("    \033[1;38;5;12mStart:\033[0m\t%s [ CronStart:  %s ]\n", nextStart, Stringify(alljobs[i].CronStartArray))
						if alljobs[i].Jitter != 0 {
							fmt.Printf("    \033[1;38;5;12mJitter:\033[0m\t%d\n", alljobs[i].Jitter)
						}
					}
				}
			}
			if len(alljobs[i].CronEndArray) > 0 {
				if cronErr.Schedule == CronEnd {
					if verbose {
						fmt.Printf(ErrorColor, "    End:\t** FAILED ** (see below)\n")
					}
				} else {
					_, next := NextCronStart(alljobs[i].cronEndArray)
					if verbose {
						nextStart := next.In(alljobs[i]._location).Format(time.RFC1123)
						if next.IsZero() {
							nextStart = "TBD"
						}
						fmt.Printf("    \033[1;38;5;12mNextEnd:\033[0m\t%s [ CronEnd:  %s ]\n", nextStart, Stringify(alljobs[i].CronEndArray))
					}
				}
			}
			if alljobs[i].CronRestart != nil {
				if cronErr.Schedule == CronRestart {
					if verbose {
						fmt.Printf(ErrorColor, "    Restart:\t** FAILED ** (see below)\n")
					}
				} else {
					_, next := NextCronStart([]Cron{alljobs[i].cronRestart})
					if verbose {
						nextStart := next.In(alljobs[i]._location).Format(time.RFC1123)
						if next.IsZero() {
							nextStart = "TBD"
						}
						fmt.Printf("    \033[1;38;5;12mRestart:\033[0m\t%s [ CronRestart:  %s ]\n", nextStart, *alljobs[i].CronRestart)
					}
				}
			}
			// TODO: add LocalEnv and LocalDateEnv var overrides
			if alljobs[i].Dependency != nil && verbose {
				if alljobs[i].AnyExceptions(Dependencies) {
					fmt.Printf("    \033[1;38;5;12mDependency Check:\033[0m %s\n", fmt.Sprintf(ErrorColor, "FAILED"))
					alljobs[i].getDependencyGraph(&ServerData{jobs: jobmap, jobNameUUID: jobNameUUID}).Print(true, 0)
				} else {
					fmt.Printf("    \033[1;38;5;12mDependency Check:\033[0m Done\n")
					alljobs[i].getDependencyGraph(&ServerData{jobs: jobmap, jobNameUUID: jobNameUUID}).Print(true, 0)
				}
			}
			if verbose {
				if len(alljobs[i].jve.Errors) > 0 {
					fmt.Printf(ErrorColor, "\n    Errors:\n")
					for _, e := range alljobs[i].jve.Errors {
						fmt.Printf(ErrorColor, fmt.Sprintf("      %s\n", e.Error()))
						nerr++
					}
				}
				if len(alljobs[i].jve.Warnings) > 0 {
					fmt.Printf(WarningColor, "\n    Warnings:\n")
					for _, e := range alljobs[i].jve.Warnings {
						fmt.Printf(WarningColor, fmt.Sprintf("      %s\n", e.Error()))
						nwarn++
					}
				}
				fmt.Println()
			}

			jve = appendJobValidationExceptions(jve, alljobs[i].jve)

		}
	}

	if verbose {
		fmt.Printf("Templates:\n\n")
	}
	t := 0
	if totaltemplates == 0 {
		if verbose {
			fmt.Println("  None")
		}
	} else {
		for i, _ := range alljobs {
			if alljobs[i].isTemplate() {
				evaluatedCmd(alljobs[i], false, "")
				if verbose {
					fmt.Println("  ---------------------------------------------------------------------------------------------------")
					fmt.Printf("  [%d of %d] Template:%s [%s] (%s)\n", t+1, totaltemplates, alljobs[i].Name, alljobs[i].JobUUID, alljobs[i].src)
					if len(alljobs[i].jve.Errors) > 0 {
						fmt.Printf(ErrorColor, "\n    Errors:\n")
						for _, e := range alljobs[i].jve.Errors {
							fmt.Printf(ErrorColor, fmt.Sprintf("    %s\n", e.Error()))
						}
					}
					if len(alljobs[i].jve.Warnings) > 0 {
						fmt.Printf(WarningColor, "\n    Warnings:\n")
						for _, e := range alljobs[i].jve.Warnings {
							fmt.Printf(WarningColor, fmt.Sprintf("    %s\n", e.Error()))
						}
					}
					fmt.Println()
					fmt.Println()
				}
				t++
			}
		}
	}

	if verbose {
		fmt.Println("====================================================================================================")
		fmt.Println()
		fmt.Printf("Total files: %d\n", len(files))
		fmt.Printf("Total jobs: %d (enabled: %d, disabled: %d)\n", totaljobs-totaltemplates, totaljobs-totaltemplates-totaldisabled, totaldisabled)
		fmt.Printf("Total templates: %d\n", totaltemplates)
		fmt.Printf(ErrorColor, fmt.Sprintf("Total errors: %d\n", len(jve.Errors)))
		fmt.Printf(WarningColor, fmt.Sprintf("Total warnings: %d\n", len(jve.Warnings)))
		fmt.Println()
	}

	alljve := make(map[uuid.UUID]JobValidationExceptions)
	for i := range alljobs {
		alljve[alljobs[i].JobUUID] = alljobs[i].jve
	}

	jve.checked = true
	return jve, alljve
}
