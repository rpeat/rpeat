package rpeat

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var _ = os.Exit

type CronFields int

const (
	Wday CronFields = 1 + iota
	Sec
	Min
	Hour
	Mday
	Mon
	Year
)

func (cf CronFields) String() string {
	names := [...]string{"weekday", "sec", "min", "hour", "mday", "mon", "year"}
	return names[cf-1]
}

type Cron struct {
	// internal representation of expanded string format
	Spec map[CronFields][]int

	// calendar and calendars path
	Calendar     string
	CalendarDirs []string // this should probably be a cache at the very least and more likely read from ENV like TZ

	// Timezone of cron
	Timezone string

	// has cron been successfully parse - FIXME: should be private
	Parsed bool

	// no setting - disabled cron
	Null bool

	// Set to true when job is dependent on another for start
	Dependent bool
	// Set to true when job is contingent on another for start
	Contingent bool

	// require failure for dates outside of calendar range
	RequireCal bool

	// should next date roll back to last valid calendar day
	Rollback bool

	// should date be rolled back to prior period
	EndOf bool

	// spec is the string form of the passed cron
	spec string

	//
	at     string
	every  time.Duration
	adj    string
	r      map[CronFields]bool
	jitter int

	// is Cron part of an array of crons
	array bool
}

// Cron Exceptions
type CronException int

const (
	IncorrectNumberOfFields CronException = iota
	MalformedAt
	MalformedEvery
	UnrecognizedAt
	ExpansionError
	CalendarSyntaxError
)

type CronError struct {
	Schedule  CronType
	Spec      string
	Msg       string
	Exception CronException

	//
	CalException CalendarException
	field        int
}

func (e CronException) String() string {
	names := [...]string{"MissingFields", "Malformed @at", "Malformed @every", "Unrecongnized @", "ExpansionError", "CalendarSyntaxError"}
	return names[e]
}

func (e CronError) Error() string {
	var s string
	switch e.Exception {
	case IncorrectNumberOfFields:
		s = fmt.Sprintf("%s %s [ %s ] requires 5 or 6, %d fields found", e.Exception, e.Schedule, e.Spec, e.field)
	default:
		s = fmt.Sprintf("%s in %s", e.Spec, e.Exception)
	}
	return s
}

// ParseCron takes a cron spec plus optionally specified
// and returns a Cron struct and error if any problem occurs
func ParseCron(spec string, timezone string, calendar string, calendarDirs []string, rollback bool, required bool, jitter int) (Cron, error) {

	var cron Cron

	var fields []string

	if len(spec) == 0 {
		cron = atManual(timezone)
		return cron, nil
	}

	if strings.HasPrefix(spec, "@") {
		if strings.HasPrefix(spec, "@at ") {
			fields = strings.Fields(spec)
			if len(fields) != 2 {
				return cron, CronError{Spec: spec, Msg: "malformed @at - requires a valid timestamp", Exception: MalformedAt}
			}
			cron = atAt(timezone, fields[1])
		} else if strings.HasPrefix(spec, "@every ") {
			fields = strings.Fields(spec)
			if len(fields) != 2 {
				return cron, CronError{Spec: spec, Msg: "malformed @every - requires a valid duration", Exception: MalformedEvery}
			}
			cron = atEvery(timezone, fields[1])
		} else {
			fields = strings.Fields(spec)
			switch fields[0] {
			case "@minutely", "@always":
				cron = atMinutely(timezone)
			case "@hourly":
				cron = atHourly(timezone)
			case "@daily":
				cron = atDaily(timezone)
			case "@midnight":
				cron = atMidnight(timezone)
			case "@weekly":
				cron = atWeekly(timezone, fields)
			case "@eow":
				cron = atWeekly(timezone, fields)
				rollback = true
				cron.EndOf = true
			case "@monthly":
				cron = atMonthly(timezone, fields)
			case "@eom":
				cron = atMonthly(timezone, fields)
				rollback = true
				cron.EndOf = true
			case "@quarterly":
				cron = atQuarterly(timezone, fields)
			case "@eoq":
				cron = atQuarterly(timezone, fields)
				rollback = true
				cron.EndOf = true
			case "@yearly":
				cron = atYearly(timezone, fields)
			case "@eoy":
				cron = atYearly(timezone, fields)
				rollback = true
				cron.EndOf = true
			case "@annual":
				cron = atAnnual(timezone, fields) // FIXME: why not just atYearly?
			case "@depends":
				cron = DependentCron()
			case "@manual", "@never":
				cron = atManual(timezone)
			default:
				return cron, CronError{Spec: spec, Msg: "unrecognized @ spec", Exception: UnrecognizedAt}
			}
			if len(fields) > 1 {
				ServerLogger.Printf("adding adjustment %s", fields[1])
				cron.adj = fields[1]
			}
		}
		cron.Calendar = calendar
		cron.CalendarDirs = calendarDirs
	} else {
		fields = strings.Fields(spec)
		nfields := len(fields)
		if nfields != 5 && nfields != 6 {
			return cron, CronError{Spec: spec, Msg: "cron format requires 5 fields exactly (min, hour, month day, month, weekday) or 6 if seconds are specified (sec, min, hour, month day, month, weekday)", Exception: IncorrectNumberOfFields, field: nfields}
		}
		cron = NewCron(timezone)
		cron.Calendar = calendar
		cron.CalendarDirs = calendarDirs

		secOffset := 0
		if nfields == 6 {
			if fields[0] != "*" {
				cron.Spec[Sec], cron.r[Sec] = expandField(fields[0], 0, 59)
			}
			secOffset = 1
		} else {
			cron.Spec[Sec] = []int{0}
		}
		if fields[0+secOffset] != "*" {
			cron.Spec[Min], cron.r[Min] = expandField(fields[0+secOffset], 0, 59)
		}
		if fields[1+secOffset] != "*" {
			cron.Spec[Hour], cron.r[Hour] = expandField(fields[1+secOffset], 0, 23)
		}
		if fields[3+secOffset] != "*" {
			cron.Spec[Mon], cron.r[Mon] = expandField(fields[3+secOffset], 1, 12)
		}
		// handle Mday and Wday parse:
		if fields[2+secOffset] != "*" && fields[4+secOffset] == "*" {
			//if mday is number and wday == * : set wday = -1
			cron.Spec[Mday], cron.r[Mday] = expandField(fields[2+secOffset], 1, 31)
			cron.Spec[Wday] = []int{-1}
		} else if fields[2+secOffset] == "*" && fields[4+secOffset] != "*" {
			//if mday == * and wday == number : set mday = -1
			cron.Spec[Wday], cron.r[Wday] = expandField(fields[4+secOffset], 0, 6)
			cron.Spec[Mday] = []int{-1}
		} else {
			//if mday == * and wday == *  or mday is defined and wday is defined: leave alone
			cron.Spec[Mday], cron.r[Mday] = expandField(fields[2+secOffset], 1, 31)
			cron.Spec[Wday], cron.r[Wday] = expandField(fields[4+secOffset], 0, 6)
		}
	}

	cron.jitter = jitter
	cron.spec = spec
	cron.Rollback = rollback
	cron.RequireCal = required
	return cron, nil
}

func expandField(field string, start, end int) ([]int, bool) {
	var e []int
	var d int
	var rr []int
	var err error
	var r bool

	// TODO: Add LANGUAGE support options
	field = strings.ToUpper(field)
	if strings.HasPrefix(field, "R") {
		r = true
		field = strings.TrimPrefix(field, "R")
	}
	if field == "M-F" || field == "MF" || field == "WEEKDAYS" || field == "WEEKDAY" {
		field = "1-5"
	}
	if field == "WEEKEND" || field == "WEEKENDS" {
		field = "6,0"
	}
	if field == "EVERYDAY" {
		field = "*"
	}
	if strings.ContainsAny(field, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		field = strings.Replace(field, "SUN", "0", -1)
		field = strings.Replace(field, "MON", "1", -1)
		field = strings.Replace(field, "TUE", "2", -1)
		field = strings.Replace(field, "WED", "3", -1)
		field = strings.Replace(field, "THU", "4", -1)
		field = strings.Replace(field, "FRI", "5", -1)
		field = strings.Replace(field, "SAT", "6", -1)
		field = strings.Replace(field, "SUNDAY", "0", -1)
		field = strings.Replace(field, "MONDAY", "1", -1)
		field = strings.Replace(field, "TUESDAY", "2", -1)
		field = strings.Replace(field, "WEDNESDAY", "3", -1)
		field = strings.Replace(field, "THURSDAY", "4", -1)
		field = strings.Replace(field, "FRIDAY", "5", -1)
		field = strings.Replace(field, "SATURDAY", "6", -1)

		field = strings.Replace(field, "JAN", "1", -1)
		field = strings.Replace(field, "FEB", "2", -1)
		field = strings.Replace(field, "MAR", "3", -1)
		field = strings.Replace(field, "APR", "4", -1)
		field = strings.Replace(field, "MAY", "5", -1)
		field = strings.Replace(field, "JUN", "6", -1)
		field = strings.Replace(field, "JUL", "7", -1)
		field = strings.Replace(field, "AUG", "8", -1)
		field = strings.Replace(field, "SEP", "9", -1)
		field = strings.Replace(field, "OCT", "10", -1)
		field = strings.Replace(field, "NOV", "11", -1)
		field = strings.Replace(field, "DEC", "12", -1)
		field = strings.Replace(field, "JANUARY", "1", -1)
		field = strings.Replace(field, "FEBRUARY", "2", -1)
		field = strings.Replace(field, "MARCH", "3", -1)
		field = strings.Replace(field, "APRIL", "4", -1)
		field = strings.Replace(field, "MAY", "5", -1)
		field = strings.Replace(field, "JUNE", "6", -1)
		field = strings.Replace(field, "JULY", "7", -1)
		field = strings.Replace(field, "AUGUST", "8", -1)
		field = strings.Replace(field, "SEPTEMBER", "9", -1)
		field = strings.Replace(field, "OCTOBER", "10", -1)
		field = strings.Replace(field, "NOVEMBER", "11", -1)
		field = strings.Replace(field, "DECEMBER", "12", -1)
	}

	step := 1
	fields := strings.Split(field, "/")
	if len(fields) > 1 {
		step, err = strconv.Atoi(fields[1])
		if err != nil {
			ServerLogger.Fatal(err)
		}
	}
	if fields[0] == "*" {
		// e.g.   */2
		rr = makerange(start, end-start+1, step)
		e = append(e, rr...)
	} else {
		// e.g.   1,2-21/2
		vals := strings.Split(fields[0], ",")
		for _, v := range vals {
			if strings.Contains(v, "-") {
				r := strings.Split(v, "-")
				start, err = strconv.Atoi(r[0])
				if err != nil {
					ServerLogger.Fatal(err) // FIXME: should be caught instead
				}
				end, err = strconv.Atoi(r[1])
				if err != nil {
					ServerLogger.Fatal(err)
				}
				rr = makerange(start, end-start+1, step)
				e = append(e, rr...)
			} else {
				d, _ = strconv.Atoi(v)
				e = append(e, d)
			}
		}
	}
	sort.Ints(e)

	// if enabled for field, select random value from range and assign
	if r {
		e = []int{e[rand.Intn(len(e))]}
	}
	return e, r
}

func lastDayOfMonth(year, mon int) int {
	dt := time.Date(year, time.Month(mon+1), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	_, _, d := dt.Date()
	//ServerLogger.Printf("last day of %s %d is %d", m, y, d)
	return d
}
func (c *Cron) addMonth() {
	if c.Spec[Mon][0] == 12 {
		c.Spec[Mon] = []int{1}
	} else {
		c.Spec[Mon] = []int{c.Spec[Mon][0] + 1}
	}
	c.Spec[Mday] = []int{1}
}
func (c Cron) Years() []int {
	return c.Spec[Year]
}
func (c Cron) Months() []int {
	return c.Spec[Mon]
}
func (c Cron) Days() []int {
	return c.Spec[Mday]
}
func (c Cron) Mdays() []int {
	return c.Spec[Mday]
}
func (c Cron) Wdays() []int {
	return c.Spec[Wday]
}
func (c Cron) Hours() []int {
	return c.Spec[Hour]
}
func (c Cron) Minutes() []int {
	return c.Spec[Min]
	//minutes := c.Spec[Min]
	//if ok, _ := c.r[Min]; ok {
	//    minutes = []int{minutes[rand.Intn(len(minutes))]}
	//}
	//return minutes
}
func (c Cron) Seconds() []int {
	return c.Spec[Sec]
}

func (c Cron) isValid() bool {
	isvalid := false
	dt := time.Date(c.Spec[Year][0], time.Month(c.Spec[Mon][0]), c.Spec[Mday][0], 0, 0, 0, 0, time.UTC)
	y, m, d := dt.Date()
	if y == c.Spec[Year][0] && m == time.Month(c.Spec[Mon][0]) && d == c.Spec[Mday][0] {
		isvalid = true
	}
	return isvalid
}
func isValidDayInMonth(year, mon, day int) bool {
	isValid := false
	allValidDays := makeMdaysValid(year, mon, makerange(1, 31, 1))
	for _, d := range allValidDays {
		if d == day {
			isValid = true
		}
	}
	return isValid
}
func (c Cron) IsAt() bool {
	return c.at != ""
}

func (c Cron) IsEvery() bool {
	if c.every > time.Duration(0) {
		return true
	}
	return false
}
func (job *Job) isEvery() bool {
	return job.cronStartArray[0].IsEvery()
}
func NullCron() Cron {
	cron := Cron{}
	cron.Null = true
	return cron
}
func (c Cron) IsNull() bool {
	return c.Null && !c.array
}
func (job *Job) isCronNull() bool {
	return job.cronStartArray[0].IsNull()
}
func DependentCron() Cron {
	cron := Cron{}
	cron.Dependent = true
	return cron
}
func (c Cron) isDependent() bool {
	return c.Dependent
}
func (job *Job) isCronDependent() bool {
	return job.cronStartArray[0].isDependent()
}
func (job *Job) isContingent() bool {
	return job.cronStart.Contingent
}
func ParseTime(timestring string, timezone string, calendar string, calendarDirs []string) (time.Time, Cron) {
	loc, _ := time.LoadLocation(timezone)
	t, err := time.ParseInLocation("20060102150405", timestring, loc)
	if err != nil {
		ServerLogger.Fatal("error parsing time:", err)
	}

	if t.Before(time.Now().In(loc)) {
		return t, NullCron()
	}

	cron := NewCron(timezone)

	second := t.Second()
	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := t.Month()
	year := int(t.Year())

	cron.Spec[Sec] = []int{int(second)}
	cron.Spec[Min] = []int{int(minute)}
	cron.Spec[Hour] = []int{int(hour)}
	cron.Spec[Mday] = []int{int(day)}
	cron.Spec[Mon] = []int{int(month)}
	cron.Spec[Wday] = []int{int(-1)}
	cron.Spec[Year] = []int{year, year + 1}

	return t, cron
}

// days: "M-F", "weekdays", "monday"
// times: "12:00", "9:00,3:00", "hourly"
func parseDayAndTime(days, times string, timezone string, calendar string, calendarDirs []string, rollback bool, required bool, jitter int) ([]Cron, string) {
	if days == "" {
		days = "*"
	}
	ts := strings.Split(times, ",")
	var cronspec []string

	for _, t := range ts {
		mm := "0"
		ss := "0"
		hhmmss := strings.Split(strings.Replace(t, " ", "", -1), ":")
		hh := hhmmss[0]
		if len(hhmmss) > 1 {
			mm = hhmmss[1]
		}
		if len(hhmmss) > 2 {
			ss = hhmmss[2]
		}
		cronspec = append(cronspec, fmt.Sprintf("%s %s %s * * %s", ss, mm, hh, days))

	}

	var cronspecarray []string
	crons := make([]Cron, len(cronspec))
	for i := range cronspec {
		crons[i], _ = ParseCron(cronspec[i], timezone, calendar, calendarDirs, rollback, required, jitter)
		cronspecarray = append(cronspecarray, fmt.Sprintf("%s", cronspec[i]))
		crons[i].Str()
	}
	cronstring := strings.Join(cronspecarray, ",")

	return crons, cronstring
}

var ParseDayAndTime = parseDayAndTime

func NewCron(location string) Cron {
	c := Cron{}
	c.Spec = make(map[CronFields][]int)
	c.Spec[Sec] = makerange(0, 61, 1)  // 0-60 - 0-59 plus leap second
	c.Spec[Min] = makerange(0, 60, 1)  // 0-59
	c.Spec[Hour] = makerange(0, 24, 1) // 0-23
	c.Spec[Mday] = makerange(1, 31, 1) // 1-31
	c.Spec[Mon] = makerange(1, 12, 1)  // 1-12
	c.Calendar = ""
	c.Timezone = location
	yr := now(location, "").Year()

	c.Spec[Year] = []int{yr, yr + 1}
	c.Spec[Wday] = makerange(0, 7, 1) // 0-6
	c.r = make(map[CronFields]bool)
	return c
}

func (c *Cron) updateWday() {
	loc, _ := time.LoadLocation("UTC")
	dt := time.Date(c.Spec[Year][0], time.Month(c.Spec[Mon][0]), c.Spec[Mday][0], int(0), int(0), int(0), int(0), loc)
	wday := int(dt.Weekday())
	c.Spec[Wday] = []int{wday}
}

func (c Cron) Add(d time.Duration) Cron {
	var cron Cron
	return cron
}
func (c Cron) String() string {
	return fmt.Sprintf("Cron{%d %d %d %d %d %d %s %s}", c.Spec[Sec][0], c.Spec[Min][0], c.Spec[Hour][0], c.Spec[Mday][0], c.Spec[Mon][0], c.Spec[Wday][0], c.Timezone, c.Calendar)
}
func (c Cron) Str() {
	fmt.Printf("Cron: %s\n", c.spec)
	fmt.Println("  Sec:", c.Spec[Sec])
	fmt.Println("  Min", c.Spec[Min])
	fmt.Println("  Hour", c.Spec[Hour])
	fmt.Println("  Mday", c.Spec[Mday])
	fmt.Println("  Mon", c.Spec[Mon])
	fmt.Println("  Wday", c.Spec[Wday])
	fmt.Println("  Year", c.Spec[Year])
}

// array version for handling disjoint cron starts
func NextCronStart(cron []Cron) (d time.Duration, next time.Time) {

	for i := 0; i < len(cron); i++ {
		d_i, next_i := getNextStart(cron[i])
		if i == 0 || d > d_i {
			d = d_i
			next = next_i
		}
	}
	return
}

func getNextStart(cron Cron) (d time.Duration, next time.Time) {
	if cron.IsEvery() {
		d = cron.every
		//loc, _ := time.LoadLocation(cron.Timezone)
		//now := time.Now().In(loc)
		//next = now.Add(d)
		next = now(cron.Timezone, "").Add(d) // USING rpeat.now
		return
	}
	if cron.IsAt() { // FIXME: IsNull should not be true for @at
		d, next, _ = cron.NextStart("")
		return
	}
	if cron.IsNull() || cron.isDependent() {
		d = time.Duration(math.MaxInt64)
		//next = time.Unix(0,0)  // zero instant
		next = ZeroTime
	} else {
		d, next, _ = cron.NextStart("")
	}
	return
}

// @shortcut processing for crons
func atManual(timezone string) Cron {
	// @manual
	cron := NullCron()
	cron.Timezone = timezone
	return cron
}
func atAt(timezone string, at string) Cron {
	// @at 20220122
	//_, cron := ParseTime(at, timezone, "", []string{}) // don't think we want cal support here
	cron := NullCron() // FIXME: change to NewCron(timezone) to prevent IsNull == true
	cron.at = at
	cron.Timezone = timezone
	return cron
}
func atEvery(timezone string, duration string) Cron {
	// @every 25m
	cron := NullCron()
	every, err := time.ParseDuration(duration)
	if err != nil {
		ServerLogger.Fatal("malformed duration string in @every", duration)
	}
	cron.every = every
	return cron
}
func atYearly(timezone string, fields []string) Cron {
	// @yearly    0 0 1 1 *
	// @yearly    0 0 0 1 1 *
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Min], _ = expandField(fields[1], 0, 59)
	}
	cron.Spec[Hour] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Hour], _ = expandField(fields[2], 0, 23)
	}
	cron.Spec[Mday] = []int{1}
	cron.Spec[Mon] = []int{1}
	cron.Spec[Wday] = []int{-1}
	return cron
}
func atAnnual(timezone string, fields []string) Cron {
	// @annual alias @yearly
	return atYearly(timezone, fields)
}
func atQuarterly(timezone string, fields []string) Cron {
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Min], _ = expandField(fields[1], 0, 59)
	}
	cron.Spec[Hour] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Hour], _ = expandField(fields[2], 0, 23)
	}
	cron.Spec[Mday] = []int{1}
	cron.Spec[Mon] = []int{1, 4, 7, 10}
	cron.Spec[Wday] = []int{-1}
	return cron
}
func atMonthly(timezone string, fields []string) Cron {
	// @monthly   0 0 1 * *
	// @monthly   0 0 0 1 * *
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Min], _ = expandField(fields[1], 0, 59)
	}
	cron.Spec[Hour] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Hour], _ = expandField(fields[2], 0, 23)
	}
	cron.Spec[Mday] = []int{1}
	cron.Spec[Wday] = []int{-1}
	return cron
}
func atWeekly(timezone string, fields []string) Cron {
	// @weekly    0 0 * * 0
	// @weekly    0 0 0 * * 0
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Min], _ = expandField(fields[1], 0, 59)
	}
	cron.Spec[Hour] = []int{0}
	if len(fields) == 3 {
		cron.Spec[Hour], _ = expandField(fields[2], 0, 23)
	}
	cron.Spec[Mday] = []int{-1}
	cron.Spec[Wday] = []int{0}
	return cron
}
func atDaily(timezone string) Cron {
	// @daily    0 0 * * *
	// @daily    0 0 0 * * *
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	cron.Spec[Hour] = []int{0}
	cron.Spec[Wday] = []int{-1}
	return cron
}
func atMidnight(timezone string) Cron {
	// @midnight  alias to @daily
	return atDaily(timezone)
}
func atHourly(timezone string) Cron {
	// @hourly    0 * * * *
	// @hourly    0 0 * * * *
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	cron.Spec[Min] = []int{0}
	return cron
}
func atMinutely(timezone string) Cron {
	// @minutely  * * * * *
	// @minutely  0 * * * * *
	cron := NewCron(timezone)
	cron.Spec[Sec] = []int{0}
	return cron
}
func atSecondly(timezone string) Cron {
	// @secondly  * * * * * *
	cron := NewCron(timezone)
	return cron
}
