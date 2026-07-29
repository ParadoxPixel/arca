package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/mux"

	"arca/internal/auth"
	"arca/internal/blob"
	"arca/internal/config"
	"arca/internal/proxy"
	"arca/internal/store"
)

const diskUsageTTL = time.Minute

type Server struct {
	config   config.Config
	store    *store.Store
	blobs    *blob.Store
	resolver *auth.Resolver
	traffic  *trafficRecorder
	remote   *proxy.Client
	flight   *proxy.Flight
	router   *mux.Router
	http     *http.Server
	started  time.Time

	diskMutex      sync.Mutex
	diskUsageCache blob.Usage
	diskMeasuredAt time.Time
}

func New(configuration config.Config) (*Server, error) {
	database, err := store.Open(configuration.DatabasePath)
	if err != nil {
		return nil, err
	}

	blobs, err := blob.New(configuration.StorageDir)
	if err != nil {
		return nil, err
	}

	if configuration.ProxyTimeout <= 0 {
		configuration.ProxyTimeout = config.DefaultProxyTimeout
	}

	server := &Server{
		config:   configuration,
		store:    database,
		blobs:    blobs,
		resolver: auth.NewResolver(database),
		traffic:  newTrafficRecorder(database),
		remote:   proxy.NewClient(configuration.ProxyTimeout),
		flight:   proxy.NewFlight(),
		router:   mux.NewRouter(),
		started:  time.Now(),
	}
	server.routes()

	server.http = &http.Server{
		Addr:              fmt.Sprintf(":%d", configuration.Port),
		Handler:           server.router,
		ReadHeaderTimeout: 20 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	return server, nil
}

func (s *Server) Start() error {
	log.Infof("listening on http://localhost:%d", s.config.Port)

	go s.runMaintenance()

	if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	log.Info("shutting down")

	if err := s.http.Shutdown(ctx); err != nil {
		log.Errorf("graceful shutdown failed: %v", err)
		_ = s.http.Close()
	}
	s.traffic.close()
	if err := s.store.Close(); err != nil {
		log.Errorf("closing the database failed: %v", err)
	}
}

func (s *Server) runMaintenance() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	// Retention is measured in days, so a server that restarts more often than
	// the tick would otherwise never get around to evicting anything.
	s.purgeIdleCaches(time.Now())

	for range ticker.C {
		if removed, err := s.store.DeleteExpiredSessions(); err != nil {
			log.Errorf("session cleanup failed: %v", err)
		} else if removed > 0 {
			log.Infof("removed %d expired sessions", removed)
		}

		cutoff := time.Now().Add(-trafficRetention).UnixMilli()
		if removed, err := s.store.DeleteTrafficBefore(cutoff); err != nil {
			log.Errorf("traffic cleanup failed: %v", err)
		} else if removed > 0 {
			log.Infof("removed %d traffic events older than %s", removed, trafficRetention)
		}

		if removed, err := s.store.DeleteExpiredRemoteMisses(); err != nil {
			log.Errorf("upstream miss cleanup failed: %v", err)
		} else if removed > 0 {
			log.Infof("removed %d expired upstream misses", removed)
		}

		s.purgeIdleCaches(time.Now())
	}
}
