package server

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/blob"
	"arca/internal/format"
	"arca/internal/maven"
	"arca/internal/store/models"
)

func (s *Server) serveMaven(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	required := models.PermissionWrite
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		required = models.PermissionRead
	}
	if !s.authorize(writer, request, repository, required) {
		return
	}
	if repository.IsProxy() && required != models.PermissionRead {
		plain(writer, http.StatusMethodNotAllowed, "Proxy repositories are read-only")
		return
	}

	switch request.Method {
	case http.MethodGet, http.MethodHead:
		s.serveMavenRead(writer, request, repository, path)
	case http.MethodPut, http.MethodPost:
		s.serveMavenUpload(writer, request, repository, path)
	case http.MethodDelete:
		s.serveMavenDelete(writer, repository, path)
	default:
		plain(writer, http.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

func (s *Server) serveMavenRead(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	if path == "" || strings.HasSuffix(path, "/") {
		exists, err := s.store.DirectoryExists(repository.ID, path)
		if err != nil {
			log.Errorf("directory lookup failed: %v", err)
			plain(writer, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		if !exists {
			plain(writer, http.StatusNotFound, "Not Found")
			return
		}
		s.writeDirectoryIndex(writer, repository, path)
		return
	}

	if repository.IsProxy() {
		s.serveProxyRead(writer, request, repository, path)
		return
	}

	if asset, err := s.store.FindAsset(repository.ID, path); err == nil {
		if request.Method == http.MethodGet {
			s.recordAssetTraffic(request, models.TrafficDownload, repository, asset)
		}
		if !s.writeAsset(writer, request, asset) {
			plain(writer, http.StatusNotFound, "Not Found")
		}
		return
	}

	if served := s.writeGeneratedMetadata(writer, repository, path); served {
		return
	}

	base, checksum := maven.SplitChecksum(path)
	if checksum != "" {
		if parent, err := s.store.FindAsset(repository.ID, base); err == nil {
			writer.Header().Set("Content-Type", "text/plain")
			writer.Header().Set("Cache-Control", "no-cache")
			fmt.Fprint(writer, parent.Checksum(checksum))
			return
		}
	}

	if exists, err := s.store.DirectoryExists(repository.ID, path+"/"); err == nil && exists {
		http.Redirect(writer, request, request.URL.Path+"/", http.StatusMovedPermanently)
		return
	}

	plain(writer, http.StatusNotFound, "Not Found")
}

func digesterFor(kind string) hash.Hash {
	switch kind {
	case "md5":
		return md5.New()
	case "sha1":
		return sha1.New()
	case "sha256":
		return sha256.New()
	case "sha512":
		return sha512.New()
	default:
		return nil
	}
}

func (s *Server) writeGeneratedMetadata(writer http.ResponseWriter, repository *models.Repository, path string) bool {
	segments := strings.Split(path, "/")
	base, checksum := maven.SplitChecksum(segments[len(segments)-1])
	if base != maven.MetadataFilename {
		return false
	}

	metadataPath := path
	if checksum != "" {
		metadataPath = path[:len(path)-len(checksum)-1]
	}

	document, err := s.renderMetadata(repository, maven.ParsePath(metadataPath))
	if err != nil {
		log.Errorf("rendering metadata failed: %v", err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return true
	}
	if document == "" {
		plain(writer, http.StatusNotFound, "Not Found")
		return true
	}

	if checksum != "" {
		digester := digesterFor(checksum)
		digester.Write([]byte(document))
		writer.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(writer, hex.EncodeToString(digester.Sum(nil)))
		return true
	}

	writer.Header().Set("Content-Type", "application/xml")
	writer.Header().Set("Cache-Control", "no-cache")
	fmt.Fprint(writer, document)
	return true
}

func (s *Server) renderMetadata(repository *models.Repository, parsed maven.ParsedPath) (string, error) {
	switch parsed.Kind {
	case maven.KindArtifactMetadata:
		return s.renderArtifactMetadata(repository, parsed.GroupID, parsed.ArtifactID)
	case maven.KindVersionMetadata:
		return s.renderVersionMetadata(repository, parsed.GroupID, parsed.ArtifactID, parsed.Version)
	default:
		return "", nil
	}
}

func (s *Server) renderArtifactMetadata(repository *models.Repository, groupID, artifactID string) (string, error) {
	components, err := s.store.ComponentVersions(repository.ID, groupID, artifactID)
	if err != nil {
		return "", err
	}
	if len(components) == 0 {
		return "", nil
	}

	versions := make([]string, 0, len(components))
	var lastUpdated int64
	for _, component := range components {
		versions = append(versions, component.Version)
		lastUpdated = max(lastUpdated, component.UpdatedAt)
	}

	return maven.RenderArtifactMetadata(maven.ArtifactMetadata{
		GroupID:     groupID,
		ArtifactID:  artifactID,
		Versions:    versions,
		LastUpdated: lastUpdated,
	}), nil
}

func (s *Server) renderVersionMetadata(repository *models.Repository, groupID, artifactID, version string) (string, error) {
	prefix := maven.ComponentDirectory(groupID, artifactID, version) + "/"

	files, err := s.store.FilesInDirectory(repository.ID, prefix)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	latest := map[string]maven.SnapshotEntry{}
	var lastUpdated int64

	for _, file := range files {
		lastUpdated = max(lastUpdated, file.UpdatedAt)

		identity, ok := maven.ParseFilename(artifactID, version, file.Path[len(prefix):])
		if !ok {
			continue
		}

		key := identity.Classifier + ":" + identity.Extension
		candidate := maven.SnapshotEntry{
			Classifier:  identity.Classifier,
			Extension:   identity.Extension,
			Value:       identity.VersionPart,
			Updated:     file.UpdatedAt,
			Timestamp:   identity.Timestamp,
			BuildNumber: identity.BuildNumber,
		}
		if existing, seen := latest[key]; !seen || candidate.BuildNumber >= existing.BuildNumber {
			latest[key] = candidate
		}
	}

	entries := make([]maven.SnapshotEntry, 0, len(latest))
	for _, entry := range latest {
		entries = append(entries, entry)
	}

	return maven.RenderVersionMetadata(maven.VersionMetadata{
		GroupID:     groupID,
		ArtifactID:  artifactID,
		Version:     version,
		Entries:     entries,
		LastUpdated: lastUpdated,
	}), nil
}

func (s *Server) serveMavenUpload(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	if !format.IsSafe(path) {
		plain(writer, http.StatusBadRequest, "Invalid path")
		return
	}

	segments := strings.Split(path, "/")
	filename := segments[len(segments)-1]
	base, checksum := maven.SplitChecksum(filename)

	if base == maven.MetadataFilename {
		_, _ = io.Copy(io.Discard, request.Body)
		plain(writer, http.StatusCreated, "Metadata accepted")
		return
	}

	if checksum != "" {
		s.verifyUploadedChecksum(writer, request, repository, path, checksum)
		return
	}

	if request.ContentLength > s.config.MaxUploadBytes {
		plain(writer, http.StatusRequestEntityTooLarge, "Artifact exceeds upload limit")
		return
	}

	parsed := maven.ParsePath(path)
	if parsed.Kind == maven.KindArtifact && !repository.AcceptsPolicy(parsed.IsSnapshot) {
		plain(writer, http.StatusBadRequest, fmt.Sprintf("Repository policy '%s' rejects this version", repository.Policy))
		return
	}

	_, lookupErr := s.store.FindAsset(repository.ID, path)
	replacing := lookupErr == nil
	if replacing && !repository.AllowRedeploy && parsed.Kind == maven.KindArtifact && !parsed.IsSnapshot {
		plain(writer, http.StatusConflict, "Repository does not allow redeploying releases")
		return
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	asset, err := s.storeUpload(repository, path, request.Body, uploadDetails{UploadedBy: uploadedBy})
	if errors.Is(err, blob.ErrTooLarge) {
		plain(writer, http.StatusRequestEntityTooLarge, "Artifact exceeds upload limit")
		return
	}
	if err != nil {
		log.Errorf("storing an upload failed: %v", err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	s.recordAssetTraffic(request, models.TrafficUpload, repository, asset)

	status := http.StatusCreated
	if replacing {
		status = http.StatusOK
	}

	writer.Header().Set("X-Checksum-SHA1", asset.SHA1)
	writer.Header().Set("X-Checksum-MD5", asset.MD5)
	writer.WriteHeader(status)
}

func (s *Server) verifyUploadedChecksum(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path, checksum string) {
	parentPath := path[:len(path)-len(checksum)-1]

	body, err := io.ReadAll(io.LimitReader(request.Body, 4096))
	if err != nil {
		plain(writer, http.StatusBadRequest, "Invalid checksum body")
		return
	}

	submitted := strings.ToLower(strings.TrimSpace(string(body)))
	if index := strings.IndexAny(submitted, " \t\r\n"); index >= 0 {
		submitted = submitted[:index]
	}

	parent, err := s.store.FindAsset(repository.ID, parentPath)
	if err == nil && submitted != "" && submitted != parent.Checksum(checksum) {
		plain(writer, http.StatusBadRequest, "Checksum mismatch for "+checksum)
		return
	}

	plain(writer, http.StatusCreated, "Checksum accepted")
}

func (s *Server) serveMavenDelete(writer http.ResponseWriter, repository *models.Repository, path string) {
	var keys []string
	var err error

	if path == "" || strings.HasSuffix(path, "/") {
		keys, err = s.store.DeleteAssetTree(repository.ID, path)
	} else {
		keys, err = s.store.DeleteAsset(repository.ID, path)
	}
	if err != nil {
		log.Errorf("deleting assets failed: %v", err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if len(keys) == 0 {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}

	if err := s.blobs.Delete(keys...); err != nil {
		log.Errorf("removing stored objects failed: %v", err)
	}

	writer.WriteHeader(http.StatusNoContent)
}
