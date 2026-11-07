package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModuleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sqlc.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "db", "queries")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{root, nested} {
		got, err := moduleRoot(dir)
		if err != nil || got != root {
			t.Errorf("moduleRoot(%s) = %q, %v; want %q", dir, got, err, root)
		}
	}

	if _, err := moduleRoot(t.TempDir()); err == nil {
		t.Error("moduleRoot without sqlc.yaml: want error")
	}
}

// The real config must be found from the package that runs go:generate.
func TestModuleRootFindsBackend(t *testing.T) {
	got, err := moduleRoot(filepath.Join("..", "..", "..", "db"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "backend" {
		t.Errorf("moduleRoot = %q, want the backend directory", got)
	}
}
