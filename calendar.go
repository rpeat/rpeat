package rpeat

import (
	"bufio"
	"math"
	"os"
	"sort"
	"strconv"
	"time"
	//    "errors"
	"path/filepath"
)

type CalAdjust int

const (
	CalFollowing CalAdjust = iota
	CalFollowingModified
	CalProceeding
	CalProceedingModified
	CalNoAdjust
)

type Cal struct {
	Calendar string // Mondays
	Forward  bool
	DatesIn  []int // 20190101, 20190203, ...
	DatesEx  []int // 20190101, 20190203, ...
	Adjust   CalAdjust
}

func NewCal(calendar string) Cal {
	cal := Cal{Calendar: calendar}
	return cal
}

func ReadCalendar(calendar string, calendarPath []string) (Cal, error) {
	var err error
	var calendarFile string
	for _, path := range calendarPath {
		exists, err := FileExists(path)
		if !exists {
			return Cal{}, CalendarError{Exception: CalendarDirNotFound, Calendar: calendar, CalendarDirs: []string{path}, fileErr: err}
		}
		calendarFile = filepath.Join(path, calendar)
		exists, err = FileExists(calendarFile)
		if exists {
			break
		} else {
			return Cal{}, CalendarError{Exception: CalendarNotFound, Calendar: calendar, CalendarDirs: calendarPath, fileErr: err}
		}
	}
	if calendarFile == "" {
		return Cal{}, CalendarError{Exception: MissingCalendar, Calendar: calendar, CalendarDirs: calendarPath}
	}
	file, err := os.Open(calendarFile)
	if err != nil {
		return Cal{}, CalendarError{Exception: CalendarReadError, Calendar: calendar, CalendarDirs: calendarPath, fileErr: err}
	}
	scanner := bufio.NewScanner(file)

	var nlines int
	for scanner.Scan() {
		nlines++
	}
	_, err = file.Seek(0, 0)
	if err != nil {
		return Cal{}, CalendarError{Exception: CalendarReadError, Calendar: calendar, CalendarDirs: calendarPath, fileErr: err}
	}
	scanner = bufio.NewScanner(file)

	dates := make([]int, nlines)

	var n int
	for scanner.Scan() {
		txt := scanner.Text()
		date, err := strconv.ParseInt(txt, 10, 32)
		if err != nil {
			ServerLogger.Printf("error parsing %s into date(int)\n", txt)
			err = CalendarError{Exception: CalendarReadError, Calendar: calendar, CalendarDirs: calendarPath, fileErr: err}
		}
		dates[n] = int(date)
		n++
	}
	cal := NewCal(calendar)
	cal.DatesIn = dates
	return cal, err

}

func NextAvailableDate(t time.Time, calendar string, calendarPath []string) (int, time.Month, int) {
	cal, _ := ReadCalendar(calendar, calendarPath) // FIXME: handle error
	y, m, d := t.Date()
	date := y*10000 + int(m)*100 + d

	idx := sort.SearchInts(cal.DatesIn, date)
	nexttime, _ := time.Parse("20060102", strconv.Itoa(cal.DatesIn[idx]))
	y, m, d = nexttime.Date()

	//log.Printf("y=%d,m=%d,d=%d\tdate:%d, cal.DatesIn[idx]:%d", y,m,d,date, cal.DatesIn[idx])
	return y, m, d
}

// FIXME: these cal vs path are inconsistent
func addDateWithCal(t time.Time, years int, months int, days int, cal Cal) time.Time {
	y, m, d := t.Date()
	date := y*10000 + int(m)*100 + d

	idx := sort.SearchInts(cal.DatesIn, date)

	// rebase idx if beyond initial date
	if !cal.Forward && cal.DatesIn[idx] > date {
		idx = idx - 1
	}

	dt := cal.DatesIn[idx+days]
	yr := dt / 10000
	mo := time.Month(math.Mod(float64(dt/100), 100))
	dy := int(math.Mod(float64(dt), 100))
	return time.Date(yr, mo, dy, 0, 0, 0, 0, time.UTC)
}

func addDayWithCal(t time.Time, days int, calendar string, calendarPath []string, forward bool) time.Time {
	cal, err := ReadCalendar(calendar, calendarPath)
	if err != nil {
		ServerLogger.Fatal("Calendar error")
	}
	cal.Forward = forward
	return addDateWithCal(t, 0, 0, days, cal)
}
