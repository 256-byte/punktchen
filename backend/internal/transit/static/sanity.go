package static

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SanityLimits are the thresholds a staging schema must meet before it may
// replace the live schedule (ARCHITECTURE §3.1 step 5).
type SanityLimits struct {
	MinInScopePoles int64   // absolute floor
	MinStopTimes    int64   // absolute floor
	MaxDrop         float64 // max relative drop vs. the last successful import, e.g. 0.5
}

// DefaultSanityLimits fit the VBB feed: ~10,800 poles in the Berlin bbox and
// ~5.8 M stop times (DATA_MODEL §7 #12, #22).
var DefaultSanityLimits = SanityLimits{MinInScopePoles: 5_000, MinStopTimes: 1_000_000, MaxDrop: 0.5}

// check validates the staging schema. It runs every check and returns all
// problems at once. prev holds the row counts of the last successful import
// (nil on the first import); today is the service day in Europe/Berlin.
func check(ctx context.Context, pool *pgxpool.Pool, res *LoadResult, prev map[string]int64, today time.Time, lim SanityLimits) error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	// Size: absolute floors and a sudden shrink against the previous feed.
	if res.InScopePoles < lim.MinInScopePoles {
		fail("only %d poles in scope, want at least %d", res.InScopePoles, lim.MinInScopePoles)
	}
	if res.Rows["stop_times"] < lim.MinStopTimes {
		fail("only %d stop_times, want at least %d", res.Rows["stop_times"], lim.MinStopTimes)
	}
	for key, now := range map[string]int64{"stop_times": res.Rows["stop_times"], keyInScopePoles: res.InScopePoles} {
		if before := prev[key]; before > 0 && float64(now) < float64(before)*(1-lim.MaxDrop) {
			fail("%s dropped from %d to %d (more than %.0f %%)", key, before, now, lim.MaxDrop*100)
		}
	}

	// References that the departure queries rely on. Counted, not enforced
	// by foreign keys, so one run reports all of them (DATA_MODEL §2).
	refs := []struct{ what, query string }{
		{"stop_times with unknown trip_id", `SELECT count(*) FROM gtfs_staging.stop_times st WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.trips t WHERE t.trip_id = st.trip_id)`},
		{"stop_times with unknown stop_id", `SELECT count(*) FROM gtfs_staging.stop_times st WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.stops s WHERE s.stop_id = st.stop_id)`},
		{"trips with unknown route_id", `SELECT count(*) FROM gtfs_staging.trips t WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.routes r WHERE r.route_id = t.route_id)`},
		{"trips whose service never runs", `SELECT count(*) FROM gtfs_staging.trips t WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.service_dates d WHERE d.service_id = t.service_id)`},
		{"trips without stop_times", `SELECT count(*) FROM gtfs_staging.trips t WHERE NOT EXISTS (SELECT 1 FROM gtfs_staging.stop_times st WHERE st.trip_id = t.trip_id)`},
		{"served poles without coordinates", `SELECT count(*) FROM gtfs_staging.stops s WHERE (s.stop_lat IS NULL OR s.stop_lon IS NULL) AND EXISTS (SELECT 1 FROM gtfs_staging.stop_times st WHERE st.stop_id = s.stop_id)`},
		{"stop_times without departure_time", `SELECT count(*) FROM gtfs_staging.stop_times WHERE departure_secs IS NULL`},
	}
	for _, r := range refs {
		var n int64
		if err := pool.QueryRow(ctx, r.query).Scan(&n); err != nil {
			return fmt.Errorf("sanity check %q: %w", r.what, err)
		}
		if n > 0 {
			fail("%d %s", n, r.what)
		}
	}

	// The feed must cover the coming days: an expired or not yet valid feed
	// would serve no departures.
	var upcoming int64
	day := today.Format(time.DateOnly)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gtfs_staging.service_dates
		WHERE service_date BETWEEN $1::date AND $1::date + 7`, day).Scan(&upcoming); err != nil {
		return fmt.Errorf("sanity check validity: %w", err)
	}
	if upcoming == 0 {
		fail("no service on %s or the 7 days after (feed expired or not yet valid)", day)
	}

	if len(problems) > 0 {
		return fmt.Errorf("gtfs sanity checks failed: %w", errors.Join(problems...))
	}
	return nil
}
