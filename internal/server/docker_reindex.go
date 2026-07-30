package server

import (
	"io"
	"sort"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/format"
	"arca/internal/store/models"
)

// Indexing a manifest reads the config blob it points at, so it can only be complete
// once that blob is local. Two paths cannot guarantee that at the time the manifest
// arrives: a proxy fetches the manifest first by definition, and a migration copies in
// whatever order the source pages. Both are repaired by walking what is stored and
// indexing it afresh, which is also the only way to fix a repository whose copy was
// interrupted halfway.

type reindexResult struct {
	Repository string `json:"repository"`
	Manifests  int    `json:"manifests"`
	Failed     int    `json:"failed"`
}

// storedManifest is one manifest found on disk, with the image it belongs to, which the
// path carries and the document does not.
type storedManifest struct {
	image   string
	tag     string
	asset   *models.Asset
	parsed  docker.Manifest
	size    int64
	isIndex bool
}

func (s *Server) reindexDockerRepository(repository *models.Repository) (reindexResult, error) {
	result := reindexResult{Repository: repository.Name}

	files, err := s.store.DockerStoredFiles(repository.ID, "")
	if err != nil {
		return result, err
	}

	manifests := make([]storedManifest, 0, len(files))
	for _, file := range files {
		found, ok, err := s.readStoredManifest(repository, file.Path)
		if err != nil {
			result.Failed++
			log.Warnf("reading %s/%s failed: %v", repository.Name, file.Path, err)
			continue
		}
		if ok {
			manifests = append(manifests, found)
		}
	}

	// Children before parents: an index's total size is the sum of its children's, so
	// indexing it first would record only what its descriptors declare. Nested indexes
	// are legal and vanishingly rare, and would need a second pass they do not get.
	sort.SliceStable(manifests, func(i, j int) bool {
		return !manifests[i].isIndex && manifests[j].isIndex
	})

	for _, manifest := range manifests {
		if err := s.indexStoredManifest(repository, manifest); err != nil {
			result.Failed++
			log.Warnf("indexing %s/%s failed: %v", repository.Name, manifest.asset.Path, err)
			continue
		}
		result.Manifests++
	}

	if result.Manifests > 0 || result.Failed > 0 {
		log.Infof("indexed %d manifests of %s, %d failed", result.Manifests, repository.Name, result.Failed)
	}
	return result, nil
}

// readStoredManifest recognises a manifest by its path and parses it. Anything else in
// the repository is a blob, which reports false rather than an error.
func (s *Server) readStoredManifest(repository *models.Repository, path string) (storedManifest, bool, error) {
	found := storedManifest{}

	switch image, tag, ok := docker.ParseTagPath(path); {
	case ok:
		found.image, found.tag = image, tag
	case docker.IsDigestManifestPath(path):
		found.image = docker.ImageOfManifestPath(path)
	default:
		return found, false, nil
	}

	asset, err := s.store.FindAsset(repository.ID, path)
	if err != nil {
		return found, false, err
	}
	found.asset = asset

	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return found, false, err
	}
	defer file.Close()

	document, err := io.ReadAll(io.LimitReader(file, maxManifestBytes))
	if err != nil {
		return found, false, err
	}

	found.parsed, err = docker.ParseManifest(document)
	if err != nil {
		// A stored document that is not a manifest is worth reporting once and then
		// leaving alone: it is servable either way, just not describable.
		log.Warnf("%s/%s is not a manifest this server can parse: %v", repository.Name, path, err)
		return found, false, nil
	}

	found.size = int64(len(document))
	found.isIndex = found.parsed.IsIndex()
	return found, true, nil
}

func (s *Server) indexStoredManifest(repository *models.Repository, manifest storedManifest) error {
	route := docker.Route{Reference: manifest.tag, Tag: manifest.tag}
	if manifest.tag == "" {
		digest := docker.SHA256(manifest.asset.SHA256)
		route = docker.Route{Reference: digest.String(), Digest: digest, ByDigest: true}
	}

	registry := registryRequest{repository: repository, image: manifest.image, route: route}

	// The layout could only guess the media type from the filename, which cannot tell
	// an index from an image manifest. The document knows.
	if manifest.parsed.MediaType != manifest.asset.ContentType {
		if err := s.store.SetAssetContentType(manifest.asset.ID, manifest.parsed.MediaType); err != nil {
			return err
		}
	}

	return s.indexDockerManifest(registry, docker.SHA256(manifest.asset.SHA256), manifest.parsed, manifest.size)
}

// reindexDockerRepositories rebuilds the metadata of every docker repository. It is
// offered as a maintenance action so a migration that was interrupted, or one run
// before this server knew how to index, can be repaired without recopying anything.
func (s *Server) reindexDockerRepositories() ([]reindexResult, error) {
	repositories, err := s.store.RepositoriesOfFormat(format.Docker)
	if err != nil {
		return nil, err
	}

	results := make([]reindexResult, 0, len(repositories))
	for index := range repositories {
		repository := &repositories[index]
		if repository.IsGroup() {
			continue
		}

		result, err := s.reindexDockerRepository(repository)
		if err != nil {
			log.Errorf("reindexing %s failed: %v", repository.Name, err)
			continue
		}
		results = append(results, result)
	}
	return results, nil
}
