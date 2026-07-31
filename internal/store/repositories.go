package store

import (
	"regexp"

	"arca/internal/format"
	"arca/internal/store/models"
)

var repositoryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

func IsValidRepositoryName(name string) bool { return repositoryNamePattern.MatchString(name) }

var permissionRank = map[string]int{
	models.PermissionRead:  1,
	models.PermissionWrite: 2,
	models.PermissionAdmin: 3,
}

func Satisfies(granted, required string) bool {
	return granted != "" && permissionRank[granted] >= permissionRank[required]
}

func (s *Store) CreateRepository(repository *models.Repository) error {
	if repository.ID == "" {
		repository.ID = NewID()
	}
	if repository.CreatedAt == 0 {
		repository.CreatedAt = NowMillis()
	}
	if repository.Format == "" {
		repository.Format = format.Maven2
	}

	// A repository that claims a name takes it back from whichever rename left
	// it behind as an alias, which is what stops a redirect outliving its point.
	return s.Transaction(func(tx *Store) error {
		if err := tx.ReleaseAlias(repository.Name); err != nil {
			return err
		}
		return tx.query().Create(repository).Error
	})
}

func (s *Store) RepositoryByName(name string) (*models.Repository, error) {
	var repository models.Repository
	if err := s.db.First(&repository, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &repository, nil
}

func (s *Store) EvictableRepositories() ([]models.Repository, error) {
	var repositories []models.Repository
	err := s.db.
		Where("type = ? AND cache_retention_days > 0", models.TypeProxy).
		Order("name").
		Find(&repositories).Error
	return repositories, err
}

func (s *Store) UpdateRepository(id string, changes map[string]any) error {
	return s.db.Model(&models.Repository{}).Where("id = ?", id).Updates(changes).Error
}

func (s *Store) VisibleRepositories(user *models.User) ([]models.Repository, error) {
	var repositories []models.Repository

	query := s.db.Model(&models.Repository{}).Order("name")
	switch {
	case user != nil && user.IsAdmin():
	case user != nil:
		query = query.
			Joins("LEFT JOIN repository_grants ON repository_grants.repository_id = repositories.id AND repository_grants.user_id = ?", user.ID).
			Where("repositories.visibility = ? OR repository_grants.user_id IS NOT NULL", models.VisibilityPublic)
	default:
		query = query.Where("visibility = ?", models.VisibilityPublic)
	}

	err := query.Find(&repositories).Error
	return repositories, err
}

func (s *Store) EffectivePermission(repository *models.Repository, user *models.User) (string, error) {
	if user != nil && user.IsAdmin() {
		return models.PermissionAdmin, nil
	}

	if user != nil {
		var grant models.Grant
		err := s.db.First(&grant, "repository_id = ? AND user_id = ?", repository.ID, user.ID).Error
		switch {
		case err == nil:
			return grant.Permission, nil
		case !isNotFound(err):
			return "", err
		}
	}

	if repository.Visibility == models.VisibilityPublic {
		return models.PermissionRead, nil
	}
	return "", nil
}

type GrantDetail struct {
	UserID     string `json:"userId"`
	Permission string `json:"permission"`
	Email      string `json:"email"`
	Name       string `json:"name"`
}

func (s *Store) ListGrants(repositoryID string) ([]GrantDetail, error) {
	grants := []GrantDetail{}
	err := s.db.Model(&models.Grant{}).
		Select("repository_grants.user_id AS user_id, repository_grants.permission, users.email, users.name").
		Joins("JOIN users ON users.id = repository_grants.user_id").
		Where("repository_grants.repository_id = ?", repositoryID).
		Order("users.email").
		Scan(&grants).Error
	return grants, err
}

func (s *Store) ReplaceGrants(repositoryID string, grants []models.Grant) error {
	return s.Transaction(func(tx *Store) error {
		if err := tx.query().Where("repository_id = ?", repositoryID).Delete(&models.Grant{}).Error; err != nil {
			return err
		}
		if len(grants) == 0 {
			return nil
		}
		return tx.query().Create(&grants).Error
	})
}

type RepositorySummary struct {
	RepositoryID string `json:"-"`
	Components   int64  `json:"components"`
	UpdatedAt    int64  `json:"updatedAt"`
}

func (s *Store) ComponentSummaries(repositoryIDs []string) (map[string]RepositorySummary, error) {
	summaries := map[string]RepositorySummary{}
	if len(repositoryIDs) == 0 {
		return summaries, nil
	}

	rows := []RepositorySummary{}
	err := s.db.Model(&models.Component{}).
		Select("repository_id, COUNT(*) AS components, COALESCE(MAX(updated_at), 0) AS updated_at").
		Where("repository_id IN ?", repositoryIDs).
		Group("repository_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		summaries[row.RepositoryID] = row
	}
	return summaries, nil
}

type RepositoryStats struct {
	Components int64 `json:"components"`
	Assets     int64 `json:"assets"`
	Bytes      int64 `json:"bytes"`
	UpdatedAt  int64 `json:"updatedAt"`
}

func (s *Store) RepositoryStats(repositoryID string) (RepositoryStats, error) {
	var stats RepositoryStats

	err := s.db.Raw(
		`SELECT
		   (SELECT COUNT(*) FROM components WHERE repository_id = ?) AS components,
		   (SELECT COUNT(*) FROM assets WHERE repository_id = ?) AS assets,
		   (SELECT COALESCE(SUM(size), 0) FROM assets WHERE repository_id = ?) AS bytes,
		   (SELECT COALESCE(MAX(updated_at), 0) FROM components WHERE repository_id = ?) AS updated_at`,
		repositoryID, repositoryID, repositoryID, repositoryID,
	).Scan(&stats).Error

	return stats, err
}

func (s *Store) DeleteRepository(repositoryID string) ([]string, error) {
	var keys []string

	err := s.Transaction(func(tx *Store) error {
		if err := tx.query().Model(&models.Asset{}).
			Where("repository_id = ?", repositoryID).
			Pluck("storage_key", &keys).Error; err != nil {
			return err
		}
		related := []any{
			&models.Asset{}, &models.Component{}, &models.DistTag{}, &models.Grant{},
			&models.RemoteMiss{}, &models.RepositoryAlias{}, &models.DeploymentLink{},
		}
		for _, model := range related {
			if err := tx.query().Where("repository_id = ?", repositoryID).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.query().Where("id = ?", repositoryID).Delete(&models.Repository{}).Error
	})

	return keys, err
}
