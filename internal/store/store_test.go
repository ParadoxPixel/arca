package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRejectsAnUnwritableDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anywhere, so there is nothing to reject")
	}

	directory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(directory, 0o555); err != nil {
		t.Fatalf("creating a read-only directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })

	_, err := Open(filepath.Join(directory, "arca.db"))
	if err == nil {
		t.Fatal("opening a database in a read-only directory succeeded")
	}

	// The whole point is that the message names the directory rather than
	// blaming memory the way SQLite does.
	if !strings.Contains(err.Error(), directory) {
		t.Fatalf("error = %q, want it to name %q", err, directory)
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("error = %q, want it to say the directory is not writable", err)
	}
}

func TestOpenCreatesTheDatabaseDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "arca.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("opening a database in a missing directory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database file was not created: %v", err)
	}
}
