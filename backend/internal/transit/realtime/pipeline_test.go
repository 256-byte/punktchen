package realtime

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"

	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/transit"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// withFeedTime returns a recorded feed with header.timestamp set to t.
func withFeedTime(t *testing.T, body []byte, ts time.Time) []byte {
	t.Helper()
	var msg gtfs.FeedMessage
	if err := proto.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	msg.Header.Timestamp = proto.Uint64(uint64(ts.Unix()))
	out, err := proto.Marshal(&msg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// upstream is a fake GTFS-RT server whose answer the test switches.
type upstream struct {
	mu   sync.Mutex
	body []byte
	code int
}

func (u *upstream) serve(body []byte) {
	u.mu.Lock()
	u.body, u.code = body, http.StatusOK
	u.mu.Unlock()
}
func (u *upstream) kill() { u.mu.Lock(); u.code = http.StatusServiceUnavailable; u.mu.Unlock() }

func (u *upstream) handler(w http.ResponseWriter, _ *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.code != http.StatusOK {
		w.WriteHeader(u.code)
		return
	}
	_, _ = w.Write(u.body)
}

// oneDeparture is a window loader with a single scheduled departure.
type oneDeparture struct{ d departures.Departure }

func (l oneDeparture) Load(_ context.Context, from, to time.Time) (*departures.Window, error) {
	w := &departures.Window{From: from, To: to, ScheduleID: 1}
	if !l.d.Scheduled.Before(from) && l.d.Scheduled.Before(to) {
		w.Departures = []departures.Departure{l.d}
	}
	return w, nil
}

// TestPipelineHealth is task 4.4's acceptance: fake upstream → poller →
// store → snapshot builder → transit health. Killing upstream makes health
// degraded while the schedule is still served; so does a stale empty feed.
func TestPipelineHealth(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	c := t0()
	recorded := readFixture(t, "vbb-20261108T230040Z-wedding.pb")

	// A departure of a trip the feed has a delay for.
	st, _, err := Parse(recorded)
	if err != nil {
		t.Fatal(err)
	}
	var dep departures.Departure
	for _, k := range st.Keys() {
		u := st.Trip(k)
		for _, s := range u.Stops {
			if s.Sequence != NoSequence && s.Departure.HasDelay && s.Departure.Delay != 0 && dep.TripID == "" {
				dep = departures.Departure{TripID: k.TripID, ServiceDate: k.ServiceDate, StopID: s.StopID,
					StopSequence: s.Sequence, Scheduled: c.Now().Add(20 * time.Minute)}
			}
		}
	}
	if dep.TripID == "" {
		t.Fatal("no delayed stop in the fixture")
	}

	store := NewStore(log)
	builder := departures.NewBuilder(departures.BuilderConfig{Now: c.Now, Realtime: store.Merger(DefaultMaxAge)},
		oneDeparture{dep}, nil, log)
	store.OnApply(func() { builder.Republish() })
	provider := transit.NewProvider(nil, builder, transit.WithRealtime(transit.DefaultRealtimeStaleAfter), transit.WithClock(c.Now))

	up := &upstream{}
	srv := newServer(t, up.handler)
	poller, _ := newPoller(t, srv.URL, store.Apply, c, nil)

	check := func(step string, want layer.Status, realtime bool) {
		t.Helper()
		h := provider.Health()
		if h.Status != want {
			t.Errorf("%s: health %+v, want %s", step, h, want)
		}
		s := builder.Current()
		got := s.Departures(dep.StopID)
		if len(got) != 1 {
			t.Fatalf("%s: %d departures; the schedule must be served", step, len(got))
		}
		if got[0].Realtime != realtime {
			t.Errorf("%s: realtime %v, want %v", step, got[0].Realtime, realtime)
		}
	}
	poll := func(want Result) {
		t.Helper()
		if got, _ := poller.Poll(ctx); got != want {
			t.Fatalf("poll %s, want %s", got, want)
		}
	}
	later := func(d time.Duration) {
		t.Helper()
		_ = c.Wait(ctx, d)
		if _, err := builder.Refresh(ctx); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := builder.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	check("before any feed", layer.StatusDegraded, false)

	up.serve(withFeedTime(t, recorded, c.Now().Add(-5*time.Second)))
	poll(ResultUpdated)
	check("fresh feed", layer.StatusOK, true)
	if h := provider.Health(); h.RealtimeAge == nil || *h.RealtimeAge != 5*time.Second {
		t.Errorf("realtime age %v, want 5s", h.RealtimeAge)
	}

	up.kill()
	poll(ResultError)
	later(3 * time.Minute)
	poll(ResultError)
	check("upstream down for 3 min", layer.StatusDegraded, true) // still merged: < 5 min
	later(3 * time.Minute)
	check("upstream down for 6 min", layer.StatusDegraded, false) // too old to merge: schedule only

	up.serve(withFeedTime(t, recorded, c.Now().Add(-5*time.Second)))
	poll(ResultUpdated)
	check("upstream back", layer.StatusOK, true)

	up.serve(readFixture(t, "vbb-empty-stale.pb")) // empty, header 9 days old
	poll(ResultUpdated)                            // the poller hands it over; the store keeps the last state
	check("stale empty feed, at once", layer.StatusOK, true)
	later(2*time.Minute + time.Second)
	check("stale empty feed for 2 min", layer.StatusDegraded, true)
}
