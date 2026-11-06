package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/platform/pgtest"
)

func TestNewPoolConnects(t *testing.T) {
	url := pgtest.Start(t)
	ctx := context.Background()

	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var app string
	if err := pool.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&app); err != nil {
		t.Fatal(err)
	}
	if app != "livestop" {
		t.Fatalf("application_name = %q, want livestop", app)
	}
}

func TestNewPoolFailsFastWhenUnreachable(t *testing.T) {
	// Port 1 on localhost: nothing listens there, connection is refused at once.
	const url = "postgres://livestop:s3cret-pw@127.0.0.1:1/livestop?sslmode=disable&connect_timeout=2"

	start := time.Now()
	pool, err := NewPool(context.Background(), url)
	if err == nil {
		pool.Close()
		t.Fatal("want error for unreachable database")
	}
	if time.Since(start) > dbConnectTimeout {
		t.Fatalf("took %s, want fail fast", time.Since(start))
	}
	if strings.Contains(err.Error(), "s3cret-pw") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestNewPoolRejectsInvalidURL(t *testing.T) {
	if _, err := NewPool(context.Background(), "postgres://u:p@[bad"); err == nil {
		t.Fatal("want error for invalid URL")
	}
}
