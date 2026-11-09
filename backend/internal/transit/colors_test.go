package transit

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/256-byte/punktchen/backend/gen/openapi"
)

func TestLineColorList(t *testing.T) {
	// Spot checks against the published list.
	for _, c := range []struct {
		agency, line string
		mode         openapi.Mode
		want         string
	}{
		{"796", "U6", openapi.ModeUbahn, "#8C6DAB"},
		{"796", "U4", openapi.ModeUbahn, "#F0D722"},
		{"1", "S45", openapi.ModeSbahn, "#CD9C53"},
		{"1", "S41", openapi.ModeSbahn, "#AD5937"},
		{"596", "RB25", openapi.ModeRegional, "#007CB0"}, // list says route_Type 100, feed 106: same mode
		{"47", "B", openapi.ModeBus, "#201771"},          // no hex in the list, only R/G/B
	} {
		if got := listColors[colorKey{c.agency, c.line, c.mode}]; got != c.want {
			t.Errorf("%s %s %s: %q, want %q", c.agency, c.line, c.mode, got, c.want)
		}
	}
	for k := range listColors {
		if k.agency == "" || k.mode == openapi.ModeOther {
			t.Errorf("unexpected key %+v", k)
		}
	}
	if n := len(listColors); n < 140 {
		t.Errorf("%d colours, want the ~150 lines of the list", n)
	}

	// Where the list has a hex value too, it must equal R/G/B.
	rows, err := func() ([][]string, error) {
		r := csv.NewReader(strings.NewReader(linienfarbenCSV))
		r.Comma = ';'
		return r.ReadAll()
	}()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	checked := 0
	for _, row := range rows[1:] {
		hex := row[col["background_Hex"]]
		if hex == "" || row[col["agency_id"]] == "" {
			continue
		}
		k := colorKey{agency: row[col["agency_id"]], line: row[col["Linie"]], mode: modeOf(atoi(t, row[col["route_Type"]]))}
		if got := listColors[k]; !strings.EqualFold(got, hex) {
			t.Errorf("%v: R/G/B gives %s, hex column %s", k, got, hex)
		}
		checked++
	}
	if checked < 70 {
		t.Errorf("only %d rows with hex checked", checked)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("not a number: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func TestParseLineColorsErrors(t *testing.T) {
	head := "Linie;agency_id;route_Type;background_R;background_G;background_B\n"
	for name, s := range map[string]string{
		"missing column": "Linie;agency_id\nU6;796\n",
		"bad route type": head + "U6;796;x;1;2;3\n",
		"bad colour":     head + "U6;796;400;1;2;300\n",
		"conflict":       head + "U6;796;400;1;2;3\nU6;796;400;4;5;6\n",
	} {
		if _, err := parseLineColors(s); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	m, err := parseLineColors(head + "Tram;;;226;0;26\nU6;796;400;140;109;171\nU6;796;400;140;109;171\n")
	if err != nil || len(m) != 1 {
		t.Errorf("generic rows skipped, duplicates with the same colour allowed: %v %v", m, err)
	}
}

func TestLineColor(t *testing.T) {
	for _, c := range []struct {
		name                     string
		routeColor, agency, line string
		mode                     openapi.Mode
		want                     string // "" = nil
	}{
		{"route_color wins", "8c6dab", "796", "U6", openapi.ModeUbahn, "#8C6DAB"},
		{"route_color with #", "#ff0000", "796", "U6", openapi.ModeUbahn, "#FF0000"},
		{"list when route_color empty", "", "1", "S45", openapi.ModeSbahn, "#CD9C53"},
		{"list when route_color invalid", "red", "1", "S45", openapi.ModeSbahn, "#CD9C53"},
		{"rail replacement bus gets no train colour", "", "1", "S45", openapi.ModeBus, ""},
		{"other agency", "", "999", "U6", openapi.ModeUbahn, ""},
		{"no colour at all", "", "796", "M10", openapi.ModeTram, ""},
	} {
		got := lineColor(c.routeColor, c.agency, c.line, c.mode)
		if (got == nil) != (c.want == "") || (got != nil && *got != c.want) {
			t.Errorf("%s: %v, want %q", c.name, deref(got), c.want)
		}
	}
}

func TestModeOf(t *testing.T) {
	// DATA_MODEL §6; the same CASE is in db/gtfs/derived.sql.
	for rt, want := range map[int]openapi.Mode{
		109: openapi.ModeSbahn, 400: openapi.ModeUbahn, 1: openapi.ModeUbahn,
		900: openapi.ModeTram, 0: openapi.ModeTram, 700: openapi.ModeBus, 3: openapi.ModeBus,
		1000: openapi.ModeFerry, 4: openapi.ModeFerry,
		100: openapi.ModeRegional, 106: openapi.ModeRegional, 2: openapi.ModeRegional,
		715: openapi.ModeOther,
	} {
		if got := modeOf(rt); got != want {
			t.Errorf("route_type %d: %s, want %s", rt, got, want)
		}
	}
}

func TestCompareLines(t *testing.T) {
	sorted := []string{"", "1", "2", "10", "M4", "M10", "M10E", "N7", "N40", "S1", "S41", "S42", "U6", "U8", "X9"}
	for i := range sorted {
		for j := range sorted {
			got := compareLines(sorted[i], sorted[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if (got < 0) != (want < 0) || (got > 0) != (want > 0) {
				t.Errorf("compareLines(%q, %q) = %d, want sign %d", sorted[i], sorted[j], got, want)
			}
		}
	}
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"S+U Wedding (Berlin)":   "S+U Wedding",
		"Rathaus (Potsdam)":      "Rathaus (Potsdam)",
		"U Alt-Tegel":            "U Alt-Tegel",
		"(Berlin) Hauptbahnhof":  "(Berlin) Hauptbahnhof",
		"Ringbahn S 41 (Berlin)": "Ringbahn S 41",
	} {
		if got := displayName(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
