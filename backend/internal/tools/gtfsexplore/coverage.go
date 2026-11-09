package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"github.com/jackc/pgx/v5"

	"github.com/256-byte/punktchen/backend/internal/platform"
)

// defaultDevDB is the Docker Compose database as seen from the host.
const defaultDevDB = "postgres://livestop:livestop@127.0.0.1:5432/livestop?sslmode=disable"

// modeOrder is the API's mode order (DATA_MODEL §5.2).
var modeOrder = []string{"regional", "sbahn", "ubahn", "tram", "bus", "ferry", "other"}

// coverage reports, per mode, how many trips running at each sample's
// header time have realtime data (task 4.0). A trip counts as realtime when
// its TripUpdate has a real delay: an arrival/departure delay or
// TripUpdate.delay. Times without a delay are filled in from the schedule
// (DATA_MODEL §7 #18). Running trips come from the imported schedule in the
// dev database: only routes that serve an in-scope pole.
func coverage(w *printer, dbURL string, files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("no sample files given")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to the dev database: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	modes, err := tripModes(ctx, conn)
	if err != nil {
		return err
	}
	section(w, "realtime coverage of running trips (in-scope routes)")
	w.printf("  realtime = TripUpdate with an arrival/departure delay or TripUpdate.delay; running = scheduled between first departure and last arrival\n")
	sort.Strings(files)
	total := map[string]*modeCount{}
	for _, path := range files {
		msg, err := readFeed(path)
		if err != nil {
			return err
		}
		at := time.Unix(int64(msg.GetHeader().GetTimestamp()), 0)
		running, err := runningTrips(ctx, conn, at)
		if err != nil {
			return err
		}
		counts := countCoverage(msg, modes, running)
		w.printf("\n  %s (header %s, Berlin %s)\n", filepath.Base(path), at.UTC().Format(time.RFC3339), at.In(platform.Berlin).Format("Mon 15:04"))
		printCoverage(w, counts)
		for m, c := range counts {
			if total[m] == nil {
				total[m] = &modeCount{}
			}
			total[m].add(c)
		}
	}
	if len(files) > 1 {
		w.printf("\n  all samples\n")
		printCoverage(w, total)
	}
	return nil
}

type modeCount struct {
	running    int // scheduled to run at the header time
	updated    int // running, with a TripUpdate
	realtime   int // running, with a real delay
	cancelled  int // running, TripUpdate CANCELED
	extraTrips int // TripUpdates of this mode for trips not running now (before start, after end)
	stus       int // stop time updates of running, updated trips
	stuDelay   int // ... with an arrival or departure delay
	stuNonZero int // ... with a delay other than 0
	nonZero    int // running trips with at least one delay other than 0
}

func (c *modeCount) add(o *modeCount) {
	c.running += o.running
	c.updated += o.updated
	c.realtime += o.realtime
	c.cancelled += o.cancelled
	c.extraTrips += o.extraTrips
	c.stus += o.stus
	c.stuDelay += o.stuDelay
	c.stuNonZero += o.stuNonZero
	c.nonZero += o.nonZero
}

// countCoverage matches one feed against the running trips.
func countCoverage(msg *gtfs.FeedMessage, modes map[string]string, running map[string]bool) map[string]*modeCount {
	counts := map[string]*modeCount{}
	get := func(mode string) *modeCount {
		if counts[mode] == nil {
			counts[mode] = &modeCount{}
		}
		return counts[mode]
	}
	for id := range running {
		if mode := modes[id]; mode != "" {
			get(mode).running++
		}
	}
	seen := map[string]bool{}
	match := get(matchKey)
	for _, e := range msg.GetEntity() {
		tu := e.GetTripUpdate()
		if tu == nil {
			continue
		}
		id := tu.GetTrip().GetTripId()
		mode, known := modes[id]
		match.running++ // here: TripUpdates seen
		if known {
			match.updated++ // here: trip_id in the imported schedule
		}
		if mode == "" || seen[id] {
			continue // unknown, out of scope, or a second update of the same trip
		}
		seen[id] = true
		c := get(mode)
		if !running[id] {
			c.extraTrips++
			continue
		}
		c.updated++
		if tu.GetTrip().GetScheduleRelationship() == gtfs.TripDescriptor_CANCELED {
			c.cancelled++
		}
		if hasDelay(tu) {
			c.realtime++
		}
		moved := tu.GetDelay() != 0
		for _, stu := range tu.GetStopTimeUpdate() {
			c.stus++
			for _, ev := range []*gtfs.TripUpdate_StopTimeEvent{stu.GetArrival(), stu.GetDeparture()} {
				if ev == nil || ev.Delay == nil {
					continue
				}
				c.stuDelay++
				if ev.GetDelay() != 0 {
					c.stuNonZero++
					moved = true
				}
				break // count each stop time update once
			}
		}
		if moved {
			c.nonZero++
		}
	}
	return counts
}

func hasDelay(tu *gtfs.TripUpdate) bool {
	if tu.Delay != nil {
		return true
	}
	for _, stu := range tu.GetStopTimeUpdate() {
		if a := stu.GetArrival(); a != nil && a.Delay != nil {
			return true
		}
		if d := stu.GetDeparture(); d != nil && d.Delay != nil {
			return true
		}
	}
	return false
}

// matchKey holds, in counts, how many TripUpdates (running) have a trip_id
// of the imported schedule (updated).
const matchKey = "_match"

func printCoverage(w *printer, counts map[string]*modeCount) {
	if m := counts[matchKey]; m != nil {
		w.printf("    TripUpdates %d, trip_id in the imported schedule %s\n", m.running, pct(m.updated, m.running))
	}
	w.printf("    %-9s %8s %10s %9s %10s %9s %13s\n", "mode", "running", "TripUpd.", "realtime", "% realtime", "cancelled", "TU not running")
	sum := &modeCount{}
	for _, m := range modeOrder {
		c := counts[m]
		if c == nil {
			continue
		}
		sum.add(c)
		w.printf("    %-9s %8d %10d %9d %10s %9d %13d\n", m, c.running, c.updated, c.realtime, pct(c.realtime, c.running), c.cancelled, c.extraTrips)
	}
	w.printf("    %-9s %8d %10d %9d %10s %9d %13d\n", "all", sum.running, sum.updated, sum.realtime, pct(sum.realtime, sum.running), sum.cancelled, sum.extraTrips)
	w.printf("    %-9s %14s %16s %22s\n", "mode", "STUs w/ delay", "STUs delay ≠ 0", "running w/ delay ≠ 0")
	for _, m := range append(slices.Clone(modeOrder), "all") {
		c := counts[m]
		if m == "all" {
			c = sum
		}
		if c == nil {
			continue
		}
		w.printf("    %-9s %14s %16s %22s\n", m, pct(c.stuDelay, c.stus), pct(c.stuNonZero, c.stus), pct(c.nonZero, c.running))
	}
}

// tripModes maps every trip of the imported schedule to its mode (the
// mapping of derived.sql, via pole_directions); "" for routes that serve no
// in-scope pole.
func tripModes(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, `
		SELECT ti.trip_id, coalesce(m.mode, '')
		FROM gtfs.trip_info ti
		LEFT JOIN (SELECT DISTINCT route_id, mode FROM gtfs.pole_directions) m USING (route_id)`)
	if err != nil {
		return nil, fmt.Errorf("trip modes: %w", err)
	}
	modes := map[string]string{}
	var id, mode string
	_, err = pgx.ForEachRow(rows, []any{&id, &mode}, func() error {
		if mode != "" && !slices.Contains(modeOrder, mode) {
			mode = "other"
		}
		modes[id] = mode
		return nil
	})
	return modes, err
}

// runningTrips returns the trips scheduled between their first departure and
// last arrival at time at (service days today and yesterday, for times past
// 24:00).
func runningTrips(ctx context.Context, conn *pgx.Conn, at time.Time) (map[string]bool, error) {
	local := at.In(platform.Berlin)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	secs := local.Hour()*3600 + local.Minute()*60 + local.Second()
	rows, err := conn.Query(ctx, `
		SELECT ti.trip_id
		FROM gtfs.trip_info ti
		JOIN gtfs.stop_times f ON f.trip_id = ti.trip_id AND f.stop_sequence = ti.first_stop_sequence
		JOIN gtfs.stop_times l ON l.trip_id = ti.trip_id AND l.stop_sequence = ti.last_stop_sequence
		JOIN gtfs.service_dates sd ON sd.service_id = ti.service_id
		WHERE (sd.service_date = $1::date AND $2::int BETWEEN f.departure_secs AND l.arrival_secs)
		   OR (sd.service_date = $1::date - 1 AND $2::int + 86400 BETWEEN f.departure_secs AND l.arrival_secs)`,
		day, secs)
	if err != nil {
		return nil, fmt.Errorf("running trips: %w", err)
	}
	running := map[string]bool{}
	var id string
	_, err = pgx.ForEachRow(rows, []any{&id}, func() error {
		running[id] = true
		return nil
	})
	return running, err
}
