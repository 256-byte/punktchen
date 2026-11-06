// Package pgtest starts a throwaway PostGIS container for integration tests.
// Tests using it need Docker and are skipped with `go test -short`.
package pgtest

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image is the database image for tests. Keep in sync with docker-compose.yml.
const Image = "postgis/postgis:18-3.7"

// Start runs a fresh database container for the test and returns its URL.
// The container is removed when the test ends.
func Start(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker (skipped with -short)")
	}
	ctx := context.Background()

	ctr, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("livestop_test"),
		postgres.WithUsername("livestop"),
		postgres.WithPassword("livestop"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	return url
}
