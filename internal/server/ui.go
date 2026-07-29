package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/charmbracelet/log"

	"arca/internal/frontend"
)

func (s *Server) mountFrontend() {
	if s.config.DevMode {
		log.Infof("serving the frontend from the vite dev server at %s", s.config.ViteURL)
		s.router.PathPrefix("/").Handler(s.visitMiddleware(s.viteProxy()))
		return
	}

	log.Info("serving the embedded frontend bundle")
	s.router.PathPrefix("/").Handler(s.visitMiddleware(frontend.StaticHandler()))
}

func (s *Server) viteProxy() http.Handler {
	target, err := url.Parse(s.config.ViteURL)
	if err != nil {
		log.Fatalf("invalid vite url %q: %v", s.config.ViteURL, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, err error) {
		log.Errorf("vite proxy failed: %v", err)
		plain(writer, http.StatusBadGateway, "The vite dev server is not reachable")
	}
	return proxy
}
