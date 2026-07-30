package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/store/models"
)

func (s *Server) serveDockerManifest(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		s.serveDockerManifestRead(writer, request, registry)
	case http.MethodPut:
		s.serveDockerManifestPut(writer, request, registry)
	case http.MethodDelete:
		s.serveDockerManifestDelete(writer, registry)
	default:
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
	}
}

// manifestPathFor names where a reference is stored. A digest reads from the
// content-addressed store and a tag from its own directory, which holds the same
// bytes so that a tag pull needs no second lookup.
func manifestPathFor(registry registryRequest) string {
	if registry.route.ByDigest {
		return docker.ManifestPath(registry.image, registry.route.Digest)
	}
	return docker.TagPath(registry.image, registry.route.Tag)
}

func (s *Server) serveDockerManifestRead(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if registry.repository.IsProxy() {
		s.serveDockerProxyManifest(writer, request, registry)
		return
	}

	asset, err := s.store.FindAsset(registry.repository.ID, manifestPathFor(registry))
	if err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorManifestUnknown, "Not found")
		return
	}

	// A client that asked for manifest types and excluded the stored one is told
	// the manifest is absent, which is the spec's answer and is what sends it on to
	// another reference rather than leaving it to choke on a type it cannot read.
	if accept := request.Header.Values("Accept"); !docker.Accepts(accept, asset.ContentType) {
		log.Warnf("%s/%s is stored as %s, which %q does not accept",
			registry.repository.Name, registry.image, asset.ContentType, strings.Join(accept, ", "))
		dockerError(writer, http.StatusNotFound, docker.ErrorManifestUnknown,
			"This manifest is stored as "+asset.ContentType+", which the request does not accept")
		return
	}

	writer.Header().Set(docker.ContentDigestHeader, docker.SHA256(asset.SHA256).String())

	if request.Method == http.MethodGet {
		s.recordAssetTraffic(request, models.TrafficDownload, registry.repository, asset)
	}
	if !s.writeAsset(writer, request, asset) {
		dockerError(writer, http.StatusNotFound, docker.ErrorManifestUnknown, "Not found")
	}
}

func (s *Server) serveDockerManifestPut(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	document, err := io.ReadAll(io.LimitReader(request.Body, maxManifestBytes+1))
	if err != nil {
		dockerError(writer, http.StatusBadRequest, docker.ErrorManifestInvalid, "The manifest could not be read")
		return
	}
	if len(document) > maxManifestBytes {
		dockerError(writer, http.StatusRequestEntityTooLarge, docker.ErrorManifestInvalid, "The manifest is too large")
		return
	}

	manifest, err := docker.ParseManifest(document)
	if err != nil {
		dockerError(writer, http.StatusBadRequest, docker.ErrorManifestInvalid, err.Error())
		return
	}

	// The digest is of the bytes exactly as they arrived, so it is computed here
	// rather than taken from the storage layer, which would see them re-encoded.
	sum := sha256.Sum256(document)
	digest := docker.SHA256(hex.EncodeToString(sum[:]))

	// A client may PUT by digest, in which case the two have to agree.
	if registry.route.ByDigest && registry.route.Digest.String() != digest.String() {
		dockerError(writer, http.StatusBadRequest, docker.ErrorDigestInvalid,
			"The manifest does not match the digest it was pushed under")
		return
	}

	if code, message, ok := s.checkDockerManifestBlobs(registry, manifest); !ok {
		dockerError(writer, http.StatusNotFound, code, message)
		return
	}
	if !registry.route.ByDigest && !s.checkDockerTagWritable(writer, registry) {
		return
	}

	mediaType := manifest.MediaType
	if declared := request.Header.Get("Content-Type"); docker.IsManifestMediaType(declared) {
		mediaType = declared
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}
	details := uploadDetails{UploadedBy: uploadedBy, ContentType: mediaType}

	stored, err := s.storeUpload(registry.repository, docker.ManifestPath(registry.image, digest), bytes.NewReader(document), details)
	if err != nil {
		log.Errorf("storing a docker manifest failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	// The tag copy is what carries the component, so it is written second: a
	// failure here leaves the content addressable but untagged, which is a state
	// the registry already has to handle.
	if !registry.route.ByDigest {
		tagged, err := s.storeUpload(registry.repository, docker.TagPath(registry.image, registry.route.Tag), bytes.NewReader(document), details)
		if err != nil {
			log.Errorf("tagging a docker manifest failed: %v", err)
			dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
			return
		}
		stored = tagged
	}

	if err := s.indexDockerManifest(registry, digest, manifest, int64(len(document))); err != nil {
		log.Errorf("indexing a docker manifest failed: %v", err)
	}

	s.recordAssetTraffic(request, models.TrafficUpload, registry.repository, stored)

	writer.Header().Set(docker.ContentDigestHeader, digest.String())
	writer.Header().Set("Location", "/v2/"+registry.route.Name+"/manifests/"+registry.route.Reference)
	writer.WriteHeader(http.StatusCreated)
}

// checkDockerManifestBlobs enforces the spec's rule that everything a manifest
// references must already be pushed. Nondistributable layers are exempt: they name
// content a client is expected to fetch from its vendor, so they are never
// uploaded here.
func (s *Server) checkDockerManifestBlobs(registry registryRequest, manifest docker.Manifest) (string, string, bool) {
	for _, reference := range manifest.References() {
		if docker.IsNondistributable(reference.MediaType) {
			continue
		}

		digest, ok := docker.ParseDigest(reference.Digest)
		if !ok {
			return docker.ErrorDigestInvalid, reference.Digest + " is not a digest this server stores", false
		}

		path := docker.BlobPath(digest)
		code := docker.ErrorManifestBlobUnknown
		if reference.Kind == docker.ReferenceManifest {
			path = docker.ManifestPath(registry.image, digest)
			code = docker.ErrorManifestUnknown
		}

		if _, err := s.store.FindAsset(registry.repository.ID, path); err != nil {
			return code, reference.Digest + " has not been pushed to this repository", false
		}
	}
	return "", "", true
}

// checkDockerTagWritable applies the repository's version policy and, for a tag
// that already exists, its redeploy setting. Moving a tag is ordinary Docker
// practice, so a docker repository allows it by default and only an explicitly
// locked one refuses.
func (s *Server) checkDockerTagWritable(writer http.ResponseWriter, registry registryRequest) bool {
	tag := registry.route.Tag

	if !registry.repository.AcceptsPolicy(docker.Prerelease(tag)) {
		dockerError(writer, http.StatusBadRequest, docker.ErrorTagInvalid,
			"Repository policy '"+registry.repository.Policy+"' rejects the tag "+tag)
		return false
	}

	if registry.repository.AllowRedeploy {
		return true
	}
	if _, err := s.store.FindAsset(registry.repository.ID, docker.TagPath(registry.image, tag)); err == nil {
		dockerError(writer, http.StatusConflict, docker.ErrorDenied,
			"The tag "+tag+" already exists and this repository does not allow tags to move")
		return false
	}
	return true
}

// indexDockerManifest records the parsed manifest and everything it references, so
// a version's real size and its layer list can be read without reopening any
// document. It runs after the bytes are stored, because the config blob it reads
// is looked up the same way a pull would.
func (s *Server) indexDockerManifest(registry registryRequest, digest docker.Digest, manifest docker.Manifest, size int64) error {
	namespace, name, ok := registry.namespace()
	if !ok {
		return errors.New("server: the image name is not one that maps onto coordinates")
	}

	record := &models.DockerManifest{
		RepositoryID: registry.repository.ID,
		Digest:       digest.String(),
		MediaType:    manifest.MediaType,
		Size:         size,
		Namespace:    namespace,
		Name:         name,
		ConfigDigest: manifest.Config.Digest,
		LayerCount:   len(manifest.Layers),
		Annotations:  encodeJSONMap(manifest.Annotations),
	}
	if manifest.Subject != nil {
		record.Subject = manifest.Subject.Digest
	}

	references := make([]models.DockerReference, 0, len(manifest.Layers)+len(manifest.Manifests)+1)
	for _, reference := range manifest.References() {
		references = append(references, models.DockerReference{
			RepositoryID:   registry.repository.ID,
			ManifestDigest: digest.String(),
			ChildDigest:    reference.Digest,
			Kind:           reference.Kind,
			MediaType:      reference.MediaType,
			Size:           reference.Size,
			Position:       reference.Position,
			Platform:       reference.Platform,
			URLs:           strings.Join(reference.URLs, "\n"),
			Annotations:    encodeJSONMap(reference.Annotations),
		})
	}

	if manifest.IsIndex() {
		record.TotalSize, record.ImageCreated = s.describeDockerIndex(registry, manifest)
	} else {
		record.TotalSize = manifest.DeclaredSize()
		s.describeDockerImage(registry, manifest, record)
	}

	return s.store.SaveDockerManifest(record, references)
}

// describeDockerIndex sums the children rather than the descriptors, because a
// descriptor's size is the child document's own and says nothing about its layers. A
// child that has not been indexed yet contributes what it declares.
//
// The build time comes from the children too: an index has no config of its own, and
// reporting nothing when every platform knows when it was built is needlessly bare.
func (s *Server) describeDockerIndex(registry registryRequest, manifest docker.Manifest) (int64, int64) {
	var total, created int64

	for _, child := range manifest.Manifests {
		indexed, err := s.store.DockerManifest(registry.repository.ID, child.Digest)
		if err != nil {
			total += child.Size
			continue
		}
		total += indexed.TotalSize
		created = max(created, indexed.ImageCreated)
	}
	return total, created
}

// describeDockerImage reads the platform and labels out of the config blob. A
// config that was never pushed is normal in a partially migrated repository, so
// its absence leaves the fields empty rather than failing the index.
func (s *Server) describeDockerImage(registry registryRequest, manifest docker.Manifest, record *models.DockerManifest) {
	digest, ok := docker.ParseDigest(manifest.Config.Digest)
	if !ok {
		return
	}

	asset, err := s.store.FindAsset(registry.repository.ID, docker.BlobPath(digest))
	if err != nil {
		return
	}

	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return
	}
	defer file.Close()

	document, err := io.ReadAll(io.LimitReader(file, maxManifestBytes))
	if err != nil {
		return
	}

	config, err := docker.ParseConfig(document)
	if err != nil {
		log.Warnf("the config blob of %s/%s is unreadable: %v", registry.repository.Name, registry.image, err)
		return
	}

	record.Architecture = config.Architecture
	record.OS = config.OS
	record.Variant = config.Variant
	record.ImageCreated = config.Created
	record.Labels = encodeJSONMap(config.Labels)
}

func encodeJSONMap(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// serveDockerManifestDelete removes a tag or an untagged manifest. Blobs are left
// alone either way: they are shared, so reclaiming them is the sweep's job and not
// something a single delete can reason about.
func (s *Server) serveDockerManifestDelete(writer http.ResponseWriter, registry registryRequest) {
	path := manifestPathFor(registry)

	asset, err := s.store.FindAsset(registry.repository.ID, path)
	if err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorManifestUnknown, "Not found")
		return
	}

	keys, err := s.store.DeleteAsset(registry.repository.ID, path)
	if err != nil {
		log.Errorf("deleting a docker manifest failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	if err := s.blobs.Delete(keys...); err != nil {
		log.Errorf("removing a deleted docker manifest failed: %v", err)
	}

	// Only a digest delete retires the parsed record. Deleting a tag leaves the
	// manifest reachable by digest, which is what the spec means by untagging.
	if registry.route.ByDigest {
		if err := s.store.DeleteDockerManifest(registry.repository.ID, docker.SHA256(asset.SHA256).String()); err != nil {
			log.Errorf("deleting a docker manifest record failed: %v", err)
		}
	}

	writer.WriteHeader(http.StatusAccepted)
}
