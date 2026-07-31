package server

import (
	"sort"
	"strings"

	"arca/internal/format"
	"arca/internal/link"
	"arca/internal/store/models"
)

// resolution is what a deployment link points at right now. A link stores an
// artifact rather than a path, so every follow of it resolves again.
type resolution struct {
	version string
	asset   *models.Asset
}

// filename is the name the file is offered under: the link's own if it sets
// one, and otherwise whatever the artifact was published as.
func (r resolution) filename(configured string) string {
	if configured != "" {
		return configured
	}
	return r.asset.Path[strings.LastIndex(r.asset.Path, "/")+1:]
}

// eligibleVersions orders the versions a link may resolve to, newest first, with
// the ones its policy excludes dropped. The policy values are the repository's
// own, so a link on a mixed repository can still pin itself to releases.
func eligibleVersions(layout format.Layout, policy string, components []models.Component) []models.Component {
	eligible := make([]models.Component, 0, len(components))

	for _, component := range components {
		prerelease := layout.Prerelease(component.Version)
		switch policy {
		case models.PolicySnapshot:
			if !prerelease {
				continue
			}
		case models.PolicyMixed:
		default:
			if prerelease {
				continue
			}
		}
		eligible = append(eligible, component)
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		return layout.Compare(eligible[i].Version, eligible[j].Version) > 0
	})
	return eligible
}

// resolveLink walks the eligible versions newest first and answers with the
// first file that matches. It does not stop at the newest version: a release
// that shipped without the file the link names should not break the link, it
// should keep serving the newest release that does have one.
func (s *Server) resolveLink(repository *models.Repository, deployment *models.DeploymentLink) (*resolution, error) {
	components, err := s.store.ComponentVersions(repository.ID, deployment.Namespace, deployment.Artifact)
	if err != nil {
		return nil, err
	}

	layout := layoutFor(repository)

	for _, component := range eligibleVersions(layout, deployment.Policy, components) {
		matcher, err := link.Compile(deployment.MatchType, deployment.Pattern, link.Fields{
			Namespace:   component.Namespace,
			Name:        component.Name,
			Version:     component.Version,
			BaseVersion: component.BaseVersion,
		})
		if err != nil {
			return nil, err
		}

		files, err := s.store.ComponentFiles(repository.ID, deployment.Namespace, deployment.Artifact, component.Version)
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			if !matcher.MatchString(file.Name) {
				continue
			}
			asset, err := s.store.FindAsset(repository.ID, file.Path)
			if err != nil {
				// The listing and the asset came from the same row, so this only
				// happens if the file was deleted between the two queries.
				continue
			}
			return &resolution{version: component.Version, asset: asset}, nil
		}
	}

	return nil, nil
}
