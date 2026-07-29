package store

import (
	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

func (s *Store) DistTags(repositoryID, namespace, name string) (map[string]string, error) {
	var rows []models.DistTag
	err := s.db.
		Where("repository_id = ? AND namespace = ? AND name = ?", repositoryID, namespace, name).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string, len(rows))
	for _, row := range rows {
		tags[row.Tag] = row.Version
	}
	return tags, nil
}

func (s *Store) SetDistTag(repositoryID, namespace, name, tag, version string) error {
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "repository_id"}, {Name: "namespace"}, {Name: "name"}, {Name: "tag"},
		},
		DoUpdates: clause.Assignments(map[string]any{"version": version, "updated_at": NowMillis()}),
	}).Create(&models.DistTag{
		ID:           NewID(),
		RepositoryID: repositoryID,
		Namespace:    namespace,
		Name:         name,
		Tag:          tag,
		Version:      version,
		UpdatedAt:    NowMillis(),
	}).Error
}

func (s *Store) DeleteDistTag(repositoryID, namespace, name, tag string) (int64, error) {
	result := s.db.
		Where("repository_id = ? AND namespace = ? AND name = ? AND tag = ?", repositoryID, namespace, name, tag).
		Delete(&models.DistTag{})
	return result.RowsAffected, result.Error
}

// DeleteDistTagsFor drops tags whose version no longer exists, which is what
// unpublishing leaves behind.
func (s *Store) DeleteDistTagsFor(repositoryID, namespace, name string, versions []string) error {
	query := s.db.Where("repository_id = ? AND namespace = ? AND name = ?", repositoryID, namespace, name)
	if len(versions) > 0 {
		query = query.Where("version IN ?", versions)
	}
	return query.Delete(&models.DistTag{}).Error
}
