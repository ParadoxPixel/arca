package store

import (
	"gorm.io/gorm/clause"

	"arca/internal/store/models"
)

func (s *Store) CreateDockerUpload(upload *models.DockerUpload) error {
	now := NowMillis()
	upload.CreatedAt = now
	upload.UpdatedAt = now
	return s.db.Create(upload).Error
}

func (s *Store) DockerUpload(repositoryID, id string) (*models.DockerUpload, error) {
	var upload models.DockerUpload
	err := s.db.First(&upload, "id = ? AND repository_id = ?", id, repositoryID).Error
	if err != nil {
		return nil, err
	}
	return &upload, nil
}

func (s *Store) SetDockerUploadSize(id string, size int64) error {
	return s.db.Model(&models.DockerUpload{}).Where("id = ?", id).
		UpdateColumns(map[string]any{"size": size, "updated_at": NowMillis()}).Error
}

func (s *Store) DeleteDockerUpload(id string) error {
	return s.db.Delete(&models.DockerUpload{}, "id = ?", id).Error
}

// StaleDockerUploads names the sessions a client walked away from, so the sweep
// can drop their files before removing the rows.
func (s *Store) StaleDockerUploads(cutoff int64) ([]string, error) {
	var ids []string
	err := s.db.Model(&models.DockerUpload{}).
		Where("updated_at < ?", cutoff).
		Pluck("id", &ids).Error
	return ids, err
}

func (s *Store) DeleteDockerUploads(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Delete(&models.DockerUpload{}, "id IN ?", ids).Error
}

// SaveDockerManifest records a parsed manifest together with everything it
// references. The references are replaced rather than merged so a re-pushed
// digest cannot accumulate edges from an earlier parse.
func (s *Store) SaveDockerManifest(manifest *models.DockerManifest, references []models.DockerReference) error {
	now := NowMillis()

	return s.Transaction(func(tx *Store) error {
		manifest.ID = NewID()
		manifest.CreatedAt = now
		manifest.UpdatedAt = now

		err := tx.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repository_id"}, {Name: "digest"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"media_type", "size", "namespace", "name", "config_digest",
				"architecture", "os", "variant", "image_created", "layer_count",
				"total_size", "labels", "annotations", "subject", "updated_at",
			}),
		}).Create(manifest).Error
		if err != nil {
			return err
		}

		err = tx.query().Where("repository_id = ? AND manifest_digest = ?", manifest.RepositoryID, manifest.Digest).
			Delete(&models.DockerReference{}).Error
		if err != nil {
			return err
		}
		if len(references) == 0 {
			return nil
		}
		return tx.db.Create(&references).Error
	})
}

func (s *Store) DockerManifest(repositoryID, digest string) (*models.DockerManifest, error) {
	var manifest models.DockerManifest
	err := s.db.First(&manifest, "repository_id = ? AND digest = ?", repositoryID, digest).Error
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}

// UpdateDockerManifestPlatform writes back the fields that could only be read once the
// config blob was local, which on a proxy is always after the manifest itself.
func (s *Store) UpdateDockerManifestPlatform(manifest *models.DockerManifest) error {
	return s.db.Model(&models.DockerManifest{}).
		Where("repository_id = ? AND digest = ?", manifest.RepositoryID, manifest.Digest).
		UpdateColumns(map[string]any{
			"architecture":  manifest.Architecture,
			"os":            manifest.OS,
			"variant":       manifest.Variant,
			"image_created": manifest.ImageCreated,
			"labels":        manifest.Labels,
		}).Error
}

func (s *Store) DockerReferences(repositoryID, digest string) ([]models.DockerReference, error) {
	var references []models.DockerReference
	err := s.db.
		Where("repository_id = ? AND manifest_digest = ?", repositoryID, digest).
		Order("kind, position").
		Find(&references).Error
	return references, err
}

// DockerBlobSharing counts how many manifests in a repository still reference each
// digest. It is what turns "this layer is 400 MB" into "this layer is 400 MB shared
// with four other tags", which is the honest answer to why deleting a tag reclaims
// so little.
func (s *Store) DockerBlobSharing(repositoryID string, digests []string) (map[string]int, error) {
	sharing := map[string]int{}
	if len(digests) == 0 {
		return sharing, nil
	}

	var rows []struct {
		ChildDigest string
		Manifests   int
	}
	err := s.db.Model(&models.DockerReference{}).
		Select("child_digest, COUNT(DISTINCT manifest_digest) AS manifests").
		Where("repository_id = ? AND child_digest IN ?", repositoryID, digests).
		Group("child_digest").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		sharing[row.ChildDigest] = row.Manifests
	}
	return sharing, nil
}

// DockerManifestsByDigest reads several parsed manifests at once, which is how the
// children of a multi-platform index are described without a query each.
func (s *Store) DockerManifestsByDigest(repositoryID string, digests []string) (map[string]models.DockerManifest, error) {
	found := map[string]models.DockerManifest{}
	if len(digests) == 0 {
		return found, nil
	}

	var manifests []models.DockerManifest
	err := s.db.Where("repository_id = ? AND digest IN ?", repositoryID, digests).Find(&manifests).Error
	if err != nil {
		return nil, err
	}

	for _, manifest := range manifests {
		found[manifest.Digest] = manifest
	}
	return found, nil
}

func (s *Store) DeleteDockerManifest(repositoryID, digest string) error {
	return s.Transaction(func(tx *Store) error {
		err := tx.query().Where("repository_id = ? AND manifest_digest = ?", repositoryID, digest).
			Delete(&models.DockerReference{}).Error
		if err != nil {
			return err
		}
		return tx.query().Where("repository_id = ? AND digest = ?", repositoryID, digest).
			Delete(&models.DockerManifest{}).Error
	})
}

// DockerImages lists the images a repository holds, rebuilt from the coordinates
// of its tags, so the catalog needs no table of its own.
func (s *Store) DockerImages(repositoryID string) ([]string, error) {
	var rows []struct {
		Namespace string
		Name      string
	}
	err := s.db.Model(&models.Component{}).
		Select("DISTINCT namespace, name").
		Where("repository_id = ?", repositoryID).
		Order("namespace, name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	images := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Namespace == "" {
			images = append(images, row.Name)
			continue
		}
		images = append(images, row.Namespace+"/"+row.Name)
	}
	return images, nil
}

// StoredFile is one asset a sweep has to decide about: its path to delete by, and
// its digest to match against what is still reachable.
type StoredFile struct {
	Path   string
	SHA256 string
}

// DockerTagRoots names every manifest a tag points at. A tag manifest is the only
// docker asset linked to a component, and its own SHA256 is the digest it resolves
// to, so the roots of the reachability graph need no table of their own.
func (s *Store) DockerTagRoots(repositoryID string) ([]StoredFile, error) {
	files := []StoredFile{}
	err := s.db.Model(&models.Asset{}).
		Select("path, sha256").
		Where("repository_id = ? AND component_id IS NOT NULL", repositoryID).
		Scan(&files).Error
	return files, err
}

// DockerStoredFiles lists the assets under a path prefix with their digests, which
// is how the sweep enumerates the blob and digest-manifest stores.
func (s *Store) DockerStoredFiles(repositoryID, prefix string) ([]StoredFile, error) {
	files := []StoredFile{}

	bounds, bounded := RangeForPrefix(prefix)
	query := s.db.Model(&models.Asset{}).
		Select("path, sha256").
		Where("repository_id = ?", repositoryID)
	if bounded {
		query = query.Where("path >= ? AND path < ?", bounds.Lower, bounds.Upper)
	}

	return files, query.Scan(&files).Error
}

// DockerChildrenOf names what the given manifests reference, split by whether the
// child is another manifest or a blob. The sweep walks the first to find everything
// reachable and keeps the second.
func (s *Store) DockerChildrenOf(repositoryID string, manifests []string) (children []string, blobs []string, err error) {
	if len(manifests) == 0 {
		return nil, nil, nil
	}

	var rows []struct {
		ChildDigest string
		Kind        string
	}
	err = s.db.Model(&models.DockerReference{}).
		Select("child_digest, kind").
		Where("repository_id = ? AND manifest_digest IN ?", repositoryID, manifests).
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	for _, row := range rows {
		if row.Kind == models.DockerReferenceManifest {
			children = append(children, row.ChildDigest)
			continue
		}
		blobs = append(blobs, row.ChildDigest)
	}
	return children, blobs, nil
}

// DeleteDockerManifests drops the parsed records and edges of manifests a sweep has
// decided are unreachable. The asset holding the bytes is removed separately.
func (s *Store) DeleteDockerManifests(repositoryID string, digests []string) error {
	if len(digests) == 0 {
		return nil
	}

	return s.Transaction(func(tx *Store) error {
		err := tx.query().Where("repository_id = ? AND manifest_digest IN ?", repositoryID, digests).
			Delete(&models.DockerReference{}).Error
		if err != nil {
			return err
		}
		return tx.query().Where("repository_id = ? AND digest IN ?", repositoryID, digests).
			Delete(&models.DockerManifest{}).Error
	})
}

// RepositoriesOfFormat is what the periodic docker sweep walks. The format is passed
// in rather than named here, so this package stays free of format constants.
func (s *Store) RepositoriesOfFormat(repositoryFormat string) ([]models.Repository, error) {
	var repositories []models.Repository
	err := s.db.Where("format = ?", repositoryFormat).Order("name").Find(&repositories).Error
	return repositories, err
}

// HasPublicRepositories reports whether anyone can read anything of a format
// without signing in. The registry version check is repository-agnostic, so it
// is the one endpoint that has to answer before a repository is even named.
func (s *Store) HasPublicRepositories(repositoryFormat string) (bool, error) {
	var count int64
	err := s.db.Model(&models.Repository{}).
		Where("format = ? AND visibility = ?", repositoryFormat, models.VisibilityPublic).
		Limit(1).
		Count(&count).Error
	return count > 0, err
}

// RepositoryByNameAndFormat keeps a docker request from resolving onto a Maven
// repository that happens to share the leading path segment of an image name.
func (s *Store) RepositoryByNameAndFormat(name, repositoryFormat string) (*models.Repository, error) {
	var repository models.Repository
	err := s.db.First(&repository, "name = ? AND format = ?", name, repositoryFormat).Error
	if err != nil {
		return nil, err
	}
	return &repository, nil
}
