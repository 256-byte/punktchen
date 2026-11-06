// Package config loads the backend configuration from LIVESTOP_* environment
// variables. Load validates everything up front and reports all problems at
// once, so a misconfigured server fails at startup instead of at first use.
//
// The variables are documented in .env.example at the repository root.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/256-byte/punktchen/backend/internal/geo"
)

// Environment variable names.
const (
	EnvDev                = "LIVESTOP_DEV"
	EnvLogLevel           = "LIVESTOP_LOG_LEVEL"
	EnvHTTPAddr           = "LIVESTOP_HTTP_ADDR"
	EnvMetricsAddr        = "LIVESTOP_METRICS_ADDR"
	EnvDatabaseURL        = "LIVESTOP_DATABASE_URL"
	EnvGTFSURL            = "LIVESTOP_GTFS_URL"
	EnvGTFSImportTime     = "LIVESTOP_GTFS_IMPORT_TIME"
	EnvScopeBBox          = "LIVESTOP_SCOPE_BBOX"
	EnvGTFSRTURL          = "LIVESTOP_GTFSRT_URL"
	EnvGTFSRTPollInterval = "LIVESTOP_GTFSRT_POLL_INTERVAL"
)

// Defaults. They match .env.example.
const (
	DefaultHTTPAddr           = ":8080"
	DefaultMetricsAddr        = ":9090"
	DefaultGTFSURL            = "https://unternehmen.vbb.de/fileadmin/user_upload/VBB/Dokumente/API-Datensaetze/gtfs-mastscharf/GTFS.zip"
	DefaultGTFSImportTime     = "03:30"
	DefaultScopeBBox          = "13.088,52.338,13.761,52.675"
	DefaultGTFSRTURL          = "https://production.gtfsrt.vbb.de/data"
	DefaultGTFSRTPollInterval = 15 * time.Second
)

// MinGTFSRTPollInterval is the lowest accepted poll interval.
// VBB allows at most 60 requests/minute (one per second); 5 s keeps a wide
// safety margin so a config typo can never get us close to the limit.
const MinGTFSRTPollInterval = 5 * time.Second

// Config is the validated backend configuration.
type Config struct {
	Dev         bool
	LogLevel    slog.Level
	HTTPAddr    string
	MetricsAddr string
	DatabaseURL Secret

	GTFSURL        string
	GTFSImportTime ClockTime // daily, Europe/Berlin
	ScopeBBox      geo.BBox

	GTFSRTURL          string
	GTFSRTPollInterval time.Duration
}

// LookupFunc returns the value of an environment variable. os.LookupEnv fits.
type LookupFunc func(key string) (string, bool)

// Load reads and validates the configuration. The returned error lists every
// invalid or missing variable.
func Load(lookup LookupFunc) (Config, error) {
	p := &parser{lookup: lookup}
	cfg := Config{
		Dev:         p.bool(EnvDev, false),
		LogLevel:    p.logLevel(EnvLogLevel, slog.LevelInfo),
		HTTPAddr:    p.addr(EnvHTTPAddr, DefaultHTTPAddr),
		MetricsAddr: p.addr(EnvMetricsAddr, DefaultMetricsAddr),
		DatabaseURL: Secret(p.postgresURL(EnvDatabaseURL)),

		GTFSURL:        p.httpURL(EnvGTFSURL, DefaultGTFSURL),
		GTFSImportTime: p.clockTime(EnvGTFSImportTime, DefaultGTFSImportTime),
		ScopeBBox:      p.bbox(EnvScopeBBox, DefaultScopeBBox),

		GTFSRTURL:          p.httpURL(EnvGTFSRTURL, DefaultGTFSRTURL),
		GTFSRTPollInterval: p.duration(EnvGTFSRTPollInterval, DefaultGTFSRTPollInterval, MinGTFSRTPollInterval),
	}
	if cfg.HTTPAddr != "" && cfg.HTTPAddr == cfg.MetricsAddr {
		p.fail(EnvMetricsAddr, "must differ from %s", EnvHTTPAddr)
	}
	if err := errors.Join(p.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

// LogValue is the config summary logged at startup. Secrets are redacted.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Bool("dev", c.Dev),
		slog.String("log_level", c.LogLevel.String()),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("metrics_addr", c.MetricsAddr),
		slog.String("database_url", redactURL(string(c.DatabaseURL))),
		slog.String("gtfs_url", c.GTFSURL),
		slog.String("gtfs_import_time", c.GTFSImportTime.String()),
		slog.String("scope_bbox", c.ScopeBBox.String()),
		slog.String("gtfsrt_url", c.GTFSRTURL),
		slog.String("gtfsrt_poll_interval", c.GTFSRTPollInterval.String()),
	)
}

// Secret is a string that must not end up in logs or error messages.
// Printing it with fmt or slog shows a placeholder; use Value to read it.
type Secret string

// Value returns the secret value.
func (s Secret) Value() string { return string(s) }

// String implements fmt.Stringer.
func (s Secret) String() string { return "[redacted]" }

// LogValue implements slog.LogValuer.
func (s Secret) LogValue() slog.Value { return slog.StringValue(s.String()) }

// ClockTime is a wall-clock time of day (HH:MM).
type ClockTime struct {
	Hour, Minute int
}

func (t ClockTime) String() string { return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute) }

// redactURL keeps scheme, host and path of a URL but hides the password.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable]"
	}
	return u.Redacted()
}

// parser reads variables and collects errors instead of stopping at the first.
type parser struct {
	lookup LookupFunc
	errs   []error
}

func (p *parser) fail(key, format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...)))
}

// get returns the trimmed value; empty values count as unset.
func (p *parser) get(key string) (string, bool) {
	v, ok := p.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (p *parser) bool(key string, def bool) bool {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail(key, "%q is not a boolean (use true or false)", v)
		return def
	}
	return b
}

func (p *parser) logLevel(key string, def slog.Level) slog.Level {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(v)); err != nil {
		p.fail(key, "%q is not a log level (debug, info, warn, error)", v)
		return def
	}
	return l
}

func (p *parser) addr(key, def string) string {
	v, ok := p.get(key)
	if !ok {
		v = def
	}
	_, port, err := net.SplitHostPort(v)
	if err != nil {
		p.fail(key, "%q is not host:port (e.g. :8080)", v)
		return ""
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		p.fail(key, "port %q must be a number between 1 and 65535", port)
		return ""
	}
	return v
}

func (p *parser) postgresURL(key string) string {
	v, ok := p.get(key)
	if !ok {
		p.fail(key, "is required (e.g. postgres://user:pass@localhost:5432/livestop?sslmode=disable)")
		return ""
	}
	u, err := url.Parse(v)
	// Never echo the value: it contains the password.
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		p.fail(key, "must be a URL like postgres://user:pass@host:5432/dbname")
		return ""
	}
	return v
}

func (p *parser) httpURL(key, def string) string {
	v, ok := p.get(key)
	if !ok {
		v = def
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		p.fail(key, "%q is not an http(s) URL", v)
		return ""
	}
	return v
}

func (p *parser) clockTime(key, def string) ClockTime {
	v, ok := p.get(key)
	if !ok {
		v = def
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		p.fail(key, "%q is not a time of day in HH:MM format", v)
		return ClockTime{}
	}
	return ClockTime{Hour: t.Hour(), Minute: t.Minute()}
}

func (p *parser) bbox(key, def string) geo.BBox {
	v, ok := p.get(key)
	if !ok {
		v = def
	}
	b, err := geo.ParseBBox(v)
	if err != nil {
		p.fail(key, "%v", strings.ReplaceAll(err.Error(), "\n", "; "))
		return geo.BBox{}
	}
	return b
}

func (p *parser) duration(key string, def, minimum time.Duration) time.Duration {
	v, ok := p.get(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		p.fail(key, "%q is not a duration (e.g. 15s)", v)
		return def
	}
	if d < minimum {
		p.fail(key, "%s is below the minimum of %s", d, minimum)
		return def
	}
	return d
}
