package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arca/internal/docker"
)

func TestSetupRequiresTheToken(t *testing.T) {
	body := func(token string) string {
		return `{"setupToken":"` + token + `","instanceName":"x","name":"Admin","email":"` + administratorEmail + `","password":"` + administratorPassword + `"}`
	}

	t.Run("a missing or wrong token is refused", func(t *testing.T) {
		instance := newTestInstance(t)

		expectStatus(t, instance.api(http.MethodPost, "/api/setup", body("")), http.StatusUnauthorized)
		expectStatus(t, instance.api(http.MethodPost, "/api/setup", body("guess")), http.StatusUnauthorized)

		payload := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
		if !strings.Contains(payload, `"initialized":false`) {
			t.Fatalf("a refused setup must leave the instance uninitialized: %s", payload)
		}
	})

	t.Run("the token may travel in a header", func(t *testing.T) {
		instance := newTestInstance(t)

		request := instance.request(http.MethodPost, "/api/setup", body(""))
		request.Header.Set("X-Setup-Token", instance.app.SetupToken())
		expectStatus(t, instance.do(request), http.StatusCreated)

		if instance.app.SetupToken() != "" {
			t.Fatal("the token must be forgotten once setup has run")
		}
	})

	t.Run("a configured token is used as given", func(t *testing.T) {
		instance := newTestInstance(t)
		instance.app.config.SetupToken = "from-the-environment"
		if err := instance.app.mintSetupToken(); err != nil {
			t.Fatal(err)
		}
		expectStatus(t, instance.api(http.MethodPost, "/api/setup", body("from-the-environment")), http.StatusCreated)
	})

	t.Run("the initialized marker can be claimed once", func(t *testing.T) {
		instance := newTestInstance(t)
		if err := instance.app.store.ClaimInitialization(); err != nil {
			t.Fatalf("first claim: %v", err)
		}
		if err := instance.app.store.ClaimInitialization(); err == nil {
			t.Fatal("a second claim must fail")
		}
	})
}

func TestStoredContentIsNeverRenderedOnTheAppOrigin(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	const pomPath = "/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.pom"
	instance.publish(pomPath, `<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></html>`)

	t.Run("an artifact is a download under a policy that runs nothing", func(t *testing.T) {
		response := instance.maven(http.MethodGet, pomPath, "", true)
		expectStatus(t, response, http.StatusOK)

		for header, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"Content-Security-Policy": contentPolicy,
			"Content-Disposition":     "attachment",
		} {
			if got := response.Header.Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
	})

	t.Run("a directory index keeps its own stylesheet and nothing else", func(t *testing.T) {
		response := instance.maven(http.MethodGet, "/repository/maven-releases/com/example/app/", "", true)
		expectStatus(t, response, http.StatusOK)

		if got := response.Header.Get("Content-Security-Policy"); got != indexPolicy {
			t.Fatalf("Content-Security-Policy = %q, want %q", got, indexPolicy)
		}
		if got := response.Header.Get("Content-Disposition"); got != "" {
			t.Fatalf("an index must render, not download, got Content-Disposition %q", got)
		}
		if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", got)
		}
	})

	t.Run("a deployment link keeps the filename it was given", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories/maven-releases/links",
			`{"slug":"pom","namespace":"com.example","artifact":"app","pattern":"{name}-{version}.pom","filename":"app.pom"}`), http.StatusCreated)

		response := instance.doAnonymously(instance.request(http.MethodGet, "/download/pom", ""))
		expectStatus(t, response, http.StatusOK)
		if got := response.Header.Get("Content-Disposition"); got != `attachment; filename="app.pom"` {
			t.Fatalf("Content-Disposition = %q", got)
		}
		if got := response.Header.Get("Content-Security-Policy"); got != contentPolicy {
			t.Fatalf("Content-Security-Policy = %q", got)
		}
	})

	t.Run("a docker manifest cannot declare itself as html", func(t *testing.T) {
		registry := instance.registry("images")
		config := []byte(testImageConfig)
		configDigest := registry.pushBlob("app", config)
		layer := registry.pushBlob("app", bytes.Repeat([]byte("layer"), 32))

		manifest := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"text/html","config":{"mediaType":%q,"digest":%q,"size":%d},"layers":[{"mediaType":%q,"digest":%q,"size":160}]}`,
			docker.MediaTypeOCIConfig, configDigest, len(config), "application/vnd.oci.image.layer.v1.tar", layer))

		body := expectStatus(t, registry.pushManifest("app", "1.0", manifest), http.StatusBadRequest)
		if !strings.Contains(body, docker.ErrorManifestInvalid) {
			t.Fatalf("body = %s", body)
		}

		// The declared Content-Type is not a way around it either.
		response := registry.do(http.MethodPut, "/v2/images/app/manifests/1.0", manifest, "text/html")
		expectStatus(t, response, http.StatusBadRequest)
	})

	t.Run("a registry response carries the same headers", func(t *testing.T) {
		registry := instance.registry("more-images")
		registry.pushImage("app", "1.0")

		response := registry.do(http.MethodGet, "/v2/more-images/app/manifests/1.0", nil, "")
		expectStatus(t, response, http.StatusOK)
		if got := response.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", got)
		}
		if got := response.Header.Get("Content-Security-Policy"); got != contentPolicy {
			t.Fatalf("Content-Security-Policy = %q", got)
		}
	})
}

func TestTreeDeletesNeedARepositoryAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	const jar = "/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar"
	instance.publish(jar, "bytes")

	users := expectStatus(t, instance.api(http.MethodPost, "/api/users",
		`{"email":"writer@example.com","name":"Writer","password":"writersecret1","role":"user"}`), http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(users), &created); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, instance.api(http.MethodPut, "/api/repositories/maven-releases/grants",
		`{"grants":[{"userId":"`+created.ID+`","permission":"write"}]}`), http.StatusNoContent)

	asWriter := func(method, path string) *http.Response {
		request := instance.request(method, path, "")
		request.SetBasicAuth("writer@example.com", "writersecret1")
		return instance.doAnonymously(request)
	}

	expectStatus(t, asWriter(http.MethodDelete, "/repository/maven-releases/"), http.StatusForbidden)
	expectStatus(t, asWriter(http.MethodDelete, "/repository/maven-releases/com/example/"), http.StatusForbidden)
	expectStatus(t, asWriter(http.MethodDelete, jar), http.StatusForbidden)
	expectStatus(t, asWriter(http.MethodGet, jar), http.StatusOK)

	expectStatus(t, instance.maven(http.MethodDelete, "/repository/maven-releases/com/example/", "", true), http.StatusNoContent)
	expectStatus(t, instance.maven(http.MethodGet, jar, "", true), http.StatusNotFound)
}

func TestPostIsNotAnUpload(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"site","format":"p2"}`), http.StatusCreated)

	for _, path := range []string{
		"/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar",
		"/repository/site/plugins/org.example_1.0.0.jar",
	} {
		expectStatus(t, instance.maven(http.MethodPost, path, "bytes", true), http.StatusMethodNotAllowed)
		expectStatus(t, instance.maven(http.MethodGet, path, "", true), http.StatusNotFound)
	}
}

func TestJSONBodiesMustComeFromThisOrigin(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	cases := []struct {
		name        string
		origin      string
		contentType string
		status      int
	}{
		{"no origin, as a build tool sends", "", "application/json", http.StatusCreated},
		{"the app's own origin", "http://" + strings.TrimPrefix(instance.server.URL, "http://"), "application/json; charset=utf-8", http.StatusCreated},
		{"a sibling subdomain", "https://evil.example.com", "application/json", http.StatusForbidden},
		{"an opaque origin", "null", "application/json", http.StatusForbidden},
		{"a form encoding", "", "application/x-www-form-urlencoded", http.StatusBadRequest},
		{"text/plain, which needs no preflight", "", "text/plain", http.StatusBadRequest},
	}

	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := instance.request(http.MethodPost, "/api/categories", fmt.Sprintf(`{"name":"category-%d"}`, index))
			request.Header.Set("Content-Type", testCase.contentType)
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			expectStatus(t, instance.do(request), testCase.status)
		})
	}
}

func TestRefusalsDoNotRevealRepositories(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.publish("/repository/maven-releases/com/example/app/1.0.0/app-1.0.0.jar", "bytes")
	registry := instance.registry("images")
	registry.pushImage("app", "1.0")
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/maven-releases", `{"name":"releases"}`), http.StatusOK)

	expectStatus(t, instance.api(http.MethodPost, "/api/users",
		`{"email":"member@example.com","name":"Member","password":"membersecret1","role":"user"}`), http.StatusCreated)
	member := instance.signInAs("member@example.com", "membersecret1")

	anonymous := func(method, path string) *http.Response {
		return instance.doAnonymously(instance.request(method, path, ""))
	}

	t.Run("an anonymous client is challenged the same way for private and missing", func(t *testing.T) {
		for _, path := range []string{
			"/repository/releases/", "/repository/nowhere/", "/repository/maven-releases/",
			"/v2/images/app/manifests/1.0", "/v2/nowhere/app/manifests/1.0",
			"/api/repositories/releases", "/api/repositories/nowhere",
		} {
			response := anonymous(http.MethodGet, path)
			body := expectStatus(t, response, http.StatusUnauthorized)
			if strings.Contains(body, "not found") || strings.Contains(body, "Not found") {
				t.Errorf("%s revealed existence: %s", path, body)
			}
		}
	})

	t.Run("a signed-in user without access is told not found either way", func(t *testing.T) {
		for _, path := range []string{
			"/api/repositories/releases", "/api/repositories/nowhere",
		} {
			expectStatus(t, member.api(http.MethodGet, path, ""), http.StatusNotFound)
		}

		for _, path := range []string{
			"/repository/releases/", "/repository/nowhere/", "/repository/maven-releases/",
		} {
			request := member.request(http.MethodGet, path, "")
			request.SetBasicAuth("member@example.com", "membersecret1")
			expectStatus(t, instance.doAnonymously(request), http.StatusNotFound)
		}

		for _, path := range []string{"/v2/images/app/manifests/1.0", "/v2/nowhere/app/manifests/1.0"} {
			request := member.request(http.MethodGet, path, "")
			request.SetBasicAuth("member@example.com", "membersecret1")
			body := expectStatus(t, instance.doAnonymously(request), http.StatusNotFound)
			if !strings.Contains(body, docker.ErrorNameUnknown) {
				t.Errorf("%s = %s, want %s", path, body, docker.ErrorNameUnknown)
			}
		}
	})

	t.Run("a renamed repository redirects only for a reader", func(t *testing.T) {
		expectStatus(t, anonymous(http.MethodGet, "/repository/maven-releases/"), http.StatusUnauthorized)

		response := instance.maven(http.MethodGet, "/repository/maven-releases/", "", true)
		expectStatus(t, response, http.StatusMovedPermanently)
		if location := response.Header.Get("Location"); location != "/repository/releases/" {
			t.Fatalf("Location = %q", location)
		}
	})

	t.Run("a reader who may not write is told so", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPut, "/api/repositories/releases/grants",
			`{"grants":[{"userId":"`+memberID(t, instance)+`","permission":"read"}]}`), http.StatusNoContent)

		request := member.request(http.MethodPut, "/repository/releases/com/example/app/2.0.0/app-2.0.0.jar", "bytes")
		request.SetBasicAuth("member@example.com", "membersecret1")
		expectStatus(t, instance.doAnonymously(request), http.StatusForbidden)
	})
}

func memberID(t *testing.T, instance *testInstance) string {
	t.Helper()

	body := expectStatus(t, instance.api(http.MethodGet, "/api/users", ""), http.StatusOK)
	var payload struct {
		Users []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	for _, user := range payload.Users {
		if user.Email == "member@example.com" {
			return user.ID
		}
	}
	t.Fatal("the member is missing from the user list")
	return ""
}

func TestRemoteDetailsAreOnlyShownToAdministrators(t *testing.T) {
	instance, _ := newProxyInstance(t, map[string]string{}, `,"remoteUsername":"ci","remotePassword":"secret"`)

	admin := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/central", ""), http.StatusOK)
	if !strings.Contains(admin, `"remoteUsername":"ci"`) {
		t.Fatalf("an administrator must see the remote: %s", admin)
	}

	for _, path := range []string{"/api/repositories/central", "/api/repositories"} {
		body := expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, path, "")), http.StatusOK)
		if strings.Contains(body, `"remoteUsername":"ci"`) || strings.Contains(body, `"remoteUrl":"http`) {
			t.Fatalf("%s showed a reader the remote: %s", path, body)
		}
	}
}

func TestDockerProxyRefusesContentThatDoesNotMatchItsDigest(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	served := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		served++
		writer.Write([]byte("not what was asked for"))
	}))
	t.Cleanup(upstream.Close)

	proxy := instance.proxyOf("hub", upstream.URL)
	wanted := digestOf([]byte("the real layer"))

	for attempt := 0; attempt < 2; attempt++ {
		expectStatus(t, proxy.do(http.MethodGet, "/v2/hub/nginx/blobs/"+wanted, nil, ""), http.StatusBadGateway)
	}
	if served != 2 {
		t.Fatalf("upstream was asked %d times, want 2: a mismatch must not be cached", served)
	}
	if instance.cachedFiles() != 0 {
		t.Fatalf("%d files were cached from a mismatched fetch", instance.cachedFiles())
	}
}

func TestDockerSweepSparesAFreshPush(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	registry := instance.registry("images")

	config := []byte(testImageConfig)
	layer := registry.pushBlob("team/api", bytes.Repeat([]byte("fresh"), 32))
	manifest := imageManifestFor(digestOf(config), len(config), map[string]int{layer: 160})
	registry.pushBlob("team/api", config)
	expectStatus(t, registry.pushManifest("team/api", "1.0", manifest), http.StatusCreated)
	expectStatus(t, registry.do(http.MethodDelete, "/v2/images/team/api/manifests/1.0", nil, ""), http.StatusAccepted)

	// With the grace period in force, content pushed a moment ago is left alone
	// even though nothing references it: the manifest that will may still be on
	// its way.
	body := expectStatus(t, instance.api(http.MethodPost, "/api/admin/docker/sweep", ""), http.StatusOK)
	if strings.Contains(body, `"blobs":1`) || strings.Contains(body, `"blobs":2`) || strings.Contains(body, `"manifests":1`) {
		t.Fatalf("a sweep reclaimed a push in flight: %s", body)
	}
	expectStatus(t, registry.do(http.MethodGet, "/v2/images/team/api/blobs/"+layer, nil, ""), http.StatusOK)

	blobs, manifests := instance.sweepOf(instance.sweep(http.MethodPost), "images")
	if blobs != 2 || manifests != 1 {
		t.Fatalf("once settled the sweep removed %d blobs and %d manifests, want 2 and 1", blobs, manifests)
	}
}

func TestPasswordGuessingIsThrottled(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	login := func(password string) *http.Response {
		return instance.doAnonymously(instance.request(http.MethodPost, "/api/auth/login",
			`{"email":"`+administratorEmail+`","password":"`+password+`"}`))
	}

	for attempt := 0; attempt < 20; attempt++ {
		expectStatus(t, login("wrong-password"), http.StatusUnauthorized)
	}

	response := login("wrong-password")
	expectStatus(t, response, http.StatusTooManyRequests)
	if response.Header.Get("Retry-After") == "" {
		t.Fatal("a throttled login must say how long to wait")
	}

	// The right password is refused too while the account is throttled, and
	// Basic credentials on a repository request draw on the same budget.
	expectStatus(t, login(administratorPassword), http.StatusTooManyRequests)
	expectStatus(t, instance.maven(http.MethodGet, "/repository/maven-releases/", "", true), http.StatusUnauthorized)

	// A session that already exists is unaffected.
	expectStatus(t, instance.api(http.MethodGet, "/api/auth/me", ""), http.StatusOK)
}

func TestUnknownAndWrongCredentialsAreIndistinguishable(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	unknown := expectStatus(t, instance.doAnonymously(instance.request(http.MethodPost, "/api/auth/login",
		`{"email":"nobody@example.com","password":"whatever123"}`)), http.StatusUnauthorized)
	wrong := expectStatus(t, instance.doAnonymously(instance.request(http.MethodPost, "/api/auth/login",
		`{"email":"`+administratorEmail+`","password":"whatever123"}`)), http.StatusUnauthorized)
	if unknown != wrong {
		t.Fatalf("responses differ:\n%s\n%s", unknown, wrong)
	}
}

func TestConfiguredBaseURLWinsOverForwardedHeaders(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/maven-releases/links",
		`{"slug":"app","artifact":"app","pattern":"*.jar"}`), http.StatusCreated)

	t.Run("without one the forwarded host is used", func(t *testing.T) {
		request := instance.request(http.MethodGet, "/api/repositories/maven-releases/links", "")
		request.Header.Set("X-Forwarded-Host", "attacker.example")
		request.Header.Set("X-Forwarded-Proto", "https")
		body := expectStatus(t, instance.do(request), http.StatusOK)
		if !strings.Contains(body, `"url":"https://attacker.example/download/app"`) {
			t.Fatalf("body = %s", body)
		}
	})

	t.Run("a configured base is used as is", func(t *testing.T) {
		instance.app.config.BaseURL = "https://artifacts.example.com"

		request := instance.request(http.MethodGet, "/api/repositories/maven-releases/links", "")
		request.Header.Set("X-Forwarded-Host", "attacker.example")
		body := expectStatus(t, instance.do(request), http.StatusOK)
		if !strings.Contains(body, `"url":"https://artifacts.example.com/download/app"`) {
			t.Fatalf("body = %s", body)
		}
	})
}

func TestPackumentsAreNotSharedCacheable(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	instance.publishNPM("greeter", "1.0.0", "content")

	response := instance.npm(http.MethodGet, "greeter", "", true)
	expectStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Cache-Control"); !strings.Contains(got, "private") {
		t.Fatalf("Cache-Control = %q, want private", got)
	}
	if got := strings.Join(response.Header.Values("Vary"), ", "); !strings.Contains(got, "X-Forwarded-Host") {
		t.Fatalf("Vary = %q", got)
	}
}
