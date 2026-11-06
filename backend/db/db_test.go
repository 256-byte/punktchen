package db

import (
	"fmt"
	"io/fs"
	"regexp"
	"testing"
)

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.(up|down)\.sql$`)

// TestMigrationFiles enforces the file conventions: NNNN_title.{up,down}.sql,
// versions 1..n without gaps, and a down file for every up file.
func TestMigrationFiles(t *testing.T) {
	entries, err := fs.ReadDir(Migrations, MigrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations embedded")
	}

	up := map[int]bool{}
	down := map[int]bool{}
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("bad migration file name %q (want NNNN_title.up.sql / .down.sql)", e.Name())
			continue
		}
		var v int
		_, _ = fmt.Sscan(m[1], &v)
		if m[2] == "up" {
			if up[v] {
				t.Errorf("duplicate up migration for version %d", v)
			}
			up[v] = true
		} else {
			down[v] = true
		}
	}

	for v := 1; v <= len(up); v++ {
		if !up[v] {
			t.Errorf("missing up migration for version %04d (versions must be 1..n without gaps)", v)
		}
		if !down[v] {
			t.Errorf("missing down migration for version %04d", v)
		}
	}
	for v := range down {
		if !up[v] {
			t.Errorf("down migration %04d has no up migration", v)
		}
	}
}
