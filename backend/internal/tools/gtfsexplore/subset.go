package main

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// subset writes a small but real GTFS zip: every trip that serves a pole
// within radius of the center (or another pole of the same station), kept
// whole with all its stop times, stops, route, agency and calendar rows.
// Used as a test fixture that matches the trimmed GTFS-RT samples.
func subset(w *printer, zipPath, dst string, lat, lon, radius float64) error {
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	stops, err := loadStops(z)
	if err != nil {
		return err
	}

	// Stations with a pole in the circle (grouped like DATA_MODEL §3.4).
	station := func(id string) string {
		if s := stops[id]; s != nil && s.parent != "" {
			return s.parent
		}
		if p := dhidStation(id); p != "" {
			return p
		}
		return id
	}
	nearStations := map[string]bool{}
	for id, s := range stops {
		if (s.locType == "" || s.locType == "0") && distanceMeters(lat, lon, s.lat, s.lon) <= radius {
			nearStations[station(id)] = true
		}
	}

	// Trips serving one of those stations.
	keepTrips := map[string]bool{}
	if _, err := scan(z, "stop_times.txt", func(get func(string) string) error {
		if nearStations[station(get("stop_id"))] {
			keepTrips[get("trip_id")] = true
		}
		return nil
	}); err != nil {
		return err
	}

	out, err := newZipOut(dst)
	if err != nil {
		return err
	}
	defer out.abort()

	keepStops, keepRoutes, keepServices, keepAgencies := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	var parents map[string]bool // parent stations of kept poles
	steps := []struct {
		file string
		keep func(get func(string) string) bool
	}{
		{"stop_times.txt", func(get func(string) string) bool {
			if !keepTrips[get("trip_id")] {
				return false
			}
			keepStops[get("stop_id")] = true
			return true
		}},
		{"trips.txt", func(get func(string) string) bool {
			if !keepTrips[get("trip_id")] {
				return false
			}
			keepRoutes[get("route_id")] = true
			keepServices[get("service_id")] = true
			return true
		}},
		{"routes.txt", func(get func(string) string) bool {
			if !keepRoutes[get("route_id")] {
				return false
			}
			keepAgencies[get("agency_id")] = true
			return true
		}},
		{"agency.txt", func(get func(string) string) bool { return keepAgencies[get("agency_id")] }},
		{"calendar.txt", func(get func(string) string) bool { return keepServices[get("service_id")] }},
		{"calendar_dates.txt", func(get func(string) string) bool { return keepServices[get("service_id")] }},
		{"stops.txt", func(get func(string) string) bool {
			if parents == nil { // first row: stop_times is done, keepStops is final
				parents = map[string]bool{}
				for kept := range keepStops {
					if s := stops[kept]; s != nil && s.parent != "" {
						parents[s.parent] = true
					}
				}
			}
			id := get("stop_id")
			// Also every pole of a near station, served or not, so the
			// fixture's stations are complete.
			isPole := get("location_type") == "" || get("location_type") == "0"
			return keepStops[id] || parents[id] || (isPole && nearStations[station(id)])
		}},
	}
	for _, step := range steps {
		n, err := out.copyFiltered(z, step.file, step.keep)
		if err != nil {
			return err
		}
		w.printf("  %-20s %8d rows\n", step.file, n)
	}
	if err := out.close(); err != nil {
		return err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(nearStations))
	for s := range nearStations {
		names = append(names, s)
	}
	sort.Strings(names)
	w.printf("%d stations near %.4f,%.4f (r=%.0f m), %d trips → %s (%d bytes)\n", len(names), lat, lon, radius, len(keepTrips), dst, info.Size())
	if info.Size() > 5<<20 {
		return fmt.Errorf("%s is %d bytes, over the 5 MB fixture limit (CLAUDE.md rule 11)", dst, info.Size())
	}
	return nil
}

type zipOut struct {
	path string
	f    *os.File
	zw   *zip.Writer
}

func newZipOut(path string) (*zipOut, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &zipOut{path: path, f: f, zw: zip.NewWriter(f)}, nil
}

// copyFiltered writes the header and every row of name for which keep is true.
func (o *zipOut) copyFiltered(z *zip.ReadCloser, name string, keep func(get func(string) string) bool) (int, error) {
	t, err := openTable(z, name)
	if err != nil {
		return 0, err
	}
	header := make([]string, len(t.cols))
	for col, i := range t.cols {
		header[i] = col
	}
	fw, err := o.zw.Create(name)
	if err != nil {
		return 0, err
	}
	cw := csv.NewWriter(fw)
	if err := cw.Write(header); err != nil {
		return 0, err
	}
	written := 0
	var rec []string
	_, err = t.each(func(get func(string) string) error {
		if !keep(get) {
			return nil
		}
		rec = rec[:0]
		for _, col := range header {
			rec = append(rec, get(col))
		}
		written++
		return cw.Write(rec)
	})
	if err != nil {
		return 0, err
	}
	cw.Flush()
	return written, cw.Error()
}

func (o *zipOut) close() error {
	err := errors.Join(o.zw.Close(), o.f.Close())
	o.f = nil
	return err
}

// abort removes a partly written file after an error.
func (o *zipOut) abort() {
	if o.f == nil {
		return
	}
	_ = o.zw.Close()
	_ = o.f.Close()
	_ = os.Remove(o.path)
}
