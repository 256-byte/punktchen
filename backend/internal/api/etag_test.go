package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestETag(t *testing.T) {
	a, b := etagOf("u33dbjh|500|transit=1"), etagOf("u33dbjh|500|transit=2")
	if !strings.HasPrefix(a, `W/"`) || len(a) != 36 || a == b || a != etagOf("u33dbjh|500|transit=1") {
		t.Errorf("etags %s %s", a, b)
	}
	strong := strings.TrimPrefix(a, "W/")
	for _, c := range []struct {
		header *string
		want   bool
	}{
		{nil, false},
		{ptr(a), true},
		{ptr(strong), true}, // weak comparison
		{ptr(`W/"x", ` + a), true},
		{ptr("*"), true},
		{ptr(b), false},
		{ptr(""), false},
	} {
		if got := matchesETag(c.header, a); got != c.want {
			t.Errorf("If-None-Match %v: %v, want %v", deref(c.header), got, c.want)
		}
	}
	if matchesETag(ptr("*"), "") {
		t.Error("an answer without ETag matched *")
	}
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                     false,
		"gzip":                 true,
		"GZIP":                 true,
		"gzip, deflate, br":    true, // iOS, Android, curl --compressed
		"deflate, br":          false,
		"gzip;q=0":             false,
		"gzip; q=0.0":          false,
		"br;q=1, gzip;q=0.5":   true,
		"*":                    true,
		"identity, *;q=0":      false,
		"gzip;q=0.001, br;q=1": true,
	} {
		if got := acceptsGzip(header); got != want {
			t.Errorf("Accept-Encoding %q: %v, want %v", header, got, want)
		}
	}
}

// raw sends GET without Go's transparent gzip handling.
func raw(t *testing.T, url string, header http.Header) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header = header
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGzip(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	gz := http.Header{"Accept-Encoding": {"gzip"}}
	nearby := srv.URL + "/v1/nearby?lat=52.5427&lng=13.366"

	plainResp, plain := raw(t, nearby, nil)
	resp, body := raw(t, nearby, gz)
	if resp.Header.Get("Content-Encoding") != "gzip" || !strings.Contains(resp.Header.Get("Vary"), "Accept-Encoding") {
		t.Fatalf("headers %v", resp.Header)
	}
	if got := gunzip(t, body); !bytes.Equal(got, plain) {
		t.Errorf("decompressed body differs:\n%s\n%s", got, plain)
	}
	if plainResp.Header.Get("Content-Encoding") != "" || !strings.Contains(plainResp.Header.Get("Vary"), "Accept-Encoding") {
		t.Errorf("without Accept-Encoding: %v", plainResp.Header)
	}

	// Errors and 304s stay as they are.
	resp, body = raw(t, srv.URL+"/v1/nearby?lat=999&lng=13.4", gz)
	if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Content-Encoding") != "" || !bytes.HasPrefix(body, []byte("{")) {
		t.Errorf("400: %v %s", resp.Header, body)
	}
	etag := plainResp.Header.Get("ETag")
	resp, body = raw(t, nearby, http.Header{"Accept-Encoding": {"gzip"}, "If-None-Match": {etag}})
	if resp.StatusCode != http.StatusNotModified || resp.Header.Get("Content-Encoding") != "" || len(body) != 0 {
		t.Errorf("304: %d %v %q", resp.StatusCode, resp.Header, body)
	}
}

// TestNearbyNotModified: a matching If-None-Match answers 304 without
// building; a new layer version or a failing layer does not.
func TestNearbyNotModified(t *testing.T) {
	fl := &fakeLayer{}
	srv := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, fl) })
	url := srv.URL + "/v1/nearby?lat=52.5427&lng=13.366"

	resp, _ := raw(t, url, nil)
	etag := resp.Header.Get("ETag")
	if resp.StatusCode != http.StatusOK || etag == "" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("first: %d %v", resp.StatusCode, resp.Header)
	}
	// Another user in the same cell revalidates the same answer.
	resp, body := raw(t, srv.URL+"/v1/nearby?lat=52.54275&lng=13.36605", http.Header{"If-None-Match": {etag}})
	if resp.StatusCode != http.StatusNotModified || resp.Header.Get("ETag") != etag || len(body) != 0 {
		t.Errorf("revalidation: %d %v %q", resp.StatusCode, resp.Header, body)
	}

	fl.mu.Lock()
	fl.queries = nil
	fl.mu.Unlock()
	fl.setVersion(1794220476761)
	resp, _ = raw(t, url, http.Header{"If-None-Match": {etag}})
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") == etag || fl.calls() != 1 {
		t.Errorf("new version: %d, ETag %q, %d builds", resp.StatusCode, resp.Header.Get("ETag"), fl.calls())
	}

	down := &fakeLayer{err: errors.New("no snapshot")}
	srv2 := newTestServer(t, fakePinger{}, func(c *Config) { c.Layers = mustRegistry(t, down) })
	resp, _ = raw(t, srv2.URL+"/v1/nearby?lat=52.5427&lng=13.366", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("ETag") != "" {
		t.Errorf("failing layer: %d, ETag %q; want no ETag", resp.StatusCode, resp.Header.Get("ETag"))
	}
}

func TestStationNotModified(t *testing.T) {
	srv := newTestServer(t, fakePinger{})
	url := srv.URL + "/v1/stations/de:11000:900009104"
	resp, _ := raw(t, url, nil)
	etag := resp.Header.Get("ETag")
	if resp.StatusCode != http.StatusOK || etag != etagOf("station|de:11000:900009104|42") {
		t.Fatalf("first: %d, ETag %q", resp.StatusCode, etag)
	}
	if resp, body := raw(t, url, http.Header{"If-None-Match": {etag}}); resp.StatusCode != http.StatusNotModified || len(body) != 0 {
		t.Errorf("revalidation: %d %q", resp.StatusCode, body)
	}
}

// TestGzipHandler covers responses the API does not produce yet: no
// Content-Type, a handler that never writes.
func TestGzipHandler(t *testing.T) {
	h := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/text" {
			_, _ = w.Write([]byte("plain"))
		}
	}))
	for path, want := range map[string]string{"/text": "plain", "/empty": ""} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != want {
			t.Errorf("%s: %v %q", path, rec.Header(), rec.Body.String())
		}
	}
}

func ptr(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
