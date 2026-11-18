package amenities

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/geo"
)

var berlin = geo.BBox{MinLon: 13.088, MinLat: 52.338, MaxLon: 13.761, MaxLat: 52.675}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/amenities/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func yes(p *bool) string {
	switch {
	case p == nil:
		return "?"
	case *p:
		return "ja"
	default:
		return "nein"
	}
}

// TestParseToiletsRecorded maps the recorded WFS answer; the expected
// numbers are those of DATA_MODEL §7 #34.
func TestParseToiletsRecorded(t *testing.T) {
	rows, err := ParseToilets(fixture(t, "toiletten-20261118.json"), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 525 {
		t.Fatalf("rows = %d, want 525", len(rows))
	}
	count := func(pred func(r queries.InsertToiletsParams) bool) int {
		n := 0
		for _, r := range rows {
			if pred(r) {
				n++
			}
		}
		return n
	}
	checks := []struct {
		name string
		got  int
		want int
	}{
		{"urinals", count(func(r queries.InsertToiletsParams) bool { return r.Kind == "urinal" }), 14},
		{"event only", count(func(r queries.InsertToiletsParams) bool { return r.EventOnly }), 16},
		{"open 24h", count(func(r queries.InsertToiletsParams) bool { return r.Open24h }), 330},
		{"no hours", count(func(r queries.InsertToiletsParams) bool { return r.OpeningHours == nil }), 41},
		{"free", count(func(r queries.InsertToiletsParams) bool { return r.FeeEur != nil && *r.FeeEur == 0 }), 323},
		{"fee unknown", count(func(r queries.InsertToiletsParams) bool { return r.FeeEur == nil }), 4},
		{"card/app only", count(func(r queries.InsertToiletsParams) bool { return str(r.Payment) == "Karte/App" }), 170},
		{"accessible", count(func(r queries.InsertToiletsParams) bool { return yes(r.Accessible) == "ja" }), 367},
		{"accessible unknown", count(func(r queries.InsertToiletsParams) bool { return r.Accessible == nil }), 42},
		{"mostly accessible", count(func(r queries.InsertToiletsParams) bool { return yes(r.MostlyAccessible) == "ja" }), 372},
		{"changing table", count(func(r queries.InsertToiletsParams) bool { return yes(r.ChangingTable) == "ja" }), 137},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}

	byID := map[string]queries.InsertToiletsParams{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	alex := byID["toiletten.2"]
	if alex.Address != "WC-Center Alexanderplatz 1" || str(alex.District) != "Mitte" || str(alex.Operator) != "Wall GmbH" ||
		alex.Kind != "toilet" || alex.EventOnly || alex.Open24h || str(alex.OpeningHours) != "Mo-Sa 07:00-22:00\nSo 10:00-22:00" ||
		alex.FeeEur == nil || *alex.FeeEur != 0.5 || str(alex.Payment) != "Bar" ||
		yes(alex.Accessible) != "nein" || yes(alex.MostlyAccessible) != "ja" || yes(alex.ChangingTable) != "ja" ||
		alex.Lat != 52.52146304 || alex.Lng != 13.412954 {
		t.Errorf("toiletten.2 = %+v", alex)
	}
	// "bei Veranstaltungen\n\n": stray empty lines dropped, event only.
	if r := byID["toiletten.171"]; str(r.OpeningHours) != "bei Veranstaltungen" || !r.EventOnly {
		t.Errorf("toiletten.171 = %q event=%v", str(r.OpeningHours), r.EventOnly)
	}
}

func TestParseFountainsRecorded(t *testing.T) {
	rows, err := ParseFountains(fixture(t, "trinkwasserbrunnen-20261118.json"), berlin)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 242 {
		t.Fatalf("rows = %d, want 242", len(rows))
	}
	outOfService := 0
	for _, r := range rows {
		if r.SeasonFromMonth != 5 || r.SeasonToMonth != 10 {
			t.Errorf("%s: season %d–%d, want 5–10", r.ID, r.SeasonFromMonth, r.SeasonToMonth)
		}
		if r.OutOfService != nil {
			outOfService++
		}
	}
	if outOfService != 8 {
		t.Errorf("out of service = %d, want 8", outOfService)
	}
	first := rows[0]
	if first.ID != "trinkwasserbrunnen.1" || str(first.Model) != "Kaiser" || str(first.District) != "Neukölln" ||
		first.BuiltYear == nil || *first.BuiltYear != 1985 ||
		str(first.OutOfService) != "zur Zeit wegen Reparatur außer Betrieb" ||
		!strings.HasPrefix(first.Location, "Sangerhauser Weg") {
		t.Errorf("first = %+v", first)
	}
}

func TestParseRejects(t *testing.T) {
	point := func(id, lng, lat, props string) string {
		return `{"type":"Feature","id":"` + id + `","geometry":{"type":"Point","coordinates":[` + lng + `,` + lat + `]},"properties":{` + props + `}}`
	}
	collection := func(features ...string) []byte {
		return []byte(`{"type":"FeatureCollection","features":[` + strings.Join(features, ",") + `]}`)
	}
	const okToilet = `"standort":"A","symbol":"WC","barrierefrei":"ja"`
	const okFountain = `"standort":"A","informationen":"Betriebszeit: Mai bis Oktober"`

	toilets := []struct {
		name, wantErr string
		body          []byte
	}{
		{"not json", "decode geojson", []byte(`<html>`)},
		{"not a collection", "want FeatureCollection", []byte(`{"type":"Feature"}`)},
		{"no id", "no id", collection(point("", "13.4", "52.5", okToilet))},
		{"duplicate id", "duplicate id", collection(point("t.1", "13.4", "52.5", okToilet), point("t.1", "13.4", "52.5", okToilet))},
		{"no point", "not a point", collection(`{"type":"Feature","id":"t.1","geometry":null,"properties":{}}`)},
		{"outside scope", "outside the scope", collection(point("t.1", "11.5", "48.1", okToilet))},
		{"bad yes/no", "want ja or nein", collection(point("t.1", "13.4", "52.5", `"barrierefrei":"vielleicht"`))},
		{"fee not a number", "want a number", collection(point("t.1", "13.4", "52.5", `"nutzungsentgelt":"0,50 €"`))},
	}
	for _, tc := range toilets {
		t.Run("toilets/"+tc.name, func(t *testing.T) {
			if _, err := ParseToilets(tc.body, berlin); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}

	fountains := []struct {
		name, wantErr string
		body          []byte
	}{
		{"no season", "no season", collection(point("f.1", "13.4", "52.5", `"standort":"A","informationen":"Link: x"`))},
		{"unknown month", "unknown month", collection(point("f.1", "13.4", "52.5", `"informationen":"Betriebszeit: Frühling bis Herbst"`))},
		{"bad year", "want a year", collection(point("f.1", "13.4", "52.5", okFountain+`,"baujahr":19.5`))},
	}
	for _, tc := range fountains {
		t.Run("fountains/"+tc.name, func(t *testing.T) {
			if _, err := ParseFountains(tc.body, berlin); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}

	// Minimal valid features: optional fields missing → nil, address falls back to the id.
	rows, err := ParseToilets(collection(point("t.1", "13.4", "52.5", `"symbol":"Pissoir"`)), berlin)
	if err != nil || len(rows) != 1 || rows[0].Kind != "urinal" || rows[0].Address != "t.1" ||
		rows[0].Accessible != nil || rows[0].FeeEur != nil || rows[0].OpeningHours != nil || rows[0].Open24h {
		t.Fatalf("minimal toilet = %+v, %v", rows, err)
	}
}

func TestCheck(t *testing.T) {
	lim := SanityLimits{MinRows: 100, MaxDrop: 0.3}
	if err := check(150, 0, lim); err != nil {
		t.Errorf("first import: %v", err)
	}
	if err := check(150, 200, lim); err != nil {
		t.Errorf("25 %% drop: %v", err)
	}
	if err := check(130, 200, lim); err == nil || !strings.Contains(err.Error(), "dropped from 200 to 130") {
		t.Errorf("35 %% drop: %v", err)
	}
	if err := check(50, 0, lim); err == nil || !strings.Contains(err.Error(), "only 50 rows") {
		t.Errorf("too few: %v", err)
	}
}

// TestLiveWFS parses today's answers of the real Geoportal Berlin WFS.
// Manual only: PUNKTCHEN_LIVE_TESTS=1 go test -run LiveWFS ./internal/amenities/
func TestLiveWFS(t *testing.T) {
	if os.Getenv("PUNKTCHEN_LIVE_TESTS") != "1" {
		t.Skip("set PUNKTCHEN_LIVE_TESTS=1 to download from gdi.berlin.de")
	}
	get := func(url string) []byte {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "punktchen-test (+https://github.com/256-byte/punktchen)")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %s, %v", url, resp.Status, err)
		}
		return body
	}
	toilets, err := ParseToilets(get(DefaultToiletsURL), berlin)
	if err != nil || len(toilets) < Toilets("").Limits.MinRows {
		t.Fatalf("toilets: %d rows, %v", len(toilets), err)
	}
	fountains, err := ParseFountains(get(DefaultFountainsURL), berlin)
	if err != nil || len(fountains) < Fountains("").Limits.MinRows {
		t.Fatalf("fountains: %d rows, %v", len(fountains), err)
	}
	t.Logf("live: %d toilets, %d fountains", len(toilets), len(fountains))
}
