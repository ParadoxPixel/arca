package blob

import (
	"strings"
	"testing"
)

func TestUsageCountsArtifactsOnly(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	keys := []string{
		"repo/com/example/app.jar",
		"repo/com/example/app.pom",
		".branding/logo",
	}
	for _, key := range keys {
		if _, _, err := store.Put(key, strings.NewReader("hello"), 0); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
	}

	usage, err := store.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Files != 2 {
		t.Fatalf("files = %d, want the two artifacts", usage.Files)
	}
	if usage.Bytes != 10 {
		t.Fatalf("bytes = %d, want 10", usage.Bytes)
	}
}
