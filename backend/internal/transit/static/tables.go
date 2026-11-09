package static

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// kind is how a CSV value is converted before COPY. Empty values are NULL.
type kind int

const (
	kText kind = iota
	kInt
	kFloat
	kBool // "0" / "1"
	kDate // YYYYMMDD
	kTime // H:MM:SS, may exceed 24:00:00; becomes <name> text + <name without _time>_secs int
)

type column struct {
	name     string
	kind     kind
	required bool // must be present in the header and non-empty
}

type tableSpec struct {
	file  string // in the zip
	table string // in the staging schema
	cols  []column
}

// dbColumns lists the target columns; a kTime column fills two.
func (t tableSpec) dbColumns() []string {
	var out []string
	for _, c := range t.cols {
		out = append(out, c.name)
		if c.kind == kTime {
			out = append(out, secsColumn(c.name))
		}
	}
	return out
}

func secsColumn(timeCol string) string {
	return strings.TrimSuffix(timeCol, "_time") + "_secs"
}

func req(name string, k kind) column { return column{name: name, kind: k, required: true} }
func opt(name string, k kind) column { return column{name: name, kind: k} }

// tables are imported in this order. Columns match db/gtfs/schema.sql
// (checked by a test).
var tables = []tableSpec{
	{"agency.txt", "agency", []column{
		opt("agency_id", kText), req("agency_name", kText), opt("agency_url", kText), opt("agency_timezone", kText),
		opt("agency_lang", kText), opt("agency_phone", kText), opt("agency_fare_url", kText), opt("agency_email", kText),
	}},
	{"routes.txt", "routes", []column{
		req("route_id", kText), opt("agency_id", kText), opt("route_short_name", kText), opt("route_long_name", kText),
		opt("route_desc", kText), req("route_type", kInt), opt("route_url", kText), opt("route_color", kText),
		opt("route_text_color", kText), opt("route_sort_order", kInt),
	}},
	{"trips.txt", "trips", []column{
		req("trip_id", kText), req("route_id", kText), req("service_id", kText), opt("trip_headsign", kText),
		opt("trip_short_name", kText), opt("direction_id", kInt), opt("block_id", kText), opt("shape_id", kText),
		opt("wheelchair_accessible", kInt), opt("bikes_allowed", kInt),
	}},
	{"stops.txt", "stops", []column{
		req("stop_id", kText), opt("stop_code", kText), opt("stop_name", kText), opt("stop_desc", kText),
		opt("stop_lat", kFloat), opt("stop_lon", kFloat), opt("zone_id", kText), opt("stop_url", kText),
		opt("location_type", kInt), opt("parent_station", kText), opt("stop_timezone", kText),
		opt("wheelchair_boarding", kInt), opt("level_id", kText), opt("platform_code", kText),
	}},
	{"calendar.txt", "calendar", []column{
		req("service_id", kText), req("monday", kBool), req("tuesday", kBool), req("wednesday", kBool),
		req("thursday", kBool), req("friday", kBool), req("saturday", kBool), req("sunday", kBool),
		req("start_date", kDate), req("end_date", kDate),
	}},
	{"calendar_dates.txt", "calendar_dates", []column{
		req("service_id", kText), req("date", kDate), req("exception_type", kInt),
	}},
	// Largest file last, so small-file errors show up fast.
	{"stop_times.txt", "stop_times", []column{
		req("trip_id", kText), req("stop_sequence", kInt), req("stop_id", kText),
		opt("arrival_time", kTime), opt("departure_time", kTime), opt("stop_headsign", kText),
		opt("pickup_type", kInt), opt("drop_off_type", kInt), opt("shape_dist_traveled", kFloat), opt("timepoint", kInt),
	}},
}

// convert appends the COPY values of one CSV value to dst.
func (c column) convert(dst []any, raw string) ([]any, error) {
	if raw == "" {
		if c.required {
			return dst, fmt.Errorf("%s is empty", c.name)
		}
		dst = append(dst, nil)
		if c.kind == kTime {
			dst = append(dst, nil)
		}
		return dst, nil
	}
	switch c.kind {
	case kText:
		return append(dst, raw), nil
	case kInt:
		v, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dst, fmt.Errorf("%s %q: not an integer", c.name, raw)
		}
		return append(dst, int32(v)), nil
	case kFloat:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return dst, fmt.Errorf("%s %q: not a number", c.name, raw)
		}
		return append(dst, v), nil
	case kBool:
		switch raw {
		case "0":
			return append(dst, false), nil
		case "1":
			return append(dst, true), nil
		}
		return dst, fmt.Errorf("%s %q: want 0 or 1", c.name, raw)
	case kDate:
		v, err := time.Parse("20060102", raw)
		if err != nil {
			return dst, fmt.Errorf("%s %q: want YYYYMMDD", c.name, raw)
		}
		return append(dst, v), nil
	case kTime:
		secs, err := parseGTFSTime(raw)
		if err != nil {
			return dst, fmt.Errorf("%s %q: %w", c.name, raw, err)
		}
		return append(dst, raw, int32(secs)), nil
	}
	return dst, fmt.Errorf("%s: unknown kind %d", c.name, c.kind)
}

// parseGTFSTime parses H:MM:SS or HH:MM:SS into seconds since the start of
// the service day. Hours above 23 are valid (trips after midnight).
func parseGTFSTime(s string) (int, error) {
	h, rest, ok1 := strings.Cut(s, ":")
	m, sec, ok2 := strings.Cut(rest, ":")
	if !ok1 || !ok2 || len(h) == 0 || len(h) > 3 || len(m) != 2 || len(sec) != 2 {
		return 0, fmt.Errorf("want H:MM:SS")
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	ss, err3 := strconv.Atoi(sec)
	if err1 != nil || err2 != nil || err3 != nil || hh < 0 || mm > 59 || ss > 59 || mm < 0 || ss < 0 {
		return 0, fmt.Errorf("want H:MM:SS")
	}
	return hh*3600 + mm*60 + ss, nil
}
