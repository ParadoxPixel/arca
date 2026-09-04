package nexus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDownloadStaysOnTheConfiguredHost(t *testing.T) {
	var seen []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = append(seen, "elsewhere "+request.Header.Get("Authorization"))
		writer.Write([]byte("leaked"))
	}))
	t.Cleanup(elsewhere.Close)

	nexus := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = append(seen, "nexus "+request.URL.Path)
		switch request.URL.Path {
		case "/repository/files/a.jar":
			writer.Write([]byte("bytes"))
		case "/repository/files/redirected.jar":
			http.Redirect(writer, request, elsewhere.URL+"/a.jar", http.StatusFound)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(nexus.Close)

	client, err := New(nexus.URL, "admin", "secret", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("an asset on the configured host downloads", func(t *testing.T) {
		body, err := client.Download(context.Background(), Asset{Path: "a.jar", DownloadURL: nexus.URL + "/repository/files/a.jar"})
		if err != nil {
			t.Fatal(err)
		}
		body.Close()
	})

	t.Run("a download URL on another host is refused before any request", func(t *testing.T) {
		before := len(seen)
		_, err := client.Download(context.Background(), Asset{Path: "b.jar", DownloadURL: elsewhere.URL + "/b.jar"})
		if err == nil || !strings.Contains(err.Error(), "not the configured host") {
			t.Fatalf("err = %v", err)
		}
		if len(seen) != before {
			t.Fatalf("a request was made: %v", seen[before:])
		}
	})

	t.Run("a redirect off the host is not followed", func(t *testing.T) {
		_, err := client.Download(context.Background(), Asset{Path: "redirected.jar", DownloadURL: nexus.URL + "/repository/files/redirected.jar"})
		if err == nil {
			t.Fatal("expected the redirect to be refused")
		}
		for _, entry := range seen {
			if strings.HasPrefix(entry, "elsewhere") {
				t.Fatalf("the other host was contacted: %q", entry)
			}
		}
	})
}
