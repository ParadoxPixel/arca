package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/docker"
	"arca/internal/format"
	"arca/internal/store"
	"arca/internal/store/models"
)

const (
	maxManifestBytes = 4 << 20
	blobContentType  = "application/octet-stream"
)

// dockerBlobLimit translates the configured ceiling into what storeUpload expects,
// where a negative limit means unlimited and zero would fall back to the much
// smaller artifact limit.
func (s *Server) dockerBlobLimit() int64 {
	if s.config.MaxBlobBytes > 0 {
		return s.config.MaxBlobBytes
	}
	return -1
}

func dockerError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{
		"errors": []map[string]any{{"code": code, "message": message, "detail": nil}},
	})
}

func (s *Server) dockerChallenge(writer http.ResponseWriter, request *http.Request, message string) {
	if currentUser(request) != nil {
		dockerError(writer, http.StatusForbidden, docker.ErrorDenied, message)
		return
	}
	writer.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", s.store.InstanceName()))
	dockerError(writer, http.StatusUnauthorized, docker.ErrorUnauthorized, message)
}

// registryRequest is one resolved registry call. Route.Name is kept alongside the
// image because every Location header has to echo the name the client used, which
// carries the repository prefix that the image name on its own has lost.
type registryRequest struct {
	repository *models.Repository
	image      string
	route      docker.Route
}

func (r registryRequest) namespace() (string, string, bool) { return docker.SplitImage(r.image) }

func (r registryRequest) uploadLocation(id string) string {
	return "/v2/" + r.route.Name + "/blobs/uploads/" + id
}

// handleDockerRegistry serves /v2 at the host root, because a Docker client
// derives the registry from the image reference and has no way to be pointed at a
// path prefix the way Maven and npm clients do.
func (s *Server) handleDockerRegistry(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set(docker.APIVersionHeader, docker.APIVersion)

	route := docker.Resolve(request.Method, strings.TrimPrefix(request.URL.Path, "/v2"))

	switch route.Kind {
	case docker.RouteBase:
		s.serveDockerBase(writer, request)
		return
	case docker.RouteCatalog:
		s.serveDockerCatalog(writer, request)
		return
	case docker.RouteUnknown:
		dockerError(writer, http.StatusNotFound, docker.ErrorNameInvalid, "Not found")
		return
	}

	repository, image, ok := s.resolveDockerRepository(route.Name)
	if !ok {
		dockerError(writer, http.StatusNotFound, docker.ErrorNameUnknown,
			fmt.Sprintf("%q does not name a repository and an image on this server", route.Name))
		return
	}

	required := models.PermissionRead
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		required = models.PermissionWrite
	}
	if !s.authorizeDocker(writer, request, repository, required) {
		return
	}
	if required != models.PermissionRead && (repository.IsProxy() || repository.IsGroup()) {
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "This repository is read-only")
		return
	}

	registry := registryRequest{repository: repository, image: image, route: route}

	switch route.Kind {
	case docker.RouteTags:
		s.serveDockerTags(writer, request, registry)
	case docker.RouteManifest:
		s.serveDockerManifest(writer, request, registry)
	case docker.RouteBlob:
		s.serveDockerBlob(writer, request, registry)
	case docker.RouteUploadStart:
		s.serveDockerUploadStart(writer, request, registry)
	case docker.RouteUpload:
		s.serveDockerUploadSession(writer, request, registry)
	case docker.RouteReferrers:
		// Unimplemented on purpose. A 404 is the spec's own signal for that and
		// it sends clients to the fallback tag scheme.
		dockerError(writer, http.StatusNotFound, docker.ErrorUnsupported, "The referrers API is not implemented")
	default:
		dockerError(writer, http.StatusNotFound, docker.ErrorNameUnknown, "Not found")
	}
}

func (s *Server) authorizeDocker(writer http.ResponseWriter, request *http.Request, repository *models.Repository, required string) bool {
	permission, err := s.store.EffectivePermission(repository, currentUser(request))
	if err != nil {
		log.Errorf("resolving repository permission failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return false
	}
	if store.Satisfies(permission, required) {
		return true
	}

	s.dockerChallenge(writer, request, "You do not have permission to do that")
	return false
}

// resolveDockerRepository splits an image name into the repository serving it and
// the image inside it. The leading segment wins when it names a docker
// repository, so "docker-hosted/team/api" is the team/api image of docker-hosted.
// Otherwise the whole name resolves against the instance default, which is what
// makes a bare "docker pull host/nginx" work on a single-repository install.
//
// A former name resolves in place rather than redirecting the way the Maven and
// npm routes do. A Docker client derives every URL of a pull or a push from the
// reference it was given, so a redirect would only move the one request that
// received it, and a push would lose its body on the way.
func (s *Server) resolveDockerRepository(name string) (*models.Repository, string, bool) {
	if prefix, rest, found := strings.Cut(name, "/"); found && rest != "" {
		repository, _, err := s.store.ResolveRepositoryOfFormat(prefix, format.Docker)
		if err == nil && docker.IsValidName(rest) {
			return repository, rest, true
		}
	}

	fallback, err := s.store.Setting(store.SettingDockerRepository)
	if err != nil || fallback == "" {
		return nil, "", false
	}

	repository, _, err := s.store.ResolveRepositoryOfFormat(fallback, format.Docker)
	if err != nil {
		return nil, "", false
	}
	return repository, name, true
}

// serveDockerBase answers the version check. It is the one endpoint that names no
// repository, so it cannot consult a grant: it succeeds for anyone who
// authenticated, and for anonymous clients only when some docker repository is
// public. Answering 401 with a challenge is also what makes docker login work,
// since that command has nothing else to post to.
func (s *Server) serveDockerBase(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
		return
	}

	if currentUser(request) != nil {
		writeJSON(writer, http.StatusOK, map[string]any{})
		return
	}

	public, err := s.store.HasPublicRepositories(format.Docker)
	if err != nil {
		log.Errorf("checking for public docker repositories failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	if public {
		writeJSON(writer, http.StatusOK, map[string]any{})
		return
	}

	s.dockerChallenge(writer, request, "Authentication required")
}

// serveDockerCatalog lists every image the caller can read, each prefixed with
// the repository that serves it. The spec assumes one registry per host, so the
// prefix is an addition rather than an omission: without it a client could not
// turn a catalog entry back into something it can pull.
func (s *Server) serveDockerCatalog(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
		return
	}

	repositories, err := s.store.VisibleRepositories(currentUser(request))
	if err != nil {
		log.Errorf("listing repositories for the docker catalog failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}

	names := []string{}
	for _, repository := range repositories {
		if repository.Format != format.Docker {
			continue
		}

		images, err := s.store.DockerImages(repository.ID)
		if err != nil {
			log.Errorf("listing images of %s failed: %v", repository.Name, err)
			continue
		}
		for _, image := range images {
			names = append(names, repository.Name+"/"+image)
		}
	}

	writeJSON(writer, http.StatusOK, map[string]any{"repositories": names})
}

func (s *Server) serveDockerTags(writer http.ResponseWriter, request *http.Request, registry registryRequest) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		dockerError(writer, http.StatusMethodNotAllowed, docker.ErrorUnsupported, "Method not allowed")
		return
	}

	namespace, name, ok := registry.namespace()
	if !ok {
		dockerError(writer, http.StatusNotFound, docker.ErrorNameInvalid, "Not found")
		return
	}

	components, err := s.store.ComponentVersions(registry.repository.ID, namespace, name)
	if err != nil {
		log.Errorf("listing tags failed: %v", err)
		dockerError(writer, http.StatusInternalServerError, docker.ErrorUnsupported, "Internal server error")
		return
	}
	if len(components) == 0 {
		dockerError(writer, http.StatusNotFound, docker.ErrorNameUnknown, "Not found")
		return
	}

	tags := make([]string, 0, len(components))
	for _, component := range components {
		tags = append(tags, component.Version)
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"name": registry.route.Name,
		// Sorting through the format's own comparator is what makes the spec's
		// "last" parameter stable across requests.
		"tags": paginateTags(format.SortVersions(docker.Layout{}, tags), request),
	})
}

// serveDockerBrowse answers /repository/<name>/. A docker repository is pulled
// from at /v2 instead, so this exists only so that following the URL the rest of
// the UI shows lands on the stored tree rather than on a Maven handler.
func (s *Server) serveDockerBrowse(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		plain(writer, http.StatusMethodNotAllowed, "Push to /v2/"+repository.Name+"/<image> instead")
		return
	}
	if !s.authorize(writer, request, repository, models.PermissionRead) {
		return
	}

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

	asset, err := s.store.FindAsset(repository.ID, path)
	if err != nil {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}
	if request.Method == http.MethodGet {
		s.recordAssetTraffic(request, models.TrafficDownload, repository, asset)
	}
	if !s.writeAsset(writer, request, asset) {
		plain(writer, http.StatusNotFound, "Not Found")
	}
}

func paginateTags(tags []string, request *http.Request) []string {
	query := request.URL.Query()

	if last := query.Get("last"); last != "" {
		for index, tag := range tags {
			if tag == last {
				tags = tags[index+1:]
				break
			}
		}
	}

	if limit, err := strconv.Atoi(query.Get("n")); err == nil && limit >= 0 && len(tags) > limit {
		tags = tags[:limit]
	}
	return tags
}
