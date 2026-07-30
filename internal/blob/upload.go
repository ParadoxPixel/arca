package blob

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const uploadPrefix = "session-"

// Upload sessions back the registry API's chunked blob push, where one blob
// arrives across a POST, any number of PATCHes and a PUT. Put cannot serve that
// shape because it writes a whole reader in one call.
//
// A session is a file in the same temporary directory Put uses, so an abandoned
// one costs nothing but the space until it is swept.

func isValidUploadID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for index := 0; index < len(id); index++ {
		character := id[index]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '-', character == '_':
		default:
			return false
		}
	}
	return true
}

func (s *Store) uploadPath(id string) (string, error) {
	if !isValidUploadID(id) {
		return "", ErrInvalidUploadID
	}
	return filepath.Join(s.root, temporaryDirectory, uploadPrefix+id), nil
}

// BeginUpload opens an empty session. It fails if one already exists under the
// same id rather than truncating, because a repeated id means the caller lost
// track of a session that may still be receiving chunks.
func (s *Store) BeginUpload(id string) error {
	path, err := s.uploadPath(id)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open upload session: %w", err)
	}
	return file.Close()
}

// AppendUpload adds a chunk and reports the total size of the session. A chunk
// that would push the session past limit is rejected without being written, so
// a client cannot fill the disk one PATCH at a time.
func (s *Store) AppendUpload(id string, reader io.Reader, limit int64) (int64, error) {
	path, err := s.uploadPath(id)
	if err != nil {
		return 0, err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return 0, ErrNoSuchUpload
	}
	if err != nil {
		return 0, fmt.Errorf("open upload session: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("measure upload session: %w", err)
	}
	started := info.Size()

	source := reader
	if limit > 0 {
		remaining := limit - started
		if remaining < 0 {
			remaining = 0
		}
		source = io.LimitReader(reader, remaining+1)
	}

	written, err := io.Copy(file, source)
	if err != nil {
		return started + written, fmt.Errorf("write upload chunk: %w", err)
	}
	if limit > 0 && started+written > limit {
		// The overshoot is truncated away so the session stays consistent with
		// the size the caller is told about.
		if truncateErr := file.Truncate(started); truncateErr != nil {
			return started + written, fmt.Errorf("truncate oversized upload: %w", truncateErr)
		}
		return started, ErrTooLarge
	}

	return started + written, nil
}

func (s *Store) UploadSize(id string) (int64, error) {
	path, err := s.uploadPath(id)
	if err != nil {
		return 0, err
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, ErrNoSuchUpload
	}
	if err != nil {
		return 0, fmt.Errorf("measure upload session: %w", err)
	}
	return info.Size(), nil
}

// CompleteUpload verifies the session against the digest the client committed it
// with and only then moves it into place. Hashing here costs one extra pass over
// a file that has already crossed the network, and it is the only point at which
// the bytes on disk can be checked against what the client said they were.
//
// A mismatch leaves the session alone so the caller can decide whether to let
// the client retry or abort it.
func (s *Store) CompleteUpload(id, key, expectedSHA256 string) (int64, Digests, error) {
	source, err := s.uploadPath(id)
	if err != nil {
		return 0, Digests{}, err
	}

	target, err := s.resolve(key)
	if err != nil {
		return 0, Digests{}, err
	}

	size, digests, err := digestFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return 0, Digests{}, ErrNoSuchUpload
	}
	if err != nil {
		return 0, Digests{}, err
	}

	if !strings.EqualFold(digests.SHA256, expectedSHA256) {
		return size, digests, ErrDigestMismatch
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return size, digests, fmt.Errorf("create storage directory: %w", err)
	}
	if err := os.Rename(source, target); err != nil {
		return size, digests, fmt.Errorf("store upload: %w", err)
	}
	return size, digests, nil
}

func (s *Store) AbortUpload(id string) error {
	path, err := s.uploadPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func digestFile(path string) (int64, Digests, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, Digests{}, err
	}
	defer file.Close()

	digesters := newDigesters()

	size, err := io.Copy(digesters.writer(), file)
	if err != nil {
		return 0, Digests{}, fmt.Errorf("hash upload session: %w", err)
	}
	return size, digesters.digests(), nil
}
