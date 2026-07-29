package blob

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Usage struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Usage measures the artifacts. Directories the server keeps for itself are
// named with a leading dot, which no repository key can start with, and they are
// left out so what is left over reads as artifact bytes nothing points at.
func (s *Store) Usage() (Usage, error) {
	var usage Usage

	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if filepath.Dir(path) == s.root && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		usage.Files++
		usage.Bytes += info.Size()
		return nil
	})

	return usage, err
}

func (s *Store) Root() string { return s.root }
