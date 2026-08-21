package rpeat

import (
	"fmt"
	"strconv"
	"strings"
)

var UTC = "UTC"

// ConvertDate returns an expanded date string taking into account optionally specified
// forward or backward date shifts, using an optionally specified calendar.
//
// d takes the form  [MM|DD|YY|CC|hh|mm|ss]+[,[+-]?[0-9]+[Y|Q|M|W|D]][,CALENDAR]
//
// e.g.
//
//	CCYY-MM           converts to 2020-03 when called between 2020-03-01 and 2020-03-31
//	CCYY-MM,+1M       converts to 2020-04 when called between 2020-03-01 and 2020-03-31
//	CCYYMM,+1D        converts to 202004 when called on and 2020-03-31
//	CCYY/MM,-5D       converts to 2020/03 when called on and 2020-04-01
//	CCYY-MM-DD,-1D    converts to 2020-03-29 when called on and 2020-03-30
//	CCYY-MM-DD,-1D,MF converts to 2020-03-27 when called on and 2020-03-30 (using MF only)
//	CCYY-MM-DD,+5D,MF converts to 2020-04-06 when called on and 2020-03-30 (using MF only)
//
// As this is only converting the date component, hh, mm, or ss are simply converted to
// 00. Any value not matching one of these magic variables remains in final string
//
// e.g.
//
//	CCYYMM22          converts to 20200322 when called between 2020-03-01 and 2020-03-01
//	CCYYMM22,+2D      converts to 20200322 when called on 2020-03-22 (Feature!)
func ConvertDate(d string, timezone string, calendarPath []string, asof string) (datestring string, err error) {

	dt := strings.Split(d, ",")
	datestring = dt[0]
	datestring = strings.Replace(datestring, "CC", "20", 1)
	datestring = strings.Replace(datestring, "YY", "06", 1)
	datestring = strings.Replace(datestring, "MM", "01", 1)
	datestring = strings.Replace(datestring, "DD", "02", 1)
	datestring = strings.Replace(datestring, "hh", "15", 1)
	datestring = strings.Replace(datestring, "mm", "04", 1)
	datestring = strings.Replace(datestring, "ss", "05", 1)

	if strings.Contains(datestring, "CC") ||
		strings.Contains(datestring, "YY") ||
		strings.Contains(datestring, "MM") ||
		strings.Contains(datestring, "DD") ||
		strings.Contains(datestring, "hh") ||
		strings.Contains(datestring, "mm") ||
		strings.Contains(datestring, "ss") {
		// duplicated magic var (Warning)
		err = DateEnvError{Exception: DuplicateMagicVar, MagicDate: fmt.Sprintf("only one magic var per type processed: %s", datestring)}
		return
	}
	unit := ""
	var n int64
	n = 0
	useCal := false
	var calendar string
	if len(dt) > 1 {
		var unitErr error
		if len(dt[1]) < 2 {
			// malformed shift
			err = DateEnvError{Exception: ErrorInShiftValue, UnitField: "second argument (shift) incorrectly formatted"}
			return
		}
		unit = dt[1][len(dt[1])-1:]
		n, unitErr = strconv.ParseInt(dt[1][:len(dt[1])-1], 0, 64)
		if unitErr != nil {
			// bad unit value
			err = DateEnvError{Exception: ErrorInShiftValue, UnitField: unitErr.Error()}
			return
		}
		if len(dt) == 3 {
			calendar = dt[2]
			if len(calendar) == 0 {
				// missing cal
				err = DateEnvError{Exception: UnknownCalendar, Calendar: "empty calendar"}
				return
			}
			// cal not found
			if _, calErr := ReadCalendar(calendar, calendarPath); calErr != nil {
				err = DateEnvError{Exception: UnknownCalendar, Calendar: calErr.Error()}
				return
			}
			useCal = true
		}
	}

	/*
	   loc, tzerr := time.LoadLocation(timezone)
	   if tzerr != nil {
	       loc = time.UTC
	   }
	   t := time.Now().In(loc) //FIXME: this needs to use rpeat.Now to facilitate testing
	*/
	t := now(timezone, asof)

	if len(dt) > 1 {
		switch unit {
		case "Y":
			t = t.AddDate(int(n), 0, 0)
		case "Q":
			t = t.AddDate(0, int(n)*3, 0)
		case "M":
			t = t.AddDate(0, int(n), 0)
		case "W":
			t = t.AddDate(0, 0, int(n)*7)
		case "D":
			if useCal { // NB: is this covered below?
				t = addDayWithCal(t, int(n), calendar, calendarPath, false)
			} else {
				t = t.AddDate(0, 0, int(n))
			}
		default:
			err = DateEnvError{Exception: UnknownShiftUnit, UnitField: fmt.Sprintf("Unknown unit: %s", unit)}
			return
		}
	}

	if useCal {
		t = addDayWithCal(t, 0, calendar, calendarPath, false) // adjust to appropriate non-holiday
	}
	datestring = t.Format(datestring)

	// this needs to happen after date shift to be able to calculate correct qtr
	if strings.Contains(datestring, "QTR") {
		mo := int(t.Month())
		qtr := fmt.Sprintf("%d", (mo-1)/3+1)
		datestring = strings.Replace(datestring, "QTR", qtr, -1)
	}
	return
}
