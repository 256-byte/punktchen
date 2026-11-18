package platform

import (
	"time"
	// Embed the IANA time zone database, so Europe/Berlin works on any host
	// or minimal image without system tzdata.
	_ "time/tzdata"
)

// Berlin is the time zone of all API timestamps (DATA_MODEL §5.1) and of
// the VBB schedule.
var Berlin = mustLoadLocation("Europe/Berlin")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err) // embedded tzdata: cannot fail for a valid name
	}
	return loc
}

// NextDaily is the first hour:minute in loc strictly after now. time.Date
// keeps the wall clock across DST changes (03:30 stays 03:30). Used by the
// daily imports (GTFS, amenities).
func NextDaily(now time.Time, hour, minute int, loc *time.Location) time.Time {
	l := now.In(loc)
	t := time.Date(l.Year(), l.Month(), l.Day(), hour, minute, 0, 0, loc)
	if !t.After(now) {
		t = time.Date(l.Year(), l.Month(), l.Day()+1, hour, minute, 0, 0, loc)
	}
	return t
}
