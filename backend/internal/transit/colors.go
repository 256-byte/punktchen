package transit

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/256-byte/punktchen/backend/gen/openapi"
)

// linienfarbenCSV is VBB's official line colour list (data/README.md).
//
//go:embed data/linienfarben.csv
var linienfarbenCSV string

// colorKey identifies a line in the colour list. The mode, not the raw
// route_type, is part of the key: the list says 100 where the feed says 106
// for the same regional train, and a rail replacement bus named like a
// train line (mode bus) must not get the train's colour.
type colorKey struct {
	agency, line string
	mode         openapi.Mode
}

// listColors are the colours of the VBB list, as "#RRGGBB".
var listColors = mustParseLineColors(linienfarbenCSV)

func mustParseLineColors(s string) map[colorKey]string {
	m, err := parseLineColors(s)
	if err != nil {
		panic("transit: embedded line colours: " + err.Error())
	}
	return m
}

// parseLineColors reads the list. Rows without agency_id are generic mode
// colours ("Tram", "Bus", "Fähre"), not line colours, and are skipped.
func parseLineColors(s string) (map[colorKey]string, error) {
	r := csv.NewReader(strings.NewReader(s))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no header")
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	for _, name := range []string{"Linie", "agency_id", "route_Type", "background_R", "background_G", "background_B"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("column %q missing", name)
		}
	}

	out := map[colorKey]string{}
	for n, row := range rows[1:] {
		line, agency := row[col["Linie"]], row[col["agency_id"]]
		if agency == "" {
			continue
		}
		routeType, err := strconv.Atoi(row[col["route_Type"]])
		if err != nil {
			return nil, fmt.Errorf("row %d (%s): route_Type: %w", n+2, line, err)
		}
		var rgb [3]int
		for i, c := range []string{"background_R", "background_G", "background_B"} {
			v, err := strconv.Atoi(row[col[c]])
			if err != nil || v < 0 || v > 255 {
				return nil, fmt.Errorf("row %d (%s): %s %q", n+2, line, c, row[col[c]])
			}
			rgb[i] = v
		}
		k := colorKey{agency: agency, line: line, mode: modeOf(routeType)}
		c := fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
		if prev, ok := out[k]; ok && prev != c {
			return nil, fmt.Errorf("row %d: %v listed twice with %s and %s", n+2, k, prev, c)
		}
		out[k] = c
	}
	return out, nil
}

// lineColor is the colour of a direction's line: the feed's route_color,
// else the VBB list, else nil (DATA_MODEL §5.2).
func lineColor(routeColor, agency, line string, mode openapi.Mode) *string {
	if c, ok := normalizeColor(routeColor); ok {
		return &c
	}
	if c, ok := listColors[colorKey{agency: agency, line: line, mode: mode}]; ok {
		return &c
	}
	return nil
}

// normalizeColor turns a GTFS colour ("8c6dab") into "#8C6DAB".
func normalizeColor(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return "", false
	}
	if _, err := strconv.ParseUint(s, 16, 32); err != nil {
		return "", false
	}
	return "#" + strings.ToUpper(s), true
}

// modeOf maps a GTFS route_type to a mode. It must match the CASE in
// db/gtfs/derived.sql (pole_directions.mode), DATA_MODEL §6; the feed's
// directions get their mode there, this is only used for the colour list.
func modeOf(routeType int) openapi.Mode {
	switch routeType {
	case 109:
		return openapi.ModeSbahn
	case 400, 1:
		return openapi.ModeUbahn
	case 900, 0:
		return openapi.ModeTram
	case 700, 3:
		return openapi.ModeBus
	case 1000, 4:
		return openapi.ModeFerry
	case 100, 106, 2:
		return openapi.ModeRegional
	default:
		return openapi.ModeOther
	}
}
