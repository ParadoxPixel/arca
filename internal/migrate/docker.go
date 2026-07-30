package migrate

import (
	"strings"

	"arca/internal/docker"
)

// Nexus lays a docker repository out as the registry API addresses it, under a v2
// prefix, and stores blobs once on a shared path rather than per image. Verified
// against a Nexus 3.70 instance, which serves only the shared shape; the per-image one
// is kept because other versions may differ and it costs a case.
//
//	v2/-/blobs/sha256:<hex>                 the shared blob store
//	v2/<image>/blobs/sha256:<hex>           a per-image blob
//	v2/<image>/manifests/sha256:<hex>       a manifest by digest
//	v2/<image>/manifests/<tag>              a tag, which only the components walk lists
const (
	nexusRoot        = "v2/"
	nexusSharedBlobs = "-"
	nexusBlobs       = "blobs"
	nexusManifests   = "manifests"
)

// DockerPath maps a Nexus docker path onto this server's layout. It reports false for
// anything it does not recognise, so an unexpected shape is skipped and counted rather
// than stored somewhere nothing will read it back from.
func DockerPath(path string) (string, bool) {
	rest, found := strings.CutPrefix(strings.TrimPrefix(path, "/"), nexusRoot)
	if !found {
		return "", false
	}

	cut := strings.LastIndex(rest, "/"+nexusBlobs+"/")
	if cut >= 0 {
		// Every blob lands in one store regardless of which image Nexus filed it
		// under, because a digest names the same bytes either way.
		digest, ok := docker.ParseDigest(rest[cut+len(nexusBlobs)+2:])
		if !ok {
			return "", false
		}
		image := rest[:cut]
		if image != nexusSharedBlobs && !docker.IsValidName(image) {
			return "", false
		}
		return docker.BlobPath(digest), true
	}

	cut = strings.LastIndex(rest, "/"+nexusManifests+"/")
	if cut < 0 {
		return "", false
	}

	image, reference := rest[:cut], rest[cut+len(nexusManifests)+2:]
	if !docker.IsValidName(image) {
		return "", false
	}

	if digest, ok := docker.ParseDigest(reference); ok {
		return docker.ManifestPath(image, digest), true
	}
	if docker.IsValidTag(reference) {
		return docker.TagPath(image, reference), true
	}
	return "", false
}
