package blob

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutComputesDigests(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	size, digests, err := store.Put("repo/com/example/app.jar", strings.NewReader("hello"), 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}

	cases := []struct {
		algorithm string
		got       string
		want      string
	}{
		{"md5", digests.MD5, "5d41402abc4b2a76b9719d911017c592"},
		{"sha1", digests.SHA1, "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"},
		{"sha256", digests.SHA256, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}

	for _, testCase := range cases {
		t.Run(testCase.algorithm, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("%s = %q, want %q", testCase.algorithm, testCase.got, testCase.want)
			}
		})
	}
}

func TestPutEnforcesLimit(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, _, err := store.Put("repo/big.jar", strings.NewReader("0123456789"), 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestPutRejectsEscapingKeys(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []string{"../escape.jar", "repo/../../escape.jar", ""}

	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			if _, _, err := store.Put(key, strings.NewReader("x"), 0); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("err = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestDeletePrunesEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	key := "repo/com/example/app.jar"
	if _, _, err := store.Put(key, strings.NewReader("hello"), 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, _, err := store.Open(key); err == nil {
		t.Fatal("expected the object to be gone")
	}
	if entries, err := filepath.Glob(filepath.Join(root, "repo")); err == nil && len(entries) > 0 {
		t.Fatalf("expected empty parents to be pruned, found %v", entries)
	}
}

func TestOpenReturnsContent(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, _, err := store.Put("repo/app.jar", strings.NewReader("payload"), 0); err != nil {
		t.Fatalf("Put: %v", err)
	}

	file, info, err := store.Open("repo/app.jar")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer file.Close()

	if info.Size() != 7 {
		t.Fatalf("size = %d, want 7", info.Size())
	}

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(content) != "payload" {
		t.Fatalf("content = %q, want %q", content, "payload")
	}
}
