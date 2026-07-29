package npm

import (
	"net/http"
	"strings"
)

type RouteKind string

const (
	RouteUnknown   RouteKind = "unknown"
	RouteIndex     RouteKind = "index"
	RoutePing      RouteKind = "ping"
	RouteWhoami    RouteKind = "whoami"
	RouteLogin     RouteKind = "login"
	RoutePackument RouteKind = "packument"
	RouteVersion   RouteKind = "version"
	RouteTarball   RouteKind = "tarball"
	RoutePublish   RouteKind = "publish"
	RouteDistTags  RouteKind = "dist-tags"
	RouteSetTag    RouteKind = "set-tag"
	RouteDeleteTag RouteKind = "delete-tag"
	RouteUnpublish RouteKind = "unpublish"
	RouteSearch    RouteKind = "search"
	RouteTokens    RouteKind = "tokens"
	RouteMakeToken RouteKind = "make-token"
	RouteDropToken RouteKind = "drop-token"
)

type Route struct {
	Kind      RouteKind
	Namespace string
	Name      string
	Version   string
	Tag       string
	Username  string
	Token     string
}

func (r Route) Package() string { return PackageName(r.Namespace, r.Name) }

const loginPrefix = "org.couchdb.user:"

// Resolve maps a registry request onto the endpoint it addresses. It is the
// only place that knows npm's URL shapes, so the storage layout can differ.
func Resolve(method, path string) Route {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return Route{Kind: RouteIndex}
	}

	segments := strings.Split(trimmed, "/")
	if segments[0] == reservedSegment {
		return resolveReserved(method, segments[1:])
	}

	namespace, name, rest, ok := splitStoragePath(trimmed)
	if !ok {
		return Route{Kind: RouteUnknown}
	}
	route := Route{Namespace: namespace, Name: name}

	switch {
	case len(rest) == 0:
		if method == http.MethodPut || method == http.MethodPost {
			route.Kind = RoutePublish
			return route
		}
		route.Kind = RoutePackument
		return route

	case len(rest) == 2 && rest[0] == reservedSegment:
		version, ok := versionFromTarball(name, rest[1])
		if !ok {
			return Route{Kind: RouteUnknown}
		}
		route.Kind, route.Version = RouteTarball, version
		return route

	case len(rest) == 2 && rest[0] == "-rev" && method == http.MethodDelete:
		route.Kind = RouteUnpublish
		return route

	case len(rest) == 1 && IsValidVersion(rest[0]):
		route.Kind, route.Version = RouteVersion, rest[0]
		return route
	}

	return Route{Kind: RouteUnknown}
}

func resolveReserved(method string, segments []string) Route {
	switch {
	case len(segments) == 1 && segments[0] == "ping":
		return Route{Kind: RoutePing}

	case len(segments) == 1 && segments[0] == "whoami":
		return Route{Kind: RouteWhoami}

	case len(segments) == 2 && segments[0] == "user" && method == http.MethodPut:
		username, found := strings.CutPrefix(segments[1], loginPrefix)
		if !found || username == "" {
			return Route{Kind: RouteUnknown}
		}
		return Route{Kind: RouteLogin, Username: username}

	case len(segments) == 2 && segments[0] == "v1" && segments[1] == "search" && method == http.MethodGet:
		return Route{Kind: RouteSearch}

	case len(segments) >= 3 && segments[0] == "package":
		return resolveDistTags(method, segments[1:])

	case len(segments) >= 3 && segments[0] == "npm" && segments[1] == "v1" && segments[2] == "tokens":
		return resolveTokens(method, segments[3:])
	}

	return Route{Kind: RouteUnknown}
}

func resolveTokens(method string, segments []string) Route {
	if len(segments) == 0 {
		switch method {
		case http.MethodGet:
			return Route{Kind: RouteTokens}
		case http.MethodPost:
			return Route{Kind: RouteMakeToken}
		}
		return Route{Kind: RouteUnknown}
	}

	if len(segments) == 2 && segments[0] == "token" && segments[1] != "" && method == http.MethodDelete {
		return Route{Kind: RouteDropToken, Token: segments[1]}
	}
	return Route{Kind: RouteUnknown}
}

func resolveDistTags(method string, segments []string) Route {
	marker := -1
	for index, segment := range segments {
		if segment == "dist-tags" {
			marker = index
			break
		}
	}
	if marker <= 0 || len(segments) > marker+2 {
		return Route{Kind: RouteUnknown}
	}

	namespace, name, ok := SplitPackage(strings.Join(segments[:marker], "/"))
	if !ok {
		return Route{Kind: RouteUnknown}
	}
	route := Route{Namespace: namespace, Name: name}

	if len(segments) == marker+1 {
		if method != http.MethodGet {
			return Route{Kind: RouteUnknown}
		}
		route.Kind = RouteDistTags
		return route
	}

	route.Tag = segments[marker+1]
	if route.Tag == "" {
		return Route{Kind: RouteUnknown}
	}

	switch method {
	case http.MethodPut, http.MethodPost:
		route.Kind = RouteSetTag
	case http.MethodDelete:
		route.Kind = RouteDeleteTag
	case http.MethodGet:
		route.Kind = RouteDistTags
	default:
		return Route{Kind: RouteUnknown}
	}
	return route
}

func versionFromTarball(name, filename string) (string, bool) {
	body, found := strings.CutSuffix(filename, TarballSuffix)
	if !found {
		return "", false
	}
	version, found := strings.CutPrefix(body, name+"-")
	if !found || !IsValidVersion(version) {
		return "", false
	}
	return version, true
}
