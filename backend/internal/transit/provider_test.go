package transit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/platform"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
	"github.com/256-byte/punktchen/backend/internal/transit/static/statictest"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

var berlin = platform.Berlin

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v.In(berlin)
}

// snapshotAt builds the departures snapshot of the live schedule at now.
func snapshotAt(t *testing.T, pool *pgxpool.Pool, now time.Time) *departures.Builder {
	t.Helper()
	b := departures.NewBuilder(departures.BuilderConfig{Now: func() time.Time { return now }},
		departures.NewLoader(pool), nil, slog.New(slog.DiscardHandler))
	if _, err := b.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return b
}

func nearby(t *testing.T, p *Provider, lat, lng, radius float64) (*openapi.TransitLayer, uint64) {
	t.Helper()
	res, err := p.Nearby(context.Background(), layer.NearbyQuery{Center: layer.Location{Lat: lat, Lng: lng}, RadiusMeters: radius})
	if err != nil {
		t.Fatal(err)
	}
	tl, ok := res.Payload.(*openapi.TransitLayer)
	if !ok {
		t.Fatalf("payload %T", res.Payload)
	}
	return tl, res.Version
}

const (
	alpha = "de:11000:900000001::1"
	beta  = "de:11000:900000002::1"
	gamma = "de:11000:900000003::1"
)

// rulesFeed: line M10 shuttles Alpha ⇄ Beta ⇄ Gamma every 10 minutes (plus
// a short-turn to Beta at night); U6 twice under two trip headsigns that
// riders see as one; S45 (no route_color: VBB list), a rail replacement bus
// "S45" once; night line N10 only after the window, mostly to Gamma.
// A stop is "stop_id" or "stop_id|stop_headsign".
func rulesFeed() map[string]string {
	var st strings.Builder
	st.WriteString("trip_id,stop_id,stop_sequence,arrival_time,departure_time,stop_headsign\n")
	var trips strings.Builder
	trips.WriteString("route_id,service_id,trip_id,trip_headsign,direction_id\n")
	trip := func(route, id, headsign string, dir int, start time.Duration, stops ...string) {
		fmt.Fprintf(&trips, "%s,TUE,%s,%s,%d\n", route, id, headsign, dir)
		for i, s := range stops {
			t := start + time.Duration(i)*5*time.Minute
			hms := fmt.Sprintf("%02d:%02d:00", int(t.Hours()), int(t.Minutes())%60)
			stop, headsign, _ := strings.Cut(s, "|")
			fmt.Fprintf(&st, "%s,%s,%d,%s,%s,%s\n", id, stop, i+1, hms, hms, headsign)
		}
	}
	for i := range 6 { // 17:00 … 17:50
		start := 17*time.Hour + time.Duration(i)*10*time.Minute
		trip("m10", fmt.Sprintf("m10-out-%d", i), "Gamma (Berlin)", 0, start, alpha, beta, gamma)
		trip("m10", fmt.Sprintf("m10-back-%d", i), "Alpha (Berlin)", 1, start, gamma, beta, alpha)
	}
	trip("u6", "u6-1", "Gamma (Berlin)", 0, 17*time.Hour+3*time.Minute, alpha, gamma)
	trip("u6", "u6-2", "Gamma Süd (Berlin)", 0, 17*time.Hour+13*time.Minute, alpha+"|Gamma (Berlin)", gamma)
	trip("m10", "m10-short", "Beta (Berlin)", 0, 23*time.Hour, alpha, beta)
	trip("s45", "s45-1", "Gamma (Berlin)", 0, 17*time.Hour+4*time.Minute, alpha, gamma)
	trip("sev", "sev-1", "Gamma (Berlin)", 0, 17*time.Hour+6*time.Minute, alpha, gamma)
	for i := range 2 { // 02:00, outside the window
		trip("n10", fmt.Sprintf("n10-%d", i), "Gamma (Berlin)", 0, 26*time.Hour+time.Duration(i)*time.Hour, alpha, gamma)
	}
	trip("n10", "n10-beta", "Beta (Berlin)", 0, 27*time.Hour+30*time.Minute, alpha, beta)

	return map[string]string{
		"agency.txt": "agency_id,agency_name\n796,BVG\n1,S-Bahn Berlin GmbH\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon,platform_code\n" +
			alpha + ",Alpha (Berlin),52.50,13.40,1\n" + beta + ",Beta (Berlin),52.51,13.41,\n" +
			gamma + ",Gamma (Berlin),52.52,13.42,\n",
		"routes.txt": "route_id,agency_id,route_short_name,route_type,route_color\n" +
			"m10,796,M10,900,\nu6,796,U6,400,8c6dab\ns45,1,S45,109,\nsev,1,S45,700,\nn10,796,N10,700,\n",
		"trips.txt":          trips.String(),
		"calendar.txt":       "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n",
		"calendar_dates.txt": "service_id,date,exception_type\nTUE,20261110,1\n",
		"stop_times.txt":     st.String(),
	}
}

func TestProviderRules(t *testing.T) {
	pool := statictest.NewDB(t)
	statictest.LoadFiles(t, pool, rulesFeed())
	snaps := snapshotAt(t, pool, at(t, "2026-11-10T17:00:00+01:00"))
	p := NewProvider(pool, snaps)

	tl, version := nearby(t, p, 52.50, 13.40, 5000)
	if version != snaps.Current().Version || version != p.Version() {
		t.Errorf("result version %d, snapshot %d, Version() %d", version, snaps.Current().Version, p.Version())
	}
	if tl.Layer != openapi.TransitLayerLayerTransit {
		t.Errorf("layer %q", tl.Layer)
	}
	var names []string
	for _, s := range tl.Features {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "Alpha,Beta,Gamma" {
		t.Fatalf("stations %v, want Alpha,Beta,Gamma (nearest first, \" (Berlin)\" dropped)", names)
	}

	alphaSt := tl.Features[0]
	if *alphaSt.DistanceMeters != 0 || alphaSt.Kind != openapi.StationKindStation || alphaSt.Id != "de:11000:900000001" {
		t.Errorf("alpha station %+v", alphaSt)
	}
	if got := fmt.Sprint(alphaSt.Modes); got != "[sbahn ubahn tram bus]" {
		t.Errorf("modes %s, want by importance", got)
	}
	dirs := alphaSt.Poles[0].Directions
	var got []string
	for _, d := range dirs {
		got = append(got, fmt.Sprintf("%s %s→%s %s terminal=%v deps=%d", d.Mode, d.Line, d.Headsign, deref(d.Color), d.IsTerminal, len(d.Departures)))
	}
	want := []string{
		"sbahn S45→Gamma #CD9C53 terminal=false deps=1", // colour from the VBB list
		"ubahn U6→Gamma #8C6DAB terminal=false deps=2",  // route_color; both trip headsigns merged
		"tram M10→Gamma  terminal=false deps=3",         // max 3; M10→Alpha ends here, empty M10→Beta: hidden
		"bus N10→Gamma  terminal=false deps=0",          // no departure in the window: only the most frequent variant
		"bus S45→Gamma  terminal=false deps=1",          // rail replacement: bus, no train colour
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("alpha directions:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	m10 := dirs[2].Departures
	if m10[0].Scheduled.Format(time.RFC3339) != "2026-11-10T17:00:00+01:00" || m10[2].TripId != "m10-out-2" ||
		m10[0].ServiceDate.String() != "2026-11-10" || m10[0].Source != openapi.DepartureSourceScheduled ||
		m10[0].Predicted != nil || m10[0].DelaySeconds != nil || m10[0].Cancelled {
		t.Errorf("M10 departures %+v", m10)
	}
	if p := alphaSt.Poles[0]; p.Platform == nil || *p.Platform != "1" {
		t.Errorf("platform %v", p.Platform)
	}

	// Beta is no terminal; Gamma only has M10 back to Alpha (all others end there).
	if n := len(tl.Features[1].Poles[0].Directions); n != 2 {
		t.Errorf("beta: %d directions, want M10 both ways", n)
	}
	gammaDirs := tl.Features[2].Poles[0].Directions
	if len(gammaDirs) != 1 || gammaDirs[0].Headsign != "Alpha" || len(gammaDirs[0].Departures) != 3 {
		t.Errorf("gamma directions %+v, want only M10→Alpha", gammaDirs)
	}

	t.Run("departures from the snapshot time on", func(t *testing.T) {
		p := NewProvider(pool, snapshotAt(t, pool, at(t, "2026-11-10T17:45:30+01:00")))
		tl, _ := nearby(t, p, 52.50, 13.40, 100)
		for _, d := range tl.Features[0].Poles[0].Directions {
			if d.Line == "M10" && (len(d.Departures) != 1 || d.Departures[0].TripId != "m10-out-5") {
				t.Errorf("M10 at 17:45:30: %+v, want only the 17:50", d.Departures)
			}
		}
	})

	t.Run("station sheet", func(t *testing.T) {
		st, v, err := p.Station(context.Background(), "de:11000:900000003")
		if err != nil {
			t.Fatal(err)
		}
		if st.Name != "Gamma" || st.DistanceMeters != nil || v != version || len(st.Poles) != 1 {
			t.Fatalf("station %+v v%d", st, v)
		}
		if n := len(st.Poles[0].Directions[0].Departures); n != 6 {
			t.Errorf("%d departures, want all 6 (limit 10)", n)
		}
		if _, _, err := p.Station(context.Background(), "de:11000:999"); !errors.Is(err, ErrUnknownStation) {
			t.Errorf("unknown station: %v", err)
		}
	})

	t.Run("nothing nearby", func(t *testing.T) {
		tl, _ := nearby(t, p, 52.40, 13.20, 500)
		if tl.Features == nil || len(tl.Features) != 0 {
			t.Errorf("features %v, want an empty list", tl.Features)
		}
		b, _ := json.Marshal(tl)
		if string(b) != `{"features":[],"layer":"transit"}` {
			t.Errorf("JSON %s", b)
		}
	})
}

type fixedSnaps struct{ s *departures.Snapshot }

func (f fixedSnaps) Current() *departures.Snapshot { return f.s }

func TestProviderWithoutSnapshot(t *testing.T) {
	p := &Provider{snaps: fixedSnaps{}, now: time.Now, staleAfter: DefaultStaleAfter}
	if _, err := p.Nearby(context.Background(), layer.NearbyQuery{RadiusMeters: 500}); !errors.Is(err, ErrNoDepartures) {
		t.Errorf("Nearby: %v", err)
	}
	if _, _, err := p.Station(context.Background(), "x"); !errors.Is(err, ErrNoDepartures) {
		t.Errorf("Station: %v", err)
	}
	if h := p.Health(); h.Status != layer.StatusDown || h.LastError == "" || p.Version() != 0 {
		t.Errorf("health %+v version %d", h, p.Version())
	}
	if i := p.Info(); i.ID != "transit" || len(i.Attribution) != 1 || i.Attribution[0] != "Data: VBB (CC BY 4.0)" {
		t.Errorf("info %+v", i)
	}
}

func TestProviderHealth(t *testing.T) {
	generated := time.Date(2026, 11, 10, 17, 0, 0, 0, berlin)
	s := &departures.Snapshot{Version: 7, GeneratedAt: generated}
	p := &Provider{snaps: fixedSnaps{s}, staleAfter: DefaultStaleAfter}
	for _, c := range []struct {
		age  time.Duration
		want layer.Status
	}{
		{0, layer.StatusOK},
		{DefaultStaleAfter, layer.StatusOK},
		{DefaultStaleAfter + time.Second, layer.StatusDegraded},
	} {
		p.now = func() time.Time { return generated.Add(c.age) }
		if h := p.Health(); h.Status != c.want || (c.want == layer.StatusDegraded) != (h.LastError != "") {
			t.Errorf("age %s: %+v, want %s", c.age, h, c.want)
		}
	}
	if p.Version() != 7 {
		t.Errorf("version %d", p.Version())
	}
}

// TestProviderWeddingGolden is the PLAN 2.4 criterion: the transit features
// around U Wedding on the real Wedding subset, Tuesday 2026-11-10 17:00,
// compared with testdata/*.golden.json. Run with -update to rewrite them.
func TestProviderWeddingGolden(t *testing.T) {
	pool := statictest.NewDB(t)
	statictest.LoadZip(t, pool, filepath.Join("..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip"))
	p := NewProvider(pool, snapshotAt(t, pool, at(t, "2026-11-10T17:00:00+01:00")))

	tl, _ := nearby(t, p, 52.5427, 13.3660, 500)
	golden(t, "nearby-wedding-500m.golden.json", tl)

	st, _, err := p.Station(context.Background(), "de:11000:900009104")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "station-wedding.golden.json", st)

	var poles, dirs, deps, colored int
	for _, s := range tl.Features {
		for _, pl := range s.Poles {
			poles++
			for _, d := range pl.Directions {
				dirs++
				deps += len(d.Departures)
				if d.Color != nil {
					colored++
				}
			}
		}
	}
	t.Logf("nearby 500 m: %d stations, %d poles, %d directions (%d with colour), %d departures", len(tl.Features), poles, dirs, colored, deps)
}

func golden(t *testing.T, name string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file; inspect with -update and git diff", name)
	}
}
