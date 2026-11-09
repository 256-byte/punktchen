package api

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// gzipWriters reuses compressors: a new gzip.Writer allocates ~800 KB.
var gzipWriters = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression)
	return w
}}

// withGzip compresses successful JSON responses for clients that accept
// gzip (task 5.3). Errors and 304s are sent as they are.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.finish()
		next.ServeHTTP(gw, r)
	})
}

// acceptsGzip reads an Accept-Encoding header: the first gzip (or *) entry
// decides; q=0 refuses it.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(part, ";")
		coding = strings.ToLower(strings.TrimSpace(coding))
		if coding != "gzip" && coding != "*" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(p, "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		return q > 0
	}
	return false
}

// gzipWriter decides at WriteHeader: 2xx with a JSON body → compressed.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.decided {
		g.decided = true
		h := g.Header()
		if code >= 200 && code < 300 && code != http.StatusNoContent &&
			h.Get("Content-Encoding") == "" && strings.HasPrefix(h.Get("Content-Type"), "application/json") {
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
			g.gz = gzipWriters.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// finish flushes the compressed stream and returns the compressor.
func (g *gzipWriter) finish() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipWriters.Put(g.gz)
		g.gz = nil
	}
}
