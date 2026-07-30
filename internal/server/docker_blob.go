package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/blob"
	"arca/internal/docker"
	"arca/internal/store"
	"arca/internal/store/models"
)

// staleUploadWindow is how long an upload session survives without a chunk. A
// push of a large layer over a slow link is one long PATCH rather than a pause,
// so a day is generous without letting an abandoned session linger.
const staleUploadWindow = 24 * time.Hour

func (s *Server) serveDockerBlob(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		s.serveDockerBlobRead(writer, request, registry)
	case http.MethodDelete:
		s.serveDockerBlobDelete(writer, registry)
	default:
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
	}
}

func (s *Server) serveDockerBlobRead(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if registry.repository.IsProxy() {
		s.serveDockerProxyBlob(writer, request, registry)
		return
	}

	asset, err := s.store.FindAsset(registry.repository.ID, docker.BlobPath(registry.route.Digest))
	if err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUnknown, "Not found")
		return
	}

	writer.Header().Set(docker.ContentDigestHeader, registry.route.Digest.String())

	// A blob request names the image but not the tag, so traffic is recorded
	// against the image with no version rather than guessed at.
	if request.Method == http.MethodGet {
		s.recordDockerTraffic(request, registry, models.TrafficDownload, asset.Size)
	}
	if !s.writeAsset(writer, request, asset) {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUnknown, "Not found")
	}
}

func (s *Server) serveDockerBlobDelete(writer http.ResponseWriter, registry registryRequest) {
	keys, err := s.store.DeleteAsset(registry.repository.ID, docker.BlobPath(registry.route.Digest))
	if err != nil {
		log.Errorf("deleting a docker blob failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	if len(keys) == 0 {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUnknown, "Not found")
		return
	}
	if err := s.blobs.Delete(keys...); err != nil {
		log.Errorf("removing a deleted docker blob failed: %v", err)
	}

	writer.WriteHeader(http.StatusAccepted)
}

// serveDockerUploadStart opens a push. The same endpoint covers three shapes: a
// mount of a blob this server already holds, a whole blob in the body, and the
// session a chunked push appends to.
func (s *Server) serveDockerUploadStart(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if request.Method != http.MethodPost {
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
		return
	}

	query := request.URL.Query()

	if mount := query.Get("mount"); mount != "" {
		if s.mountDockerBlob(writer, request, registry, mount, query.Get("from")) {
			return
		}
		// A mount that cannot be served falls through to an ordinary session,
		// which is the fallback the spec prescribes.
	}

	if digest := query.Get("digest"); digest != "" {
		s.completeDockerMonolith(writer, request, registry, digest)
		return
	}

	id := store.NewID()
	if err := s.blobs.BeginUpload(id); err != nil {
		log.Errorf("opening a docker upload session failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	var userID *string
	if user := currentUser(request); user != nil {
		userID = &user.ID
	}
	upload := &models.DockerUpload{
		ID:           id,
		RepositoryID: registry.repository.ID,
		Image:        registry.image,
		UserID:       userID,
	}
	if err := s.store.CreateDockerUpload(upload); err != nil {
		_ = s.blobs.AbortUpload(id)
		log.Errorf("recording a docker upload session failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	writeDockerUploadProgress(writer, registry, id, 0, http.StatusAccepted)
}

// mountDockerBlob reports whether it answered. Blob keys are scoped per
// repository, so the bytes are copied rather than linked, which still saves the
// client the upload and keeps per-repository storage accounting truthful.
func (s *Server) mountDockerBlob(writer http.ResponseWriter, request *http.Request, registry registryRequest, mount, from string) bool {
	digest, ok := docker.ParseDigest(mount)
	if !ok {
		return false
	}

	source := registry.repository
	if from != "" && from != registry.route.Name {
		found, err := s.dockerMountSource(request, from)
		if err != nil {
			return false
		}
		source = found
	}

	existing, err := s.store.FindAsset(source.ID, docker.BlobPath(digest))
	if err != nil {
		return false
	}

	file, _, err := s.blobs.Open(existing.StorageKey)
	if err != nil {
		return false
	}
	defer file.Close()

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	stored, err := s.storeUpload(registry.repository, docker.BlobPath(digest), file,
		uploadDetails{UploadedBy: uploadedBy, ContentType: existing.ContentType, Limit: s.dockerBlobLimit()})
	if err != nil {
		log.Errorf("mounting a docker blob failed: %v", err)
		return false
	}
	if stored.SHA256 != digest.Hex {
		log.Warnf("a mounted blob of %s hashed to %s rather than %s", source.Name, stored.SHA256, digest.Hex)
		if _, err := s.store.DeleteAsset(registry.repository.ID, stored.Path); err != nil {
			log.Errorf("dropping a mismatched mounted blob failed: %v", err)
		}
		return false
	}

	writeDockerBlobCreated(writer, registry, digest)
	return true
}

// dockerMountSource resolves the repository a cross-repository mount reads from,
// which the caller must be able to read in its own right.
func (s *Server) dockerMountSource(request *http.Request, from string) (*models.Repository, error) {
	repository, _, ok := s.resolveDockerRepository(from)
	if !ok {
		return nil, errors.New("server: the mount source names no repository")
	}

	permission, err := s.store.EffectivePermission(repository, currentUser(request))
	if err != nil {
		return nil, err
	}
	if !store.Satisfies(permission, models.PermissionRead) {
		return nil, errors.New("server: the mount source is not readable by this user")
	}
	return repository, nil
}

// completeDockerMonolith stores a blob whose whole body arrived with the POST or
// PUT that named its digest.
func (s *Server) completeDockerMonolith(writer http.ResponseWriter, request *http.Request, registry registryRequest, raw string) {
	digest, ok := docker.ParseDigest(raw)
	if !ok {
		dockerError(writer, http.StatusBadRequest, docker.ErrorDigestInvalid, raw+" is not a digest this server stores")
		return
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	path := docker.BlobPath(digest)
	stored, err := s.storeUpload(registry.repository, path, request.Body,
		uploadDetails{UploadedBy: uploadedBy, ContentType: blobContentType, Limit: s.dockerBlobLimit()})
	if errors.Is(err, blob.ErrTooLarge) {
		dockerError(writer, http.StatusRequestEntityTooLarge, docker.ErrorSizeInvalid, "The blob exceeds the upload limit")
		return
	}
	if err != nil {
		log.Errorf("storing a docker blob failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	if stored.SHA256 != digest.Hex {
		if _, deleteErr := s.store.DeleteAsset(registry.repository.ID, path); deleteErr != nil {
			log.Errorf("dropping a mismatched docker blob failed: %v", deleteErr)
		}
		_ = s.blobs.Delete(stored.StorageKey)
		dockerError(writer, http.StatusBadRequest, docker.ErrorDigestInvalid,
			"The blob hashed to sha256:"+stored.SHA256+" rather than "+digest.String())
		return
	}

	s.recordDockerTraffic(request, registry, models.TrafficUpload, stored.Size)
	writeDockerBlobCreated(writer, registry, digest)
}

func (s *Server) serveDockerUploadSession(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	id := registry.route.Upload

	if _, err := s.store.DockerUpload(registry.repository.ID, id); err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUploadUnknown, "Not found")
		return
	}

	switch request.Method {
	case http.MethodGet, http.MethodHead:
		s.serveDockerUploadStatus(writer, registry, id)
	case http.MethodPatch:
		s.serveDockerUploadChunk(writer, request, registry, id)
	case http.MethodPut:
		s.serveDockerUploadFinish(writer, request, registry, id)
	case http.MethodDelete:
		s.serveDockerUploadAbort(writer, registry, id)
	default:
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
	}
}

func (s *Server) serveDockerUploadStatus(writer http.ResponseWriter, registry registryRequest, id string) {
	size, err := s.blobs.UploadSize(id)
	if err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUploadUnknown, "Not found")
		return
	}
	writeDockerUploadProgress(writer, registry, id, size, http.StatusNoContent)
}

func (s *Server) serveDockerUploadChunk(writer http.ResponseWriter, request *http.Request, registry registryRequest, id string) {
	current, err := s.blobs.UploadSize(id)
	if err != nil {
		dockerError(writer, http.StatusNotFound, docker.ErrorBlobUploadUnknown, "Not found")
		return
	}

	// A chunk that does not start where the session ended would silently corrupt
	// the blob, so it is refused with the offset the client should resume from.
	if start, ok := parseContentRangeStart(request.Header.Get("Content-Range")); ok && start != current {
		writer.Header().Set("Range", "0-"+strconv.FormatInt(current-1, 10))
		dockerError(writer, http.StatusRequestedRangeNotSatisfiable, docker.ErrorBlobUploadInvalid,
			"The chunk does not continue from the end of the session")
		return
	}

	size, err := s.blobs.AppendUpload(id, request.Body, s.config.MaxBlobBytes)
	if errors.Is(err, blob.ErrTooLarge) {
		dockerError(writer, http.StatusRequestEntityTooLarge, docker.ErrorSizeInvalid, "The blob exceeds the upload limit")
		return
	}
	if err != nil {
		log.Errorf("appending to a docker upload session failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	if err := s.store.SetDockerUploadSize(id, size); err != nil {
		log.Errorf("recording docker upload progress failed: %v", err)
	}

	writeDockerUploadProgress(writer, registry, id, size, http.StatusAccepted)
}

func (s *Server) serveDockerUploadFinish(writer http.ResponseWriter, request *http.Request, registry registryRequest, id string) {
	digest, ok := docker.ParseDigest(request.URL.Query().Get("digest"))
	if !ok {
		dockerError(writer, http.StatusBadRequest, docker.ErrorDigestInvalid, "The upload was committed without a valid digest")
		return
	}

	// The final PUT may carry the last chunk, and for a small blob it carries the
	// whole of it.
	if _, err := s.blobs.AppendUpload(id, request.Body, s.config.MaxBlobBytes); errors.Is(err, blob.ErrTooLarge) {
		dockerError(writer, http.StatusRequestEntityTooLarge, docker.ErrorSizeInvalid, "The blob exceeds the upload limit")
		return
	} else if err != nil {
		log.Errorf("appending the final docker chunk failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	path := docker.BlobPath(digest)
	key := registry.repository.ID + "/" + path

	size, digests, err := s.blobs.CompleteUpload(id, key, digest.Hex)
	if errors.Is(err, blob.ErrDigestMismatch) {
		dockerError(writer, http.StatusBadRequest, docker.ErrorDigestInvalid,
			"The upload hashed to sha256:"+digests.SHA256+" rather than "+digest.String())
		return
	}
	if err != nil {
		log.Errorf("committing a docker upload failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	// The bytes are already in place, so the asset row is written directly rather
	// than through storeUpload, which would want to copy them a second time.
	asset := &models.Asset{
		RepositoryID: registry.repository.ID,
		Path:         path,
		StorageKey:   key,
		Size:         size,
		ContentType:  blobContentType,
		MD5:          digests.MD5,
		SHA1:         digests.SHA1,
		SHA256:       digests.SHA256,
		SHA512:       digests.SHA512,
		UploadedBy:   uploadedBy,
	}
	if err := s.store.UpsertAsset(asset); err != nil {
		log.Errorf("recording a docker blob failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	if err := s.store.DeleteDockerUpload(id); err != nil {
		log.Errorf("closing a docker upload session failed: %v", err)
	}

	s.recordDockerTraffic(request, registry, models.TrafficUpload, size)
	writeDockerBlobCreated(writer, registry, digest)
}

func (s *Server) serveDockerUploadAbort(writer http.ResponseWriter, registry registryRequest, id string) {
	if err := s.blobs.AbortUpload(id); err != nil {
		log.Errorf("discarding a docker upload session failed: %v", err)
	}
	if err := s.store.DeleteDockerUpload(id); err != nil {
		log.Errorf("closing a docker upload session failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func writeDockerUploadProgress(writer http.ResponseWriter, registry registryRequest, id string, size int64, status int) {
	header := writer.Header()
	header.Set("Location", registry.uploadLocation(id))
	header.Set("Docker-Upload-UUID", id)
	// An empty session reports 0-0 rather than 0--1, which is what the spec's
	// inclusive range would otherwise produce.
	if size == 0 {
		header.Set("Range", "0-0")
	} else {
		header.Set("Range", "0-"+strconv.FormatInt(size-1, 10))
	}
	header.Set("Content-Length", "0")
	writer.WriteHeader(status)
}

func writeDockerBlobCreated(writer http.ResponseWriter, registry registryRequest, digest docker.Digest) {
	header := writer.Header()
	header.Set("Location", "/v2/"+registry.route.Name+"/blobs/"+digest.String())
	header.Set(docker.ContentDigestHeader, digest.String())
	header.Set("Content-Length", "0")
	writer.WriteHeader(http.StatusCreated)
}

// parseContentRangeStart reads the offset a chunk claims to begin at. The registry
// API uses a bare "start-end" here rather than the "bytes start-end/total" form
// that HTTP defines, so this is deliberately narrow.
func parseContentRangeStart(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}

	start, _, found := strings.Cut(trimmed, "-")
	if !found {
		return 0, false
	}

	offset, err := strconv.ParseInt(strings.TrimSpace(start), 10, 64)
	if err != nil || offset < 0 {
		return 0, false
	}
	return offset, true
}

// recordDockerTraffic attributes a blob transfer to the image. A blob request
// names no tag, so the version is left empty rather than guessed at.
func (s *Server) recordDockerTraffic(request *http.Request, registry registryRequest, kind string, bytes int64) {
	namespace, name, ok := registry.namespace()
	if !ok {
		return
	}

	var userID *string
	if user := currentUser(request); user != nil {
		userID = &user.ID
	}

	s.traffic.record(models.TrafficEvent{
		Kind:         kind,
		RepositoryID: registry.repository.ID,
		Namespace:    namespace,
		Name:         name,
		Bytes:        bytes,
		UserID:       userID,
	})
}

// purgeStaleDockerUploads drops sessions a client walked away from. The file goes
// first: a row without a file reports a length nothing can satisfy, while a file
// without a row is unreachable and would never be swept again.
func (s *Server) purgeStaleDockerUploads(now time.Time) {
	ids, err := s.store.StaleDockerUploads(now.Add(-staleUploadWindow).UnixMilli())
	if err != nil {
		log.Errorf("listing stale docker uploads failed: %v", err)
		return
	}
	if len(ids) == 0 {
		return
	}

	for _, id := range ids {
		if err := s.blobs.AbortUpload(id); err != nil {
			log.Errorf("discarding the stale docker upload %s failed: %v", id, err)
		}
	}
	if err := s.store.DeleteDockerUploads(ids); err != nil {
		log.Errorf("removing stale docker upload records failed: %v", err)
		return
	}

	log.Infof("discarded %d abandoned docker upload sessions", len(ids))
}
