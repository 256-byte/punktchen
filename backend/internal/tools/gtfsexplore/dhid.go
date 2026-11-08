package main

import (
	"archive/zip"
	"sort"
	"strings"
)

// dhidStation returns the station part of a German DHID stop id
// (de:<AGS>:<station>[:<area>:<platform>] or de:<AGS>:<station>::<pole>),
// or "" for ids that are not DHIDs.
func dhidStation(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) < 3 || parts[0] != "de" {
		return ""
	}
	return strings.Join(parts[:3], ":")
}

// reportDHID checks how poles relate to stations: via parent_station, via
// the DHID prefix, or not at all.
func reportDHID(w *printer, stops map[string]*stop) {
	section(w, "pole → station relation (parent_station vs. DHID prefix)")
	rel := counter{}
	prefixPoles := map[string]int{}
	parentEqualsPrefix := 0
	for id, s := range stops {
		if s.locType != "" && s.locType != "0" {
			continue
		}
		prefix := dhidStation(id)
		if prefix != "" {
			prefixPoles[prefix]++
		}
		_, prefixIsStation := stops[prefix]
		switch {
		case s.parent != "":
			rel["has parent_station"]++
			if s.parent == prefix {
				parentEqualsPrefix++
			}
		case prefix == "":
			rel["no parent, not a DHID"]++
		case prefixIsStation:
			rel["no parent, DHID prefix is a stop in stops.txt"]++
		default:
			rel["no parent, DHID prefix not in stops.txt"]++
		}
	}
	w.printf("  %s\n", rel.format(0))
	w.printf("  parent_station equals DHID prefix: %d of %d\n", parentEqualsPrefix, rel["has parent_station"])
	dist := counter{}
	for _, n := range prefixPoles {
		switch {
		case n == 1:
			dist["1"]++
		case n <= 4:
			dist["2-4"]++
		case n <= 10:
			dist["5-10"]++
		default:
			dist[">10"]++
		}
	}
	w.printf("  distinct DHID station prefixes of poles: %d; poles per prefix: %s\n", len(prefixPoles), dist.format(0))

	// Do poles with the same DHID prefix share a name?
	names := map[string]map[string]bool{}
	for id, s := range stops {
		if p := dhidStation(id); p != "" && (s.locType == "" || s.locType == "0") {
			if names[p] == nil {
				names[p] = map[string]bool{}
			}
			names[p][s.name] = true
		}
	}
	multi := 0
	var example string
	for p, ns := range names {
		if len(ns) > 1 {
			multi++
			if example == "" || p < example {
				example = p
			}
		}
	}
	w.printf("  prefixes whose poles have different stop_names: %d", multi)
	if example != "" {
		var ns []string
		for n := range names[example] {
			ns = append(ns, n)
		}
		sort.Strings(ns)
		w.printf(" (e.g. %s: %q)", example, ns)
	}
	w.printf("\n")
}

// listStops prints every stop whose id starts with prefix or whose name
// contains nameSub.
func listStops(w *printer, zipPath, prefix, nameSub string) error {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	stops, err := loadStops(z)
	if err != nil {
		return err
	}
	var ids []string
	for id, s := range stops {
		if (prefix != "" && strings.HasPrefix(id, prefix)) || (nameSub != "" && strings.Contains(s.name, nameSub)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := stops[id]
		w.printf("%-28s type=%-2q parent=%-22q platform=%-4q %.5f,%.5f %s\n", id, s.locType, s.parent, s.platform, s.lat, s.lon, s.name)
	}
	w.printf("(%d stops)\n", len(ids))
	return nil
}
