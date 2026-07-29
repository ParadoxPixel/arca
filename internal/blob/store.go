package blob

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidKey = errors.New("blob: key escapes the storage root")
	ErrTooLarge   = errors.New("blob: payload exceeds the configured limit")
)

const temporaryDirectory = ".uploads"

type Digests struct {
	MD5    string
	SHA1   string
	SHA256 string
	SHA512 string
}

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, temporaryDirectory), 0o755); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	return &Store{root: absolute}, nil
}

func (s *Store) resolve(key string) (string, error) {
	if key == "" || strings.Contains(key, "\x00") {
		return "", ErrInvalidKey
	}

	target := filepath.Clean(filepath.Join(s.root, filepath.FromSlash(key)))
	if target != s.root && !strings.HasPrefix(target, s.root+string(os.PathSeparator)) {
		return "", ErrInvalidKey
	}
	return target, nil
}

func (s *Store) Put(key string, reader io.Reader, limit int64) (int64, Digests, error) {
	target, err := s.resolve(key)
	if err != nil {
		return 0, Digests{}, err
	}

	temporary, err := os.CreateTemp(filepath.Join(s.root, temporaryDirectory), "upload-*")
	if err != nil {
		return 0, Digests{}, fmt.Errorf("create temporary file: %w", err)
	}
	defer func() {
		temporary.Close()
		os.Remove(temporary.Name())
	}()

	digesters := map[string]hash.Hash{
		"md5":    md5.New(),
		"sha1":   sha1.New(),
		"sha256": sha256.New(),
		"sha512": sha512.New(),
	}

	writers := []io.Writer{temporary}
	for _, digester := range digesters {
		writers = append(writers, digester)
	}

	source := reader
	if limit > 0 {
		source = io.LimitReader(reader, limit+1)
	}

	size, err := io.Copy(io.MultiWriter(writers...), source)
	if err != nil {
		return 0, Digests{}, fmt.Errorf("buffer upload: %w", err)
	}
	if limit > 0 && size > limit {
		return 0, Digests{}, ErrTooLarge
	}
	if err := temporary.Close(); err != nil {
		return 0, Digests{}, fmt.Errorf("flush upload: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, Digests{}, fmt.Errorf("create storage directory: %w", err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return 0, Digests{}, fmt.Errorf("store upload: %w", err)
	}

	digests := Digests{
		MD5:    hex.EncodeToString(digesters["md5"].Sum(nil)),
		SHA1:   hex.EncodeToString(digesters["sha1"].Sum(nil)),
		SHA256: hex.EncodeToString(digesters["sha256"].Sum(nil)),
		SHA512: hex.EncodeToString(digesters["sha512"].Sum(nil)),
	}
	return size, digests, nil
}

func (s *Store) Open(key string) (*os.File, os.FileInfo, error) {
	target, err := s.resolve(key)
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(target)
	if err != nil {
		return nil, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func (s *Store) Delete(keys ...string) error {
	for _, key := range keys {
		target, err := s.resolve(key)
		if err != nil {
			return err
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		s.pruneEmptyParents(filepath.Dir(target))
	}
	return nil
}

func (s *Store) pruneEmptyParents(directory string) {
	for directory != s.root && strings.HasPrefix(directory, s.root+string(os.PathSeparator)) {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}
