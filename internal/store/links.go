package store

import (
	"regexp"

	"gorm.io/gorm"

	"arca/internal/store/models"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

func IsValidSlug(slug string) bool { return slugPattern.MatchString(slug) }

func (s *Store) CreateLink(link *models.DeploymentLink) error {
	if link.ID == "" {
		link.ID = NewID()
	}
	if link.CreatedAt == 0 {
		link.CreatedAt = NowMillis()
	}
	return s.db.Create(link).Error
}

func (s *Store) ListLinks(repositoryID string) ([]models.DeploymentLink, error) {
	links := []models.DeploymentLink{}
	err := s.db.
		Where("repository_id = ?", repositoryID).
		Order("slug").
		Find(&links).Error
	return links, err
}

func (s *Store) LinkBySlug(slug string) (*models.DeploymentLink, error) {
	var link models.DeploymentLink
	if err := s.db.First(&link, "slug = ?", slug).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *Store) LinkByID(id, repositoryID string) (*models.DeploymentLink, error) {
	var link models.DeploymentLink
	if err := s.db.First(&link, "id = ? AND repository_id = ?", id, repositoryID).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *Store) UpdateLink(id string, changes map[string]any) error {
	return s.db.Model(&models.DeploymentLink{}).Where("id = ?", id).Updates(changes).Error
}

func (s *Store) DeleteLink(id, repositoryID string) (int64, error) {
	result := s.db.Where("id = ? AND repository_id = ?", id, repositoryID).Delete(&models.DeploymentLink{})
	return result.RowsAffected, result.Error
}

// RecordLinkDownload counts a follow of the link. It runs after the file has
// been resolved and is deliberately not part of serving it: a failed count must
// not fail a download.
func (s *Store) RecordLinkDownload(id string) error {
	return s.db.Model(&models.DeploymentLink{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"downloads":    gorm.Expr("downloads + 1"),
			"last_used_at": NowMillis(),
		}).Error
}
