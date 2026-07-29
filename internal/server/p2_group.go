package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"

	"arca/internal/p2"
	"arca/internal/store"
	"arca/internal/store/models"
)

// serveP2Group answers a group with generated composite documents. It holds no
// content: the client reads the children itself, from their own URLs on this
// server, which is why nothing here merges metadata or proxies member content.
func (s *Server) serveP2Group(writer http.ResponseWriter, request *http.Request, repository *models.Repository, path string) {
	document, contentType, ok := s.renderP2Group(request, repository, path)
	if !ok {
		plain(writer, http.StatusNotFound, "Not Found")
		return
	}

	digest := sha256.Sum256([]byte(document))

	header := writer.Header()
	header.Set("Content-Type", contentType)
	header.Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
	// The children a caller may read decide the document, so a shared cache
	// must not hand one user's composite to another.
	header.Set("Cache-Control", "no-cache, private")

	if request.Method == http.MethodHead {
		return
	}
	writer.Write([]byte(document))
}

func (s *Server) renderP2Group(request *http.Request, repository *models.Repository, path string) (string, string, bool) {
	switch p2.Filename(path) {
	case p2.IndexFilename:
		return p2.RenderIndex(), "text/plain", true

	case p2.CompositeContentFilename:
		return p2.RenderCompositeContent(s.compositeFor(request, repository)), "application/xml", true

	case p2.CompositeArtifactsFilename:
		return p2.RenderCompositeArtifacts(s.compositeFor(request, repository)), "application/xml", true
	}

	return "", "", false
}

func (s *Server) compositeFor(request *http.Request, repository *models.Repository) p2.Composite {
	members, err := s.store.Members(repository.ID)
	if err != nil {
		log.Errorf("listing the members of %s failed: %v", repository.Name, err)
	}

	timestamp, err := s.store.MembersUpdatedAt(repository.ID)
	if err != nil {
		log.Errorf("reading the member timestamp of %s failed: %v", repository.Name, err)
	}

	name := repository.Description
	if strings.TrimSpace(name) == "" {
		name = repository.Name
	}

	children := make([]string, 0, len(members))
	for _, member := range members {
		if s.canReadMember(request, member.Target) {
			children = append(children, p2.ChildLocation(member.Target))
		}
	}

	return p2.Composite{Name: name, Timestamp: timestamp, Children: children}
}

// canReadMember keeps a child out of the composite when its repository is not
// readable by the caller. Atomic loading means one unreadable child fails the
// whole composite, so filtering is what lets a partly granted user load it at
// all rather than a nicety.
func (s *Server) canReadMember(request *http.Request, target string) bool {
	name, _, _ := strings.Cut(strings.Trim(target, "/"), "/")

	member, err := s.store.RepositoryByName(name)
	if err != nil {
		return false
	}

	permission, err := s.store.EffectivePermission(member, currentUser(request))
	if err != nil {
		log.Errorf("resolving permission for member %s failed: %v", name, err)
		return false
	}
	return store.Satisfies(permission, models.PermissionRead)
}
