package blob

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newUploadStore(t *testing.T) *Store {
	t.Helper()

	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store
}

func sha256Of(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func TestUploadSessionAcrossChunks(t *testing.T) {
	store := newUploadStore(t)
	payload := []byte("the whole blob, delivered in pieces")

	if err := store.BeginUpload("session-one"); err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}

	size, err := store.UploadSize("session-one")
	if err != nil || size != 0 {
		t.Fatalf("a fresh session reported size %d, %v, want 0", size, err)
	}

	chunks := [][]byte{payload[:10], payload[10:20], payload[20:]}
	expected := 0
	for _, chunk := range chunks {
		expected += len(chunk)

		size, err := store.AppendUpload("session-one", bytes.NewReader(chunk), 0)
		if err != nil {
			t.Fatalf("AppendUpload: %v", err)
		}
		if size != int64(expected) {
			t.Fatalf("after appending %d bytes the session is %d, want %d", len(chunk), size, expected)
		}
	}

	size, digests, err := store.CompleteUpload("session-one", "repo/blob", sha256Of(payload))
	if err != nil {
		t.Fatalf("CompleteUpload: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}
	if digests.SHA256 != sha256Of(payload) {
		t.Fatalf("SHA256 = %q, want %q", digests.SHA256, sha256Of(payload))
	}

	file, _, err := store.Open("repo/blob")
	if err != nil {
		t.Fatalf("the committed blob is not readable: %v", err)
	}
	file.Close()
}

// A mismatch must leave the session in place, so the caller can decide whether the
// client retries or the session is abandoned.
func TestCompleteUploadRejectsAMismatch(t *testing.T) {
	store := newUploadStore(t)

	if err := store.BeginUpload("mismatch"); err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if _, err := store.AppendUpload("mismatch", strings.NewReader("actual content"), 0); err != nil {
		t.Fatalf("AppendUpload: %v", err)
	}

	_, digests, err := store.CompleteUpload("mismatch", "repo/blob", sha256Of([]byte("different content")))
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("CompleteUpload returned %v, want ErrDigestMismatch", err)
	}
	if digests.SHA256 != sha256Of([]byte("actual content")) {
		t.Fatalf("the reported digest is not the one on disk: %q", digests.SHA256)
	}

	if _, err := store.UploadSize("mismatch"); err != nil {
		t.Fatalf("the session was discarded on mismatch: %v", err)
	}
	if _, _, err := store.Open("repo/blob"); err == nil {
		t.Fatal("a mismatched upload was moved into place")
	}
}

func TestAppendUploadHonoursTheLimit(t *testing.T) {
	store := newUploadStore(t)

	if err := store.BeginUpload("bounded"); err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if _, err := store.AppendUpload("bounded", strings.NewReader("12345"), 10); err != nil {
		t.Fatalf("the first chunk was refused: %v", err)
	}

	size, err := store.AppendUpload("bounded", strings.NewReader("678901234"), 10)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("AppendUpload returned %v, want ErrTooLarge", err)
	}
	// The overshoot is truncated, so the session stays consistent with the size
	// the caller was told about.
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}

	stored, err := store.UploadSize("bounded")
	if err != nil {
		t.Fatalf("UploadSize: %v", err)
	}
	if stored != 5 {
		t.Fatalf("the session is %d bytes on disk, want 5", stored)
	}
}

func TestUploadSessionErrors(t *testing.T) {
	store := newUploadStore(t)

	t.Run("appending to an unknown session", func(t *testing.T) {
		if _, err := store.AppendUpload("absent", strings.NewReader("x"), 0); !errors.Is(err, ErrNoSuchUpload) {
			t.Fatalf("err = %v, want ErrNoSuchUpload", err)
		}
	})

	t.Run("measuring an unknown session", func(t *testing.T) {
		if _, err := store.UploadSize("absent"); !errors.Is(err, ErrNoSuchUpload) {
			t.Fatalf("err = %v, want ErrNoSuchUpload", err)
		}
	})

	t.Run("completing an unknown session", func(t *testing.T) {
		if _, _, err := store.CompleteUpload("absent", "repo/blob", sha256Of(nil)); !errors.Is(err, ErrNoSuchUpload) {
			t.Fatalf("err = %v, want ErrNoSuchUpload", err)
		}
	})

	t.Run("reopening a live session", func(t *testing.T) {
		if err := store.BeginUpload("taken"); err != nil {
			t.Fatalf("BeginUpload: %v", err)
		}
		if err := store.BeginUpload("taken"); err == nil {
			t.Fatal("BeginUpload truncated a session that already existed")
		}
	})

	t.Run("aborting an unknown session is not an error", func(t *testing.T) {
		if err := store.AbortUpload("absent"); err != nil {
			t.Fatalf("AbortUpload: %v", err)
		}
	})
}

// An upload id becomes a filename, so one carrying separators would let a caller
// write outside the temporary directory.
func TestUploadIDsCannotEscape(t *testing.T) {
	store := newUploadStore(t)

	cases := []string{
		"",
		"../escape",
		"a/b",
		"a\\b",
		"with space",
		"with.dot",
		strings.Repeat("a", 129),
	}

	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			if err := store.BeginUpload(id); !errors.Is(err, ErrInvalidUploadID) {
				t.Fatalf("BeginUpload(%q) = %v, want ErrInvalidUploadID", id, err)
			}
			if _, err := store.UploadSize(id); !errors.Is(err, ErrInvalidUploadID) {
				t.Fatalf("UploadSize(%q) = %v, want ErrInvalidUploadID", id, err)
			}
			if err := store.AbortUpload(id); !errors.Is(err, ErrInvalidUploadID) {
				t.Fatalf("AbortUpload(%q) = %v, want ErrInvalidUploadID", id, err)
			}
		})
	}
}

func TestAbortUploadRemovesTheFile(t *testing.T) {
	store := newUploadStore(t)

	if err := store.BeginUpload("doomed"); err != nil {
		t.Fatalf("BeginUpload: %v", err)
	}
	if _, err := store.AppendUpload("doomed", strings.NewReader("content"), 0); err != nil {
		t.Fatalf("AppendUpload: %v", err)
	}
	if err := store.AbortUpload("doomed"); err != nil {
		t.Fatalf("AbortUpload: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(store.Root(), temporaryDirectory, uploadPrefix+"*"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("the aborted session is still on disk: %v", matches)
	}
}

// Concurrent sessions are the normal case: buildx pushes several layers at once,
// and nothing may serialise them.
func TestConcurrentUploadSessions(t *testing.T) {
	store := newUploadStore(t)

	payloads := map[string][]byte{
		"first":  []byte("the first layer"),
		"second": []byte("the second layer, which is longer"),
		"third":  []byte("third"),
	}

	for id := range payloads {
		if err := store.BeginUpload(id); err != nil {
			t.Fatalf("BeginUpload(%q): %v", id, err)
		}
	}
	// Interleaved deliberately, so a shared handle or a shared offset would show
	// up as cross-contamination.
	for id, payload := range payloads {
		if _, err := store.AppendUpload(id, bytes.NewReader(payload[:3]), 0); err != nil {
			t.Fatalf("AppendUpload(%q): %v", id, err)
		}
	}
	for id, payload := range payloads {
		if _, err := store.AppendUpload(id, bytes.NewReader(payload[3:]), 0); err != nil {
			t.Fatalf("AppendUpload(%q): %v", id, err)
		}
	}

	for id, payload := range payloads {
		t.Run(id, func(t *testing.T) {
			_, digests, err := store.CompleteUpload(id, "repo/"+id, sha256Of(payload))
			if err != nil {
				t.Fatalf("CompleteUpload: %v", err)
			}
			if digests.SHA256 != sha256Of(payload) {
				t.Fatalf("session %q committed the wrong bytes", id)
			}

			content, err := os.ReadFile(filepath.Join(store.Root(), "repo", id))
			if err != nil {
				t.Fatalf("reading the committed blob: %v", err)
			}
			if !bytes.Equal(content, payload) {
				t.Fatalf("session %q holds %q, want %q", id, content, payload)
			}
		})
	}
}
