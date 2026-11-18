package amenities

import (
	"errors"
	"strings"

	"github.com/256-byte/punktchen/backend/gen/queries"
	"github.com/256-byte/punktchen/backend/internal/geo"
)

// SourceToilets is the meta.feed_imports source of the toilet dataset.
const SourceToilets = "berlin-toilets"

// DefaultToiletsURL is the Geoportal Berlin WFS for public toilets as GeoJSON
// in WGS84 (DATA_MODEL §7 #34).
const DefaultToiletsURL = "https://gdi.berlin.de/services/wfs/toiletten?service=WFS&version=2.0.0&request=GetFeature" +
	"&typeNames=toiletten:toiletten&outputFormat=application/json&srsName=EPSG:4326"

// ParseToilets maps the WFS answer to table rows (DATA_MODEL §3.7):
//   - kind: "Pissoir" → urinal, every other symbol → toilet;
//   - eventOnly: the symbol or the opening hours say "bei Veranstaltungen",
//     or the hours say "Trauerfeier" (cemeteries, opened with a key);
//   - openingHours: lines trimmed, empty lines dropped; open24h when "24h".
func ParseToilets(body []byte, scope geo.BBox) ([]queries.InsertToiletsParams, error) {
	features, err := decode(body, scope)
	if err != nil {
		return nil, err
	}
	rows := make([]queries.InsertToiletsParams, 0, len(features))
	var errs []error
	for _, f := range features {
		row, err := toiletRow(f)
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

func toiletRow(f feature) (queries.InsertToiletsParams, error) {
	symbol := f.text("symbol")
	hours := f.multiline("oeffnungszeiten")
	hoursText := ""
	if hours != nil {
		hoursText = *hours
	}
	fee, errFee := f.number("nutzungsentgelt")
	accessible, errA := f.yesNo("barrierefrei")
	mostly, errM := f.yesNo("barrierearm")
	changing, errC := f.yesNo("wickeltisch")
	if err := errors.Join(errFee, errA, errM, errC); err != nil {
		return queries.InsertToiletsParams{}, err
	}
	address := f.text("standort")
	if address == "" {
		address = f.ID
	}
	kind := "toilet"
	if strings.EqualFold(symbol, "Pissoir") {
		kind = "urinal"
	}
	return queries.InsertToiletsParams{
		ID:               f.ID,
		Lat:              f.lat(),
		Lng:              f.lng(),
		Address:          address,
		District:         f.optText("bezirk"),
		Operator:         f.optText("betreiber"),
		Kind:             kind,
		EventOnly:        eventOnly(symbol, hoursText),
		OpeningHours:     hours,
		Open24h:          strings.EqualFold(hoursText, "24h"),
		FeeEur:           fee,
		Payment:          f.optText("zahlungsart"),
		Accessible:       accessible,
		MostlyAccessible: mostly,
		ChangingTable:    changing,
	}, nil
}

func eventOnly(symbol, hours string) bool {
	s, h := strings.ToLower(symbol), strings.ToLower(hours)
	return strings.Contains(s, "bei veranstaltungen") ||
		strings.Contains(h, "bei veranstaltungen") ||
		strings.Contains(h, "trauerfeier")
}
