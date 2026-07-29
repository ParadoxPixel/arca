package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/npm"
	"arca/internal/store/models"
)

var errPackumentMissing = errors.New("server: the cached packument went missing")

func packumentFetch(route npm.Route) proxyFetch {
	return proxyFetch{
		CachePath:  npm.PackumentPath(route.Namespace, route.Name),
		RemotePath: npm.RemotePackumentPath(route.Namespace, route.Name),
		Accept:     "application/json",
	}
}

// proxyPackument answers from the cached upstream document, rewritten on the
// way out. Caching it verbatim keeps one entry valid for every hostname this
// server answers on and lets the abbreviated install document be derived from
// the same bytes.
func (s *Server) proxyPackument(request *http.Request, repository *models.Repository, route npm.Route) ([]byte, error) {
	asset, err := s.proxyAsset(repository, packumentFetch(route))
	if err != nil {
		return nil, err
	}

	file, _, err := s.blobs.Open(asset.StorageKey)
	if err != nil {
		s.dropDanglingAsset(asset)
		return nil, errPackumentMissing
	}
	defer file.Close()

	s.recordCacheHit(request, repository, asset)

	return npm.Rewrite(file, npm.RewriteOptions{
		Package:     route.Package(),
		Base:        publicBase(request) + "/repository/" + repository.Name,
		Abbreviated: npm.WantsAbbreviated(request.Header.Get("Accept")),
	})
}

func (s *Server) readProxyPackument(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) ([]byte, bool) {
	document, err := s.proxyPackument(request, repository, route)
	switch {
	case errors.Is(err, errRemoteMissing):
		npmError(writer, http.StatusNotFound, "Not found")
		return nil, false
	case err != nil:
		log.Errorf("proxying the %s packument failed: %v", route.Package(), err)
		npmError(writer, http.StatusBadGateway, "Upstream fetch failed")
		return nil, false
	}
	return document, true
}

func (s *Server) serveNPMProxyPackument(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	if document, ok := s.readProxyPackument(writer, request, repository, route); ok {
		writeNPMDocument(writer, request, document)
	}
}

func (s *Server) serveNPMProxyVersion(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	document, ok := s.readProxyPackument(writer, request, repository, route)
	if !ok {
		return
	}

	manifest, found := npm.VersionOf(document, route.Version)
	if !found {
		npmError(writer, http.StatusNotFound, "Not found")
		return
	}
	writeNPMDocument(writer, request, manifest)
}

func (s *Server) serveNPMProxyDistTags(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	document, ok := s.readProxyPackument(writer, request, repository, route)
	if !ok {
		return
	}

	tags, err := npm.DistTagsOf(document)
	if err != nil {
		log.Errorf("reading upstream dist-tags for %s failed: %v", route.Package(), err)
		npmError(writer, http.StatusBadGateway, "Upstream fetch failed")
		return
	}
	writeJSON(writer, http.StatusOK, tags)
}

// serveNPMProxyTarball stores the tarball under the coordinates it belongs to
// rather than the "-" path it is fetched from, so a cached package browses,
// searches and evicts exactly like a published one.
func (s *Server) serveNPMProxyTarball(writer http.ResponseWriter, request *http.Request, repository *models.Repository, route npm.Route) {
	s.serveProxyFetch(writer, request, repository, proxyFetch{
		CachePath:  npm.TarballPath(route.Namespace, route.Name, route.Version),
		RemotePath: npm.RemoteTarballPath(route.Namespace, route.Name, route.Version),
	})
}

// writeNPMDocument validates against the bytes actually sent. The upstream ETag
// belongs to the unrewritten document and would pin tarball URLs pointing at
// the upstream in npm's cache.
func writeNPMDocument(writer http.ResponseWriter, request *http.Request, document []byte) {
	digest := sha256.Sum256(document)

	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
	http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(document))
}
