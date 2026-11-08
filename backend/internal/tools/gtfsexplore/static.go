package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/256-byte/punktchen/backend/internal/config"
	"github.com/256-byte/punktchen/backend/internal/geo"
)

func download(w *printer, url, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	start := time.Now()
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	w.printf("downloaded %s → %s\n", url, dst)
	w.printf("  bytes=%d in %s\n  ETag=%s\n  Last-Modified=%s\n  Content-Type=%s\n  sha256=%s\n",
		n, time.Since(start).Round(time.Millisecond), resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"),
		resp.Header.Get("Content-Type"), hex.EncodeToString(h.Sum(nil)))
	return nil
}

func reportStatic(w *printer, zipPath string) error {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	start := time.Now()

	section(w, "files")
	for _, f := range z.File {
		w.printf("  %-22s %12d bytes uncompressed, modified %s\n", f.Name, f.UncompressedSize64, f.Modified.Format(time.DateOnly))
	}

	if err := reportFeedInfo(w, z); err != nil {
		return err
	}
	if err := reportAgencies(w, z); err != nil {
		return err
	}
	stops, err := loadStops(z)
	if err != nil {
		return err
	}
	reportStops(w, stops)
	reportDHID(w, stops)

	routes, err := loadRoutes(z)
	if err != nil {
		return err
	}
	reportRoutes(w, routes)

	trips, err := loadTrips(z)
	if err != nil {
		return err
	}
	reportTrips(w, trips, routes)

	ends, err := reportStopTimes(w, z, stops, trips, routes)
	if err != nil {
		return err
	}
	reportLoops(w, stops, trips, routes, ends)

	if err := reportCalendar(w, z); err != nil {
		return err
	}
	w.printf("\n(report took %s)\n", time.Since(start).Round(time.Millisecond))
	return nil
}

func section(w *printer, title string) {
	w.printf("\n== %s\n", title)
}

func reportFeedInfo(w *printer, z *zip.ReadCloser) error {
	section(w, "feed_info.txt")
	t, err := openTable(z, "feed_info.txt")
	if err != nil {
		w.printf("  %v\n", err)
		return nil
	}
	cols := make([]string, 0, len(t.cols))
	for c := range t.cols {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	_, err = t.each(func(get func(string) string) error {
		for _, c := range cols {
			w.printf("  %s=%q\n", c, get(c))
		}
		return nil
	})
	return err
}

func reportAgencies(w *printer, z *zip.ReadCloser) error {
	section(w, "agency.txt")
	names := counter{}
	n, err := scan(z, "agency.txt", func(get func(string) string) error {
		names[get("agency_id")+" "+get("agency_name")]++
		return nil
	})
	w.printf("  rows=%d: %s\n", n, names.format(12))
	return err
}

func reportStops(w *printer, stops map[string]*stop) {
	section(w, "stops.txt")
	byType := counter{}
	patterns := map[string]counter{}
	samples := map[string][]string{}
	platform := map[string][2]int{} // locType → {with platform_code, total}
	poleParent := counter{}
	children := map[string]int{}
	ids := make([]string, 0, len(stops))
	for id := range stops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := stops[id]
		byType[s.locType]++
		if patterns[s.locType] == nil {
			patterns[s.locType] = counter{}
		}
		patterns[s.locType][idPattern(id)]++
		if len(samples[s.locType]) < 4 && len(ids) > 0 {
			samples[s.locType] = append(samples[s.locType], fmt.Sprintf("%s %q parent=%q platform=%q", id, s.name, s.parent, s.platform))
		}
		p := platform[s.locType]
		if s.platform != "" {
			p[0]++
		}
		p[1]++
		platform[s.locType] = p
		if s.locType == "" || s.locType == "0" {
			switch parent, ok := stops[s.parent]; {
			case s.parent == "":
				poleParent["no parent"]++
			case !ok:
				poleParent["parent missing in stops.txt"]++
			default:
				poleParent["parent location_type="+parent.locType]++
				children[s.parent]++
			}
		}
	}
	w.printf("  rows=%d, by location_type: %s\n", len(stops), byType.format(0))
	for _, lt := range byType.top(0) {
		w.printf("  location_type=%q: id patterns %s\n", lt, patterns[lt].format(6))
		w.printf("    platform_code set: %s of %d\n", pct(platform[lt][0], platform[lt][1]), platform[lt][1])
		for _, s := range samples[lt] {
			w.printf("    e.g. %s\n", s)
		}
	}
	w.printf("  poles (location_type 0/empty): %s\n", poleParent.format(0))
	dist := counter{}
	for _, n := range children {
		switch {
		case n == 1:
			dist["1"]++
		case n <= 4:
			dist["2-4"]++
		case n <= 10:
			dist["5-10"]++
		default:
			dist[">10"]++
		}
	}
	w.printf("  poles per station: %s\n", dist.format(0))

	// Scope (DATA_MODEL §4): poles inside the default bbox, stations with any such child.
	bbox, err := geo.ParseBBox(config.DefaultScopeBBox)
	if err != nil {
		w.printf("  bbox: %v\n", err)
		return
	}
	inPoles, inStations := 0, map[string]bool{}
	for _, id := range ids {
		s := stops[id]
		if s.locType != "" && s.locType != "0" {
			continue
		}
		if s.lon >= bbox.MinLon && s.lon <= bbox.MaxLon && s.lat >= bbox.MinLat && s.lat <= bbox.MaxLat {
			inPoles++
			inStations[stationOf(stops, id)] = true
		}
	}
	w.printf("  in default bbox %s: poles=%d, stations (or parentless poles)=%d\n", bbox, inPoles, len(inStations))
}

func reportRoutes(w *printer, routes map[string]*route) {
	section(w, "routes.txt")
	byType := counter{}
	names := map[string][]string{}
	colored := counter{}
	noColor := 0
	for _, r := range routes {
		byType[r.routeType]++
		names[r.routeType] = append(names[r.routeType], r.shortName)
		if r.color == "" {
			noColor++
		} else {
			colored[r.routeType]++
		}
	}
	w.printf("  rows=%d, route_color empty: %s\n", len(routes), pct(noColor, len(routes)))
	for _, rt := range byType.top(0) {
		ns := names[rt]
		sort.Strings(ns)
		if len(ns) > 12 {
			ns = append(ns[:12], "…")
		}
		w.printf("  route_type=%s: %d routes, route_color set %s, e.g. %s\n", rt, byType[rt], pct(colored[rt], byType[rt]), strings.Join(ns, " "))
	}
	for id, r := range routes {
		if r.shortName == "S41" || r.shortName == "S42" || r.shortName == "U6" || r.shortName == "M27" {
			w.printf("  sample route %s: short=%q long=%q type=%s color=%q agency=%s\n", id, r.shortName, r.longName, r.routeType, r.color, r.agency)
		}
	}
}

func reportTrips(w *printer, trips map[string]*trip, routes map[string]*route) {
	section(w, "trips.txt")
	dir := counter{}
	dirByType := map[string]counter{}
	tripsByType := counter{}
	patterns := counter{}
	noHeadsign := 0
	var sampleIDs []string
	for id, t := range trips {
		dir[t.direction]++
		rt := "?"
		if r, ok := routes[t.routeID]; ok {
			rt = r.routeType
		}
		tripsByType[rt]++
		if dirByType[rt] == nil {
			dirByType[rt] = counter{}
		}
		dirByType[rt][t.direction]++
		patterns[idPattern(id)]++
		if t.headsign == "" {
			noHeadsign++
		}
		if len(sampleIDs) < 3 {
			sampleIDs = append(sampleIDs, fmt.Sprintf("trip_id=%s service_id=%s route_id=%s", id, t.serviceID, t.routeID))
		}
	}
	w.printf("  rows=%d, direction_id: %s, trip_headsign empty: %s\n", len(trips), dir.format(0), pct(noHeadsign, len(trips)))
	w.printf("  trip_id patterns: %s\n", patterns.format(5))
	for _, s := range sampleIDs {
		w.printf("    e.g. %s\n", s)
	}
	for _, rt := range tripsByType.top(0) {
		w.printf("  route_type=%s: trips=%d, direction_id %s\n", rt, tripsByType[rt], dirByType[rt].format(0))
	}
}

func reportStopTimes(w *printer, z *zip.ReadCloser, stops map[string]*stop, trips map[string]*trip, routes map[string]*route) (map[string]*tripEnds, error) {
	section(w, "stop_times.txt")
	ends := make(map[string]*tripEnds, len(trips))
	poleTypes := map[string]map[string]bool{}
	stopLoc := counter{}
	pickup, dropOff := counter{}, counter{}
	maxDep, maxArr := -1, -1
	var maxDepTrip, maxDepRaw string
	over24, over27, over30, badTime := 0, 0, 0, 0

	n, err := scan(z, "stop_times.txt", func(get func(string) string) error {
		tripID, stopID := get("trip_id"), get("stop_id")
		seq, _ := strconv.Atoi(get("stop_sequence"))
		depRaw, arrRaw := get("departure_time"), get("arrival_time")
		dep, okDep := parseGTFSTime(depRaw)
		arr, okArr := parseGTFSTime(arrRaw)
		if !okDep || !okArr {
			badTime++
		}
		if dep > maxDep {
			maxDep, maxDepTrip, maxDepRaw = dep, tripID, depRaw
		}
		if arr > maxArr {
			maxArr = arr
		}
		switch {
		case dep >= 30*3600:
			over30++
			fallthrough
		case dep >= 27*3600:
			over27++
			fallthrough
		case dep >= 24*3600:
			over24++
		}
		pickup[get("pickup_type")]++
		dropOff[get("drop_off_type")]++
		if s, ok := stops[stopID]; ok {
			stopLoc[s.locType]++
		} else {
			stopLoc["(not in stops.txt)"]++
		}
		if t, ok := trips[tripID]; ok {
			if r, ok := routes[t.routeID]; ok {
				if poleTypes[stopID] == nil {
					poleTypes[stopID] = map[string]bool{}
				}
				poleTypes[stopID][r.routeType] = true
			}
		}
		e := ends[tripID]
		if e == nil {
			e = &tripEnds{minSeq: seq, maxSeq: seq, first: stopID, last: stopID, firstDep: dep, lastArr: arr, firstDepRaw: depRaw, lastRaw: arrRaw}
			ends[tripID] = e
		}
		if seq < e.minSeq {
			e.minSeq, e.first, e.firstDep, e.firstDepRaw = seq, stopID, dep, depRaw
		}
		if seq > e.maxSeq {
			e.maxSeq, e.last, e.lastArr, e.lastRaw = seq, stopID, arr, arrRaw
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	w.printf("  rows=%d, trips with stop_times=%d, unparseable times=%d\n", n, len(ends), badTime)
	w.printf("  max departure_time=%s (%s, trip %s), max arrival=%s\n", maxDepRaw, fmtSecs(maxDep), maxDepTrip, fmtSecs(maxArr))
	w.printf("  departures ≥24:00: %d (%s), ≥27:00: %d, ≥30:00: %d\n", over24, pct(over24, n), over27, over30)
	w.printf("  stop location_type used: %s\n", stopLoc.format(0))
	w.printf("  pickup_type: %s\n  drop_off_type: %s\n", pickup.format(0), dropOff.format(0))

	firstAfter24 := 0
	for _, e := range ends {
		if e.firstDep >= 24*3600 {
			firstAfter24++
		}
	}
	w.printf("  trips starting ≥24:00: %d\n", firstAfter24)

	// platform_code coverage of used poles, per route_type.
	cov := map[string][2]int{}
	for stopID, types := range poleTypes {
		s, ok := stops[stopID]
		if !ok {
			continue
		}
		for rt := range types {
			c := cov[rt]
			if s.platform != "" {
				c[0]++
			}
			c[1]++
			cov[rt] = c
		}
	}
	types := make([]string, 0, len(cov))
	for rt := range cov {
		types = append(types, rt)
	}
	sort.Strings(types)
	for _, rt := range types {
		w.printf("  platform_code on poles served by route_type=%s: %s of %d\n", rt, pct(cov[rt][0], cov[rt][1]), cov[rt][1])
	}
	return ends, nil
}

// reportLoops finds trips that end at the station where they started (ring
// and loop lines) and prints their headsigns.
func reportLoops(w *printer, stops map[string]*stop, trips map[string]*trip, routes map[string]*route, ends map[string]*tripEnds) {
	section(w, "loops / ring lines (first station == last station)")
	type agg struct {
		total, loops int
		headsigns    counter
		endpoints    counter
	}
	byLine := map[string]*agg{}
	for id, t := range trips {
		r, ok := routes[t.routeID]
		e, ok2 := ends[id]
		if !ok || !ok2 {
			continue
		}
		key := r.shortName + " (type " + r.routeType + ")"
		a := byLine[key]
		if a == nil {
			a = &agg{headsigns: counter{}, endpoints: counter{}}
			byLine[key] = a
		}
		a.total++
		isLoop := stationOf(stops, e.first) == stationOf(stops, e.last)
		if isLoop {
			a.loops++
		}
		if isLoop || r.shortName == "S41" || r.shortName == "S42" {
			a.headsigns[t.headsign+" [dir "+t.direction+"]"]++
			a.endpoints[name(stops, e.first)+" → "+name(stops, e.last)]++
		}
	}
	keys := make([]string, 0, len(byLine))
	for k, a := range byLine {
		if a.loops > 0 || strings.HasPrefix(k, "S41 ") || strings.HasPrefix(k, "S42 ") {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return byLine[keys[i]].loops > byLine[keys[j]].loops })
	w.printf("  lines with loop trips: %d\n", len(keys))
	for i, k := range keys {
		if i >= 25 {
			w.printf("  … %d more\n", len(keys)-i)
			break
		}
		a := byLine[k]
		w.printf("  %s: %d of %d trips loop\n    headsigns: %s\n    first→last: %s\n", k, a.loops, a.total, a.headsigns.format(4), a.endpoints.format(3))
	}
}

func name(stops map[string]*stop, id string) string {
	st := stationOf(stops, id)
	if s, ok := stops[st]; ok {
		return s.name
	}
	return id
}

func reportCalendar(w *printer, z *zip.ReadCloser) error {
	section(w, "calendar.txt / calendar_dates.txt")
	minStart, maxEnd := "99999999", "00000000"
	ends := counter{}
	n, err := scan(z, "calendar.txt", func(get func(string) string) error {
		if s := get("start_date"); s < minStart {
			minStart = s
		}
		if e := get("end_date"); e > maxEnd {
			maxEnd = e
		}
		ends[get("end_date")]++
		return nil
	})
	if err != nil {
		return err
	}
	w.printf("  calendar rows=%d, min start_date=%s, max end_date=%s, end_date values: %s\n", n, minStart, maxEnd, ends.format(5))
	minD, maxD := "99999999", "00000000"
	exc := counter{}
	n, err = scan(z, "calendar_dates.txt", func(get func(string) string) error {
		d := get("date")
		if d < minD {
			minD = d
		}
		if d > maxD {
			maxD = d
		}
		exc[get("exception_type")]++
		return nil
	})
	w.printf("  calendar_dates rows=%d, dates %s … %s, exception_type: %s\n", n, minD, maxD, exc.format(0))
	return err
}
