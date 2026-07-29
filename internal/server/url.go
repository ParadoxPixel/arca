package server

import "net/http"

// publicBase reconstructs the URL the client used, which is what npm tarball
// links have to point at. Deriving it per request keeps one instance reachable
// under several hostnames without a configured base URL to get wrong.
func publicBase(request *http.Request) string {
	scheme := "http"
	if request.TLS != nil || request.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	host := request.Host
	if forwarded := request.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}
