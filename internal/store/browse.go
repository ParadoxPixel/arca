package store

import (
	"unicode/utf8"

	"arca/internal/store/models"
)

const (
	EntryDirectory = "directory"
	EntryFile      = "file"
)

type BrowseEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	Size      *int64 `json:"size,omitempty"`
	UpdatedAt *int64 `json:"updatedAt,omitempty"`
}

type FileEntry struct {
	Path      string
	Size      int64
	UpdatedAt int64
}

func offsetAfter(prefix string) int { return utf8.RuneCountInString(prefix) + 1 }

func (s *Store) scopedAssets(repositoryID, prefix string) *assetScope {
	scope := &assetScope{repositoryID: repositoryID, offset: offsetAfter(prefix)}
	if bounds, ok := RangeForPrefix(prefix); ok {
		scope.lower, scope.upper, scope.bounded = bounds.Lower, bounds.Upper, true
	}
	return scope
}

type assetScope struct {
	repositoryID string
	offset       int
	lower        string
	upper        string
	bounded      bool
}

func (a *assetScope) where() (string, []any) {
	if !a.bounded {
		return "repository_id = ?", []any{a.repositoryID}
	}
	return "repository_id = ? AND path >= ? AND path < ?", []any{a.repositoryID, a.lower, a.upper}
}

func (s *Store) DirectoryExists(repositoryID, prefix string) (bool, error) {
	bounds, bounded := RangeForPrefix(prefix)
	if !bounded {
		return true, nil
	}

	var count int64
	err := s.db.Model(&models.Asset{}).
		Where("repository_id = ? AND path >= ? AND path < ?", repositoryID, bounds.Lower, bounds.Upper).
		Limit(1).
		Count(&count).Error

	return count > 0, err
}

func (s *Store) BrowseDirectory(repositoryID, prefix string) ([]BrowseEntry, error) {
	scope := s.scopedAssets(repositoryID, prefix)
	condition, arguments := scope.where()

	var directories []string
	err := s.db.Raw(
		`SELECT DISTINCT substr(rest, 1, instr(rest, '/') - 1) AS name FROM (
		   SELECT substr(path, ?) AS rest FROM assets WHERE `+condition+`
		 ) WHERE instr(rest, '/') > 0 ORDER BY name`,
		append([]any{scope.offset}, arguments...)...,
	).Scan(&directories).Error
	if err != nil {
		return nil, err
	}

	files, err := s.FilesInDirectory(repositoryID, prefix)
	if err != nil {
		return nil, err
	}

	entries := make([]BrowseEntry, 0, len(directories)+len(files))
	for _, name := range directories {
		entries = append(entries, BrowseEntry{
			Name: name,
			Path: prefix + name + "/",
			Type: EntryDirectory,
		})
	}
	for _, file := range files {
		size, updated := file.Size, file.UpdatedAt
		entries = append(entries, BrowseEntry{
			Name:      file.Path[len(prefix):],
			Path:      file.Path,
			Type:      EntryFile,
			Size:      &size,
			UpdatedAt: &updated,
		})
	}

	return entries, nil
}

func (s *Store) FilesInDirectory(repositoryID, prefix string) ([]FileEntry, error) {
	scope := s.scopedAssets(repositoryID, prefix)
	condition, arguments := scope.where()

	files := []FileEntry{}
	err := s.db.Raw(
		`SELECT path, size, updated_at FROM assets
		 WHERE `+condition+` AND instr(substr(path, ?), '/') = 0
		 ORDER BY path`,
		append(arguments, scope.offset)...,
	).Scan(&files).Error

	return files, err
}
