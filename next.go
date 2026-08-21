package rpeat

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"time"
	//"github.com/davecgh/go-spew/spew"
)

var _ = log.Println

var ZeroTime = time.Date(0001, 1, 1, 00, 00, 00, 00, time.UTC)

func now(location, asof string) time.Time {
	loc, tzerr := time.LoadLocation(location)
	if tzerr != nil {
		loc = time.UTC
	}
	// default is behavior of time.Now() aka now
	t := time.Now().In(loc)

	var err error
	var isNowSet bool
	// if asof is set (used in ConvertDate for Validate, rpeat-util, and GUI)
	// or in testing with ENV variable set
	if asof != "" {
		isNowSet = true
	} else {
		asof, isNowSet = os.LookupEnv("RPEAT_NOW")
	}
	if isNowSet {
		t, err = time.ParseInLocation("20060102150405", asof, loc)
		if err != nil {
			ServerLogger.Fatal(err)
		}
	}

	return t
}

func (c Cron) NextStart(asof string) (time.Duration, time.Time, error) {

	// TODO: add support for calendars for @every and @at
	if c.IsEvery() {
		d := c.every
		//loc, _ := time.LoadLocation(c.Timezone)
		//now := time.Now().In(loc)
		//return d, now.Add(d), nil
		return d, now(c.Timezone, asof).Add(d), nil
	}
	if c.IsAt() {
		loc, _ := time.LoadLocation(c.Timezone)
		nextRun, _ := time.ParseInLocation("20060102150405", c.at, loc)
		d := nextRun.Sub(now(c.Timezone, asof).Add(time.Millisecond))
		if d < 0 {
			return time.Duration(math.MaxInt64), ZeroTime, errors.New("@at has passed")
		} else {
			return d, nextRun, nil
		}
	}
	if c.IsNull() || c.isDependent() {
		ServerLogger.Println("NON TRIGGERING CRON")
		return time.Duration(math.MaxInt64), ZeroTime, errors.New("non-triggering Cron")
	}
	m := c.Months()
	d := c.Mdays()
	wd := c.Wdays()
	H := c.Hours()
	M := c.Minutes()
	S := c.Seconds()
	tz := c.Timezone

	var err error
	useCal := c.Calendar != "" && c.Calendar != "ALL"
	rollback := c.Rollback
	endof := c.EndOf

	current := now(tz, asof).Add(time.Millisecond)
	//currentIn := current.In(time.UTC)  /// FIXME: Why is this in UTC?  Currently breaks when we are in final day of a month (year too?) and TZ adjustment pushes to next month
	currentIn := current

	dates := nextYearDates(currentIn.Year(), int(currentIn.Month()), d, m, wd)

	var cal Cal

	if len(dates) == 0 {
		return time.Duration(math.MaxInt64), ZeroTime, errors.New("non-triggering Cron")
	}
	// FIXME: somehow we can end up with an out-of-bounds panic len(dates)==0 ??
	lastDateInCal := dates[len(dates)-1]
	// filter by calendar dates

	if useCal || rollback { // requires a complete set of available dates to be able to rollback
		var caldt int
		if useCal {
			cal, err = ReadCalendar(c.Calendar, c.CalendarDirs)
			if err != nil {
				return time.Duration(math.MaxInt64), ZeroTime, errors.New("Missing Calendar") // TODO: may be useful to fall back to no calendar version?
			}
		} else {
			nc := NewCron(c.Timezone)
			cal.DatesIn = nextYearDates(currentIn.Year(), int(currentIn.Month()), nc.Mdays(), nc.Months(), nc.Wdays())
		}
		n := 0
		for _, dt := range dates {
			i := sort.SearchInts(cal.DatesIn, dt)
			if i == len(cal.DatesIn) { // outside of calendar range - copy date to calendar
				caldt = dt
			} else {
				caldt = cal.DatesIn[i]
				lastDateInCal = caldt // keep track of last date in cal if need to remove
			}
			dates[n] = caldt
			if n > 0 && dates[n-1] == dates[n] {
				continue
			}
			n++
		}
		dates = dates[:n]
	}

	if len(dates) == 0 {
		return time.Duration(math.MaxInt64), time.Time{}, errors.New("no remaining dates - issue with calendar or cron")
	}

	loc, tzerr := time.LoadLocation(tz)
	if tzerr != nil {
		loc = time.UTC
	}
	firstTime := H[0]*10000 + M[0]*100 + S[0] // possible to use MAX(Calendar Open, firstTime) to handle simplistic intraday rules
	lastTime := H[len(H)-1]*10000 + M[len(M)-1]*100 + S[len(S)-1]

	i := sort.SearchInts(dates, dateAsInt(current))
	//ServerLogger.Printf(DebugColor, fmt.Sprintf("current: %d [%v] i:%s", dateAsInt(current), dates, i))

	var runDate int
	var nextRun time.Time

	curDate := dateAsInt(current)

	//ServerLogger.Printf(DebugColor, fmt.Sprintf("rollback:%t, curDate:%d, dates[%d]:%d",rollback,curDate,i,dates[i]))
	if curDate == dates[i] || (rollback && curDate == dates[i+1]) {
		if curDate == dates[i] {
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("curDate == dates[i] (%d == %d) [[ TRUE ]]", curDate, dates[i]))
		}
		if rollback && curDate == dates[i+1] {
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("rollback && curDate == dates[i+1] [[ TRUE ]]"))
		}
		// current day is in schedule
		if timeAsFloat(current) > float64(lastTime) || endof {
			// after last scheduled time for day, move day forward
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("timeAsFloat(current) > float64(lastTime) || endof [[ TRUE ]]"))
			runDate = dates[i+1]
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("next runDate=dates[i+1]:%d (dates[i]:%d)", runDate, dates[i]))
			runDate0 := runDate
			if rollback {
				//ServerLogger.Printf(DebugColor, fmt.Sprintf("line:153 IN ROLLBACK: %d %d (dates[%d+1]: %d)", runDate, dates[i], i, dates[i+1]))
				//spew.Dump(cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)-1])
				//spew.Dump(cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)])
				//spew.Dump([]int{len(cal.DatesIn), len(dates)})
				runDate = cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)-1]
				if runDate < runDate0 {
					runDate = runDate0
				}
			}
			nextRun, _ = time.ParseInLocation("20060102150405", fmt.Sprintf("%08d%06d", runDate, firstTime), loc)
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("(1) currentTime:%s > lastTime (firstTime) | runDate0:%d runDate:%d dates[i+1]:%d", current, runDate0, runDate, dates[i+1]))
		} else {
			runDate = dates[i]
			//runDate0 := runDate
			if rollback {
				//ServerLogger.Printf(DebugColor, fmt.Sprintf("line:162 IN ROLLBACK: %d %d", runDate, dates[i]))
				if curDate < runDate {
					runDate = cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)-1]
				} else {
					runDate = cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)]
				}
			}
			nextRun, _ = time.ParseInLocation("20060102150405", fmt.Sprintf("%d", nextDateTime(runDate, current, H, M, S)), loc)
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("(2) currentTime:%s <= lastTime (nextDateTime) | runDate0:%d runDate:%d dates[i]:%d", current, runDate, runDate, dates[i]))
		}
	} else {
		//ServerLogger.Printf(DebugColor, fmt.Sprintf("curDate !== dates[i] (%d == %d) [[ TRUE ]]", curDate, dates[i]))
		runDate = dates[i]
		runDate0 := runDate
		//ServerLogger.Printf(DebugColor, fmt.Sprintf("line:181: %d %d", runDate, dates[i]))
		if rollback || endof {
			runDate = cal.DatesIn[sort.SearchInts(cal.DatesIn, runDate)-1]
			//ServerLogger.Printf(DebugColor, fmt.Sprintf("line:184 ROLLBACK: runDate (new):%d runDate (old):%d", runDate, runDate0))
			if curDate > runDate || (curDate == runDate && timeAsFloat(current) > float64(lastTime) || runDate0 > runDate) {
				//ServerLogger.Printf(DebugColor, fmt.Sprintf(" |    resetting runDate as runDate0 | current:%d  runDate:%d [possibly: currentTime > lastTime]", curDate, runDate))
				runDate = runDate0
				//runDate = cal.DatesIn[sort.SearchInts(cal.DatesIn, dates[i+1])-1]
			}
		}
		nextRun, _ = time.ParseInLocation("20060102150405", fmt.Sprintf("%08d%06d", runDate, firstTime), loc)
		//ServerLogger.Printf(DebugColor, fmt.Sprintf("(3) currentDate:%s != dates[i]     runDate:%d dates[i]:%d", current, runDate, dates[i]))
	}

	if runDate > lastDateInCal {
		err = errors.New(fmt.Sprintf("insufficient calendar days in %s", c.Calendar))
		//ServerLogger.Printf(DebugColor, fmt.Sprintf(" date: %d is not in range of calendar: %s", runDate, c.Calendar))
		if c.RequireCal {
			return time.Duration(math.MaxInt64), time.Time{}, err
		}
	}
	if c.jitter > 0 {
		nextRun = nextRun.Add(time.Second * time.Duration(rand.Intn(c.jitter)))
	}
	return nextRun.Sub(current), nextRun, err
}

func dateAsInt(t time.Time) int {
	s := t.Format("20060102")
	i, _ := strconv.ParseInt(s, 10, 32)
	return int(i)
}
func timeAsFloat(t time.Time) float64 {
	h := t.Hour()
	m := t.Minute()
	s := t.Second()
	n := t.Nanosecond()
	f, _ := strconv.ParseFloat(fmt.Sprintf("%02d%02d%02d.%d", h, m, s, n), 64)
	return f
}

func nextDateTime(d int, t time.Time, hours, mins, secs []int) int64 {
	H := t.Hour()
	M := t.Minute()
	S := t.Second()
	hhmmss := hours[0]*10000 + mins[0]*100 + secs[0]
FindTime:
	for _, hour := range hours {
		if hour > H {
			hhmmss = hour*10000 + mins[0]*100 + secs[0]
			break FindTime
		}
		if hour == H {
			for _, min := range mins {
				if min > M {
					hhmmss = hour*10000 + min*100 + secs[0]
					break FindTime
				}
				if min == M {
					for _, sec := range secs {
						if sec > S {
							hhmmss = hour*10000 + min*100 + sec
							break FindTime
						}
					}
				}
			}
		}
	}
	yymmddhhmmss := int64(d)*1000000 + int64(hhmmss) //hours[0]*10000 + mins[0]*100 + secs[0]
	return yymmddhhmmss
}

// func nextYearDates(t time.Time, mday, mon, wday []int) (dates []int){
func nextYearDates(y0, m0 int, mday, mon, wday []int) (dates []int) {
	//y0,m0,_ := t.Date()
	// current date through end of year
	for m := int(m0); m <= 12; m++ {
		if monIsIn(m, mon) {
			tmp := allDatesInMonth(y0, m, mday, wday)
			dates = append(dates, tmp...)
		}
	}
	maxi := 5
	for i := 1; i < maxi; i++ {
		for m := 1; m <= 12; m++ {
			if monIsIn(m, mon) {
				tmp := allDatesInMonth(y0+i, m, mday, wday)
				dates = append(dates, tmp...)
			}
		}
	}
	// following year through current month + 1
	for m := 1; m <= int(m0); m++ {
		if monIsIn(m, mon) {
			tmp := allDatesInMonth(y0+maxi, m, mday, wday)
			dates = append(dates, tmp...)
		}
	}
	return
}

func allDatesInMonth(year, mon int, mday, wday []int) []int {
	_, _, lastDay := time.Date(year, time.Month(mon), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1).Date()
	//fmt.Printf("final day in %s %d is %d\n", time.Month(mon), year, lastDay)

	alldays := mday[0] == -1 && wday[0] == -1

	var dates []int
	for d := 1; d <= lastDay; d++ {
		if dayIsIn(d, mday) || weekdayIsIn(year, mon, d, wday) || alldays {
			dates = append(dates, year*10000+mon*100+d)
		}
	}

	// filter dates per calendar
	return dates
}

func dayIsIn(d int, days []int) bool {
	if days[0] == -1 {
		return false
	}
	for _, day := range days {
		if d == day {
			return true
		}
	}
	return false
}

var monIsIn = dayIsIn

func weekdayIsIn(y, m, d int, wdays []int) bool {
	if wdays[0] == -1 {
		return false
	}
	weekday := int(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).Weekday())
	for _, wday := range wdays {
		if wday == weekday {
			return true
		}
	}
	return false
}
