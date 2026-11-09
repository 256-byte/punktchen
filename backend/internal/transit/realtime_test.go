package transit

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/layer"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

// windowOf is a window loader with fixed departures.
type windowOf []departures.Departure

func (w windowOf) Load(_ context.Context, from, to time.Time) (*departures.Window, error) {
	out := &departures.Window{From: from, To: to, ScheduleID: 1}
	for _, d := range w {
		if !d.Scheduled.Before(from) && d.Scheduled.Before(to) {
			out.Departures = append(out.Departures, d)
		}
	}
	return out, nil
}

// realtimeSnapshot builds a snapshot at now whose realtime step sets the
// given delays (trip → seconds) and feed time.
func realtimeSnapshot(t *testing.T, now time.Time, deps []departures.Departure, delays map[string]int32, feed time.Time) *departures.Snapshot {
	t.Helper()
	merge := func(in []departures.Departure, _ time.Time) ([]departures.Departure, time.Time) {
		out := slices.Clone(in)
		for i := range out {
			if d, ok := delays[out[i].TripID]; ok {
				out[i].Delay, out[i].Realtime = d, true
			}
		}
		return out, feed
	}
	b := departures.NewBuilder(departures.BuilderConfig{Now: func() time.Time { return now }, Realtime: merge},
		windowOf(deps), nil, slog.New(slog.DiscardHandler))
	s, err := b.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestBuildPoleRealtime: a late vehicle stays listed until it leaves (by
// predicted time), directions list by predicted time, and departures carry
// the realtime fields (task 4.4).
func TestBuildPoleRealtime(t *testing.T) {
	now := at(t, "2026-11-10T17:00:00+01:00")
	dep := func(trip string, hhmm string, cancelled bool) departures.Departure {
		return departures.Departure{TripID: trip, StopID: "p1", RouteID: "r1", DirectionID: 0, Headsign: "Nord",
			Scheduled: at(t, "2026-11-10T"+hhmm+":00+01:00"), Cancelled: cancelled,
			ServiceDate: departures.ServiceDay{Year: 2026, Month: time.November, Day: 10}}
	}
	s := realtimeSnapshot(t, now, []departures.Departure{
		dep("late", "16:55", false),   // +10 min → 17:05
		dep("gone", "16:58", false),   // left
		dep("next", "17:02", false),   // on time
		dep("early", "17:04", false),  // -60 s → 17:03
		dep("dropped", "17:10", true), // cancelled, listed
	}, map[string]int32{"late": 600, "early": -60, "next": 0}, now.Add(-10*time.Second))

	rows := []queries.PoleDirectionsRow{{StopID: "p1", RouteID: "r1", Line: "M27", Mode: "bus", Headsign: "Nord", DisplayHeadsign: "Nord", TripCount: 10}}
	pole, ok := buildPole(s, poleRow{id: "p1"}, rows, 10)
	if !ok || len(pole.Directions) != 1 {
		t.Fatalf("pole %+v", pole)
	}
	var got []string
	for _, d := range pole.Directions[0].Departures {
		got = append(got, d.TripId)
	}
	if want := []string{"next", "early", "late", "dropped"}; !slices.Equal(got, want) {
		t.Errorf("departures %v, want %v (by predicted time; the late one kept)", got, want)
	}
	late := pole.Directions[0].Departures[2]
	if late.Source != openapi.DepartureSourceRealtime || late.DelaySeconds == nil || *late.DelaySeconds != 600 ||
		late.Predicted == nil || !late.Predicted.Equal(at(t, "2026-11-10T17:05:00+01:00")) {
		t.Errorf("late %+v", late)
	}
	if next := pole.Directions[0].Departures[0]; next.Source != openapi.DepartureSourceRealtime || *next.DelaySeconds != 0 {
		t.Errorf("on time with realtime: %+v", next)
	}
	dropped := pole.Directions[0].Departures[3]
	if !dropped.Cancelled || dropped.Source != openapi.DepartureSourceScheduled || dropped.Predicted != nil || dropped.DelaySeconds != nil {
		t.Errorf("cancelled %+v", dropped)
	}
}

func TestProviderRealtimeHealth(t *testing.T) {
	now := at(t, "2026-11-10T17:00:00+01:00")
	deps := []departures.Departure{{TripID: "a", StopID: "p1", Scheduled: now.Add(5 * time.Minute)}}
	for _, c := range []struct {
		name     string
		feed     time.Time // zero: no realtime data
		realtime bool      // provider expects realtime
		want     layer.Status
		age      time.Duration // -1: nil
	}{
		{"schedule-only provider ignores realtime", time.Time{}, false, layer.StatusOK, -1},
		{"no realtime data yet", time.Time{}, true, layer.StatusDegraded, -1},
		{"fresh feed", now.Add(-30 * time.Second), true, layer.StatusOK, 30 * time.Second},
		{"feed 2 min old", now.Add(-2 * time.Minute), true, layer.StatusOK, 2 * time.Minute},
		{"feed older than 2 min", now.Add(-2*time.Minute - time.Second), true, layer.StatusDegraded, 2*time.Minute + time.Second},
		{"feed from the future (clock skew)", now.Add(5 * time.Second), true, layer.StatusOK, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := realtimeSnapshot(t, now, deps, nil, c.feed)
			opts := []Option{WithClock(func() time.Time { return now })}
			if c.realtime {
				opts = append(opts, WithRealtime(DefaultRealtimeStaleAfter))
			}
			h := NewProvider(nil, fixedSnaps{s}, opts...).Health()
			if h.Status != c.want || (h.Status == layer.StatusDegraded) != (h.LastError != "") {
				t.Errorf("health %+v, want %s", h, c.want)
			}
			if (c.age < 0) != (h.RealtimeAge == nil) || h.RealtimeAge != nil && *h.RealtimeAge != c.age {
				t.Errorf("realtime age %v, want %v", h.RealtimeAge, c.age)
			}
		})
	}
}
