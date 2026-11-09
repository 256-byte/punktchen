package static

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/256-byte/punktchen/backend/gen/queries"
)

// SourceGTFS is the meta.feed_imports source name of the static VBB feed.
const SourceGTFS = "vbb-gtfs"

const (
	// DefaultMaxBytes caps a download; the VBB zip is ~85 MB (DATA_MODEL §7 #1).
	DefaultMaxBytes = 1 << 30
	// DefaultTimeout bounds one whole download.
	DefaultTimeout = 10 * time.Minute
)

// DownloaderConfig configures a Downloader. Zero values get defaults.
type DownloaderConfig struct {
	Source    string       // default SourceGTFS
	URL       string       // required
	UserAgent string       // required by VBB etiquette: name the project
	Dir       string       // where the zip is written; default os.TempDir()
	MaxBytes  int64        // default DefaultMaxBytes
	Client    *http.Client // default: timeout DefaultTimeout
}

// Downloader fetches the static GTFS zip only when it changed upstream.
//
// Change detection (ARCHITECTURE §3.1): a conditional GET with the ETag /
// Last-Modified of the last successful import, if the server sent them;
// otherwise, and also when the server ignores them, the SHA-256 of the
// downloaded file is compared with the last successful import.
type Downloader struct {
	cfg  DownloaderConfig
	log  *ImportLog
	slog *slog.Logger
}

// NewDownloader returns a Downloader that records attempts in il.
func NewDownloader(cfg DownloaderConfig, il *ImportLog, log *slog.Logger) (*Downloader, error) {
	if cfg.URL == "" {
		return nil, errors.New("downloader: URL is required")
	}
	if cfg.Source == "" {
		cfg.Source = SourceGTFS
	}
	if cfg.Dir == "" {
		cfg.Dir = os.TempDir()
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Downloader{cfg: cfg, log: il, slog: log}, nil
}

// Download is the result of one Fetch.
type Download struct {
	ImportID int64
	// Changed is false when upstream did not change; the attempt is then
	// already closed as "skipped" and there is no file.
	Changed bool
	// Path of the downloaded zip when Changed. The caller imports it, closes
	// the attempt with ImportLog.Succeed or Fail, and calls Remove.
	Path         string
	SHA256       string
	Size         int64
	ETag         string
	LastModified string
}

// Remove deletes the downloaded file, if any.
func (d *Download) Remove() error {
	if d == nil || d.Path == "" {
		return nil
	}
	err := os.Remove(d.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Fetch records an attempt and downloads the feed if it changed. On error the
// attempt is closed as "failed".
func (d *Downloader) Fetch(ctx context.Context) (*Download, error) {
	last, err := d.log.lastSuccessful(ctx, d.cfg.Source)
	if err != nil {
		return nil, err
	}
	id, err := d.log.start(ctx, d.cfg.Source, d.cfg.URL)
	if err != nil {
		return nil, err
	}
	dl, err := d.fetch(ctx, id, last)
	if err != nil {
		return nil, errors.Join(err, d.log.Fail(ctx, id, err))
	}
	return dl, nil
}

func (d *Downloader) fetch(ctx context.Context, id int64, last *queries.MetaFeedImport) (*Download, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.cfg.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", d.cfg.UserAgent)
	if last != nil {
		if last.Etag != nil {
			req.Header.Set("If-None-Match", *last.Etag)
		}
		if last.LastModified != nil {
			req.Header.Set("If-Modified-Since", *last.LastModified)
		}
	}

	resp, err := d.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", d.cfg.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	etag, lastModified := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	switch {
	case resp.StatusCode == http.StatusNotModified && last != nil:
		// A 304 may omit the validators; keep the ones we sent.
		etag, lastModified = orDeref(etag, last.Etag), orDeref(lastModified, last.LastModified)
		if err := d.log.recordDownload(ctx, id, etag, lastModified, ""); err != nil {
			return nil, err
		}
		d.slog.Info("gtfs unchanged", "reason", "not modified", "import_id", id)
		return d.unchanged(ctx, id, etag, lastModified, "")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GET %s: unexpected status %s", d.cfg.URL, resp.Status)
	}

	path, size, sum, err := d.save(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := d.log.recordDownload(ctx, id, etag, lastModified, sum); err != nil {
		return nil, errors.Join(err, os.Remove(path))
	}
	if last != nil && last.SHA256 != nil && *last.SHA256 == sum {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		d.slog.Info("gtfs unchanged", "reason", "same sha256", "import_id", id, "bytes", size)
		return d.unchanged(ctx, id, etag, lastModified, sum)
	}
	d.slog.Info("gtfs downloaded", "import_id", id, "bytes", size, "sha256", sum,
		"duration", time.Since(start).Round(time.Millisecond))
	return &Download{ImportID: id, Changed: true, Path: path, SHA256: sum, Size: size, ETag: etag, LastModified: lastModified}, nil
}

func (d *Downloader) unchanged(ctx context.Context, id int64, etag, lastModified, sum string) (*Download, error) {
	if err := d.log.skip(ctx, id); err != nil {
		return nil, err
	}
	return &Download{ImportID: id, SHA256: sum, ETag: etag, LastModified: lastModified}, nil
}

// save streams body to a new file in cfg.Dir and hashes it on the way.
func (d *Downloader) save(body io.Reader) (path string, size int64, sum string, err error) {
	f, err := os.CreateTemp(d.cfg.Dir, "gtfs-*.zip")
	if err != nil {
		return "", 0, "", err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(body, d.cfg.MaxBytes+1))
	if err != nil {
		return "", 0, "", fmt.Errorf("download %s: %w", d.cfg.URL, err)
	}
	if size > d.cfg.MaxBytes {
		return "", 0, "", fmt.Errorf("download %s: larger than %d bytes", d.cfg.URL, d.cfg.MaxBytes)
	}
	if err = f.Close(); err != nil {
		return "", 0, "", err
	}
	return f.Name(), size, hex.EncodeToString(h.Sum(nil)), nil
}

func orDeref(s string, fallback *string) string {
	if s == "" && fallback != nil {
		return *fallback
	}
	return s
}
