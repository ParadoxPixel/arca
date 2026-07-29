package server

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"arca/internal/npm"
)

const npmRepository = "npm-registry"

func (i *testInstance) createNPMRepository(name, extra string) {
	i.t.Helper()

	body := fmt.Sprintf(`{"name":%q,"format":"npm","policy":"mixed","visibility":"public"%s}`, name, extra)
	expectStatus(i.t, i.api(http.MethodPost, "/api/repositories", body), http.StatusCreated)
}

func (i *testInstance) npm(method, path, body string, authenticated bool) *http.Response {
	i.t.Helper()

	request := i.request(method, "/repository/"+npmRepository+"/"+path, body)
	if !authenticated {
		return i.doAnonymously(request)
	}
	return i.do(request)
}

// publishBody builds the document npm sends, with the tarball base64-encoded
// under _attachments exactly as the real client does.
func publishBody(pkg, version, content string, tag string) string {
	bare := pkg
	if index := strings.LastIndex(pkg, "/"); index >= 0 {
		bare = pkg[index+1:]
	}

	tags := ""
	if tag != "" {
		tags = fmt.Sprintf(`"dist-tags":{%q:%q},`, tag, version)
	}

	return fmt.Sprintf(`{
		"_id":%q,
		"name":%q,
		%s
		"versions":{%q:{"name":%q,"version":%q,"license":"MIT","dist":{"integrity":%q,"shasum":%q}}},
		"_attachments":{%q:{"content_type":"application/octet-stream","data":%q,"length":%d}}
	}`,
		pkg, pkg, tags, version, pkg, version,
		integrityOf(content), shasumOf(content),
		bare+"-"+version+".tgz", base64.StdEncoding.EncodeToString([]byte(content)), len(content),
	)
}

func integrityOf(content string) string {
	digest := sha512.Sum512([]byte(content))
	return "sha512-" + base64.StdEncoding.EncodeToString(digest[:])
}

func shasumOf(content string) string {
	digest := sha1.Sum([]byte(content))
	return hex.EncodeToString(digest[:])
}

func (i *testInstance) publishNPM(pkg, version, content string) {
	i.t.Helper()
	expectStatus(i.t, i.npm(http.MethodPut, pkg, publishBody(pkg, version, content, ""), true), http.StatusCreated)
}

type packument struct {
	Name     string            `json:"name"`
	DistTags map[string]string `json:"dist-tags"`
	Versions map[string]struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		License string `json:"license"`
		Dist    struct {
			Tarball   string `json:"tarball"`
			Integrity string `json:"integrity"`
			Shasum    string `json:"shasum"`
		} `json:"dist"`
	} `json:"versions"`
	Time map[string]string `json:"time"`
}

func (i *testInstance) packument(pkg string) packument {
	i.t.Helper()

	body := expectStatus(i.t, i.npm(http.MethodGet, pkg, "", true), http.StatusOK)

	var decoded packument
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		i.t.Fatalf("decoding a packument: %v (%s)", err, body)
	}
	return decoded
}

func TestNPMPublishAndReadBack(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")

	const content = "pretend-tarball-bytes"

	cases := []struct {
		name    string
		pkg     string
		version string
		tarball string
	}{
		{"unscoped", "express", "4.18.2", "express/-/express-4.18.2.tgz"},
		{"scoped drops the scope from the filename", "@types/node", "20.1.0", "@types/node/-/node-20.1.0.tgz"},
		{"prerelease", "vite", "5.0.0-beta.1", "vite/-/vite-5.0.0-beta.1.tgz"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instance.publishNPM(testCase.pkg, testCase.version, content)

			document := instance.packument(testCase.pkg)
			if document.Name != testCase.pkg {
				t.Fatalf("name = %q, want %q", document.Name, testCase.pkg)
			}

			release, ok := document.Versions[testCase.version]
			if !ok {
				t.Fatalf("packument has no %s: %+v", testCase.version, document.Versions)
			}
			if release.License != "MIT" {
				t.Fatalf("the published manifest was not preserved: %+v", release)
			}
			if !strings.HasSuffix(release.Dist.Tarball, "/repository/"+npmRepository+"/"+testCase.tarball) {
				t.Fatalf("tarball = %q, want it to end in %q", release.Dist.Tarball, testCase.tarball)
			}
			if release.Dist.Integrity != integrityOf(content) {
				t.Fatalf("integrity = %q, want %q", release.Dist.Integrity, integrityOf(content))
			}
			if release.Dist.Shasum != shasumOf(content) {
				t.Fatalf("shasum = %q, want %q", release.Dist.Shasum, shasumOf(content))
			}

			body := expectStatus(t, instance.npm(http.MethodGet, testCase.tarball, "", true), http.StatusOK)
			if body != content {
				t.Fatalf("tarball body = %q, want %q", body, content)
			}
		})
	}
}

func TestNPMScopeEncodingIsIrrelevant(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("@types/node", "20.1.0", "bytes")

	raw := expectStatus(t, instance.npm(http.MethodGet, "@types/node", "", true), http.StatusOK)
	encoded := expectStatus(t, instance.npm(http.MethodGet, "@types%2fnode", "", true), http.StatusOK)

	if raw != encoded {
		t.Fatalf("the encoded and unencoded forms disagree:\n%s\n%s", raw, encoded)
	}
}

func TestNPMPublishRejections(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("express", "4.18.2", "original")

	cases := []struct {
		name   string
		pkg    string
		body   string
		status int
	}{
		{
			"republishing a version", "express",
			publishBody("express", "4.18.2", "replacement", ""), http.StatusConflict,
		},
		{
			"a tarball that does not match its integrity", "lodash",
			strings.Replace(publishBody("lodash", "1.0.0", "bytes", ""), integrityOf("bytes"), integrityOf("other"), 1),
			http.StatusBadRequest,
		},
		{
			"a tarball that does not match its shasum", "underscore",
			strings.Replace(publishBody("underscore", "1.0.0", "bytes", ""), shasumOf("bytes"), shasumOf("other"), 1),
			http.StatusBadRequest,
		},
		{
			"a manifest naming a different package", "mismatch",
			strings.Replace(publishBody("mismatch", "1.0.0", "bytes", ""), `"name":"mismatch","version"`, `"name":"elsewhere","version"`, 1),
			http.StatusBadRequest,
		},
		{
			"a version that is not semver", "badversion",
			publishBody("badversion", "latest", "bytes", ""), http.StatusBadRequest,
		},
		// A PUT with no attachment is a metadata update, so an unpublished
		// package is missing rather than malformed.
		{
			"a metadata update for an unknown package", "empty",
			`{"name":"empty","versions":{"1.0.0":{"name":"empty","version":"1.0.0"}},"_attachments":{}}`,
			http.StatusNotFound,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.npm(http.MethodPut, testCase.pkg, testCase.body, true), testCase.status)
		})
	}

	t.Run("a rejected publish leaves nothing behind", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodGet, "lodash", "", true), http.StatusNotFound)
	})

	t.Run("the original is untouched", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "express/-/express-4.18.2.tgz", "", true), http.StatusOK)
		if body != "original" {
			t.Fatalf("tarball = %q, want the original bytes", body)
		}
	})
}

func TestNPMDistTags(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")

	instance.publishNPM("express", "4.18.2", "release-bytes")
	expectStatus(t, instance.npm(http.MethodPut, "express", publishBody("express", "5.0.0-beta.1", "beta-bytes", "beta"), true), http.StatusCreated)

	t.Run("latest ignores prereleases", func(t *testing.T) {
		document := instance.packument("express")
		if document.DistTags["latest"] != "4.18.2" {
			t.Fatalf("latest = %q, want 4.18.2", document.DistTags["latest"])
		}
		if document.DistTags["beta"] != "5.0.0-beta.1" {
			t.Fatalf("beta = %q, want 5.0.0-beta.1", document.DistTags["beta"])
		}
	})

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"read", http.MethodGet, "-/package/express/dist-tags", "", http.StatusOK},
		{"set a tag on a published version", http.MethodPut, "-/package/express/dist-tags/stable", `"4.18.2"`, http.StatusCreated},
		{"set a tag on an unpublished version", http.MethodPut, "-/package/express/dist-tags/ghost", `"9.9.9"`, http.StatusNotFound},
		{"remove a tag", http.MethodDelete, "-/package/express/dist-tags/beta", "", http.StatusOK},
		{"remove a tag that is not set", http.MethodDelete, "-/package/express/dist-tags/absent", "", http.StatusNotFound},
		{"latest cannot be removed", http.MethodDelete, "-/package/express/dist-tags/latest", "", http.StatusBadRequest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.npm(testCase.method, testCase.path, testCase.body, true), testCase.status)
		})
	}
}

func TestNPMPolicyRejectsPrereleases(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"`+npmRepository+`","format":"npm","policy":"release","visibility":"public"}`), http.StatusCreated)

	cases := []struct {
		name    string
		version string
		status  int
	}{
		{"a release is accepted", "1.0.0", http.StatusCreated},
		{"a prerelease is rejected", "2.0.0-rc.1", http.StatusBadRequest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.npm(http.MethodPut, "express", publishBody("express", testCase.version, "bytes", ""), true), testCase.status)
		})
	}
}

func TestNPMAuthentication(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.createNPMRepository("npm-private", `,"visibility":"private"`)
	instance.publishNPM("express", "4.18.2", "bytes")

	t.Run("a public packument is readable anonymously", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodGet, "express", "", false), http.StatusOK)
	})

	t.Run("an anonymous publish is challenged with bearer", func(t *testing.T) {
		response := instance.npm(http.MethodPut, "lodash", publishBody("lodash", "1.0.0", "bytes", ""), false)
		defer response.Body.Close()

		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.StatusCode)
		}
		if challenge := response.Header.Get("WWW-Authenticate"); !strings.HasPrefix(challenge, "Bearer ") {
			t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", challenge)
		}
	})

	t.Run("a private repository is not readable anonymously", func(t *testing.T) {
		request := instance.request(http.MethodGet, "/repository/npm-private/express", "")
		response := instance.doAnonymously(request)
		defer response.Body.Close()

		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.StatusCode)
		}
	})

	t.Run("login issues a token that authenticates a publish", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodPut, "-/user/org.couchdb.user:"+administratorEmail,
			`{"name":"`+administratorEmail+`","password":"`+administratorPassword+`","type":"user"}`, false), http.StatusCreated)

		var login struct {
			OK    bool   `json:"ok"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal([]byte(body), &login); err != nil {
			t.Fatalf("decoding the login response: %v", err)
		}
		if !login.OK || login.Token == "" {
			t.Fatalf("login response = %s", body)
		}

		request := instance.request(http.MethodPut, "/repository/"+npmRepository+"/lodash", publishBody("lodash", "1.0.0", "bytes", ""))
		request.Header.Set("Authorization", "Bearer "+login.Token)
		expectStatus(t, instance.doAnonymously(request), http.StatusCreated)
	})

	t.Run("a bad password is refused", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPut, "-/user/org.couchdb.user:"+administratorEmail,
			`{"name":"`+administratorEmail+`","password":"wrong-password","type":"user"}`, false), http.StatusUnauthorized)
	})

	t.Run("whoami reports the token owner", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "-/whoami", "", true), http.StatusOK)
		if !strings.Contains(body, administratorEmail) {
			t.Fatalf("whoami = %s, want %s", body, administratorEmail)
		}
	})
}

func TestNPMUnpublishRemovesEverything(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("@demo/greeter", "1.0.0", "bytes")
	expectStatus(t, instance.npm(http.MethodPut, "-/package/@demo/greeter/dist-tags/beta", `"1.0.0"`, true), http.StatusCreated)

	expectStatus(t, instance.npm(http.MethodDelete, "@demo/greeter/-rev/1-abc", "", true), http.StatusOK)

	checks := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"the packument is gone", http.MethodGet, "@demo/greeter", http.StatusNotFound},
		{"the tarball is gone", http.MethodGet, "@demo/greeter/-/greeter-1.0.0.tgz", http.StatusNotFound},
		{"unpublishing again finds nothing", http.MethodDelete, "@demo/greeter/-rev/1-abc", http.StatusNotFound},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			expectStatus(t, instance.npm(check.method, check.path, "", true), check.status)
		})
	}

	t.Run("the dist-tags are gone", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "-/package/@demo/greeter/dist-tags", "", true), http.StatusOK)
		if strings.Contains(body, "beta") {
			t.Fatalf("dist-tags survived the unpublish: %s", body)
		}
	})

	t.Run("no files are left on disk", func(t *testing.T) {
		if count := instance.cachedFiles(); count != 0 {
			t.Fatalf("%d files remain in storage, want 0", count)
		}
	})
}

func TestNPMSharesTheContentAPIWithMaven(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("@demo/greeter", "1.0.0", "bytes")
	instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "jar-bytes")

	t.Run("search spans both formats", func(t *testing.T) {
		npmResult := expectStatus(t, instance.api(http.MethodGet, "/api/search?q=greeter", ""), http.StatusOK)
		if !strings.Contains(npmResult, `"format":"npm"`) || !strings.Contains(npmResult, `"namespace":"demo"`) {
			t.Fatalf("npm search result = %s", npmResult)
		}

		mavenResult := expectStatus(t, instance.api(http.MethodGet, "/api/search?q=app", ""), http.StatusOK)
		if !strings.Contains(mavenResult, `"format":"maven2"`) || !strings.Contains(mavenResult, `"namespace":"com.example"`) {
			t.Fatalf("maven search result = %s", mavenResult)
		}
	})

	t.Run("browse resolves an npm package", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/"+npmRepository+"/browse?path=@demo/greeter", ""), http.StatusOK)
		if !strings.Contains(body, `"name":"greeter","namespace":"demo"`) {
			t.Fatalf("browse did not resolve the package: %s", body)
		}
	})

	t.Run("the artifact endpoint reports npm versions", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet,
			"/api/repositories/"+npmRepository+"/artifacts?namespace=demo&name=greeter", ""), http.StatusOK)
		if !strings.Contains(body, `"latest":"1.0.0"`) {
			t.Fatalf("artifact detail = %s", body)
		}
	})
}

// An unscoped npm package has no namespace at all, so every content route has to
// accept an empty one rather than treating it as a missing parameter.
func TestNPMContentAPIAcceptsEitherPackageShape(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("@demo/greeter", "1.0.0", "scoped-bytes")
	instance.publishNPM("left-pad", "1.3.0", "unscoped-bytes")

	testCases := []struct {
		name      string
		namespace string
		bare      string
		version   string
	}{
		{"scoped", "demo", "greeter", "1.0.0"},
		{"unscoped", "", "left-pad", "1.3.0"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			coordinates := fmt.Sprintf("namespace=%s&name=%s", testCase.namespace, testCase.bare)

			t.Run("artifacts", func(t *testing.T) {
				body := expectStatus(t, instance.api(http.MethodGet,
					"/api/repositories/"+npmRepository+"/artifacts?"+coordinates, ""), http.StatusOK)
				if !strings.Contains(body, `"latest":"`+testCase.version+`"`) {
					t.Fatalf("artifact detail = %s", body)
				}
			})

			t.Run("files", func(t *testing.T) {
				body := expectStatus(t, instance.api(http.MethodGet,
					"/api/repositories/"+npmRepository+"/artifacts/files?"+coordinates+"&version="+testCase.version, ""),
					http.StatusOK)
				if !strings.Contains(body, npm.TarballFilename(testCase.bare, testCase.version)) {
					t.Fatalf("files = %s", body)
				}
			})

			t.Run("recent lists it", func(t *testing.T) {
				body := expectStatus(t, instance.api(http.MethodGet,
					"/api/repositories/"+npmRepository+"/recent", ""), http.StatusOK)
				if !strings.Contains(body, `"name":"`+testCase.bare+`"`) {
					t.Fatalf("recent = %s", body)
				}
			})
		})
	}
}
