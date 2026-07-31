package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

// ResolveRepository finds the repository a name addresses. The second return
// reports that the name was an alias rather than the repository's own, which is
// what tells a caller to redirect instead of serving in place.
func (s *Store) ResolveRepository(name string) (repository *models.Repository, aliased bool, err error) {
	repository, err = s.RepositoryByName(name)
	if err == nil {
		return repository, false, nil
	}
	if !isNotFound(err) {
		return nil, false, err
	}

	var alias models.RepositoryAlias
	if err := s.db.First(&alias, "name = ?", name).Error; err != nil {
		return nil, false, err
	}

	repository, err = s.RepositoryByID(alias.RepositoryID)
	if err != nil {
		return nil, false, err
	}
	return repository, true, nil
}

// ResolveRepositoryOfFormat is ResolveRepository narrowed to one format, so an
// alias cannot pull a docker request onto a Maven repository any more than a
// name can.
func (s *Store) ResolveRepositoryOfFormat(name, repositoryFormat string) (*models.Repository, bool, error) {
	repository, aliased, err := s.ResolveRepository(name)
	if err != nil {
		return nil, false, err
	}
	if repository.Format != repositoryFormat {
		return nil, false, gorm.ErrRecordNotFound
	}
	return repository, aliased, nil
}

func (s *Store) RepositoryByID(id string) (*models.Repository, error) {
	var repository models.Repository
	if err := s.db.First(&repository, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &repository, nil
}

func (s *Store) Aliases(repositoryID string) ([]models.RepositoryAlias, error) {
	aliases := []models.RepositoryAlias{}
	err := s.db.
		Where("repository_id = ?", repositoryID).
		Order("created_at DESC").
		Find(&aliases).Error
	return aliases, err
}

// CreateAlias points a name at a repository, taking the name over from whichever
// repository held it as an alias before. A name that belongs to a live
// repository is rejected by the caller, not here: this layer only knows aliases.
func (s *Store) CreateAlias(name, repositoryID string) error {
	now := NowMillis()

	return s.query().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}},
		DoUpdates: clause.Assignments(map[string]any{
			"repository_id": repositoryID,
			"created_at":    now,
		}),
	}).Create(&models.RepositoryAlias{
		Name:         name,
		RepositoryID: repositoryID,
		CreatedAt:    now,
	}).Error
}

func (s *Store) DeleteAlias(name, repositoryID string) (int64, error) {
	result := s.query().Where("name = ? AND repository_id = ?", name, repositoryID).Delete(&models.RepositoryAlias{})
	return result.RowsAffected, result.Error
}

// ReleaseAlias drops a name from the alias table whoever holds it, which is what
// makes a name reusable: the repository that claims it wins over the redirect.
func (s *Store) ReleaseAlias(name string) error {
	return s.query().Where("name = ?", name).Delete(&models.RepositoryAlias{}).Error
}

// AliasNames maps repository identifiers to the names that redirect to them,
// which is one query for a whole listing rather than one per row.
func (s *Store) AliasNames() (map[string][]string, error) {
	var aliases []models.RepositoryAlias
	if err := s.db.Order("created_at DESC").Find(&aliases).Error; err != nil {
		return nil, err
	}

	names := map[string][]string{}
	for _, alias := range aliases {
		names[alias.RepositoryID] = append(names[alias.RepositoryID], alias.Name)
	}
	return names, nil
}

// RenameRepository moves a repository to a new name and leaves the old one
// behind pointing at it. Everything that referred to the repository by name
// moves with it in the same transaction, because a rename that half applied
// would strand the old name, lose the new one, or empty a group.
func (s *Store) RenameRepository(repositoryID, from, to string) error {
	return s.Transaction(func(tx *Store) error {
		if err := tx.ReleaseAlias(to); err != nil {
			return err
		}
		err := tx.query().Model(&models.Repository{}).
			Where("id = ?", repositoryID).
			Update("name", to).Error
		if err != nil {
			return err
		}
		if err := tx.renameMemberTargets(from, to); err != nil {
			return err
		}
		if err := tx.renameDockerDefault(from, to); err != nil {
			return err
		}
		return tx.CreateAlias(from, repositoryID)
	})
}

// renameMemberTargets follows the rename into the groups that list this
// repository as a child. A member is a repository name, optionally followed by a
// path inside it, so only the first segment moves.
func (s *Store) renameMemberTargets(from, to string) error {
	var members []models.RepositoryMember
	err := s.query().
		Where("target = ? OR target LIKE ?", from, from+"/%").
		Find(&members).Error
	if err != nil {
		return err
	}

	for _, member := range members {
		target := to + member.Target[len(from):]
		err := s.query().Model(&models.RepositoryMember{}).
			Where("repository_id = ? AND position = ?", member.RepositoryID, member.Position).
			Update("target", target).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// renameDockerDefault keeps a bare "docker pull host/image" pointing at the same
// repository, since that setting stores a name rather than an identifier.
func (s *Store) renameDockerDefault(from, to string) error {
	current, err := s.Setting(SettingDockerRepository)
	if err != nil || current != from {
		return err
	}
	return s.SetSetting(SettingDockerRepository, to)
}
