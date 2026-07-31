package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func decodeLink(t *testing.T, body string) struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	URL      string `json:"url"`
	Resolved *struct {
		Version  string `json:"version"`
		Filename string `json:"filename"`
	} `json:"resolved"`
} {
	t.Helper()

	var payload struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		URL      string `json:"url"`
		Resolved *struct {
			Version  string `json:"version"`
			Filename string `json:"filename"`
		} `json:"resolved"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decoding a link: %v (body: %s)", err, body)
	}
	return payload
}

// privateWithVersions is the shape the feature exists for: a private repository
// holding several releases of one artifact.
func privateWithVersions(t *testing.T, versions ...string) *testInstance {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"internal","visibility":"private","policy":"mixed"}`), http.StatusCreated)

	for _, version := range versions {
		publish(t, instance, "internal", version)
	}
	return instance
}

func TestDeploymentLinkServesTheLatestReleaseWithoutAuthentication(t *testing.T) {
	instance := privateWithVersions(t, "38", "39")

	body := expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", `{
		"slug":"pixelscript-latest",
		"namespace":"com.example",
		"artifact":"app",
		"pattern":"{name}-{version}.jar"
	}`), http.StatusCreated)

	created := decodeLink(t, body)
	if created.Resolved == nil || created.Resolved.Version != "39" {
		t.Fatalf("the link did not resolve to the newest version: %s", body)
	}
	if !strings.HasSuffix(created.URL, "/download/pixelscript-latest") {
		t.Fatalf("url = %q", created.URL)
	}

	t.Run("the repository itself still needs credentials", func(t *testing.T) {
		expectStatus(t, instance.maven(http.MethodGet, "/repository/internal/com/example/app/39/app-39.jar", "", false), http.StatusUnauthorized)
	})

	t.Run("the link does not", func(t *testing.T) {
		response := instance.doAnonymously(instance.request(http.MethodGet, "/download/pixelscript-latest", ""))
		payload := expectStatus(t, response, http.StatusOK)

		if payload != "payload-39" {
			t.Fatalf("body = %q, want the newest release", payload)
		}
		if version := response.Header.Get("X-Arca-Version"); version != "39" {
			t.Fatalf("X-Arca-Version = %q", version)
		}
		if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, `"app-39.jar"`) {
			t.Fatalf("Content-Disposition = %q", disposition)
		}
	})

	t.Run("a newer release moves the link", func(t *testing.T) {
		publish(t, instance, "internal", "40")

		response := instance.doAnonymously(instance.request(http.MethodGet, "/download/pixelscript-latest", ""))
		if payload := expectStatus(t, response, http.StatusOK); payload != "payload-40" {
			t.Fatalf("body = %q, want the release published since", payload)
		}
	})

	t.Run("following it is counted", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/internal/links", ""), http.StatusOK)
		if strings.Contains(body, `"downloads":0`) {
			t.Fatalf("downloads were not counted: %s", body)
		}
	})
}

func TestDeploymentLinkRenamesTheFileItServes(t *testing.T) {
	instance := privateWithVersions(t, "1.2.3")

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", `{
		"slug":"app-jar",
		"namespace":"com.example",
		"artifact":"app",
		"pattern":"{name}-{version}.jar",
		"filename":"app.jar"
	}`), http.StatusCreated)

	response := instance.doAnonymously(instance.request(http.MethodGet, "/download/app-jar", ""))
	expectStatus(t, response, http.StatusOK)

	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, `"app.jar"`) {
		t.Fatalf("Content-Disposition = %q, want the configured filename", disposition)
	}
}

func TestDeploymentLinkPolicySelectsWhichVersionsCount(t *testing.T) {
	instance := privateWithVersions(t, "1.0.0")

	// A snapshot is newer than the release but only eligible under a policy that
	// admits prereleases.
	expectStatus(t, instance.maven(http.MethodPut,
		"/repository/internal/com/example/app/2.0.0-SNAPSHOT/app-2.0.0-SNAPSHOT.jar", "payload-snapshot", true), http.StatusCreated)

	cases := []struct {
		policy string
		want   string
	}{
		{"release", "1.0.0"},
		{"mixed", "2.0.0-SNAPSHOT"},
		{"snapshot", "2.0.0-SNAPSHOT"},
	}

	body := expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", `{
		"slug":"by-policy",
		"namespace":"com.example",
		"artifact":"app",
		"pattern":"{name}-{version}.jar",
		"policy":"release"
	}`), http.StatusCreated)
	created := decodeLink(t, body)

	for _, testCase := range cases {
		t.Run(testCase.policy, func(t *testing.T) {
			body := expectStatus(t, instance.api(http.MethodPatch,
				"/api/repositories/internal/links/"+created.ID, `{"policy":"`+testCase.policy+`"}`), http.StatusOK)

			updated := decodeLink(t, body)
			if updated.Resolved == nil || updated.Resolved.Version != testCase.want {
				t.Fatalf("policy %q resolved to %v, want %s", testCase.policy, updated.Resolved, testCase.want)
			}
		})
	}
}

// A release that shipped without the file a link names should not break the
// link, it should keep serving the newest release that does have one.
func TestDeploymentLinkFallsBackToTheNewestMatchingVersion(t *testing.T) {
	instance := privateWithVersions(t, "1.0.0")

	expectStatus(t, instance.maven(http.MethodPut,
		"/repository/internal/com/example/app/2.0.0/app-2.0.0.pom", "just-a-pom", true), http.StatusCreated)

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", `{
		"slug":"app-latest",
		"namespace":"com.example",
		"artifact":"app",
		"pattern":"{name}-{version}.jar"
	}`), http.StatusCreated)

	response := instance.doAnonymously(instance.request(http.MethodGet, "/download/app-latest", ""))
	expectStatus(t, response, http.StatusOK)

	if version := response.Header.Get("X-Arca-Version"); version != "1.0.0" {
		t.Fatalf("X-Arca-Version = %q, want the newest version that has a jar", version)
	}
}

func TestDeploymentLinkValidationAndLifecycle(t *testing.T) {
	instance := privateWithVersions(t, "1.0.0")

	cases := []struct {
		name string
		body string
		want int
	}{
		{"no artifact", `{"pattern":"*.jar"}`, http.StatusBadRequest},
		{"no pattern", `{"artifact":"app"}`, http.StatusBadRequest},
		{"a pattern with a path", `{"artifact":"app","pattern":"a/b.jar"}`, http.StatusBadRequest},
		{"an unknown placeholder", `{"artifact":"app","pattern":"{revision}.jar"}`, http.StatusBadRequest},
		{"an unparseable regex", `{"artifact":"app","matchType":"regex","pattern":"app-(\\d+"}`, http.StatusBadRequest},
		{"an unknown match type", `{"artifact":"app","matchType":"glob","pattern":"*.jar"}`, http.StatusBadRequest},
		{"a filename with a path", `{"artifact":"app","pattern":"*.jar","filename":"a/b.jar"}`, http.StatusBadRequest},
		{"an invalid slug", `{"artifact":"app","pattern":"*.jar","slug":"Not A Slug"}`, http.StatusBadRequest},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", testCase.body), testCase.want)
		})
	}

	t.Run("a slug is derived from the artifact when it is left out", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links",
			`{"namespace":"com.example","artifact":"app","pattern":"{name}-{version}.jar"}`), http.StatusCreated)

		if slug := decodeLink(t, body).Slug; slug != "app-latest" {
			t.Fatalf("slug = %q, want app-latest", slug)
		}
	})

	t.Run("slugs are unique across the instance", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links",
			`{"artifact":"app","pattern":"*.jar","slug":"app-latest"}`), http.StatusConflict)
	})

	t.Run("a disabled link stops serving", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/repositories/internal/links", ""), http.StatusOK)
		var payload struct {
			Links []struct {
				ID string `json:"id"`
			} `json:"links"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decoding links: %v", err)
		}

		identifier := payload.Links[0].ID
		expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/internal/links/"+identifier, `{"disabled":true}`), http.StatusOK)
		expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/download/app-latest", "")), http.StatusNotFound)

		expectStatus(t, instance.api(http.MethodPatch, "/api/repositories/internal/links/"+identifier, `{"disabled":false}`), http.StatusOK)
		expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/download/app-latest", "")), http.StatusOK)

		expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/internal/links/"+identifier, ""), http.StatusNoContent)
		expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/download/app-latest", "")), http.StatusNotFound)
	})

	t.Run("an unknown slug is a plain not found", func(t *testing.T) {
		expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/download/nothing-here", "")), http.StatusNotFound)
	})
}

func TestDeploymentLinksAreRefusedWhereTheyCannotMeanAnything(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"images","format":"docker"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories", `{"name":"site","format":"p2"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodPost, "/api/repositories",
		`{"name":"sites","format":"p2","type":"group","members":["site"]}`), http.StatusCreated)

	for _, repository := range []string{"images", "sites"} {
		t.Run(repository, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPost, "/api/repositories/"+repository+"/links",
				`{"artifact":"app","pattern":"*.jar"}`), http.StatusBadRequest)
		})
	}
}

func TestDeploymentLinkNeedsAdministratorAccessToManage(t *testing.T) {
	instance := privateWithVersions(t, "1.0.0")

	request := instance.request(http.MethodPost, "/api/repositories/internal/links",
		`{"artifact":"app","pattern":"*.jar"}`)
	expectStatus(t, instance.doAnonymously(request), http.StatusUnauthorized)
}

func TestDeploymentLinkMatchesWithARegex(t *testing.T) {
	instance := privateWithVersions(t, "39")

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links", `{
		"slug":"any-jar",
		"namespace":"com.example",
		"artifact":"app",
		"matchType":"regex",
		"pattern":"app-\\d+\\.jar"
	}`), http.StatusCreated)

	response := instance.doAnonymously(instance.request(http.MethodGet, "/download/any-jar", ""))
	if body := expectStatus(t, response, http.StatusOK); body != "payload-39" {
		t.Fatalf("body = %q", body)
	}
}

func TestDeploymentLinksAreRemovedWithTheirRepository(t *testing.T) {
	instance := privateWithVersions(t, "1.0.0")

	expectStatus(t, instance.api(http.MethodPost, "/api/repositories/internal/links",
		`{"slug":"gone-soon","namespace":"com.example","artifact":"app","pattern":"*.jar"}`), http.StatusCreated)
	expectStatus(t, instance.api(http.MethodDelete, "/api/repositories/internal", ""), http.StatusNoContent)

	expectStatus(t, instance.doAnonymously(instance.request(http.MethodGet, "/download/gone-soon", "")), http.StatusNotFound)
}
