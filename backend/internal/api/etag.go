package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// etagOf returns a weak ETag for an answer key (task 5.3, ADR-0006). Weak:
// answers with one key carry the same data, but not the same bytes
// (generatedAt is the time of each response, gzip changes the encoding).
func etagOf(key string) string {
	sum := sha256.Sum256([]byte(key))
	return `W/"` + hex.EncodeToString(sum[:16]) + `"`
}

// matchesETag reports whether an If-None-Match header names etag, by weak
// comparison (RFC 9110 §13.1.2): a list of tags, W/ ignored, "*" matches.
func matchesETag(ifNoneMatch *string, etag string) bool {
	if ifNoneMatch == nil || etag == "" {
		return false
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, tag := range strings.Split(*ifNoneMatch, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" || strings.TrimPrefix(tag, "W/") == want {
			return true
		}
	}
	return false
}
