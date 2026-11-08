package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

const testDBURL = "postgres://livestop:s3cret-pw@db:5432/livestop?sslmode=disable"

// env builds a LookupFunc from a map.
func env(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvDatabaseURL: testDBURL}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Dev:                false,
		LogLevel:           slog.LevelInfo,
		HTTPAddr:           ":8080",
		MetricsAddr:        ":9090",
		DatabaseURL:        Secret(testDBURL),
		GTFSURL:            DefaultGTFSURL,
		GTFSImportTime:     ClockTime{Hour: 3, Minute: 30},
		ScopeBBox:          geo.BBox{MinLon: 13.088, MinLat: 52.338, MaxLon: 13.761, MaxLat: 52.675},
		GTFSRTURL:          DefaultGTFSRTURL,
		GTFSRTPollInterval: 15 * time.Second,
	}
	if cfg != want {
		t.Fatalf("Load defaults:\n got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadAllSet(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		EnvDev:                "true",
		EnvLogLevel:           "DEBUG",
		EnvHTTPAddr:           "127.0.0.1:8081",
		EnvMetricsAddr:        ":9191",
		EnvDatabaseURL:        "postgresql://u:p@localhost:5433/x",
		EnvGTFSURL:            "http://example.test/gtfs.zip",
		EnvGTFSImportTime:     "04:05",
		EnvScopeBBox:          "13,52,14,53",
		EnvGTFSRTURL:          "https://example.test/rt",
		EnvGTFSRTPollInterval: "30s",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Dev || cfg.LogLevel != slog.LevelDebug || cfg.HTTPAddr != "127.0.0.1:8081" ||
		cfg.MetricsAddr != ":9191" || cfg.GTFSImportTime != (ClockTime{4, 5}) ||
		cfg.ScopeBBox != (geo.BBox{MinLon: 13, MinLat: 52, MaxLon: 14, MaxLat: 53}) ||
		cfg.GTFSRTPollInterval != 30*time.Second || cfg.GTFSURL != "http://example.test/gtfs.zip" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadEmptyValueMeansDefault(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvDatabaseURL: testDBURL, EnvHTTPAddr: "  ", EnvLogLevel: ""}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("empty values should fall back to defaults, got %+v", cfg)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		key, value string
	}{
		{EnvDev, "yes please"},
		{EnvLogLevel, "loud"},
		{EnvHTTPAddr, "8080"},
		{EnvHTTPAddr, ":99999"},
		{EnvMetricsAddr, ":http"},
		{EnvDatabaseURL, "mysql://u:p@h/db"},
		{EnvDatabaseURL, "host=localhost user=x"},
		{EnvGTFSURL, "ftp://example.test/x.zip"},
		{EnvGTFSImportTime, "25:00"},
		{EnvGTFSImportTime, "3:30pm"},
		{EnvScopeBBox, "13,52,14"},
		{EnvGTFSRTURL, "not a url"},
		{EnvGTFSRTPollInterval, "15"},
		{EnvGTFSRTPollInterval, "1s"},
		{EnvGTFSRTPollInterval, "9s"}, // 7 requests/min upstream limit → floor 10 s
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			m := map[string]string{EnvDatabaseURL: testDBURL, tt.key: tt.value}
			_, err := Load(env(m))
			if err == nil {
				t.Fatalf("Load with %s=%q: want error, got nil", tt.key, tt.value)
			}
			if !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("error should name %s, got: %v", tt.key, err)
			}
		})
	}
}

func TestLoadPollIntervalAtFloor(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvDatabaseURL: testDBURL, EnvGTFSRTPollInterval: "10s"}))
	if err != nil || cfg.GTFSRTPollInterval != MinGTFSRTPollInterval {
		t.Fatalf("10s: cfg=%v err=%v; want accepted", cfg.GTFSRTPollInterval, err)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	_, err := Load(env(nil))
	if err == nil || !strings.Contains(err.Error(), EnvDatabaseURL) {
		t.Fatalf("want error naming %s, got %v", EnvDatabaseURL, err)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(env(map[string]string{
		EnvLogLevel:           "loud",
		EnvGTFSRTPollInterval: "1s",
		EnvScopeBBox:          "x",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, key := range []string{EnvLogLevel, EnvGTFSRTPollInterval, EnvScopeBBox, EnvDatabaseURL} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error should mention %s, got:\n%v", key, err)
		}
	}
}

func TestLoadSameAddrForHTTPAndMetrics(t *testing.T) {
	_, err := Load(env(map[string]string{EnvDatabaseURL: testDBURL, EnvHTTPAddr: ":8080", EnvMetricsAddr: ":8080"}))
	if err == nil || !strings.Contains(err.Error(), EnvMetricsAddr) {
		t.Fatalf("want error naming %s, got %v", EnvMetricsAddr, err)
	}
}

func TestSecretsAreNeverPrinted(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvDatabaseURL: testDBURL}))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("starting", "config", cfg)
	logged := buf.String()
	if strings.Contains(logged, "s3cret-pw") {
		t.Fatalf("password leaked into log: %s", logged)
	}
	if !strings.Contains(logged, "db:5432") {
		t.Fatalf("summary should still show the DB host: %s", logged)
	}

	for _, s := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprint(cfg.DatabaseURL)} {
		if strings.Contains(s, "s3cret-pw") {
			t.Fatalf("password leaked via fmt: %s", s)
		}
	}
	if cfg.DatabaseURL.Value() != testDBURL {
		t.Fatal("Value() must return the real secret")
	}
}

func TestInvalidDatabaseURLErrorHidesValue(t *testing.T) {
	_, err := Load(env(map[string]string{EnvDatabaseURL: "mysql://u:s3cret-pw@h/db"}))
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "s3cret-pw") {
		t.Fatalf("error leaks the password: %v", err)
	}
}
