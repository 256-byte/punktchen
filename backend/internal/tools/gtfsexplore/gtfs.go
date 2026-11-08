package main

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// table streams one CSV file of a GTFS zip.
type table struct {
	rc   io.ReadCloser
	r    *csv.Reader
	cols map[string]int
}

func openTable(z *zip.ReadCloser, name string) (*table, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		r := csv.NewReader(rc)
		r.FieldsPerRecord = -1
		r.ReuseRecord = true
		header, err := r.Read()
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("%s: header: %w", name, err)
		}
		cols := make(map[string]int, len(header))
		for i, h := range header {
			cols[strings.TrimPrefix(strings.TrimSpace(h), string(rune(0xFEFF)))] = i // UTF-8 BOM
		}
		return &table{rc: rc, r: r, cols: cols}, nil
	}
	return nil, fmt.Errorf("%s not in zip", name)
}

// each calls fn for every row; get(col) returns "" for missing columns.
func (t *table) each(fn func(get func(string) string) error) (int, error) {
	defer func() { _ = t.rc.Close() }()
	var rec []string
	get := func(col string) string {
		if i, ok := t.cols[col]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	rows := 0
	for {
		var err error
		rec, err = t.r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return rows, err
		}
		rows++
		if err := fn(get); err != nil {
			return rows, err
		}
	}
}

func scan(z *zip.ReadCloser, name string, fn func(get func(string) string) error) (int, error) {
	t, err := openTable(z, name)
	if err != nil {
		return 0, err
	}
	return t.each(fn)
}

type stop struct {
	name, locType, parent, platform string
	lat, lon                        float64
}

func loadStops(z *zip.ReadCloser) (map[string]*stop, error) {
	stops := map[string]*stop{}
	_, err := scan(z, "stops.txt", func(get func(string) string) error {
		lat, _ := strconv.ParseFloat(get("stop_lat"), 64)
		lon, _ := strconv.ParseFloat(get("stop_lon"), 64)
		stops[get("stop_id")] = &stop{
			name: get("stop_name"), locType: get("location_type"), parent: get("parent_station"),
			platform: get("platform_code"), lat: lat, lon: lon,
		}
		return nil
	})
	return stops, err
}

// stationOf returns the parent station id, or the stop itself without one.
func stationOf(stops map[string]*stop, id string) string {
	if s, ok := stops[id]; ok && s.parent != "" {
		return s.parent
	}
	return id
}

type trip struct {
	routeID, serviceID, headsign, direction string
}

func loadTrips(z *zip.ReadCloser) (map[string]*trip, error) {
	trips := map[string]*trip{}
	_, err := scan(z, "trips.txt", func(get func(string) string) error {
		trips[get("trip_id")] = &trip{
			routeID: get("route_id"), serviceID: get("service_id"),
			headsign: get("trip_headsign"), direction: get("direction_id"),
		}
		return nil
	})
	return trips, err
}

type route struct {
	shortName, longName, routeType, color, agency string
}

func loadRoutes(z *zip.ReadCloser) (map[string]*route, error) {
	routes := map[string]*route{}
	_, err := scan(z, "routes.txt", func(get func(string) string) error {
		routes[get("route_id")] = &route{
			shortName: get("route_short_name"), longName: get("route_long_name"),
			routeType: get("route_type"), color: get("route_color"), agency: get("agency_id"),
		}
		return nil
	})
	return routes, err
}

// tripEnds is the first and last stop of a trip, by stop_sequence.
type tripEnds struct {
	minSeq, maxSeq       int
	first, last          string
	firstDep, lastArr    int // seconds since service-day start
	firstDepRaw, lastRaw string
}

// parseGTFSTime parses H:MM:SS / HH:MM:SS, also above 24:00:00.
func parseGTFSTime(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return h*3600 + m*60 + sec, true
}

func fmtSecs(s int) string {
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// idPattern replaces digit runs by "9", e.g. de:11000:900009104::1 → de:9:9::9.
func idPattern(id string) string {
	var b strings.Builder
	inDigits := false
	for _, r := range id {
		if r >= '0' && r <= '9' {
			if !inDigits {
				b.WriteByte('9')
			}
			inDigits = true
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}
	return b.String()
}

// counter counts string keys and prints them sorted by count.
type counter map[string]int

func (c counter) top(n int) []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if c[keys[i]] != c[keys[j]] {
			return c[keys[i]] > c[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if n > 0 && len(keys) > n {
		keys = keys[:n]
	}
	return keys
}

func (c counter) format(n int) string {
	var parts []string
	for _, k := range c.top(n) {
		label := k
		if label == "" {
			label = "(empty)"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", label, c[k]))
	}
	if n > 0 && len(c) > n {
		parts = append(parts, fmt.Sprintf("… (%d distinct)", len(c)))
	}
	return strings.Join(parts, ", ")
}

func pct(part, total int) string {
	if total == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(total))
}

// distanceMeters is the haversine distance.
func distanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
