package transit

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/256-byte/punktchen/backend/gen/openapi"
	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/transit/departures"
)

type stationRow struct {
	id, name string
	lat, lng float64
	modes    []string
	distance *int32 // only on /v1/nearby
}

// poleRow is one pole with its station; rows of one station are adjacent.
type poleRow struct {
	station  stationRow
	id       string
	platform *string
	lat, lng float64
}

// dirKey identifies a direction at a pole: pole_directions' key, which is
// also what a departure carries (route, direction_id, trip headsign).
type dirKey struct {
	route     string
	direction int16
	headsign  string
}

// build turns poles into stations → poles → directions → departures
// (DATA_MODEL §5.2), with up to limit departures per direction from the
// snapshot. Order is kept: stations and poles as given.
//
// Which directions a pole shows: see buildPole. Poles without directions
// and stations without poles are left out.
func (p *Provider) build(ctx context.Context, s *departures.Snapshot, poles []poleRow, limit int) ([]openapi.Station, error) {
	ids := make([]string, len(poles))
	for i, pr := range poles {
		ids[i] = pr.id
	}
	rows, err := p.q.PoleDirections(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pole directions: %w", err)
	}
	dirs := map[string][]queries.PoleDirectionsRow{}
	for _, r := range rows {
		dirs[r.StopID] = append(dirs[r.StopID], r)
	}

	out := []openapi.Station{}
	for _, pr := range poles {
		pole, ok := buildPole(s, pr, dirs[pr.id], limit)
		if !ok {
			continue
		}
		if n := len(out); n == 0 || out[n-1].Id != pr.station.id {
			out = append(out, newStation(pr.station))
		}
		st := &out[len(out)-1]
		st.Poles = append(st.Poles, pole)
	}
	return out, nil
}

func newStation(r stationRow) openapi.Station {
	modes := make([]openapi.Mode, len(r.modes))
	for i, m := range r.modes {
		modes[i] = openapi.Mode(m)
	}
	slices.SortFunc(modes, func(a, b openapi.Mode) int { return cmp.Compare(modeRank(a), modeRank(b)) })
	return openapi.Station{
		Id:             r.id,
		Kind:           openapi.StationKindStation,
		Name:           displayName(r.name),
		Location:       openapi.LatLng{Lat: r.lat, Lng: r.lng},
		DistanceMeters: r.distance,
		Modes:          modes,
		Poles:          []openapi.Pole{},
	}
}

// label is what riders see of a direction. pole_directions rows with the
// same label (e.g. the same S41 "Ringbahn S 41" from several trip
// headsigns) are one direction in the API.
type label struct {
	mode     openapi.Mode
	line     string
	headsign string // display headsign
	ring     bool
}

// merged is one API direction built from the rows of one label.
type merged struct {
	label
	color     *string
	terminal  bool  // every row is a terminal
	direction int16 // direction_id of the most frequent row
	top       int64 // trip instances of that row
	trips     int64 // trip instances of all rows
	deps      []departures.Departure
}

// buildPole returns the pole with its directions; false if none is left.
//
// Rules (DATA_MODEL §3.2):
//  1. Terminal: a row whose last stop is this pole is left out when it has
//     no departure.
//  2. Rows with the same label are merged; their departures are combined.
//  3. Per line and direction of travel (mode, line, direction_id): only the
//     directions with departures are shown; if none has one, the most
//     frequent is shown with an empty list, so the pole still tells which
//     lines stop there (e.g. night lines in the afternoon). This hides empty
//     short-turn and night variants (U6 → U Seestr.).
func buildPole(s *departures.Snapshot, pr poleRow, rows []queries.PoleDirectionsRow, limit int) (openapi.Pole, bool) {
	// Departures from the snapshot's time on: the response depends only on
	// the snapshot (and so on its version), not on the request time. Ones
	// that left since are dropped by the app.
	next := map[dirKey][]departures.Departure{}
	for _, d := range s.Departures(pr.id) {
		if d.Scheduled.Before(s.GeneratedAt) {
			continue
		}
		k := dirKey{route: d.RouteID, direction: d.DirectionID, headsign: d.Headsign}
		if len(next[k]) < limit {
			next[k] = append(next[k], d)
		}
	}

	byLabel := map[label]*merged{}
	var labels []*merged
	for _, r := range rows {
		deps := next[dirKey{route: r.RouteID, direction: r.DirectionID, headsign: r.Headsign}]
		if r.IsTerminal && len(deps) == 0 {
			continue
		}
		mode := openapi.Mode(r.Mode)
		l := label{mode: mode, line: r.Line, headsign: displayName(r.DisplayHeadsign), ring: r.IsRing}
		m := byLabel[l]
		if m == nil {
			m = &merged{label: l, terminal: true, direction: r.DirectionID, top: r.TripCount}
			byLabel[l] = m
			labels = append(labels, m)
		}
		if m.color == nil {
			m.color = lineColor(r.RouteColor, r.AgencyID, r.Line, mode)
		}
		m.terminal = m.terminal && r.IsTerminal
		if r.TripCount > m.top {
			m.direction, m.top = r.DirectionID, r.TripCount
		}
		m.trips += r.TripCount
		m.deps = append(m.deps, deps...)
	}

	// Rule 3: per line and direction of travel.
	type travel struct {
		mode      openapi.Mode
		line      string
		direction int16
	}
	best := map[travel]*merged{} // most frequent label, for lines without departures
	hasDeps := map[travel]bool{}
	for _, m := range labels {
		tr := travel{m.mode, m.line, m.direction}
		if len(m.deps) > 0 {
			hasDeps[tr] = true
		}
		if b := best[tr]; b == nil || m.trips > b.trips || m.trips == b.trips && m.headsign < b.headsign {
			best[tr] = m
		}
	}
	var keep []*merged
	for _, m := range labels {
		tr := travel{m.mode, m.line, m.direction}
		if len(m.deps) > 0 || !hasDeps[tr] && best[tr] == m {
			keep = append(keep, m)
		}
	}
	if len(keep) == 0 {
		return openapi.Pole{}, false
	}
	slices.SortFunc(keep, func(a, b *merged) int {
		return cmp.Or(
			cmp.Compare(modeRank(a.mode), modeRank(b.mode)),
			compareLines(a.line, b.line),
			cmp.Compare(a.headsign, b.headsign),
			cmpBool(a.ring, b.ring),
		)
	})

	pole := openapi.Pole{
		Id:         pr.id,
		Location:   openapi.LatLng{Lat: pr.lat, Lng: pr.lng},
		Platform:   pr.platform,
		Directions: make([]openapi.Direction, len(keep)),
	}
	for i, m := range keep {
		slices.SortFunc(m.deps, func(a, b departures.Departure) int {
			return cmp.Or(a.Scheduled.Compare(b.Scheduled), cmp.Compare(a.TripID, b.TripID))
		})
		deps := make([]openapi.Departure, 0, min(len(m.deps), limit))
		for _, d := range m.deps[:min(len(m.deps), limit)] {
			deps = append(deps, apiDeparture(d))
		}
		pole.Directions[i] = openapi.Direction{
			Line:       m.line,
			Mode:       m.mode,
			Color:      m.color,
			Headsign:   m.headsign,
			IsTerminal: m.terminal,
			IsRing:     m.ring,
			Departures: deps,
		}
	}
	return pole, true
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	default:
		return 1
	}
}

// apiDeparture is a scheduled departure; realtime fields come in Phase 4.
func apiDeparture(d departures.Departure) openapi.Departure {
	return openapi.Departure{
		TripId:      d.TripID,
		ServiceDate: openapi_types.Date{Time: time.Date(d.ServiceDate.Year, d.ServiceDate.Month, d.ServiceDate.Day, 0, 0, 0, 0, time.UTC)},
		Scheduled:   d.Scheduled,
		Source:      openapi.DepartureSourceScheduled,
	}
}

// displayName drops the locality suffix " (Berlin)" that VBB adds to every
// Berlin stop name (DATA_MODEL §7 #3): "S+U Wedding (Berlin)" → "S+U
// Wedding". Other localities stay ("Rathaus (Potsdam)"), they tell places
// apart.
func displayName(s string) string {
	return strings.TrimSuffix(s, " (Berlin)")
}

// modeRank orders modes "by importance" for Station.modes and directions:
// rail first, by reach.
func modeRank(m openapi.Mode) int {
	switch m {
	case openapi.ModeRegional:
		return 0
	case openapi.ModeSbahn:
		return 1
	case openapi.ModeUbahn:
		return 2
	case openapi.ModeTram:
		return 3
	case openapi.ModeBus:
		return 4
	case openapi.ModeFerry:
		return 5
	default:
		return 6
	}
}

// compareLines orders line names naturally: by letter prefix, then number,
// then the rest ("M4" < "M10" < "M10E", "U6" < "U8", "N7" < "N40").
func compareLines(a, b string) int {
	pa, na, ra := splitLine(a)
	pb, nb, rb := splitLine(b)
	return cmp.Or(cmp.Compare(pa, pb), cmp.Compare(na, nb), cmp.Compare(ra, rb), cmp.Compare(a, b))
}

func splitLine(s string) (prefix string, num int, rest string) {
	i := strings.IndexFunc(s, unicode.IsDigit)
	if i < 0 {
		return s, -1, ""
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	n, err := strconv.Atoi(s[i:j])
	if err != nil {
		return s, -1, ""
	}
	return s[:i], n, s[j:]
}

// sortSheetPoles orders a station sheet's poles: by their most important
// mode (rail platforms first), then platform (natural order), then id. Not
// left to the database, whose collation ignores the ":" in pole ids.
func sortSheetPoles(poles []openapi.Pole) {
	rank := func(p openapi.Pole) int {
		r := modeRank(openapi.ModeOther)
		for _, d := range p.Directions {
			r = min(r, modeRank(d.Mode))
		}
		return r
	}
	platform := func(p openapi.Pole) string {
		if p.Platform == nil {
			return ""
		}
		return *p.Platform
	}
	slices.SortFunc(poles, func(a, b openapi.Pole) int {
		return cmp.Or(
			cmp.Compare(rank(a), rank(b)),
			compareLines(platform(a), platform(b)),
			strings.Compare(a.Id, b.Id),
		)
	})
}
