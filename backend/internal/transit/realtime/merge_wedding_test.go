package realtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/transit/departures"
	"github.com/256-byte/punktchen/backend/internal/transit/static/statictest"
)

// TestMergeWeddingFixture merges a recorded feed with the schedule it was
// made for (the Wedding subset of the 2026-11-05 feed, testdata/gtfs) and
// checks the data assumption behind matching by stop_sequence: where an
// update has the departure's stop_sequence, its stop_id is the departure's
// pole or the bare number of its station (DATA_MODEL §7 #18). Integration
// test: needs Docker.
func TestMergeWeddingFixture(t *testing.T) {
	pool := statictest.NewDB(t)
	statictest.LoadZip(t, pool, filepath.Join("..", "..", "..", "testdata", "gtfs", "vbb-wedding-800m.zip"))
	st, _, err := Parse(readFixture(t, "vbb-20261108T230040Z-wedding.pb"))
	if err != nil {
		t.Fatal(err)
	}
	from := st.FeedTime.Add(-2 * time.Minute)
	w, err := departures.NewLoader(pool).Load(context.Background(), from, from.Add(92*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	merged, stats := Merge(w.Departures, st)
	t.Logf("feed %s, window %s: %+v", st.FeedTime.Format(time.RFC3339), from.Format(time.RFC3339), stats)

	matched, mismatched := 0, 0
	for _, d := range w.Departures {
		u := st.Trip(TripKey{d.TripID, d.ServiceDate})
		if u == nil {
			continue
		}
		for _, s := range u.Stops {
			if s.Sequence != d.StopSequence {
				continue
			}
			matched++
			if s.StopID != d.StopID && !strings.Contains(d.StopID, ":"+s.StopID+":") {
				mismatched++
				if mismatched <= 5 {
					t.Errorf("trip %s seq %d: departure at %s, update at %s", d.TripID, d.StopSequence, d.StopID, s.StopID)
				}
			}
		}
	}
	if matched == 0 || stats.WithTrip == 0 || stats.Realtime == 0 {
		t.Fatalf("nothing matched: %d by sequence, %+v", matched, stats)
	}
	if mismatched > 0 {
		t.Errorf("%d of %d sequence matches point at another stop", mismatched, matched)
	}
	// Near the feed time almost every departure has a TripUpdate, although
	// the Wedding subset schedule is three days older (DATA_MODEL §7 #31).
	r := MatchRatio(w.Departures, st, st.FeedTime, MatchWindow)
	t.Logf("match ratio within %s: %.3f", MatchWindow, r)
	if r < 0.8 {
		t.Errorf("match ratio %.3f, want ≥ 0.8", r)
	}
	if len(merged)+stats.Skipped != len(w.Departures) {
		t.Errorf("%d merged + %d skipped != %d scheduled", len(merged), stats.Skipped, len(w.Departures))
	}
}
