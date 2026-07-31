package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// publish puts one jar into a hosted Maven repository and returns the path it
// was stored at.
func publish(t *testing.T, instance *testInstance, repository, version string) string {
	t.Helper()

	path := "/repository/" + repository + "/com/example/app/" + version + "/app-" + version + ".jar"
	expectStatus(t, instance.maven(http.MethodPut, path, "payload-"+version, true), http.StatusCreated)
	return path
}

func TestRenamingARepositoryLeavesTheOldNameRedirecting(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"internal","visibility":"public"}`), http.StatusCreated)
	publish(t, instance, "internal", "1.0.0")

	body := expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/internal", `{"name":"pixelib-internal"}`), http.StatusOK)
	if !strings.Contains(body, `"name":"pixelib-internal"`) {
		t.Fatalf("the response did not carry the new name: %s", body)
	}
	if !strings.Contains(body, `"aliases":["internal"]`) {
		t.Fatalf("the response did not list the old name as an alias: %s", body)
	}

	t.Run("the new name serves the artifact", func(t *testing.T) {
		body := expectStatus(t, instance.maven(http.MethodGet, "/repository/pixelib-internal/com/example/app/1.0.0/app-1.0.0.jar", "", true), http.StatusOK)
		if body != "payload-1.0.0" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("the old name redirects to it", func(t *testing.T) {
		response := instance.maven(http.MethodGet, "/repository/internal/com/example/app/1.0.0/app-1.0.0.jar", "", true)
		expectStatus(t, response, http.StatusMovedPermanently)

		want := "/repository/pixelib-internal/com/example/app/1.0.0/app-1.0.0.jar"
		if location := response.Header.Get("Location"); location != want {
			t.Fatalf("Location = %q, want %q", location, want)
		}
	})

	t.Run("an upload to the old name keeps its method", func(t *testing.T) {
		response := instance.maven(http.MethodPut, "/repository/internal/com/example/app/2.0.0/app-2.0.0.jar", "payload", true)
		expectStatus(t, response, http.StatusPermanentRedirect)
	})

	t.Run("the API answers under either name", func(t *testing.T) {
		for _, name := range []string{"internal", "pixelib-internal"} {
			body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/"+name, ""), http.StatusOK)
			if !strings.Contains(body, `"name":"pixelib-internal"`) {
				t.Fatalf("%s did not resolve to the current name: %s", name, body)
			}
		}
	})
}

func TestReusingAFormerNameEndsTheRedirect(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"internal"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/internal", `{"name":"renamed"}`), http.StatusOK)

	// The new repository takes the name over rather than being shadowed by the
	// redirect that still points at the old one.
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"internal","description":"the second one"}`), http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/internal", ""), http.StatusOK)
	if !strings.Contains(body, `"description":"the second one"`) {
		t.Fatalf("the reused name did not resolve to the new repository: %s", body)
	}

	body = expectStatus(t, instance.api(http.MethodGet, "/api/repositories/renamed", ""), http.StatusOK)
	if !strings.Contains(body, `"aliases":[]`) {
		t.Fatalf("the renamed repository kept an alias it no longer owns: %s", body)
	}
}

func TestRenameValidation(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"source"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"taken"}`), http.StatusCreated)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"a name another repository holds", `{"name":"taken"}`, http.StatusConflict},
		{"an invalid name", `{"name":"Not A Name"}`, http.StatusBadRequest},
		{"an empty name", `{"name":"  "}`, http.StatusBadRequest},
		{"the name it already has", `{"name":"source"}`, http.StatusBadRequest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/source", testCase.body), testCase.want)
		})
	}
}

func TestAliasesAreManagedByHand(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"current"}`), http.StatusCreated)

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/current/aliases", `{"name":"legacy"}`), http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/current/aliases", ""), http.StatusOK)
	var payload struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding aliases: %v", err)
	}
	if len(payload.Aliases) != 1 || payload.Aliases[0].Name != "legacy" {
		t.Fatalf("aliases = %v, want one named legacy", payload.Aliases)
	}

	t.Run("a name a repository holds is refused", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories/current/aliases", `{"name":"maven-releases"}`), http.StatusConflict)
	})

	t.Run("a repository cannot redirect to itself", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories/current/aliases", `{"name":"current"}`), http.StatusBadRequest)
	})

	t.Run("deleting one stops the redirect", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/current/aliases/legacy", ""), http.StatusNoContent)
		expectStatus(t, instance.api(http.MethodGet, "/api/repositories/legacy", ""), http.StatusNotFound)
		expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/current/aliases/legacy", ""), http.StatusNotFound)
	})
}

func TestDeletingARepositoryDropsItsAliases(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"temporary"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/temporary", `{"name":"moved"}`), http.StatusOK)
	expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/moved", ""), http.StatusNoContent)

	expectStatus(t, instance.maven(http.MethodGet, "/repository/temporary/", "", true), http.StatusNotFound)
}
