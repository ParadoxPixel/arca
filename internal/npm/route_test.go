package npm

import (
	"net/http"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name      string
		method    string
		path      string
		kind      RouteKind
		namespace string
		pkg       string
		version   string
		tag       string
		username  string
	}{
		{name: "root", method: http.MethodGet, path: "", kind: RouteIndex},
		{name: "ping", method: http.MethodGet, path: "-/ping", kind: RoutePing},
		{name: "whoami", method: http.MethodGet, path: "-/whoami", kind: RouteWhoami},

		{name: "packument", method: http.MethodGet, path: "express", kind: RoutePackument, pkg: "express"},
		{name: "scoped packument", method: http.MethodGet, path: "@types/node", kind: RoutePackument, namespace: "types", pkg: "node"},
		{name: "version", method: http.MethodGet, path: "express/4.18.2", kind: RouteVersion, pkg: "express", version: "4.18.2"},
		{name: "scoped version", method: http.MethodGet, path: "@types/node/20.1.0", kind: RouteVersion, namespace: "types", pkg: "node", version: "20.1.0"},

		{name: "tarball", method: http.MethodGet, path: "express/-/express-4.18.2.tgz", kind: RouteTarball, pkg: "express", version: "4.18.2"},
		{
			name: "scoped tarball drops the scope from the filename", method: http.MethodGet,
			path: "@types/node/-/node-20.1.0.tgz", kind: RouteTarball, namespace: "types", pkg: "node", version: "20.1.0",
		},
		{name: "prerelease tarball", method: http.MethodGet, path: "express/-/express-5.0.0-beta.1.tgz", kind: RouteTarball, pkg: "express", version: "5.0.0-beta.1"},

		{name: "publish", method: http.MethodPut, path: "express", kind: RoutePublish, pkg: "express"},
		{name: "scoped publish", method: http.MethodPut, path: "@types/node", kind: RoutePublish, namespace: "types", pkg: "node"},

		{name: "read dist-tags", method: http.MethodGet, path: "-/package/express/dist-tags", kind: RouteDistTags, pkg: "express"},
		{
			name: "read scoped dist-tags", method: http.MethodGet,
			path: "-/package/@types/node/dist-tags", kind: RouteDistTags, namespace: "types", pkg: "node",
		},
		{name: "set a tag", method: http.MethodPut, path: "-/package/express/dist-tags/beta", kind: RouteSetTag, pkg: "express", tag: "beta"},
		{name: "post a tag", method: http.MethodPost, path: "-/package/express/dist-tags/beta", kind: RouteSetTag, pkg: "express", tag: "beta"},
		{name: "delete a tag", method: http.MethodDelete, path: "-/package/express/dist-tags/beta", kind: RouteDeleteTag, pkg: "express", tag: "beta"},

		{
			name: "login", method: http.MethodPut, path: "-/user/org.couchdb.user:a@b.co",
			kind: RouteLogin, username: "a@b.co",
		},
		{name: "unpublish", method: http.MethodDelete, path: "express/-rev/1-abc", kind: RouteUnpublish, pkg: "express"},

		{name: "traversal", method: http.MethodGet, path: "../etc/passwd", kind: RouteUnknown},
		{name: "bare reserved segment", method: http.MethodGet, path: "-", kind: RouteUnknown},
		{name: "web login is not routed", method: http.MethodPost, path: "-/v1/login", kind: RouteUnknown},
		{name: "audit is not routed", method: http.MethodPost, path: "-/npm/v1/security/audits/quick", kind: RouteUnknown},
		{name: "a sidecar manifest is unreachable", method: http.MethodGet, path: "express/4.18.2/package.json", kind: RouteUnknown},
		{name: "a non-semver version segment", method: http.MethodGet, path: "express/latest", kind: RouteUnknown},
		{name: "a tarball naming another package", method: http.MethodGet, path: "express/-/other-1.0.0.tgz", kind: RouteUnknown},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			route := Resolve(testCase.method, testCase.path)

			if route.Kind != testCase.kind {
				t.Fatalf("Resolve(%q, %q).Kind = %q, want %q", testCase.method, testCase.path, route.Kind, testCase.kind)
			}
			if route.Namespace != testCase.namespace || route.Name != testCase.pkg {
				t.Fatalf("package = %q/%q, want %q/%q", route.Namespace, route.Name, testCase.namespace, testCase.pkg)
			}
			if route.Version != testCase.version {
				t.Fatalf("version = %q, want %q", route.Version, testCase.version)
			}
			if route.Tag != testCase.tag {
				t.Fatalf("tag = %q, want %q", route.Tag, testCase.tag)
			}
			if route.Username != testCase.username {
				t.Fatalf("username = %q, want %q", route.Username, testCase.username)
			}
		})
	}
}

func TestSplitPackage(t *testing.T) {
	cases := []struct {
		full      string
		namespace string
		name      string
		ok        bool
	}{
		{"express", "", "express", true},
		{"@types/node", "types", "node", true},
		{"@scope/a.b-c", "scope", "a.b-c", true},
		{"", "", "", false},
		{"@", "", "", false},
		{"@types", "", "", false},
		{"@types/", "", "", false},
		{"@/node", "", "", false},
		{"a/b", "", "", false},
		{"-", "", "", false},
		{"-hidden", "", "", false},
		{".dotfile", "", "", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.full, func(t *testing.T) {
			namespace, name, ok := SplitPackage(testCase.full)
			if ok != testCase.ok {
				t.Fatalf("SplitPackage(%q) ok = %v, want %v", testCase.full, ok, testCase.ok)
			}
			if !ok {
				return
			}
			if namespace != testCase.namespace || name != testCase.name {
				t.Fatalf("SplitPackage(%q) = %q/%q, want %q/%q", testCase.full, namespace, name, testCase.namespace, testCase.name)
			}
		})
	}
}

func TestLayoutCoordinates(t *testing.T) {
	cases := []struct {
		path      string
		known     bool
		namespace string
		name      string
		version   string
		snapshot  bool
	}{
		{path: "express/4.18.2/express-4.18.2.tgz", known: true, name: "express", version: "4.18.2"},
		{path: "express/4.18.2/package.json", known: true, name: "express", version: "4.18.2"},
		{path: "@types/node/20.1.0/node-20.1.0.tgz", known: true, namespace: "types", name: "node", version: "20.1.0"},
		{path: "express/5.0.0-beta.1/express-5.0.0-beta.1.tgz", known: true, name: "express", version: "5.0.0-beta.1", snapshot: true},
		{path: "express", known: false},
		{path: "express/4.18.2", known: false},
		{path: "express/latest/express-latest.tgz", known: false},
		{path: "express/4.18.2/other-4.18.2.tgz", known: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.path, func(t *testing.T) {
			coordinates, ok := (Layout{}).Coordinates(testCase.path)
			if ok != testCase.known {
				t.Fatalf("Coordinates(%q) ok = %v, want %v", testCase.path, ok, testCase.known)
			}
			if !ok {
				return
			}
			if coordinates.Namespace != testCase.namespace || coordinates.Name != testCase.name {
				t.Fatalf("package = %q/%q, want %q/%q", coordinates.Namespace, coordinates.Name, testCase.namespace, testCase.name)
			}
			if coordinates.Version != testCase.version {
				t.Fatalf("version = %q, want %q", coordinates.Version, testCase.version)
			}
			if coordinates.IsSnapshot != testCase.snapshot {
				t.Fatalf("isSnapshot = %v, want %v", coordinates.IsSnapshot, testCase.snapshot)
			}
		})
	}
}

func TestLayoutComponentAt(t *testing.T) {
	cases := []struct {
		prefix    string
		known     bool
		namespace string
		name      string
	}{
		{"express/", true, "", "express"},
		{"express", true, "", "express"},
		{"@types/node/", true, "types", "node"},
		{"@types/", false, "", ""},
		{"express/4.18.2/", false, "", ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.prefix, func(t *testing.T) {
			coordinates, ok := (Layout{}).ComponentAt(testCase.prefix)
			if ok != testCase.known {
				t.Fatalf("ComponentAt(%q) ok = %v, want %v", testCase.prefix, ok, testCase.known)
			}
			if !ok {
				return
			}
			if coordinates.Namespace != testCase.namespace || coordinates.Name != testCase.name {
				t.Fatalf("ComponentAt(%q) = %q/%q, want %q/%q",
					testCase.prefix, coordinates.Namespace, coordinates.Name, testCase.namespace, testCase.name)
			}
		})
	}
}
