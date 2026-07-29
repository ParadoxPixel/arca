package store

import "arca/internal/store/models"

func (s *Store) Members(repositoryID string) ([]models.RepositoryMember, error) {
	members := []models.RepositoryMember{}
	err := s.db.
		Where("repository_id = ?", repositoryID).
		Order("position").
		Find(&members).Error
	return members, err
}

// ReplaceMembers rewrites the whole list, because the order is part of what is
// being configured and a partial update has no meaning.
func (s *Store) ReplaceMembers(repositoryID string, targets []string) error {
	return s.Transaction(func(tx *Store) error {
		err := tx.db.Where("repository_id = ?", repositoryID).Delete(&models.RepositoryMember{}).Error
		if err != nil || len(targets) == 0 {
			return err
		}

		members := make([]models.RepositoryMember, 0, len(targets))
		for index, target := range targets {
			members = append(members, models.RepositoryMember{
				RepositoryID: repositoryID,
				Position:     int64(index),
				Target:       target,
			})
		}
		return tx.db.Create(&members).Error
	})
}

// MembersUpdatedAt is the newest change to the list, which a composite document
// publishes as its timestamp so clients notice a membership change.
func (s *Store) MembersUpdatedAt(repositoryID string) (int64, error) {
	var updated *int64
	err := s.db.Model(&models.RepositoryMember{}).
		Where("repository_id = ?", repositoryID).
		Select("MAX(updated_at)").
		Scan(&updated).Error
	if err != nil || updated == nil {
		return 0, err
	}
	return *updated, nil
}
