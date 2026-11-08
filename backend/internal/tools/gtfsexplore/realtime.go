package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

const sampleTimeLayout = "20060102T150405Z"

// record fetches n samples of the GTFS-RT feed and stores them unchanged.
// After the first sample it sends one conditional GET to check ETag support.
// Requests: n + 1, far below the upstream limit of 60 per minute.
func record(w *printer, url string, n int, interval time.Duration, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	for i := 0; i < n; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		etag, err := fetchSample(w, client, url, dir)
		if err != nil {
			return err
		}
		if i == 0 && etag != "" {
			if err := conditionalGet(w, client, url, etag); err != nil {
				return err
			}
		}
	}
	return nil
}

func fetchSample(w *printer, client *http.Client, url, dir string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	at := time.Now().UTC()
	path := filepath.Join(dir, "vbb-"+at.Format(sampleTimeLayout)+".pb")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	w.printf("sample %s: %d bytes in %s, ETag=%s, Last-Modified=%s, Content-Type=%q, Content-Encoding=%q (transport decompressed: %v)\n",
		path, len(body), time.Since(start).Round(time.Millisecond), resp.Header.Get("ETag"),
		resp.Header.Get("Last-Modified"), resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding"), resp.Uncompressed)
	return resp.Header.Get("ETag"), nil
}

func conditionalGet(w *printer, client *http.Client, url, etag string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("If-None-Match", etag)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	n, _ := io.Copy(io.Discard, resp.Body)
	w.printf("conditional GET (If-None-Match %s): %s, %d body bytes, ETag=%s\n", etag, resp.Status, n, resp.Header.Get("ETag"))
	return nil
}

func readFeed(path string) (*gtfs.FeedMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var msg gtfs.FeedMessage
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &msg, nil
}

// sampleTime takes the fetch time from the file name (vbb-<UTC>[-suffix].pb).
func sampleTime(path string) (time.Time, bool) {
	base := strings.TrimSuffix(filepath.Base(path), ".pb")
	for _, part := range strings.Split(base, "-") {
		if t, err := time.Parse(sampleTimeLayout, part); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

type rtStats struct {
	entities, tripUpdates, vehicles, alerts, deleted                int
	tripIDSet, routeIDSet, startDateSet, startTimeSet, directionSet int
	tripMatched, routeMatched                                       int
	tripRel, stuRel, startDates                                     counter
	stus, stuStopID, stuSeq, stuArrDelay, stuArrTime                int
	stuDepDelay, stuDepTime                                         int
	stuPole, stuStation, stuOtherType, stuUnknown                   int
	unknownStops, unknownTrips                                      counter
	tuDelaySet, tuTimestampSet, vehicleSet                          int
	startDateVsFirstDep                                             counter
	delays                                                          []int32
	modes                                                           counter
}

func reportRealtime(w *printer, zipPath string, files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no sample files given")
	}
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	stops, err := loadStops(z)
	if err != nil {
		return err
	}
	trips, err := loadTrips(z)
	if err != nil {
		return err
	}
	routes, err := loadRoutes(z)
	if err != nil {
		return err
	}
	firstDep, err := loadFirstDepartures(z)
	if err != nil {
		return err
	}

	st := &rtStats{tripRel: counter{}, stuRel: counter{}, startDates: counter{}, unknownStops: counter{},
		unknownTrips: counter{}, startDateVsFirstDep: counter{}, modes: counter{}}
	section(w, "samples")
	sort.Strings(files)
	for _, path := range files {
		msg, err := readFeed(path)
		if err != nil {
			return err
		}
		h := msg.GetHeader()
		feedTime := time.Unix(int64(h.GetTimestamp()), 0).UTC()
		age := "?"
		if at, ok := sampleTime(path); ok {
			age = at.Sub(feedTime).Round(time.Second).String()
		}
		w.printf("  %s: version=%s incrementality=%s header.timestamp=%s (age at fetch %s), entities=%d\n",
			filepath.Base(path), h.GetGtfsRealtimeVersion(), h.GetIncrementality(), feedTime.Format(time.RFC3339), age, len(msg.GetEntity()))
		st.add(msg, stops, trips, routes, firstDep)
	}
	st.print(w)
	return nil
}

func (st *rtStats) add(msg *gtfs.FeedMessage, stops map[string]*stop, trips map[string]*trip, routes map[string]*route, firstDep map[string]int) {
	for _, e := range msg.GetEntity() {
		st.entities++
		if e.GetIsDeleted() {
			st.deleted++
		}
		if e.GetVehicle() != nil {
			st.vehicles++
		}
		if e.GetAlert() != nil {
			st.alerts++
		}
		tu := e.GetTripUpdate()
		if tu == nil {
			continue
		}
		st.tripUpdates++
		td := tu.GetTrip()
		if td.TripId != nil {
			st.tripIDSet++
		}
		if td.RouteId != nil {
			st.routeIDSet++
		}
		if td.StartDate != nil {
			st.startDateSet++
			st.startDates[td.GetStartDate()]++
		}
		if td.StartTime != nil {
			st.startTimeSet++
		}
		if td.DirectionId != nil {
			st.directionSet++
		}
		st.tripRel[td.GetScheduleRelationship().String()]++
		if t, ok := trips[td.GetTripId()]; ok {
			st.tripMatched++
			if t.routeID == td.GetRouteId() {
				st.routeMatched++
			}
			if r, ok := routes[t.routeID]; ok {
				st.modes[r.routeType]++
			}
			if dep, ok := firstDep[td.GetTripId()]; ok && td.StartDate != nil {
				kind := "first departure <24:00"
				if dep >= 24*3600 {
					kind = "first departure ≥24:00"
				}
				st.startDateVsFirstDep[kind+", start_time="+td.GetStartTime()+" vs static "+fmtSecs(dep)+" equal="+fmt.Sprint(td.GetStartTime() == fmtSecs(dep))]++
			}
		} else if len(st.unknownTrips) < 5 {
			st.unknownTrips[td.GetTripId()]++
		}
		if tu.Delay != nil {
			st.tuDelaySet++
		}
		if tu.Timestamp != nil {
			st.tuTimestampSet++
		}
		if tu.GetVehicle() != nil {
			st.vehicleSet++
		}
		for _, stu := range tu.GetStopTimeUpdate() {
			st.stus++
			st.stuRel[stu.GetScheduleRelationship().String()]++
			if stu.StopSequence != nil {
				st.stuSeq++
			}
			if a := stu.GetArrival(); a != nil {
				if a.Delay != nil {
					st.stuArrDelay++
					st.delays = append(st.delays, a.GetDelay())
				}
				if a.Time != nil {
					st.stuArrTime++
				}
			}
			if d := stu.GetDeparture(); d != nil {
				if d.Delay != nil {
					st.stuDepDelay++
				}
				if d.Time != nil {
					st.stuDepTime++
				}
			}
			if stu.StopId == nil {
				continue
			}
			st.stuStopID++
			s, ok := stops[stu.GetStopId()]
			switch {
			case !ok:
				st.stuUnknown++
				st.unknownStops[stu.GetStopId()]++
			case s.locType == "" || s.locType == "0":
				st.stuPole++
			case s.locType == "1":
				st.stuStation++
			default:
				st.stuOtherType++
			}
		}
	}
}

func (st *rtStats) print(w *printer) {
	section(w, "entities (all samples)")
	w.printf("  entities=%d trip_update=%d vehicle=%d alert=%d is_deleted=%d\n", st.entities, st.tripUpdates, st.vehicles, st.alerts, st.deleted)
	tu := st.tripUpdates
	section(w, "TripDescriptor")
	w.printf("  trip_id set %s, route_id %s, start_date %s, start_time %s, direction_id %s\n",
		pct(st.tripIDSet, tu), pct(st.routeIDSet, tu), pct(st.startDateSet, tu), pct(st.startTimeSet, tu), pct(st.directionSet, tu))
	w.printf("  trip_id found in static trips.txt: %s (%d); route_id equal to static: %s\n", pct(st.tripMatched, tu), st.tripMatched, pct(st.routeMatched, st.tripMatched))
	w.printf("  unknown trip_id examples: %s\n", st.unknownTrips.format(5))
	w.printf("  schedule_relationship: %s\n", st.tripRel.format(0))
	w.printf("  start_date values: %s\n", st.startDates.format(5))
	w.printf("  matched trips by static route_type: %s\n", st.modes.format(0))
	w.printf("  start_time vs static first departure (top): %s\n", st.startDateVsFirstDep.format(6))
	w.printf("  TripUpdate.delay set %s, timestamp set %s, vehicle set %s\n", pct(st.tuDelaySet, tu), pct(st.tuTimestampSet, tu), pct(st.vehicleSet, tu))

	section(w, "StopTimeUpdate")
	w.printf("  count=%d (%.1f per trip_update)\n", st.stus, float64(st.stus)/float64(max(tu, 1)))
	w.printf("  stop_id set %s, stop_sequence set %s\n", pct(st.stuStopID, st.stus), pct(st.stuSeq, st.stus))
	w.printf("  arrival.delay %s, arrival.time %s, departure.delay %s, departure.time %s\n",
		pct(st.stuArrDelay, st.stus), pct(st.stuArrTime, st.stus), pct(st.stuDepDelay, st.stus), pct(st.stuDepTime, st.stus))
	w.printf("  stop_id → static pole %s, station %s, other location_type %s, unknown %s\n",
		pct(st.stuPole, st.stuStopID), pct(st.stuStation, st.stuStopID), pct(st.stuOtherType, st.stuStopID), pct(st.stuUnknown, st.stuStopID))
	w.printf("  unknown stop_id examples: %s\n", st.unknownStops.format(5))
	w.printf("  schedule_relationship: %s\n", st.stuRel.format(0))
	if len(st.delays) > 0 {
		d := append([]int32(nil), st.delays...)
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		q := func(p float64) int32 { return d[int(p*float64(len(d)-1))] }
		w.printf("  arrival.delay seconds: min=%d p10=%d median=%d p90=%d max=%d\n", d[0], q(0.1), q(0.5), q(0.9), d[len(d)-1])
	}
}

// loadFirstDepartures returns the departure time of each trip's first stop.
func loadFirstDepartures(z *zip.ReadCloser) (map[string]int, error) {
	type first struct{ seq, dep int }
	firsts := map[string]first{}
	_, err := scan(z, "stop_times.txt", func(get func(string) string) error {
		var seq int
		_, _ = fmt.Sscan(get("stop_sequence"), &seq)
		id := get("trip_id")
		if f, ok := firsts[id]; ok && f.seq <= seq {
			return nil
		}
		dep, _ := parseGTFSTime(get("departure_time"))
		firsts[id] = first{seq, dep}
		return nil
	})
	out := make(map[string]int, len(firsts))
	for id, f := range firsts {
		out[id] = f.dep
	}
	return out, err
}

// trim keeps only the TripUpdates that touch a stop within radius of the
// center (pole or parent station coordinates), so fixtures stay small.
func trim(w *printer, zipPath string, files []string, dir string, lat, lon, radius float64) error {
	if len(files) == 0 {
		return fmt.Errorf("no sample files given")
	}
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	stops, err := loadStops(z)
	if err != nil {
		return err
	}
	near := map[string]bool{}
	for id, s := range stops {
		if distanceMeters(lat, lon, s.lat, s.lon) <= radius {
			near[id] = true
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w.printf("stops within %.0f m of %.4f,%.4f: %d\n", radius, lat, lon, len(near))
	sort.Strings(files)
	for _, path := range files {
		msg, err := readFeed(path)
		if err != nil {
			return err
		}
		kept := msg.GetEntity()[:0:0]
		for _, e := range msg.GetEntity() {
			tu := e.GetTripUpdate()
			if tu == nil {
				continue
			}
			for _, stu := range tu.GetStopTimeUpdate() {
				if near[stu.GetStopId()] || near[stationOf(stops, stu.GetStopId())] {
					kept = append(kept, e)
					break
				}
			}
		}
		out := &gtfs.FeedMessage{Header: msg.GetHeader(), Entity: kept}
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(out)
		if err != nil {
			return err
		}
		if len(raw) > 5<<20 {
			return fmt.Errorf("%s: trimmed fixture is %d bytes, over the 5 MB limit (CLAUDE.md rule 11)", path, len(raw))
		}
		dst := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), ".pb")+"-wedding.pb")
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
		w.printf("%s: kept %d of %d entities → %s (%d bytes)\n", filepath.Base(path), len(kept), len(msg.GetEntity()), dst, len(raw))
	}
	return nil
}
