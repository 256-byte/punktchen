package departures

import (
	"slices"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

var berlin = platform.Berlin

// at parses an RFC 3339 time; the offset says which Berlin time is meant.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestServiceDayStart(t *testing.T) {
	for _, c := range []struct {
		day  ServiceDay
		want string
	}{
		{ServiceDay{2026, 11, 9}, "2026-11-09T00:00:00+01:00"},
		// Spring forward (2026-03-29 02:00 → 03:00): noon is CEST, so the day
		// starts at 23:00 CET of the evening before.
		{ServiceDay{2026, 3, 29}, "2026-03-28T23:00:00+01:00"},
		// Fall back (2026-10-25 03:00 → 02:00): noon is CET, start at 01:00 CEST.
		{ServiceDay{2026, 10, 25}, "2026-10-25T01:00:00+02:00"},
	} {
		if got := c.day.Start(berlin); !got.Equal(at(t, c.want)) {
			t.Errorf("%s: start %s, want %s", c.day, got, c.want)
		}
	}
}

func TestServiceDayAt(t *testing.T) {
	hms := func(h, m, s int) int32 { return int32(h*3600 + m*60 + s) }
	for _, c := range []struct {
		name string
		day  ServiceDay
		secs int32
		want string
	}{
		{"normal", ServiceDay{2026, 11, 9}, hms(8, 0, 0), "2026-11-09T08:00:00+01:00"},
		// PLAN 2.2: 24:30 on D is 00:30 on D+1; 31:17 on D is 07:17 on D+1.
		{"after midnight", ServiceDay{2026, 11, 9}, hms(24, 30, 0), "2026-11-10T00:30:00+01:00"},
		{"VBB maximum", ServiceDay{2026, 11, 9}, hms(31, 17, 0), "2026-11-10T07:17:00+01:00"},
		// Daytime times show the wall clock time on switch days...
		{"spring day", ServiceDay{2026, 3, 29}, hms(8, 0, 0), "2026-03-29T08:00:00+02:00"},
		{"autumn day", ServiceDay{2026, 10, 25}, hms(8, 0, 0), "2026-10-25T08:00:00+01:00"},
		// ...but times around the switch are elapsed time since the start.
		{"spring 01:30", ServiceDay{2026, 3, 29}, hms(1, 30, 0), "2026-03-29T00:30:00+01:00"},
		{"autumn 00:30", ServiceDay{2026, 10, 25}, hms(0, 30, 0), "2026-10-25T01:30:00+02:00"},
		// Night trips of the day before a switch run across it.
		{"spring eve 26:30", ServiceDay{2026, 3, 28}, hms(26, 30, 0), "2026-03-29T03:30:00+02:00"},
		{"autumn eve 26:30", ServiceDay{2026, 10, 24}, hms(26, 30, 0), "2026-10-25T02:30:00+02:00"},
		{"autumn eve 27:30", ServiceDay{2026, 10, 24}, hms(27, 30, 0), "2026-10-25T02:30:00+01:00"},
	} {
		if got := c.day.At(c.secs, berlin); !got.Equal(at(t, c.want)) {
			t.Errorf("%s: %s + %ds = %s, want %s", c.name, c.day, c.secs, got.In(berlin), c.want)
		}
	}
}

func TestServiceDayCalendar(t *testing.T) {
	d := ServiceDay{2026, 12, 31}
	if got := d.AddDays(1); got != (ServiceDay{2027, 1, 1}) {
		t.Errorf("AddDays(1) = %v", got)
	}
	if got := (ServiceDay{2026, 3, 1}).AddDays(-1); got != (ServiceDay{2026, 2, 28}) {
		t.Errorf("AddDays(-1) = %v", got)
	}
	if d.String() != "2026-12-31" {
		t.Errorf("String() = %q", d.String())
	}
	// 2026-11-09 23:30 UTC is already the 10th in Berlin.
	if got := DayOf(time.Date(2026, 11, 9, 23, 30, 0, 0, time.UTC), berlin); got != (ServiceDay{2026, 11, 10}) {
		t.Errorf("DayOf = %v", got)
	}
}

func TestDayRanges(t *testing.T) {
	r := func(day ServiceDay, from, to int32) dayRange { return dayRange{day, from, to} }
	nov8, nov9, nov10 := ServiceDay{2026, 11, 8}, ServiceDay{2026, 11, 9}, ServiceDay{2026, 11, 10}
	const vbbMax = 31*3600 + 17*60 // 31:17:00
	for _, c := range []struct {
		name     string
		from, to string
		maxSecs  int32
		want     []dayRange
	}{
		{"daytime: yesterday cannot reach it", "2026-11-10T17:00:00+01:00", "2026-11-10T18:30:00+01:00", vbbMax,
			[]dayRange{r(nov10, 61200, 66599)}},
		{"after midnight: yesterday's 24:xx plus today", "2026-11-10T00:20:00+01:00", "2026-11-10T00:40:00+01:00", vbbMax,
			[]dayRange{r(nov9, 87600, 88799), r(nov10, 1200, 2399)}},
		{"yesterday up to the feed's maximum", "2026-11-10T07:00:00+01:00", "2026-11-10T08:00:00+01:00", vbbMax,
			[]dayRange{r(nov9, 111600, vbbMax), r(nov10, 25200, 28799)}},
		{"across midnight", "2026-11-09T23:50:00+01:00", "2026-11-10T00:10:00+01:00", vbbMax,
			[]dayRange{r(nov9, 85800, 86999), r(nov10, 0, 599)}},
		{"feed with times past 48:00 reaches two days back", "2026-11-10T00:20:00+01:00", "2026-11-10T00:40:00+01:00", 50 * 3600,
			[]dayRange{r(nov8, 174000, 175199), r(nov9, 87600, 88799), r(nov10, 1200, 2399)}},
		{"fractional seconds round inward", "2026-11-10T17:00:00.5+01:00", "2026-11-10T17:00:02.5+01:00", vbbMax,
			[]dayRange{r(nov10, 61201, 61202)}},
		{"to at midnight adds no next day", "2026-11-09T23:00:00+01:00", "2026-11-10T00:00:00+01:00", vbbMax,
			[]dayRange{r(nov9, 82800, 86399)}},
		// Spring: the day starts at 23:00 CET the evening before.
		{"spring forward", "2026-03-29T07:55:00+02:00", "2026-03-29T08:05:00+02:00", vbbMax,
			[]dayRange{r(ServiceDay{2026, 3, 28}, 111300, 111899), r(ServiceDay{2026, 3, 29}, 28500, 29099)}},
		{"empty", "2026-11-10T17:00:00+01:00", "2026-11-10T17:00:00+01:00", vbbMax, nil},
	} {
		got := dayRanges(at(t, c.from), at(t, c.to), berlin, c.maxSecs)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got  %v\n want %v", c.name, got, c.want)
		}
	}
}
