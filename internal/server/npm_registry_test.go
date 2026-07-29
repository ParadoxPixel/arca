package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type searchResponse struct {
	Total   int `json:"total"`
	Objects []struct {
		Package struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"package"`
	} `json:"objects"`
}

func (i *testInstance) search(query string) searchResponse {
	i.t.Helper()

	body := expectStatus(i.t, i.npm(http.MethodGet, "-/v1/search?"+query, "", true), http.StatusOK)

	var decoded searchResponse
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		i.t.Fatalf("decoding a search response: %v (%s)", err, body)
	}
	return decoded
}

func (r searchResponse) names() []string {
	names := make([]string, 0, len(r.Objects))
	for _, object := range r.Objects {
		names = append(names, object.Package.Name)
	}
	return names
}

func newNPMInstance(t *testing.T) *testInstance {
	t.Helper()

	instance := newTestInstance(t)
	instance.setup()
	instance.createNPMRepository(npmRepository, "")
	return instance
}

func TestNPMSearch(t *testing.T) {
	instance := newNPMInstance(t)
	instance.publishNPM("express", "4.18.2", "express-bytes")
	instance.publishNPM("@types/node", "20.1.0", "types-node-bytes")
	instance.publishNPM("@types/react", "18.0.0", "types-react-bytes")

	testCases := []struct {
		name  string
		query string
		want  []string
	}{
		{"a bare name", "text=express", []string{"express"}},
		{"a scope", "text=types", []string{"@types/react", "@types/node"}},
		{"a scoped name", "text=react", []string{"@types/react"}},
		{"no matches", "text=nothing-here", []string{}},
		{"everything", "text=", []string{"@types/react", "@types/node", "express"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := instance.search(testCase.query).names()
			if strings.Join(got, ",") != strings.Join(testCase.want, ",") {
				t.Fatalf("names = %v, want %v", got, testCase.want)
			}
		})
	}

	t.Run("size caps the results", func(t *testing.T) {
		response := instance.search("text=&size=1")
		if len(response.Objects) != 1 || response.Total != 1 {
			t.Fatalf("returned %d objects (total %d), want 1", len(response.Objects), response.Total)
		}
	})

	t.Run("the latest version is reported", func(t *testing.T) {
		instance.publishNPM("express", "5.0.0", "express-5-bytes")

		response := instance.search("text=express")
		if len(response.Objects) != 1 {
			t.Fatalf("returned %d objects, want 1", len(response.Objects))
		}
		if got := response.Objects[0].Package.Version; got != "5.0.0" {
			t.Fatalf("version = %q, want 5.0.0", got)
		}
	})
}

// The npm CLI's result formatter dereferences these without checking, so a
// missing one crashes "npm search" rather than degrading it.
func TestNPMSearchCarriesTheFieldsTheCLIDereferences(t *testing.T) {
	instance := newNPMInstance(t)
	instance.publishNPM("express", "4.18.2", "bytes")

	body := expectStatus(t, instance.npm(http.MethodGet, "-/v1/search?text=express", "", true), http.StatusOK)

	var decoded struct {
		Objects []struct {
			Package map[string]json.RawMessage `json:"package"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding a search response: %v (%s)", err, body)
	}
	if len(decoded.Objects) != 1 {
		t.Fatalf("returned %d objects, want 1", len(decoded.Objects))
	}

	for _, field := range []string{"name", "version", "description", "keywords", "maintainers", "date"} {
		t.Run(field, func(t *testing.T) {
			if _, present := decoded.Objects[0].Package[field]; !present {
				t.Fatalf("the search result has no %q: %s", field, body)
			}
		})
	}
}

func TestNPMSearchIsScopedToTheRepository(t *testing.T) {
	instance := newNPMInstance(t)
	instance.publishNPM("express", "4.18.2", "express-bytes")

	instance.createNPMRepository("npm-other", "")
	body := publishBody("elsewhere", "1.0.0", "other-bytes", "")
	expectStatus(t, instance.do(func() *http.Request {
		request := instance.request(http.MethodPut, "/repository/npm-other/elsewhere", body)
		request.SetBasicAuth(administratorEmail, administratorPassword)
		return request
	}()), http.StatusCreated)

	if got := instance.search("text=").names(); strings.Join(got, ",") != "express" {
		t.Fatalf("names = %v, want only express", got)
	}
}

type tokenList struct {
	Total   int `json:"total"`
	Objects []struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	} `json:"objects"`
}

func TestNPMTokens(t *testing.T) {
	instance := newNPMInstance(t)

	created := struct {
		Key   string `json:"key"`
		Token string `json:"token"`
	}{}

	t.Run("create", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodPost, "-/npm/v1/tokens",
			fmt.Sprintf(`{"password":%q}`, administratorPassword), true), http.StatusCreated)

		if err := json.Unmarshal([]byte(body), &created); err != nil {
			t.Fatalf("decoding a created token: %v (%s)", err, body)
		}
		if created.Key == "" || created.Token == "" {
			t.Fatalf("created token = %+v, want a key and a secret", created)
		}
	})

	t.Run("the secret authenticates a publish", func(t *testing.T) {
		request := instance.request(http.MethodPut, "/repository/"+npmRepository+"/express",
			publishBody("express", "4.18.2", "bytes", ""))
		request.Header.Set("Authorization", "Bearer "+created.Token)
		expectStatus(t, instance.doAnonymously(request), http.StatusCreated)
	})

	t.Run("list", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "-/npm/v1/tokens", "", true), http.StatusOK)

		var listed tokenList
		if err := json.Unmarshal([]byte(body), &listed); err != nil {
			t.Fatalf("decoding a token list: %v (%s)", err, body)
		}
		if listed.Total != 1 || listed.Objects[0].Key != created.Key {
			t.Fatalf("listed %+v, want the created token", listed)
		}
		if strings.Contains(body, created.Token) {
			t.Fatalf("the list leaked the token secret: %s", body)
		}
	})

	t.Run("a wrong password is rejected", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPost, "-/npm/v1/tokens",
			`{"password":"not-the-password"}`, true), http.StatusUnauthorized)
	})

	t.Run("revoke", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodDelete, "-/npm/v1/tokens/token/"+created.Key, "", true),
			http.StatusNoContent)
		expectStatus(t, instance.npm(http.MethodDelete, "-/npm/v1/tokens/token/"+created.Key, "", true),
			http.StatusNotFound)
	})

	t.Run("the revoked secret no longer authenticates", func(t *testing.T) {
		request := instance.request(http.MethodPut, "/repository/"+npmRepository+"/lodash",
			publishBody("lodash", "1.0.0", "bytes", ""))
		request.Header.Set("Authorization", "Bearer "+created.Token)
		expectStatus(t, instance.doAnonymously(request), http.StatusUnauthorized)
	})
}

// deprecateBody is the attachment-free packument npm PUTs for "npm deprecate".
func deprecateBody(pkg string, messages map[string]string) string {
	versions := make([]string, 0, len(messages))
	for version, message := range messages {
		versions = append(versions, fmt.Sprintf(`%q:{"name":%q,"version":%q,"deprecated":%q}`,
			version, pkg, version, message))
	}
	return fmt.Sprintf(`{"_id":%q,"name":%q,"versions":{%s}}`, pkg, pkg, strings.Join(versions, ","))
}

func deprecationOf(t *testing.T, body string, version string) (string, bool) {
	t.Helper()

	var decoded struct {
		Versions map[string]struct {
			Deprecated *string `json:"deprecated"`
		} `json:"versions"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding a packument: %v (%s)", err, body)
	}

	entry, ok := decoded.Versions[version]
	if !ok || entry.Deprecated == nil {
		return "", false
	}
	return *entry.Deprecated, true
}

func TestNPMDeprecate(t *testing.T) {
	instance := newNPMInstance(t)
	instance.publishNPM("express", "4.18.2", "old-bytes")
	instance.publishNPM("express", "5.0.0", "new-bytes")

	const message = "no longer maintained, use 5.x"

	t.Run("marking a version", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPut, "express",
			deprecateBody("express", map[string]string{"4.18.2": message}), true), http.StatusOK)

		body := expectStatus(t, instance.npm(http.MethodGet, "express", "", true), http.StatusOK)

		got, marked := deprecationOf(t, body, "4.18.2")
		if !marked || got != message {
			t.Fatalf("deprecated = %q (marked %v), want %q", got, marked, message)
		}
	})

	t.Run("other versions are untouched", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "express", "", true), http.StatusOK)
		if _, marked := deprecationOf(t, body, "5.0.0"); marked {
			t.Fatal("5.0.0 was deprecated too")
		}
	})

	t.Run("the tarball still resolves", func(t *testing.T) {
		body := expectStatus(t, instance.npm(http.MethodGet, "express/-/express-4.18.2.tgz", "", true), http.StatusOK)
		if body != "old-bytes" {
			t.Fatalf("tarball = %q, want the published bytes", body)
		}
	})

	t.Run("an empty message lifts the deprecation", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPut, "express",
			deprecateBody("express", map[string]string{"4.18.2": ""}), true), http.StatusOK)

		body := expectStatus(t, instance.npm(http.MethodGet, "express", "", true), http.StatusOK)
		if _, marked := deprecationOf(t, body, "4.18.2"); marked {
			t.Fatal("the deprecation survived an empty message")
		}
	})

	t.Run("an unknown version changes nothing", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPut, "express",
			deprecateBody("express", map[string]string{"9.9.9": message}), true), http.StatusNotFound)
	})

	t.Run("an unknown package is missing", func(t *testing.T) {
		expectStatus(t, instance.npm(http.MethodPut, "absent",
			deprecateBody("absent", map[string]string{"1.0.0": message}), true), http.StatusNotFound)
	})
}
