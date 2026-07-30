package docker

import (
	"net/http"
	"strings"
)

type RouteKind string

const (
	RouteUnknown     RouteKind = "unknown"
	RouteBase        RouteKind = "base"
	RouteCatalog     RouteKind = "catalog"
	RouteTags        RouteKind = "tags"
	RouteManifest    RouteKind = "manifest"
	RouteBlob        RouteKind = "blob"
	RouteUploadStart RouteKind = "upload-start"
	RouteUpload      RouteKind = "upload"
	RouteReferrers   RouteKind = "referrers"
)

const (
	segmentBlobs     = "blobs"
	segmentManifests = "manifests"
	segmentUploads   = "uploads"
	segmentTags      = "tags"
	segmentReferrers = "referrers"
	segmentCatalog   = "_catalog"
	segmentList      = "list"
)

type Route struct {
	Kind RouteKind
	// Name is the full image name from the URL, repository prefix included. The
	// server splits the prefix off, because which segment names a repository is
	// its business rather than the protocol's.
	Name string
	// Reference is the tag or digest a manifest request addresses, verbatim.
	Reference string
	Tag       string
	Digest    Digest
	ByDigest  bool
	Upload    string
}

// Resolve maps a registry request onto the endpoint it addresses. Names may
// contain slashes and a name component may legally be "manifests" or "blobs", so
// every shape is matched from the end of the path rather than the start.
func Resolve(method, path string) Route {
	trimmed := strings.TrimPrefix(path, "/")

	switch trimmed {
	case "":
		return Route{Kind: RouteBase}
	case segmentCatalog:
		return Route{Kind: RouteCatalog}
	}

	segments := strings.Split(trimmed, "/")

	if len(segments) >= 4 {
		if route, ok := resolveUpload(segments); ok {
			return route
		}
	}
	if len(segments) >= 3 {
		return resolveVerb(method, segments)
	}
	return Route{Kind: RouteUnknown}
}

// resolveUpload handles the two upload shapes, which are three segments deep
// rather than two: "<name>/blobs/uploads/" opens a session and
// "<name>/blobs/uploads/<id>" addresses one.
func resolveUpload(segments []string) (Route, bool) {
	tail := segments[len(segments)-3:]
	if tail[0] != segmentBlobs || tail[1] != segmentUploads {
		return Route{}, false
	}

	name := strings.Join(segments[:len(segments)-3], "/")
	if !IsValidName(name) {
		return Route{Kind: RouteUnknown}, true
	}

	if tail[2] == "" {
		return Route{Kind: RouteUploadStart, Name: name}, true
	}
	return Route{Kind: RouteUpload, Name: name, Upload: tail[2]}, true
}

func resolveVerb(method string, segments []string) Route {
	verb := segments[len(segments)-2]
	target := segments[len(segments)-1]

	name := strings.Join(segments[:len(segments)-2], "/")
	if !IsValidName(name) {
		return Route{Kind: RouteUnknown}
	}
	route := Route{Name: name}

	switch verb {
	case segmentManifests:
		route.Kind, route.Reference = RouteManifest, target
		if digest, ok := ParseDigest(target); ok {
			route.Digest, route.ByDigest = digest, true
			return route
		}
		if !IsValidTag(target) {
			return Route{Kind: RouteUnknown}
		}
		route.Tag = target
		return route

	case segmentBlobs:
		// A POST that leaves the trailing slash off the uploads endpoint is
		// common enough to accept, since nothing else can be meant by it.
		if target == segmentUploads && method == http.MethodPost {
			route.Kind = RouteUploadStart
			return route
		}
		digest, ok := ParseDigest(target)
		if !ok {
			return Route{Kind: RouteUnknown}
		}
		route.Kind, route.Digest, route.ByDigest = RouteBlob, digest, true
		return route

	case segmentTags:
		if target != segmentList {
			return Route{Kind: RouteUnknown}
		}
		route.Kind = RouteTags
		return route

	case segmentReferrers:
		digest, ok := ParseDigest(target)
		if !ok {
			return Route{Kind: RouteUnknown}
		}
		route.Kind, route.Digest, route.ByDigest = RouteReferrers, digest, true
		return route
	}

	return Route{Kind: RouteUnknown}
}
