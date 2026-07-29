package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const pngPayload = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01"

const svgPayload = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`

func logoURLOf(t *testing.T, body string) string {
	t.Helper()

	var decoded struct {
		LogoURL string `json:"logoUrl"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding a logo url: %v (%s)", err, body)
	}
	return decoded.LogoURL
}

func TestLogoUploadAndRemoval(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	t.Run("there is no logo to begin with", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodGet, "/api/branding/logo", ""), http.StatusNotFound)

		body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
		if url := logoURLOf(t, body); url != "" {
			t.Fatalf("logoUrl = %q, want it empty", url)
		}
	})

	t.Run("uploading one reports where it lives", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodPut, "/api/admin/branding/logo", pngPayload),
			http.StatusOK)
		if url := logoURLOf(t, body); !strings.HasPrefix(url, "/api/branding/logo?v=") {
			t.Fatalf("logoUrl = %q, want a versioned url", url)
		}
	})

	t.Run("the uploaded bytes come back", func(t *testing.T) {
		response := instance.doAnonymously(instance.request(http.MethodGet, "/api/branding/logo", ""))
		contentType := response.Header.Get("Content-Type")
		body := expectStatus(t, response, http.StatusOK)

		if contentType != "image/png" {
			t.Fatalf("content type = %q, want image/png", contentType)
		}
		if body != pngPayload {
			t.Fatal("the served logo does not match what was uploaded")
		}
	})

	t.Run("bootstrap points at it", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
		if url := logoURLOf(t, body); url == "" {
			t.Fatal("bootstrap reports no logo after an upload")
		}
	})

	t.Run("removing it takes the instance back to the default mark", func(t *testing.T) {
		expectStatus(t, instance.api(http.MethodDelete, "/api/admin/branding/logo", ""),
			http.StatusNoContent)
		expectStatus(t, instance.api(http.MethodGet, "/api/branding/logo", ""), http.StatusNotFound)

		body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
		if url := logoURLOf(t, body); url != "" {
			t.Fatalf("logoUrl = %q, want it empty", url)
		}
	})
}

func TestLogoAcceptsTheFormatsItAdvertises(t *testing.T) {
	testCases := []struct {
		name    string
		payload string
		want    string
	}{
		{"a png", pngPayload, "image/png"},
		{"a gif", "GIF89a\x01\x00\x01\x00\x00\x00\x00", "image/gif"},
		{"an svg", svgPayload, "image/svg+xml"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			instance := newTestInstance(t)
			instance.setup()

			expectStatus(t, instance.api(http.MethodPut, "/api/admin/branding/logo", testCase.payload),
				http.StatusOK)

			response := instance.api(http.MethodGet, "/api/branding/logo", "")
			contentType := response.Header.Get("Content-Type")
			expectStatus(t, response, http.StatusOK)

			if contentType != testCase.want {
				t.Fatalf("content type = %q, want %q", contentType, testCase.want)
			}
		})
	}
}

func TestLogoRejectsWhatIsNotAnImage(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	testCases := []struct {
		name    string
		payload string
		want    int
	}{
		{"plain text", "not an image at all", http.StatusBadRequest},
		{"a script pretending to be a logo", "<script>alert(1)</script>", http.StatusBadRequest},
		{"nothing at all", "", http.StatusBadRequest},
		{"more than the size limit", strings.Repeat("a", maximumLogoBytes+1), http.StatusRequestEntityTooLarge},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPut, "/api/admin/branding/logo", testCase.payload),
				testCase.want)
		})
	}
}

func TestLogoChangesNeedAnAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	testCases := []struct {
		name    string
		method  string
		payload string
	}{
		{"upload", http.MethodPut, pngPayload},
		{"removal", http.MethodDelete, ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := instance.request(testCase.method, "/api/admin/branding/logo", testCase.payload)
			expectStatus(t, instance.doAnonymously(request), http.StatusUnauthorized)
		})
	}
}

func TestLogoIsServedWithoutScriptingRights(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	expectStatus(t, instance.api(http.MethodPut, "/api/admin/branding/logo", svgPayload), http.StatusOK)

	response := instance.api(http.MethodGet, "/api/branding/logo", "")
	header := response.Header.Clone()
	expectStatus(t, response, http.StatusOK)

	if policy := header.Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("Content-Security-Policy = %q, want an SVG that cannot run scripts", policy)
	}
	if options := header.Get("X-Content-Type-Options"); options != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", options)
	}
}
