package static

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/db"
)

func TestParseGTFSTime(t *testing.T) {
	ok := map[string]int{
		"00:00:00": 0,
		"9:20:00":  9*3600 + 20*60, // single-digit hour, as in VBB data
		"09:20:00": 9*3600 + 20*60,
		"23:59:59": 86399,
		"25:10:00": 25*3600 + 10*60,
		"31:17:00": 31*3600 + 17*60, // VBB maximum (DATA_MODEL §7 #10)
	}
	for in, want := range ok {
		if got, err := parseGTFSTime(in); err != nil || got != want {
			t.Errorf("parseGTFSTime(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "9:20", "9:2:00", "09:60:00", "09:00:60", "-1:00:00", "aa:00:00", "09:00:00 ", "1000:00:00"} {
		if _, err := parseGTFSTime(in); err == nil {
			t.Errorf("parseGTFSTime(%q): want error", in)
		}
	}
}

func TestConvert(t *testing.T) {
	date := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		col  column
		raw  string
		want []any
	}{
		{opt("x", kText), "abc", []any{"abc"}},
		{opt("x", kText), "", []any{nil}},
		{opt("x", kInt), "700", []any{int32(700)}},
		{opt("x", kFloat), "52.531683000000", []any{52.531683}},
		{req("x", kBool), "1", []any{true}},
		{req("x", kBool), "0", []any{false}},
		{req("x", kDate), "20261105", []any{date}},
		{opt("arrival_time", kTime), "25:10:00", []any{"25:10:00", int32(90600)}},
		{opt("arrival_time", kTime), "", []any{nil, nil}},
	}
	for _, c := range cases {
		got, err := c.col.convert(nil, c.raw)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%v.convert(%q) = %v, %v; want %v", c.col, c.raw, got, err, c.want)
		}
	}
	for _, c := range []struct {
		col column
		raw string
	}{
		{req("x", kText), ""},
		{opt("x", kInt), "1.5"},
		{req("x", kBool), "yes"},
		{req("x", kDate), "2026-11-05"},
		{opt("x", kTime), "noon"},
	} {
		if _, err := c.col.convert(nil, c.raw); err == nil {
			t.Errorf("%v.convert(%q): want error", c.col, c.raw)
		}
	}
}

var createTable = regexp.MustCompile(`(?s)CREATE TABLE gtfs\.(\w+) \((.*?)\n\);`)

// TestSpecsMatchSchemaFile keeps tables.go and db/gtfs/schema.sql in sync:
// every column that COPY writes exists, in the same order, and the only
// extra columns are generated ones or ones with a DEFAULT that are set after
// loading (stops.in_scope).
func TestSpecsMatchSchemaFile(t *testing.T) {
	ddlCols := map[string][]string{}
	for _, m := range createTable.FindAllStringSubmatch(db.GTFSSchema, -1) {
		for _, line := range strings.Split(m[2], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(line, "GENERATED") || strings.Contains(line, "DEFAULT") {
				continue
			}
			if strings.Contains(line, "geography(") {
				continue // generated geom column, filled by Postgres
			}
			ddlCols[m[1]] = append(ddlCols[m[1]], strings.Fields(line)[0])
		}
	}
	var ddlTables []string
	for name := range ddlCols {
		ddlTables = append(ddlTables, name)
	}
	slices.Sort(ddlTables)
	want := append(loadedTables(), derivedTables...)
	slices.Sort(want)
	if !slices.Equal(ddlTables, want) {
		t.Fatalf("schema.sql tables %v, want loaded + derived %v", ddlTables, want)
	}
	for _, spec := range tables {
		if got, want := spec.dbColumns(), ddlCols[spec.table]; !slices.Equal(got, want) {
			t.Errorf("%s:\n tables.go  %v\n schema.sql %v", spec.table, got, want)
		}
	}
}

func TestStagingDDLRewritesEverySchemaReference(t *testing.T) {
	ddl := toStaging(db.GTFSSchema) + toStaging(db.GTFSIndexes)
	if regexp.MustCompile(`\bgtfs\b`).MatchString(ddl) {
		t.Fatalf("staging DDL still references schema gtfs:\n%s", ddl)
	}
	if !strings.Contains(ddl, "CREATE SCHEMA gtfs_staging;") || !strings.Contains(ddl, "CREATE TABLE gtfs_staging.stop_times") {
		t.Fatal("staging DDL does not create the staging schema")
	}
}
