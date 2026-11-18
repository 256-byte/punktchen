package amenities

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

// featureCollection is the part of a WFS GeoJSON answer we read
// (outputFormat=application/json, srsName=EPSG:4326; DATA_MODEL §7 #34).
type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	ID       string `json:"id"`
	Geometry *struct {
		Type        string    `json:"type"`
		Coordinates []float64 `json:"coordinates"`
	} `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

// decode parses a FeatureCollection of Points inside scope. Every feature
// must have an id and a point in scope; the first problems are reported
// together (at most 5), so a broken upstream answer is easy to read in the
// import log.
func decode(body []byte, scope geo.BBox) ([]feature, error) {
	var fc featureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("decode geojson: %w", err)
	}
	if fc.Type != "FeatureCollection" {
		return nil, fmt.Errorf("decode geojson: type %q, want FeatureCollection", fc.Type)
	}
	var problems []string
	seen := make(map[string]bool, len(fc.Features))
	for i, f := range fc.Features {
		switch {
		case f.ID == "":
			problems = append(problems, fmt.Sprintf("feature %d: no id", i))
		case seen[f.ID]:
			problems = append(problems, fmt.Sprintf("feature %s: duplicate id", f.ID))
		case f.Geometry == nil || f.Geometry.Type != "Point" || len(f.Geometry.Coordinates) < 2:
			problems = append(problems, fmt.Sprintf("feature %s: not a point", f.ID))
		case !inside(scope, f.lng(), f.lat()):
			problems = append(problems, fmt.Sprintf("feature %s: %.5f,%.5f outside the scope %s", f.ID, f.lat(), f.lng(), scope))
		}
		seen[f.ID] = true
		if len(problems) == 5 {
			problems = append(problems, "…")
			break
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("decode geojson: %s", strings.Join(problems, "; "))
	}
	return fc.Features, nil
}

func (f feature) lng() float64 { return f.Geometry.Coordinates[0] }
func (f feature) lat() float64 { return f.Geometry.Coordinates[1] }

func inside(b geo.BBox, lng, lat float64) bool {
	return lng >= b.MinLon && lng <= b.MaxLon && lat >= b.MinLat && lat <= b.MaxLat
}

// text returns a trimmed string property; "" when missing, null or not a string.
func (f feature) text(key string) string {
	s, _ := f.Properties[key].(string)
	return strings.TrimSpace(s)
}

// optText is text as a pointer, nil when empty.
func (f feature) optText(key string) *string {
	s := f.text(key)
	if s == "" {
		return nil
	}
	return &s
}

// multiline trims every line of a text property and drops empty lines
// (opening hours carry stray "\n\n\n", DATA_MODEL §7 #34); nil when empty.
func (f feature) multiline(key string) *string {
	var lines []string
	for _, l := range strings.Split(f.text(key), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	s := strings.Join(lines, "\n")
	return &s
}

// number returns a numeric property, nil when missing or null.
func (f feature) number(key string) (*float64, error) {
	switch v := f.Properties[key].(type) {
	case nil:
		return nil, nil
	case float64:
		return &v, nil
	default:
		return nil, fmt.Errorf("feature %s: %s = %v, want a number", f.ID, key, v)
	}
}

// yesNo maps "ja" / "nein" to a bool and missing or null to nil; anything
// else is an error (the source only uses these, DATA_MODEL §7 #34).
func (f feature) yesNo(key string) (*bool, error) {
	v := f.Properties[key]
	if v == nil {
		return nil, nil
	}
	s, _ := v.(string)
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ja":
		b := true
		return &b, nil
	case "nein":
		b := false
		return &b, nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("feature %s: %s = %v, want ja or nein", f.ID, key, v)
	}
}

// year returns a whole-number property as a year, nil when missing.
func (f feature) year(key string) (*int16, error) {
	n, err := f.number(key)
	if err != nil || n == nil {
		return nil, err
	}
	if *n != math.Trunc(*n) || *n < 1800 || *n > 2100 {
		return nil, fmt.Errorf("feature %s: %s = %v, want a year", f.ID, key, *n)
	}
	y := int16(*n)
	return &y, nil
}
