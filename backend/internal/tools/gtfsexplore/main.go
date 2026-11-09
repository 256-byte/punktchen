// Command gtfsexplore is the data exploration tool of PLAN task 1.1. It is a
// developer tool, not part of the server binary. Run it from backend/:
//
//	go run ./internal/tools/gtfsexplore download            # static GTFS zip → data/gtfs/GTFS.zip
//	go run ./internal/tools/gtfsexplore static              # report on the static feed
//	go run ./internal/tools/gtfsexplore stops -name Wedding # list matching stops
//	go run ./internal/tools/gtfsexplore record              # 5 GTFS-RT samples, 15 s apart → data/gtfsrt/
//	go run ./internal/tools/gtfsexplore rt data/gtfsrt/*.pb # report on RT samples vs. static feed
//	go run ./internal/tools/gtfsexplore trim data/gtfsrt/*.pb # small fixtures → testdata/gtfsrt/
//	go run ./internal/tools/gtfsexplore subset              # static fixture around U Wedding → testdata/gtfs/
//
// Downloaded data stays in data/ (git-ignored, CLAUDE.md rule 11). Findings
// go to docs/DATA_MODEL.md §7.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/256-byte/punktchen/backend/internal/config"
)

const (
	defaultZip    = "data/gtfs/GTFS.zip"
	defaultRawDir = "data/gtfsrt"
	userAgent     = "livestop-dev (data exploration)"
)

// printer writes the report. Write errors stick in the bufio.Writer and are
// returned by Flush at the end, so single printf calls need no checks.
type printer struct{ *bufio.Writer }

func (p *printer) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.Writer, format, args...)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	out := &printer{bufio.NewWriter(os.Stdout)}
	defer func() {
		if err := out.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, "gtfsexplore: write output:", err)
		}
	}()

	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	var err error
	switch cmd {
	case "download":
		url := fs.String("url", config.DefaultGTFSURL, "static GTFS zip URL")
		dst := fs.String("out", defaultZip, "destination file")
		_ = fs.Parse(args)
		err = download(out, *url, *dst)
	case "static":
		zipPath := fs.String("zip", defaultZip, "static GTFS zip")
		_ = fs.Parse(args)
		err = reportStatic(out, *zipPath)
	case "stops":
		zipPath := fs.String("zip", defaultZip, "static GTFS zip")
		prefix := fs.String("id", "", "list stops whose stop_id starts with this")
		nameSub := fs.String("name", "", "list stops whose stop_name contains this")
		_ = fs.Parse(args)
		err = listStops(out, *zipPath, *prefix, *nameSub)
	case "record":
		url := fs.String("url", config.DefaultGTFSRTURL, "GTFS-RT URL")
		n := fs.Int("n", 5, "number of samples")
		interval := fs.Duration("interval", 15e9, "time between samples (upstream limit: 7 requests/min)")
		dir := fs.String("out", defaultRawDir, "directory for raw samples")
		_ = fs.Parse(args)
		err = record(out, *url, *n, *interval, *dir)
	case "rt":
		zipPath := fs.String("zip", defaultZip, "static GTFS zip")
		_ = fs.Parse(args)
		err = reportRealtime(out, *zipPath, fs.Args())
	case "subset":
		zipPath := fs.String("zip", defaultZip, "static GTFS zip")
		dst := fs.String("out", "testdata/gtfs/vbb-wedding-800m.zip", "output zip")
		lat := fs.Float64("lat", 52.5427, "center latitude (default: U Wedding)")
		lon := fs.Float64("lon", 13.3660, "center longitude")
		radius := fs.Float64("radius", 800, "radius in meters")
		_ = fs.Parse(args)
		err = subset(out, *zipPath, *dst, *lat, *lon, *radius)
	case "trim":
		zipPath := fs.String("zip", defaultZip, "static GTFS zip")
		dst := fs.String("out", "testdata/gtfsrt", "fixture directory")
		lat := fs.Float64("lat", 52.5427, "center latitude (default: U Wedding)")
		lon := fs.Float64("lon", 13.3660, "center longitude")
		radius := fs.Float64("radius", 1500, "radius in meters")
		_ = fs.Parse(args)
		err = trim(out, *zipPath, fs.Args(), *dst, *lat, *lon, *radius)
	default:
		usage()
	}
	if err != nil {
		_ = out.Flush()
		fmt.Fprintln(os.Stderr, "gtfsexplore:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gtfsexplore download|static|stops|subset|record|rt|trim [flags] [files]")
	os.Exit(2)
}
