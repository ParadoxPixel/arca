package blob

import (
	"io/fs"
	"os"
	"path/filepath"
)

type Usage struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

func (s *Store) Usage() (Usage, error) {
	var usage Usage

	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == filepath.Join(s.root, temporaryDirectory) {
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
