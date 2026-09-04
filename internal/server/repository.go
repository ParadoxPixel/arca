package server

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/format"
	"arca/internal/store"
	"arca/internal/store/models"
)

func plain(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	fmt.Fprintln(writer, message)
}

func (s *Server) challenge(writer http.ResponseWriter) {
	realm := fmt.Sprintf("Basic realm=%q, charset=\"UTF-8\"", s.store.InstanceName())
	writer.Header().Set("WWW-Authenticate", realm)
	plain(writer, http.StatusUnauthorized, "Unauthorized")
}

func (s *Server) handleRepository(writer http.ResponseWriter, request *http.Request) {
	name := routeVar(request, "repository")

	repository, aliased, err := s.store.ResolveRepository(name)
	if err != nil {
		s.refuseRepository(writer, request, "")
		return
	}
	// A name left behind by a rename redirects rather than serving in place, so
	// a build that follows it learns the new URL and a build that does not
	// follow redirects fails loudly instead of resolving from a stale name
	// forever. The redirect names the repository's new name, so it is only
	// given to a caller who may read it.
	if aliased {
		if !s.authorize(writer, request, repository, models.PermissionRead) {
			return
		}
		redirectRenamed(writer, request, name, repository.Name)
		return
	}

	path := strings.TrimPrefix(strings.TrimPrefix(request.URL.Path, "/repository/"+name), "/")

	switch repository.Format {
	case format.NPM:
		s.serveNPM(writer, request, repository, path)
	case format.P2:
		s.serveP2(writer, request, repository, path)
	case format.Docker:
		s.serveDockerBrowse(writer, request, repository, path)
	default:
		s.serveMaven(writer, request, repository, path)
	}
}

// redirectRenamed points a request at the same path under the repository's
// current name. A read is answered with a 301 because the move is permanent; a
// write gets a 308, which is the one redirect that keeps the method and the body
// rather than turning an upload into a GET.
func redirectRenamed(writer http.ResponseWriter, request *http.Request, from, to string) {
	target := "/repository/" + to + strings.TrimPrefix(request.URL.EscapedPath(), "/repository/"+from)
	if query := request.URL.RawQuery; query != "" {
		target += "?" + query
	}

	status := http.StatusMovedPermanently
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		status = http.StatusPermanentRedirect
	}

	writer.Header().Set("X-Arca-Renamed-From", from)
	http.Redirect(writer, request, target, status)
}

func (s *Server) authorize(writer http.ResponseWriter, request *http.Request, repository *models.Repository, required string) bool {
	user := currentUser(request)

	permission, err := s.store.EffectivePermission(repository, user)
	if err != nil {
		log.Errorf("resolving repository permission failed: %v", err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return false
	}
	if store.Satisfies(permission, required) {
		return true
	}

	s.refuseRepository(writer, request, permission)
	return false
}

// refuseRepository answers a request that may not do what it asked, without
// confirming to anyone who cannot read the repository that it exists. An
// anonymous client is challenged, which is what lets a build tool send its
// credentials at all, and is the answer whether the repository is private or
// was never there. A signed-in user who cannot read it is told it does not
// exist, again the same as a name that never did. Only a user who can read it
// but lacks the permission the request needs is told so in as many words.
func (s *Server) refuseRepository(writer http.ResponseWriter, request *http.Request, permission string) {
	switch {
	case currentUser(request) == nil:
		s.challenge(writer)
	case permission == "":
		plain(writer, http.StatusNotFound, "Repository not found")
	default:
		plain(writer, http.StatusForbidden, "Forbidden")
	}
}

func (s *Server) writeAsset(writer http.ResponseWriter, request *http.Request, asset *models.Asset) bool {
	file, info, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		return false
	}
	defer file.Close()

	header := writer.Header()
	header.Set("Content-Type", asset.ContentType)
	// A stored file is a download, never a page. Where the caller has already
	// named the file, as a deployment link does, that disposition stands.
	if header.Get("Content-Disposition") == "" {
		header.Set("Content-Disposition", "attachment")
	}
	header.Set("X-Checksum-MD5", asset.MD5)
	header.Set("X-Checksum-SHA1", asset.SHA1)
	header.Set("X-Checksum-SHA256", asset.SHA256)
	header.Set("ETag", strconv.Quote(asset.SHA256))

	http.ServeContent(writer, request, asset.Path, info.ModTime(), file)
	return true
}

type uploadDetails struct {
	UploadedBy *string
	// ContentType wins over what the layout derives from the filename. A docker
	// manifest is the case that needs it: the same filename holds an OCI
	// manifest, a Docker one or an index, and only the push says which.
	ContentType string
	// Limit overrides the configured upload ceiling: zero takes MaxUploadBytes and
	// a negative value lifts the limit entirely, which is what a container layer
	// needs since it is routinely larger than any sane ceiling for an artifact.
	Limit              int64
	FetchedAt          int64
	ExpiresAt          int64
	LastAccessedAt     int64
	RemoteETag         string
	RemoteLastModified string
}

// storeUpload writes the body to the blob store and records the asset, together
// with the component it belongs to when the path carries coordinates.
func (s *Server) storeUpload(repository *models.Repository, path string, body io.Reader, details uploadDetails) (*models.Asset, error) {
	layout := layoutFor(repository)
	key := repository.ID + "/" + path

	limit := s.config.MaxUploadBytes
	switch {
	case details.Limit < 0:
		limit = 0
	case details.Limit > 0:
		limit = details.Limit
	}

	size, digests, err := s.blobs.Put(key, body, limit)
	if err != nil {
		return nil, err
	}

	var componentID *string
	if coordinates, ok := layout.Coordinates(path); ok {
		identifier, err := s.store.UpsertComponent(&models.Component{
			RepositoryID: repository.ID,
			Format:       repository.Format,
			Namespace:    coordinates.Namespace,
			Name:         coordinates.Name,
			Version:      coordinates.Version,
			BaseVersion:  coordinates.BaseVersion,
			IsSnapshot:   coordinates.IsSnapshot,
		})
		if err != nil {
			return nil, err
		}
		componentID = &identifier
	}

	segments := strings.Split(path, "/")

	contentType := details.ContentType
	if contentType == "" {
		contentType = layout.ContentType(segments[len(segments)-1])
	}

	asset := &models.Asset{
		RepositoryID:       repository.ID,
		ComponentID:        componentID,
		Path:               path,
		StorageKey:         key,
		Size:               size,
		ContentType:        contentType,
		MD5:                digests.MD5,
		SHA1:               digests.SHA1,
		SHA256:             digests.SHA256,
		SHA512:             digests.SHA512,
		UploadedBy:         details.UploadedBy,
		FetchedAt:          details.FetchedAt,
		ExpiresAt:          details.ExpiresAt,
		LastAccessedAt:     details.LastAccessedAt,
		RemoteETag:         details.RemoteETag,
		RemoteLastModified: details.RemoteLastModified,
	}
	if err := s.store.UpsertAsset(asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (s *Server) writeDirectoryIndex(writer http.ResponseWriter, repository *models.Repository, path string) {
	entries, err := s.store.BrowseDirectory(repository.ID, path)
	if err != nil {
		log.Errorf("listing a directory failed: %v", err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	title := html.EscapeString("/" + repository.Name + "/" + path)

	var rows strings.Builder
	if path != "" {
		rows.WriteString(`<tr><td><a href="../">../</a></td><td>-</td><td></td></tr>`)
	}
	for _, entry := range entries {
		name := entry.Name
		size := "-"
		updated := ""
		if entry.Type == store.EntryDirectory {
			name += "/"
		} else {
			if entry.Size != nil {
				size = strconv.FormatInt(*entry.Size, 10)
			}
			if entry.UpdatedAt != nil {
				updated = time.UnixMilli(*entry.UpdatedAt).UTC().Format(time.RFC3339)
			}
		}
		escaped := html.EscapeString(name)
		fmt.Fprintf(&rows, "<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td></tr>\n", escaped, escaped, size, updated)
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Security-Policy", indexPolicy)
	fmt.Fprintf(writer, `<!doctype html>
<html><head><meta charset="utf-8"><title>Index of %s</title>
<style>body{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;margin:2rem;color:#111}table{border-collapse:collapse}td{padding:.15rem 1.5rem .15rem 0}a{color:#0b57d0}</style>
</head><body><h1>Index of %s</h1><table>
%s
</table></body></html>
`, title, title, rows.String())
}
