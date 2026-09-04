package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/store/models"
)

// dockerHubHosts are the registries that expect a single-segment image to be
// addressed as library/<name>. Nothing else does, and guessing wrongly turns every
// pull of a private registry's top-level image into a 404, so the rule is keyed on
// the host rather than applied everywhere.
var dockerHubHosts = map[string]bool{
	"registry-1.docker.io":    true,
	"registry.hub.docker.com": true,
	"index.docker.io":         true,
	"docker.io":               true,
}

// manifestAccept asks for every manifest encoding this server understands. Without
// it Docker Hub answers with a schema 1 manifest, which is deprecated and which the
// parser deliberately rejects.
var manifestAccept = strings.Join([]string{
	docker.MediaTypeOCIIndex,
	docker.MediaTypeOCIManifest,
	docker.MediaTypeDockerList,
	docker.MediaTypeDockerManifest,
}, ", ")

// remoteRoot is the registry API's own prefix. Every upstream serves under it, which
// is the same reason this server mounts /v2 at its host root: a Docker client builds
// the path itself and there is nowhere else to put it.
const remoteRoot = "v2/"

// upstreamImage is the name to ask the remote for. It differs from the local name
// only on Docker Hub, whose official images live under an implicit library/ scope.
func upstreamImage(repository *models.Repository, image string) string {
	if strings.Contains(image, "/") {
		return image
	}

	parsed, err := url.Parse(repository.RemoteURL)
	if err != nil || !dockerHubHosts[parsed.Host] {
		return image
	}
	return "library/" + image
}

// dockerManifestFetch names both ends of a cached manifest. A tag is cached under its
// own directory so it carries coordinates and shows up as a version, while a digest
// is cached in the content-addressed store where it can never go stale.
func (s *Server) dockerManifestFetch(registry registryRequest) proxyFetch {
	remote := upstreamImage(registry.repository, registry.image)

	fetch := proxyFetch{
		CachePath:  manifestPathFor(registry),
		RemotePath: remoteRoot + remote + "/manifests/" + registry.route.Reference,
		Accept:     manifestAccept,
	}
	if registry.route.ByDigest {
		fetch.ExpectSHA256 = registry.route.Digest.Hex
	}
	fetch.Indexed = func(asset *models.Asset) error {
		return s.indexCachedDockerManifest(registry, asset)
	}
	// A tag request names no digest, so the header can only be filled in once the
	// cached row is known. The row's SHA256 is that digest by construction.
	fetch.Headers = func(asset *models.Asset) map[string]string {
		return map[string]string{docker.ContentDigestHeader: docker.SHA256(asset.SHA256).String()}
	}
	return fetch
}

func (s *Server) dockerBlobFetch(registry registryRequest) proxyFetch {
	remote := upstreamImage(registry.repository, registry.image)

	digest := registry.route.Digest

	return proxyFetch{
		CachePath:    docker.BlobPath(digest),
		RemotePath:   remoteRoot + remote + "/blobs/" + digest.String(),
		Limit:        s.dockerBlobLimit(),
		ExpectSHA256: digest.Hex,
		Headers: func(*models.Asset) map[string]string {
			return map[string]string{docker.ContentDigestHeader: digest.String()}
		},
	}
}

// indexCachedDockerManifest builds the same parsed metadata a push does, so a proxied
// image is as legible in the UI as a hosted one. A manifest whose blobs have not been
// pulled yet still indexes: the layer list is what the manifest says, and the sizes
// come from the document rather than from the files.
func (s *Server) indexCachedDockerManifest(registry registryRequest, asset *models.Asset) error {
	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return err
	}
	defer file.Close()

	document, err := io.ReadAll(io.LimitReader(file, maxManifestBytes))
	if err != nil {
		return err
	}

	manifest, err := docker.ParseManifest(document)
	if err != nil {
		// An upstream that served something unparseable is worth a line in the log,
		// but the bytes are cached and servable either way.
		log.Warnf("the cached manifest %s/%s is not one this server can parse: %v",
			registry.repository.Name, asset.Path, err)
		return nil
	}

	// The layout could only guess the media type from the filename, so the document's
	// own is written over it now that it has been parsed.
	if manifest.MediaType != asset.ContentType {
		if err := s.store.SetAssetContentType(asset.ID, manifest.MediaType); err != nil {
			return err
		}
	}

	return s.indexDockerManifest(registry, docker.SHA256(asset.SHA256), manifest, int64(len(document)))
}

// serveDockerProxyManifest answers from the cache, refilling it from the remote when
// the copy is cold or a tag has passed its TTL. Only a tag expires: a digest names
// exactly one document for all time.
func (s *Server) serveDockerProxyManifest(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if !s.dockerProxyReadable(writer, registry) {
		return
	}

	s.serveProxyFetch(writer, request, registry.repository, s.dockerManifestFetch(registry))
}

func (s *Server) serveDockerProxyBlob(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if !s.dockerProxyReadable(writer, registry) {
		return
	}
	s.serveProxyFetch(writer, request, registry.repository, s.dockerBlobFetch(registry))
}

// dockerProxyReadable refuses a request a proxy cannot serve. A tag list needs the
// upstream's own catalogue, which this server does not mirror, so it answers from
// what has been pulled instead and says so rather than pretending to be complete.
func (s *Server) dockerProxyReadable(writer http.ResponseWriter, registry registryRequest) bool {
	if registry.repository.RemoteURL == "" {
		dockerError(writer, http.StatusNotFound, docker.ErrorNameUnknown,
			"This proxy repository has no remote to fetch from")
		return false
	}
	return true
}
