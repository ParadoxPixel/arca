package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/auth"
	"arca/internal/blob"
	"arca/internal/npm"
	"arca/internal/store"
	"arca/internal/store/models"
)

func npmError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func (s *Server) serveNPM(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	route := npm.Resolve(request.Method, path)

	if route.Kind == npm.RouteLogin {
		s.serveNPMLogin(writer, request, route)
		return
	}

	required := models.PermissionRead
	switch route.Kind {
	case npm.RoutePublish, npm.RouteSetTag, npm.RouteDeleteTag, npm.RouteUnpublish:
		required = models.PermissionWrite
	}
	if !s.authorizeNPM(writer, request, repository, required) {
		return
	}
	if repository.IsProxy() && required != models.PermissionRead {
		npmError(writer, http.StatusMethodNotAllowed, "Proxy repositories are read-only")
		return
	}

	switch route.Kind {
	case npm.RouteIndex:
		s.writeDirectoryIndex(writer, repository, "")
	case npm.RoutePing:
		writeJSON(writer, http.StatusOK, map[string]any{})
	case npm.RouteWhoami:
		s.serveNPMWhoami(writer, request)
	case npm.RoutePackument:
		s.serveNPMPackument(writer, request, repository, route)
	case npm.RouteVersion:
		s.serveNPMVersion(writer, request, repository, route)
	case npm.RouteTarball:
		s.serveNPMTarball(writer, request, repository, route)
	case npm.RoutePublish:
		s.serveNPMPublish(writer, request, repository)
	case npm.RouteDistTags:
		s.serveNPMDistTags(writer, request, repository, route)
	case npm.RouteSetTag:
		s.serveNPMSetTag(writer, request, repository, route)
	case npm.RouteDeleteTag:
		s.serveNPMDeleteTag(writer, repository, route)
	case npm.RouteUnpublish:
		s.serveNPMUnpublish(writer, repository, route)
	case npm.RouteSearch:
		s.serveNPMSearch(writer, request, repository)
	case npm.RouteTokens:
		s.serveNPMTokens(writer, request)
	case npm.RouteMakeToken:
		s.serveNPMMakeToken(writer, request)
	case npm.RouteDropToken:
		s.serveNPMDropToken(writer, request, route)
	default:
		npmError(writer, http.StatusNotFound, "Not found")
	}
}

func (s *Server) authorizeNPM(writer http.ResponseWriter, request *http.Request, repository *models.Repository, required string) bool {
	user := currentUser(request)

	permission, err := s.store.EffectivePermission(repository, user)
	if err != nil {
		log.Errorf("resolving repository permission failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return false
	}
	if store.Satisfies(permission, required) {
		return true
	}

	if user == nil {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="`+s.store.InstanceName()+`"`)
		npmError(writer, http.StatusUnauthorized, "You must be logged in to do that")
	} else {
		npmError(writer, http.StatusForbidden, "You do not have permission to do that")
	}
	return false
}

func (s *Server) serveNPMWhoami(writer http.ResponseWriter, request *http.Request) {
	user := currentUser(request)
	if user == nil {
		npmError(writer, http.StatusUnauthorized, "You must be logged in to do that")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"username": user.Email})
}

// serveNPMLogin backs "npm login --auth-type=legacy". The modern web flow posts
// to /-/v1/login, which stays unrouted so npm falls back to this.
func (s *Server) serveNPMLogin(writer http.ResponseWriter, request *http.Request, route npm.Route) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 8<<10)).Decode(&body); err != nil {
		npmError(writer, http.StatusBadRequest, "Invalid login request")
		return
	}

	login := body.Name
	if login == "" {
		login = route.Username
	}

	user, err := s.store.UserByEmail(login)
	if err != nil || user.Disabled || !auth.VerifyPassword(body.Password, user.PasswordHash) {
		log.Warnf("rejected an npm login for %q", login)
		npmError(writer, http.StatusUnauthorized, "Incorrect username or password")
		return
	}

	secret, err := auth.GenerateAPIToken()
	if err != nil {
		log.Errorf("generating an npm login token failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	token := &models.APIToken{
		UserID:    user.ID,
		Name:      "npm login (" + time.Now().UTC().Format(time.DateOnly) + ")",
		TokenHash: auth.SHA256Hex(secret),
		Preview:   secret[:12],
	}
	if err := s.store.CreateToken(token); err != nil {
		log.Errorf("creating an npm login token failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeJSON(writer, http.StatusCreated, map[string]any{
		"ok":    true,
		"id":    "org.couchdb.user:" + user.Email,
		"token": secret,
	})
}

func (s *Server) loadPackument(request *http.Request, repository *models.Repository, route npm.Route) (npm.Packument, error) {
	components, err := s.store.ComponentVersions(repository.ID, route.Namespace, route.Name)
	if err != nil {
		return npm.Packument{}, err
	}
	if len(components) == 0 {
		return npm.Packument{}, errNoSuchPackage
	}

	tags, err := s.store.DistTags(repository.ID, route.Namespace, route.Name)
	if err != nil {
		return npm.Packument{}, err
	}

	base := publicBase(request) + "/repository/" + repository.Name + "/"
	packument := npm.Packument{
		Namespace: route.Namespace,
		Name:      route.Name,
		DistTags:  tags,
	}

	versions := make([]string, 0, len(components))
	for _, component := range components {
		versions = append(versions, component.Version)

		release := npm.Release{Version: component.Version, Published: component.CreatedAt}
		release.Manifest, err = s.readManifest(repository, route, component.Version)
		if err != nil {
			return npm.Packument{}, err
		}

		tarball, err := s.store.FindAsset(repository.ID, npm.TarballPath(route.Namespace, route.Name, component.Version))
		if err != nil {
			continue
		}
		release.Dist = npm.Dist{
			Tarball:   base + npm.PackageName(route.Namespace, route.Name) + "/-/" + npm.TarballFilename(route.Name, component.Version),
			Integrity: npm.Integrity(tarball.SHA512),
			Shasum:    tarball.SHA1,
		}

		packument.Releases = append(packument.Releases, release)
		if packument.Created == 0 || component.CreatedAt < packument.Created {
			packument.Created = component.CreatedAt
		}
		packument.Modified = max(packument.Modified, component.UpdatedAt)
	}

	if packument.DistTags == nil {
		packument.DistTags = map[string]string{}
	}
	if packument.DistTags["latest"] == "" {
		if latest := npm.LatestVersion(versions); latest != "" {
			packument.DistTags["latest"] = latest
		}
	}

	return packument, nil
}

var errNoSuchPackage = errors.New("server: the repository has no such package")

func (s *Server) readManifest(repository *models.Repository, route npm.Route, version string) (map[string]json.RawMessage, error) {
	asset, err := s.store.FindAsset(repository.ID, npm.ManifestPath(route.Namespace, route.Name, version))
	if err != nil {
		return map[string]json.RawMessage{}, nil
	}

	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return map[string]json.RawMessage{}, nil
	}
	defer file.Close()

	manifest := map[string]json.RawMessage{}
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		log.Warnf("the stored manifest for %s@%s is unreadable: %v", route.Package(), version, err)
		return map[string]json.RawMessage{}, nil
	}
	return manifest, nil
}

func (s *Server) serveNPMPackument(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	if repository.IsProxy() {
		s.serveNPMProxyPackument(writer, request, repository, route)
		return
	}

	packument, err := s.loadPackument(request, repository, route)
	if errors.Is(err, errNoSuchPackage) {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		log.Errorf("building a packument failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	document, err := npm.Render(packument)
	if err != nil {
		log.Errorf("rendering a packument failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Write(document)
}

func (s *Server) serveNPMVersion(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	if repository.IsProxy() {
		s.serveNPMProxyVersion(writer, request, repository, route)
		return
	}

	packument, err := s.loadPackument(request, repository, route)
	if errors.Is(err, errNoSuchPackage) {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		log.Errorf("building a packument failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	for _, release := range packument.Releases {
		if release.Version != route.Version {
			continue
		}

		document, err := npm.RenderVersion(packument, release)
		if err != nil {
			log.Errorf("rendering a version manifest failed: %v", err)
			npmError(writer, http.StatusInternalServerError, "Internal server error")
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Write(document)
		return
	}

	npmError(writer, http.StatusNotFound, "Not found")
}

func (s *Server) serveNPMTarball(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	if repository.IsProxy() {
		s.serveNPMProxyTarball(writer, request, repository, route)
		return
	}

	path := npm.TarballPath(route.Namespace, route.Name, route.Version)

	asset, err := s.store.FindAsset(repository.ID, path)
	if err != nil {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	if request.Method == http.MethodGet {
		s.recordAssetTraffic(request, models.TrafficDownload, repository, asset)
	}
	if !s.writeAsset(writer, request, asset) {
		npmError(writer, http.StatusNotFound, "Not found")
	}
}

func (s *Server) serveNPMPublish(writer http.ResponseWriter, request *http.Request, repository *models.Repository) {
	limit := s.config.MaxUploadBytes
	update, err := npm.ParseUpdate(io.LimitReader(request.Body, limit*4/3+(64<<10)))
	if err != nil {
		npmError(writer, http.StatusBadRequest, err.Error())
		return
	}

	if !update.CarriesTarball() {
		s.serveNPMDeprecate(writer, request, repository, update)
		return
	}

	published, err := update.Publish()
	if err != nil {
		npmError(writer, http.StatusBadRequest, err.Error())
		return
	}

	if !repository.AcceptsPolicy(npm.IsPrerelease(published.Version)) {
		npmError(writer, http.StatusBadRequest, "Repository policy '"+repository.Policy+"' rejects this version")
		return
	}

	tarballPath := npm.TarballPath(published.Namespace, published.Name, published.Version)

	// A published version is immutable regardless of allowRedeploy: npm clients
	// cache by integrity, so a replaced tarball is never picked up.
	if _, err := s.store.FindAsset(repository.ID, tarballPath); err == nil {
		npmError(writer, http.StatusConflict, "Cannot publish over an existing version")
		return
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	asset, err := s.storeUpload(repository, tarballPath, published.Tarball(), uploadDetails{UploadedBy: uploadedBy})
	if errors.Is(err, blob.ErrTooLarge) {
		npmError(writer, http.StatusRequestEntityTooLarge, "Package exceeds the upload limit")
		return
	}
	if err != nil {
		log.Errorf("storing an npm tarball failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	if err := verifyPublishedDigests(published, asset); err != nil {
		if _, removeErr := s.store.DeleteAsset(repository.ID, tarballPath); removeErr != nil {
			log.Errorf("dropping a rejected tarball failed: %v", removeErr)
		}
		_ = s.blobs.Delete(asset.StorageKey)
		npmError(writer, http.StatusBadRequest, err.Error())
		return
	}

	manifest, err := json.Marshal(published.Manifest)
	if err != nil {
		npmError(writer, http.StatusBadRequest, "The version manifest is not encodable")
		return
	}
	manifestPath := npm.ManifestPath(published.Namespace, published.Name, published.Version)
	if _, err := s.storeUpload(repository, manifestPath, bytes.NewReader(manifest), uploadDetails{UploadedBy: uploadedBy}); err != nil {
		log.Errorf("storing an npm manifest failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	for tag, version := range published.DistTags {
		if version != published.Version {
			continue
		}
		if err := s.store.SetDistTag(repository.ID, published.Namespace, published.Name, tag, version); err != nil {
			log.Errorf("recording a dist-tag failed: %v", err)
		}
	}

	s.recordAssetTraffic(request, models.TrafficUpload, repository, asset)

	writeJSON(writer, http.StatusCreated, map[string]any{
		"ok":  true,
		"id":  npm.PackageName(published.Namespace, published.Name),
		"rev": asset.SHA256,
	})
}

// serveNPMDeprecate handles the attachment-free PUT npm sends for "npm
// deprecate". The flag is written into the stored manifest, which the packument
// is rendered from, so it needs no table of its own.
func (s *Server) serveNPMDeprecate(writer http.ResponseWriter, request *http.Request, repository *models.Repository, update *npm.Update) {
	components, err := s.store.ComponentVersions(repository.ID, update.Namespace, update.Name)
	if err != nil {
		log.Errorf("reading versions to deprecate failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}
	if len(components) == 0 {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	deprecations := update.Deprecations()
	if len(deprecations) == 0 {
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
		return
	}

	var uploadedBy *string
	if user := currentUser(request); user != nil {
		uploadedBy = &user.ID
	}

	route := npm.Route{Namespace: update.Namespace, Name: update.Name}
	changed := 0

	for version, message := range deprecations {
		path := npm.ManifestPath(update.Namespace, update.Name, version)
		if _, err := s.store.FindAsset(repository.ID, path); err != nil {
			continue
		}

		manifest, err := s.readManifest(repository, route, version)
		if err != nil {
			log.Errorf("reading a manifest to deprecate failed: %v", err)
			npmError(writer, http.StatusInternalServerError, "Internal server error")
			return
		}

		if message == "" {
			delete(manifest, "deprecated")
		} else {
			encoded, err := json.Marshal(message)
			if err != nil {
				npmError(writer, http.StatusBadRequest, "The deprecation message is not encodable")
				return
			}
			manifest["deprecated"] = encoded
		}

		encoded, err := json.Marshal(manifest)
		if err != nil {
			npmError(writer, http.StatusBadRequest, "The version manifest is not encodable")
			return
		}
		if _, err := s.storeUpload(repository, path, bytes.NewReader(encoded), uploadDetails{UploadedBy: uploadedBy}); err != nil {
			log.Errorf("storing a deprecated manifest failed: %v", err)
			npmError(writer, http.StatusInternalServerError, "Internal server error")
			return
		}
		changed++
	}

	if changed == 0 {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func verifyPublishedDigests(published *npm.Publish, asset *models.Asset) error {
	if published.Integrity != "" && published.Integrity != npm.Integrity(asset.SHA512) {
		return errors.New("The tarball does not match the integrity it was published with")
	}
	if published.Shasum != "" && published.Shasum != asset.SHA1 {
		return errors.New("The tarball does not match the shasum it was published with")
	}
	return nil
}

func (s *Server) serveNPMDistTags(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	if repository.IsProxy() {
		s.serveNPMProxyDistTags(writer, request, repository, route)
		return
	}

	tags, err := s.store.DistTags(repository.ID, route.Namespace, route.Name)
	if err != nil {
		log.Errorf("reading dist-tags failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	if tags["latest"] == "" {
		components, err := s.store.ComponentVersions(repository.ID, route.Namespace, route.Name)
		if err != nil {
			log.Errorf("reading versions failed: %v", err)
			npmError(writer, http.StatusInternalServerError, "Internal server error")
			return
		}
		versions := make([]string, 0, len(components))
		for _, component := range components {
			versions = append(versions, component.Version)
		}
		if latest := npm.LatestVersion(versions); latest != "" {
			tags["latest"] = latest
		}
	}

	writeJSON(writer, http.StatusOK, tags)
}

func (s *Server) serveNPMSetTag(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	var version string
	if err := json.NewDecoder(io.LimitReader(request.Body, 1<<10)).Decode(&version); err != nil {
		npmError(writer, http.StatusBadRequest, "The request body must be a version string")
		return
	}

	components, err := s.store.ComponentVersions(repository.ID, route.Namespace, route.Name)
	if err != nil {
		log.Errorf("reading versions failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	known := false
	for _, component := range components {
		known = known || component.Version == version
	}
	if !known {
		npmError(writer, http.StatusNotFound, "That version is not published here")
		return
	}

	if err := s.store.SetDistTag(repository.ID, route.Namespace, route.Name, route.Tag, version); err != nil {
		log.Errorf("writing a dist-tag failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}

	writeJSON(writer, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) serveNPMDeleteTag(writer http.ResponseWriter, repository *models.Repository, route npm.Route) {
	if route.Tag == "latest" {
		npmError(writer, http.StatusBadRequest, "The latest tag cannot be removed")
		return
	}

	removed, err := s.store.DeleteDistTag(repository.ID, route.Namespace, route.Name, route.Tag)
	if err != nil {
		log.Errorf("removing a dist-tag failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}
	if removed == 0 {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) serveNPMUnpublish(writer http.ResponseWriter, repository *models.Repository, route npm.Route) {
	prefix := npm.PackageName(route.Namespace, route.Name) + "/"

	keys, err := s.store.DeleteAssetTree(repository.ID, prefix)
	if err != nil {
		log.Errorf("unpublishing failed: %v", err)
		npmError(writer, http.StatusInternalServerError, "Internal server error")
		return
	}
	if len(keys) == 0 {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}

	if err := s.store.DeleteDistTagsFor(repository.ID, route.Namespace, route.Name, nil); err != nil {
		log.Errorf("clearing dist-tags failed: %v", err)
	}
	if err := s.blobs.Delete(keys...); err != nil {
		log.Errorf("removing unpublished files failed: %v", err)
	}

	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}
