package store

import (
	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

func (s *Store) FindRemoteMiss(repositoryID, path string) (*models.RemoteMiss, error) {
	var miss models.RemoteMiss
	err := s.db.First(&miss, "repository_id = ? AND path = ? AND expires_at > ?", repositoryID, path, NowMillis()).Error
	if err != nil {
		return nil, err
	}
	return &miss, nil
}

func (s *Store) RecordRemoteMiss(repositoryID, path string, status int, expiresAt int64) error {
	miss := &models.RemoteMiss{
		ID:           NewID(),
		RepositoryID: repositoryID,
		Path:         path,
		Status:       status,
		ExpiresAt:    expiresAt,
		CreatedAt:    NowMillis(),
	}

	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository_id"}, {Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "expires_at"}),
	}).Create(miss).Error
}

func (s *Store) ClearRemoteMiss(repositoryID, path string) error {
	return s.db.Where("repository_id = ? AND path = ?", repositoryID, path).Delete(&models.RemoteMiss{}).Error
}

func (s *Store) ClearRemoteMisses(repositoryID string) error {
	return s.db.Where("repository_id = ?", repositoryID).Delete(&models.RemoteMiss{}).Error
}

func (s *Store) DeleteExpiredRemoteMisses() (int64, error) {
	result := s.db.Where("expires_at <= ?", NowMillis()).Delete(&models.RemoteMiss{})
	return result.RowsAffected, result.Error
}
