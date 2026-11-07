// Command sqlc runs the pinned sqlc release in Docker on the backend module:
//
//	go run ./internal/tools/sqlc generate   # what `go generate ./...` calls
//	go run ./internal/tools/sqlc vet
//
// Why Docker and not `go tool`: sqlc's PostgreSQL parser (pg_query_go) needs
// cgo and a C toolchain, which the Windows dev machine does not have. Docker
// is already required (Compose, testcontainers), and the image pinned by
// digest gives byte-identical output on every machine and in CI.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// image is sqlc v1.31.1. Bump tag and digest together; the version also
// appears in the header of every generated file.
const image = "sqlc/sqlc:1.31.1@sha256:70f53171d27b2424e9358869975455a6e955a5aa8e58a998a270a6e34e525537"

func main() {
	if err := run(os.Args[1:]); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode()) // sqlc already printed its errors
		}
		fmt.Fprintln(os.Stderr, "sqlc:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		args = []string{"generate"}
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := moduleRoot(wd)
	if err != nil {
		return err
	}

	dockerArgs := []string{"run", "--rm", "-v", root + ":/src", "-w", "/src"}
	if runtime.GOOS != "windows" {
		// Generated files belong to the caller, not root (Linux CI).
		dockerArgs = append(dockerArgs, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	dockerArgs = append(dockerArgs, image)
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("docker not found in PATH (sqlc runs in Docker)")
		}
		return err
	}
	return nil
}

// moduleRoot walks up from dir to the directory that holds sqlc.yaml and
// returns it as an absolute path (docker -v needs one).
func moduleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "sqlc.yaml")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no sqlc.yaml in %s or any parent", dir)
		}
		d = parent
	}
}
