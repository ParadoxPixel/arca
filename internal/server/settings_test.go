package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func brandingOf(t *testing.T, body string) (instanceName string, themeColor string) {
	t.Helper()

	var decoded struct {
		InstanceName string `json:"instanceName"`
		ThemeColor   string `json:"themeColor"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding branding: %v (%s)", err, body)
	}
	return decoded.InstanceName, decoded.ThemeColor
}

func settingsOf(t *testing.T, body string) map[string]string {
	t.Helper()

	decoded := map[string]string{}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decoding settings: %v (%s)", err, body)
	}
	return decoded
}

func TestSettingsDefaultsAndUpdates(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	t.Run("the theme color starts at its default", func(t *testing.T) {
		settings := settingsOf(t, expectStatus(t, instance.api(http.MethodGet, "/api/admin/settings", ""), http.StatusOK))
		if settings["themeColor"] != "#4f46e5" {
			t.Fatalf("themeColor = %q, want the default", settings["themeColor"])
		}
		if settings["instanceName"] != "test-nexus" {
			t.Fatalf("instanceName = %q, want test-nexus", settings["instanceName"])
		}
	})

	t.Run("both fields can be changed", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodPatch, "/api/admin/settings",
			`{"instanceName":"renamed","themeColor":"#FF8800"}`), http.StatusOK)

		settings := settingsOf(t, body)
		if settings["instanceName"] != "renamed" {
			t.Fatalf("instanceName = %q, want renamed", settings["instanceName"])
		}
		// Hex is normalised so the value can be interpolated into CSS as-is.
		if settings["themeColor"] != "#ff8800" {
			t.Fatalf("themeColor = %q, want #ff8800", settings["themeColor"])
		}
	})

	t.Run("bootstrap reports the change", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
		if name, color := brandingOf(t, body); name != "renamed" || color != "#ff8800" {
			t.Fatalf("bootstrap = %s", body)
		}
	})

	t.Run("one field can be changed alone", func(t *testing.T) {
		body := expectStatus(t, instance.api(http.MethodPatch, "/api/admin/settings",
			`{"themeColor":"#123456"}`), http.StatusOK)

		settings := settingsOf(t, body)
		if settings["instanceName"] != "renamed" {
			t.Fatalf("instanceName = %q, want it untouched", settings["instanceName"])
		}
		if settings["themeColor"] != "#123456" {
			t.Fatalf("themeColor = %q, want #123456", settings["themeColor"])
		}
	})
}

func TestSettingsRejectsBadInput(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	testCases := []struct {
		name string
		body string
	}{
		{"a colour with no hash", `{"themeColor":"4f46e5"}`},
		{"a three digit colour", `{"themeColor":"#fff"}`},
		{"a named colour", `{"themeColor":"rebeccapurple"}`},
		{"a css injection", `{"themeColor":"#fff;background:url(x)"}`},
		{"an empty instance name", `{"instanceName":"   "}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, instance.api(http.MethodPatch, "/api/admin/settings", testCase.body),
				http.StatusBadRequest)
		})
	}
}

func TestSettingsNeedAnAdministrator(t *testing.T) {
	instance := newTestInstance(t)
	instance.setup()

	testCases := []struct {
		name   string
		method string
		body   string
	}{
		{"read", http.MethodGet, ""},
		{"write", http.MethodPatch, `{"themeColor":"#123456"}`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := instance.request(testCase.method, "/api/admin/settings", testCase.body)
			expectStatus(t, instance.doAnonymously(request), http.StatusUnauthorized)
		})
	}
}

func TestSetupAcceptsAThemeColor(t *testing.T) {
	instance := newTestInstance(t)

	expectStatus(t, instance.api(http.MethodPost, "/api/setup", `{
		"instanceName": "coloured",
		"themeColor": "#00AA55",
		"name": "Admin",
		"email": "`+administratorEmail+`",
		"password": "`+administratorPassword+`",
		"seedRepositories": false
	}`), http.StatusCreated)

	body := expectStatus(t, instance.api(http.MethodGet, "/api/bootstrap", ""), http.StatusOK)
	if _, color := brandingOf(t, body); color != "#00aa55" {
		t.Fatalf("bootstrap = %s", body)
	}
}
