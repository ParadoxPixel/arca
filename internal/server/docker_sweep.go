package server

import (
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/format"
	"arca/internal/store"
	"arca/internal/store/models"
)

// A docker repository is the one format here that cannot reclaim space when
// something is deleted. Removing a tag leaves its layers behind, correctly, because
// they are shared: any other tag may still need them. Nothing else ever revisits
// that decision, so without a sweep a repository grows for as long as it is used.
//
// Reachability starts at the tags and follows the reference graph. An untagged
// manifest is unreachable by definition, which is what makes an index's children
// survive while a retired tag's exclusive layers do not.
//
// Only hosted repositories are swept. A proxy's content is refetchable, and a client
// that pulled an image by digest cached no tag to be reachable from, so reachability
// would delete exactly what it is actively using. Proxies age out by last access
// instead, which purgeIdleCaches already does for every format.

// sweepGracePeriod is how long a freshly stored blob or manifest is left alone
// even when nothing references it. A push stores its layers first and the
// manifest that ties them together last, so a sweep that ran in between would
// find every layer unreferenced, delete them, and leave the tag that arrived a
// moment later pointing at bytes that no longer exist. Nothing a client does
// can repair that, since a re-push sees the manifest already present. The
// grace is a day to cover any push a chunked upload session can still finish.
const sweepGracePeriod = 24 * time.Hour

type sweepResult struct {
	Repository string `json:"repository"`
	Blobs      int    `json:"blobs"`
	Manifests  int    `json:"manifests"`
	Bytes      int64  `json:"bytes"`
}

type sweepPlan struct {
	repository *models.Repository
	// paths are the assets to remove, blobs and manifests together, since both are
	// deleted the same way.
	paths []string
	// digests are the manifest records whose parsed form goes with them.
	digests []string
	result  sweepResult
}

// planDockerSweep decides what is unreachable without deleting anything, so the same
// walk backs both the preview and the sweep.
func (s *Server) planDockerSweep(repository *models.Repository) (sweepPlan, error) {
	plan := sweepPlan{repository: repository, result: sweepResult{Repository: repository.Name}}
	settledBefore := time.Now().Add(-s.sweepGrace()).UnixMilli()

	roots, err := s.store.DockerTagRoots(repository.ID)
	if err != nil {
		return plan, err
	}

	reachableManifests := map[string]bool{}
	reachableBlobs := map[string]bool{}

	frontier := make([]string, 0, len(roots))
	for _, root := range roots {
		digest := docker.SHA256(root.SHA256).String()
		if reachableManifests[digest] {
			continue
		}
		reachableManifests[digest] = true
		frontier = append(frontier, digest)
	}

	// Breadth-first through the index children. The visited set is what stops a
	// manifest that somehow references itself from looping forever.
	for len(frontier) > 0 {
		children, blobs, err := s.store.DockerChildrenOf(repository.ID, frontier)
		if err != nil {
			return plan, err
		}

		for _, blob := range blobs {
			reachableBlobs[blob] = true
		}

		frontier = frontier[:0]
		for _, child := range children {
			if reachableManifests[child] {
				continue
			}
			reachableManifests[child] = true
			frontier = append(frontier, child)
		}
	}

	blobs, err := s.store.DockerStoredFiles(repository.ID, docker.BlobsDirectory+"/")
	if err != nil {
		return plan, err
	}
	for _, blob := range blobs {
		if reachableBlobs[docker.SHA256(blob.SHA256).String()] || blob.CreatedAt >= settledBefore {
			continue
		}
		plan.paths = append(plan.paths, blob.Path)
		plan.result.Blobs++
	}

	manifests, err := s.unreachableDockerManifests(repository, reachableManifests)
	if err != nil {
		return plan, err
	}
	for _, manifest := range manifests {
		if manifest.CreatedAt >= settledBefore {
			continue
		}
		plan.paths = append(plan.paths, manifest.Path)
		plan.digests = append(plan.digests, docker.SHA256(manifest.SHA256).String())
		plan.result.Manifests++
	}

	return plan, nil
}

// unreachableDockerManifests finds digest-addressed manifests no tag can reach. They
// live under a directory per image rather than one shared store, so they are found by
// walking every asset and recognising the shape rather than by one prefix.
func (s *Server) unreachableDockerManifests(repository *models.Repository, reachable map[string]bool) ([]store.StoredFile, error) {
	files, err := s.store.DockerStoredFiles(repository.ID, "")
	if err != nil {
		return nil, err
	}

	unreachable := []store.StoredFile{}
	for _, file := range files {
		if !docker.IsDigestManifestPath(file.Path) {
			continue
		}
		if reachable[docker.SHA256(file.SHA256).String()] {
			continue
		}
		unreachable = append(unreachable, file)
	}
	return unreachable, nil
}

// applyDockerSweep deletes what the plan named. Rows go before files: a row without
// a file answers every request with a 404 it can never recover from, while a file
// without a row is merely unreferenced and the next sweep collects it.
func (s *Server) applyDockerSweep(plan sweepPlan) (sweepResult, error) {
	if len(plan.paths) == 0 {
		return plan.result, nil
	}

	keys, err := s.store.DeleteAssetsAt(plan.repository.ID, plan.paths)
	if err != nil {
		return plan.result, err
	}
	if err := s.store.DeleteDockerManifests(plan.repository.ID, plan.digests); err != nil {
		return plan.result, err
	}

	usage, err := s.blobs.SizeOf(keys)
	if err != nil {
		log.Warnf("measuring swept files of %s failed: %v", plan.repository.Name, err)
	}
	plan.result.Bytes = usage

	if err := s.blobs.Delete(keys...); err != nil {
		return plan.result, err
	}

	log.Infof("swept %d unreferenced blobs and %d untagged manifests from %s",
		plan.result.Blobs, plan.result.Manifests, plan.repository.Name)

	return plan.result, nil
}

// sweepGrace is the grace period in force. Tests shorten it through
// sweepGraceOverride; nothing else does.
func (s *Server) sweepGrace() time.Duration {
	if s.sweepGraceOverride != nil {
		return *s.sweepGraceOverride
	}
	return sweepGracePeriod
}

// sweepDockerRepositories runs the sweep over every docker repository. A failure on
// one is logged and the rest carry on, because a repository whose graph cannot be
// read should not stop the others reclaiming their space.
func (s *Server) sweepDockerRepositories(apply bool) ([]sweepResult, error) {
	repositories, err := s.store.RepositoriesOfFormat(format.Docker)
	if err != nil {
		return nil, err
	}

	results := make([]sweepResult, 0, len(repositories))
	for index := range repositories {
		repository := &repositories[index]
		if repository.IsProxy() || repository.IsGroup() {
			continue
		}

		plan, err := s.planDockerSweep(repository)
		if err != nil {
			log.Errorf("planning the %s sweep failed: %v", repository.Name, err)
			continue
		}
		if !apply {
			results = append(results, plan.result)
			continue
		}

		result, err := s.applyDockerSweep(plan)
		if err != nil {
			log.Errorf("sweeping %s failed: %v", repository.Name, err)
		}
		results = append(results, result)
	}
	return results, nil
}

// purgeUnreachableDockerContent is the periodic half. It runs alongside the proxy
// cache eviction, which is the same job for a different reason.
func (s *Server) purgeUnreachableDockerContent(time.Time) {
	if _, err := s.sweepDockerRepositories(true); err != nil {
		log.Errorf("sweeping docker repositories failed: %v", err)
	}
}
