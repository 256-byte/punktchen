// Package departures turns the schedule into absolute departure times: the
// schedule window (ARCHITECTURE §3.2) and, later, the snapshot.
package departures

import (
	"fmt"
	"time"
)

// ServiceDay is a GTFS service date: the operating day a trip belongs to,
// which is not always the calendar day it runs on (times ≥ 24:00:00).
type ServiceDay struct {
	Year  int
	Month time.Month
	Day   int
}

// DayOf returns the calendar date of t in loc as a service day.
func DayOf(t time.Time, loc *time.Location) ServiceDay {
	y, m, d := t.In(loc).Date()
	return ServiceDay{y, m, d}
}

// String formats the day as YYYY-MM-DD (the API's serviceDate).
func (d ServiceDay) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// AddDays returns the day n days later (n may be negative).
func (d ServiceDay) AddDays(n int) ServiceDay {
	y, m, dd := time.Date(d.Year, d.Month, d.Day+n, 12, 0, 0, 0, time.UTC).Date()
	return ServiceDay{y, m, dd}
}

// Start is the reference point of GTFS stop times: "noon minus 12 h" of the
// service day in loc. It is midnight except on DST switch days, where it is
// 23:00 of the day before (spring) or 01:00 (autumn), so stop times stay
// elapsed time since the start of the day.
func (d ServiceDay) Start(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, loc).Add(-12 * time.Hour)
}

// At returns the absolute time of a stop time given in seconds since Start.
func (d ServiceDay) At(secs int32, loc *time.Location) time.Time {
	return d.Start(loc).Add(time.Duration(secs) * time.Second)
}

// dayRange is the part of a time window one service day can reach, as
// inclusive bounds on GTFS seconds.
type dayRange struct {
	day              ServiceDay
	fromSecs, toSecs int32
}

// dayRanges splits the half-open window [from, to) into one seconds range per
// service day that can have stop times in it, given the feed's largest stop
// time maxSecs (VBB: 31:17:00, DATA_MODEL §7 #10). Times are whole seconds,
// so a stop time s of day d is in the window iff from ≤ d.Start+s < to.
func dayRanges(from, to time.Time, loc *time.Location, maxSecs int32) []dayRange {
	if !from.Before(to) {
		return nil
	}
	var out []dayRange
	last := DayOf(to, loc)
	// Back far enough for the longest stop time, plus a day for the rest of it
	// and for days that start an hour early (DST).
	first := DayOf(from, loc).AddDays(-int(maxSecs/86400) - 1)
	for d := first; d != last.AddDays(1); d = d.AddDays(1) {
		start := d.Start(loc)
		lo := max(ceilSeconds(from.Sub(start)), 0)
		hi := min(ceilSeconds(to.Sub(start))-1, int64(maxSecs))
		if hi < lo {
			continue
		}
		out = append(out, dayRange{day: d, fromSecs: clamp32(lo), toSecs: clamp32(hi)})
	}
	return out
}

// ceilSeconds rounds d up to whole seconds.
func ceilSeconds(d time.Duration) int64 {
	s := int64(d / time.Second) // truncates toward zero: already the ceiling for d < 0
	if d > time.Duration(s)*time.Second {
		s++
	}
	return s
}

func clamp32(v int64) int32 {
	const lim = 1<<31 - 1
	return int32(min(max(v, -lim), lim))
}
