package departures

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/platform"
)

// NoDirection is Departure.DirectionID when the feed has no direction_id
// (never in VBB data, DATA_MODEL §7 #8).
const NoDirection int16 = -1

// Departure is one scheduled, boardable departure at an in-scope pole.
type Departure struct {
	TripID       string
	ServiceDate  ServiceDay // with TripID: the trip instance realtime refers to
	StopID       string
	StopSequence int32
	RouteID      string
	DirectionID  int16
	// Headsign is the direction key (trip headsign, else the last stop's
	// name), the same as pole_directions.headsign.
	Headsign string
	// StopHeadsign is what the vehicle shows at this stop; "" if none.
	StopHeadsign string
	Scheduled    time.Time // in Europe/Berlin
}

// Window holds the departures of a time window.
type Window struct {
	From, To time.Time // half-open: From ≤ Scheduled < To
	// ScheduleID identifies the schedule the departures come from; it
	// changes with every import (and then the window must be reloaded).
	ScheduleID int64
	// Departures sorted by Scheduled, then StopID, then TripID.
	Departures []Departure
}

// ByStop groups the departures by pole, each list sorted by time.
func (w *Window) ByStop() map[string][]Departure {
	out := map[string][]Departure{}
	for _, d := range w.Departures {
		out[d.StopID] = append(out[d.StopID], d)
	}
	return out
}

// ErrNoSchedule: there is no live schedule yet (before the first import).
var ErrNoSchedule = errors.New("departures: no live schedule")

// errScheduleChanged: an import swapped the schedule during a load.
var errScheduleChanged = errors.New("departures: schedule changed during load")

// maxLoadAttempts bounds retries when imports swap the schedule mid-load.
const maxLoadAttempts = 3

// Loader reads scheduled departures from the live schedule.
type Loader struct {
	pool *pgxpool.Pool
	loc  *time.Location
}

// NewLoader returns a Loader for the VBB schedule (Europe/Berlin).
func NewLoader(pool *pgxpool.Pool) *Loader {
	return &Loader{pool: pool, loc: platform.Berlin}
}

// Load returns all departures with from ≤ time < to. Windows of any length
// work; adjacent windows never share a departure, so a caller can extend a
// window by loading only the new part.
//
// Every service day that reaches the window is asked separately (yesterday's
// trips after midnight, tomorrow's from its start). All of it comes from one
// schedule: if an import swaps the schedule meanwhile, Load starts over.
func (l *Loader) Load(ctx context.Context, from, to time.Time) (*Window, error) {
	if !from.Before(to) {
		return nil, fmt.Errorf("departures: empty window [%s, %s)", from, to)
	}
	for attempt := 1; ; attempt++ {
		w, err := l.load(ctx, from, to)
		if errors.Is(err, errScheduleChanged) && attempt < maxLoadAttempts {
			continue
		}
		return w, err
	}
}

func (l *Loader) load(ctx context.Context, from, to time.Time) (*Window, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Plan each call for its own range; a cached generic plan cannot tell a
	// 1-minute slice from a whole day and is up to 10× slower.
	if _, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_custom_plan`); err != nil {
		return nil, err
	}
	q := queries.New(tx)
	id, err := scheduleID(ctx, q)
	if err != nil {
		return nil, err
	}

	maxSecs, err := q.MaxDepartureSecs(ctx)
	if err != nil {
		return nil, err
	}

	w := &Window{From: from.In(l.loc), To: to.In(l.loc), ScheduleID: id}
	in := interner{}
	for _, r := range dayRanges(from, to, l.loc, maxSecs) {
		rows, err := q.ScheduledDepartures(ctx, queries.ScheduledDeparturesParams{
			ServiceDate: pgtype.Date{Time: time.Date(r.day.Year, r.day.Month, r.day.Day, 0, 0, 0, 0, time.UTC), Valid: true},
			FromSecs:    r.fromSecs,
			ToSecs:      r.toSecs,
		})
		if err != nil {
			return nil, fmt.Errorf("departures of service day %s: %w", r.day, err)
		}
		start := r.day.Start(l.loc)
		for _, row := range rows {
			w.Departures = append(w.Departures, Departure{
				TripID:       in.str(row.TripID),
				ServiceDate:  r.day,
				StopID:       in.str(row.StopID),
				StopSequence: row.StopSequence,
				RouteID:      in.str(row.RouteID),
				DirectionID:  row.DirectionID,
				Headsign:     in.str(row.Headsign),
				StopHeadsign: in.str(row.StopHeadsign),
				Scheduled:    start.Add(time.Duration(row.DepartureSecs) * time.Second),
			})
		}
	}

	if after, err := scheduleID(ctx, q); err != nil {
		return nil, err
	} else if after != id {
		return nil, errScheduleChanged
	}
	slices.SortFunc(w.Departures, func(a, b Departure) int {
		return cmp.Or(a.Scheduled.Compare(b.Scheduled), cmp.Compare(a.StopID, b.StopID), cmp.Compare(a.TripID, b.TripID))
	})
	return w, nil
}

func scheduleID(ctx context.Context, q *queries.Queries) (int64, error) {
	id, err := q.LiveScheduleID(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoSchedule
	}
	return id, err
}

// interner shares one copy of repeated strings (pole, route, headsign and
// trip ids repeat across thousands of departures).
type interner map[string]string

func (in interner) str(s string) string {
	if v, ok := in[s]; ok {
		return v
	}
	in[s] = s
	return s
}
