package server

import (
	"fmt"
	"net/http"

	"github.com/charmbracelet/log"

	"arca/internal/store/models"
)

// handleDownload serves a deployment link. It is the one artifact route that
// never authenticates: the point of a link is that a private repository can
// publish exactly one file of one artifact to anyone holding the URL, so the
// slug is the credential and nothing else is consulted.
//
// Nothing else about the repository leaks through it. A link resolves to one
// file of one artifact, and a slug that names no link is indistinguishable from
// one whose artifact has never been published.
func (s *Server) handleDownload(writer http.ResponseWriter, request *http.Request) {
	slug := routeVar(request, "slug")

	deployment, err := s.store.LinkBySlug(slug)
	if err != nil || deployment.Disabled {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}

	repository, err := s.store.RepositoryByID(deployment.RepositoryID)
	if err != nil {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}

	resolved, err := s.resolveLink(repository, deployment)
	if err != nil {
		log.Errorf("resolving deployment link %q failed: %v", slug, err)
		plain(writer, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if resolved == nil {
		plain(writer, http.StatusNotFound, "No published version matches this link yet")
		return
	}

	header := writer.Header()
	header.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", resolved.filename(deployment.Filename)))
	// Which file the link resolves to changes with every release, so a cache
	// has to revalidate. The ETag writeAsset sets still makes that a 304 when
	// the newest version has not moved.
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Arca-Repository", repository.Name)
	header.Set("X-Arca-Version", resolved.version)

	if !s.writeAsset(writer, request, resolved.asset) {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}

	if request.Method != http.MethodGet {
		return
	}
	s.recordAssetTraffic(request, models.TrafficDownload, repository, resolved.asset)
	if err := s.store.RecordLinkDownload(deployment.ID); err != nil {
		log.Errorf("counting a deployment link download failed: %v", err)
	}
}
