package amenities

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/geo"
)

// SourceFountains is the meta.feed_imports source of the fountain dataset.
const SourceFountains = "berlin-fountains"

// DefaultFountainsURL is the Geoportal Berlin WFS for drinking fountains as
// GeoJSON in WGS84 (DATA_MODEL §7 #34).
const DefaultFountainsURL = "https://gdi.berlin.de/services/wfs/trinkwasserbrunnen?service=WFS&version=2.0.0&request=GetFeature" +
	"&typeNames=trinkwasserbrunnen:trinkwasserbrunnen&outputFormat=application/json&srsName=EPSG:4326"

// seasonPattern finds "Betriebszeit: Mai bis Oktober" in "informationen".
var seasonPattern = regexp.MustCompile(`(?i)Betriebszeit:\s*(\p{L}+)\s+bis\s+(\p{L}+)`)

var germanMonths = map[string]int16{
	"januar": 1, "februar": 2, "märz": 3, "april": 4, "mai": 5, "juni": 6,
	"juli": 7, "august": 8, "september": 9, "oktober": 10, "november": 11, "dezember": 12,
}

// ParseFountains maps the WFS answer to table rows (DATA_MODEL §3.7). The
// season comes from "informationen"; a fountain without a readable season
// fails the whole import, so the old rows stay (an unknown season must not
// read as "all year").
func ParseFountains(body []byte, scope geo.BBox) ([]queries.InsertFountainsParams, error) {
	features, err := decode(body, scope)
	if err != nil {
		return nil, err
	}
	rows := make([]queries.InsertFountainsParams, 0, len(features))
	var errs []error
	for _, f := range features {
		row, err := fountainRow(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		rows = append(rows, row)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return rows, nil
}

func fountainRow(f feature) (queries.InsertFountainsParams, error) {
	from, to, err := season(f.text("informationen"))
	if err != nil {
		return queries.InsertFountainsParams{}, fmt.Errorf("feature %s: %w", f.ID, err)
	}
	built, err := f.year("baujahr")
	if err != nil {
		return queries.InsertFountainsParams{}, err
	}
	location := f.text("standort")
	if location == "" {
		location = f.ID
	}
	return queries.InsertFountainsParams{
		ID:              f.ID,
		Lat:             f.lat(),
		Lng:             f.lng(),
		Location:        location,
		District:        f.optText("bezirk"),
		Model:           f.optText("trinkbrunnenart"),
		BuiltYear:       built,
		SeasonFromMonth: from,
		SeasonToMonth:   to,
		OutOfService:    f.optText("einschraenkungen"),
	}, nil
}

// season reads "Betriebszeit: <month> bis <month>" (German month names).
func season(info string) (from, to int16, err error) {
	m := seasonPattern.FindStringSubmatch(info)
	if m == nil {
		return 0, 0, fmt.Errorf("no season in %q", info)
	}
	from, okFrom := germanMonths[strings.ToLower(m[1])]
	to, okTo := germanMonths[strings.ToLower(m[2])]
	if !okFrom || !okTo {
		return 0, 0, fmt.Errorf("unknown month in %q", m[0])
	}
	return from, to, nil
}
