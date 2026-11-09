package static

import (
	"regexp"
	"testing"
)

// TestLayoutOf: any change to the SQL files or the revision gives another
// layout; the same input always the same one.
func TestLayoutOf(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(Layout) {
		t.Fatalf("Layout %q, want 16 hex digits (inlined into SQL)", Layout)
	}
	base := layoutOf(1, "CREATE TABLE a;", "CREATE INDEX b;")
	if again := layoutOf(1, "CREATE TABLE a;", "CREATE INDEX b;"); again != base {
		t.Errorf("same input: %s, then %s", base, again)
	}
	for name, other := range map[string]string{
		"revision":      layoutOf(2, "CREATE TABLE a;", "CREATE INDEX b;"),
		"file content":  layoutOf(1, "CREATE TABLE a; -- new", "CREATE INDEX b;"),
		"file boundary": layoutOf(1, "CREATE TABLE a;CREATE INDEX b;", ""),
	} {
		if other == base {
			t.Errorf("%s changed, layout stayed %s", name, base)
		}
	}
}
