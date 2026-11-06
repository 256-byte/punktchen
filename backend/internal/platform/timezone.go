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
