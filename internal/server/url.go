package server

import (
	"net/http"
	"strings"
)

// publicBase is the URL clients reach this server at, which is what npm
// tarball links and deployment link URLs have to be built on. A configured
// base wins; without one it is reconstructed from the request, which relies on
// the proxy in front overwriting X-Forwarded-Host rather than passing on what
// a client sent. Everything derived from it is marked uncacheable by a shared
// cache for that reason.
func (s *Server) publicBase(request *http.Request) string {
	if s.config.BaseURL != "" {
		return s.config.BaseURL
	}
	return requestBase(request)
}

func requestBase(request *http.Request) string {
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

// publicHosts lists every hostname a same-origin request may carry, for
// checking a browser's Origin header against: the configured base, the host
// the request arrived with, and the one the proxy in front forwarded.
func (s *Server) publicHosts(request *http.Request) []string {
	hosts := []string{request.Host}
	if forwarded := request.Header.Get("X-Forwarded-Host"); forwarded != "" {
		hosts = append(hosts, forwarded)
	}
	if s.config.BaseURL != "" {
		base := strings.TrimPrefix(strings.TrimPrefix(s.config.BaseURL, "https://"), "http://")
		if index := strings.IndexByte(base, '/'); index >= 0 {
			base = base[:index]
		}
		hosts = append(hosts, base)
	}
	return hosts
}

// derivedHeaders marks a response whose body carries the public base URL. A
// shared cache must not hand one client's rendering to another, since the base
// can differ per request when it is not configured.
func derivedHeaders(header http.Header) {
	header.Set("Cache-Control", "private, no-cache")
	header.Add("Vary", "Accept")
	header.Add("Vary", "Host")
	header.Add("Vary", "X-Forwarded-Host")
	header.Add("Vary", "X-Forwarded-Proto")
}
