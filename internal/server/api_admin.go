package server

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"arca/internal/blob"
	"arca/internal/store"
)

type trafficWindow struct {
	key      string
	window   time.Duration
	interval time.Duration
}

var trafficWindows = []trafficWindow{
	{key: "24h", window: 24 * time.Hour, interval: time.Hour},
	{key: "7d", window: 7 * 24 * time.Hour, interval: 6 * time.Hour},
	{key: "30d", window: 30 * 24 * time.Hour, interval: 24 * time.Hour},
	{key: "90d", window: 90 * 24 * time.Hour, interval: 24 * time.Hour},
}

func windowFor(key string) trafficWindow {
	for _, candidate := range trafficWindows {
		if candidate.key == key {
			return candidate
		}
	}
	return trafficWindows[1]
}

func (w trafficWindow) bounds(now time.Time) (since, until, interval int64) {
	interval = w.interval.Milliseconds()
	until = (now.UnixMilli()/interval)*interval + interval
	return until - w.window.Milliseconds(), until, interval
}

func (s *Server) routeTraffic(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	selected := windowFor(request.URL.Query().Get("range"))
	since, until, interval := selected.bounds(time.Now())

	buckets, err := s.store.TrafficSeries(since, until, interval)
	if err != nil {
		return err
	}
	totals, err := s.store.TrafficTotals(since, until)
	if err != nil {
		return err
	}
	previous, err := s.store.TrafficTotals(since-(until-since), since)
	if err != nil {
		return err
	}
	artifacts, err := s.store.TopArtifacts(since, until, 15)
	if err != nil {
		return err
	}
	repositories, err := s.store.TrafficByRepository(since, until)
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"range":        selected.key,
		"since":        since,
		"until":        until,
		"interval":     interval,
		"buckets":      buckets,
		"totals":       totals,
		"previous":     previous,
		"topArtifacts": artifacts,
		"repositories": repositories,
	})
	return nil
}

type diskReport struct {
	Root          string `json:"root"`
	Files         int64  `json:"files"`
	Bytes         int64  `json:"bytes"`
	TrackedBytes  int64  `json:"trackedBytes"`
	OrphanBytes   int64  `json:"orphanBytes"`
	DatabaseBytes int64  `json:"databaseBytes"`
	MeasuredAt    int64  `json:"measuredAt"`
}

func (s *Server) databaseBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(s.config.DatabasePath + suffix); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (s *Server) diskUsage() (blob.Usage, int64, error) {
	s.diskMutex.Lock()
	defer s.diskMutex.Unlock()

	if time.Since(s.diskMeasuredAt) < diskUsageTTL {
		return s.diskUsageCache, s.diskMeasuredAt.UnixMilli(), nil
	}

	usage, err := s.blobs.Usage()
	if err != nil {
		return blob.Usage{}, 0, err
	}

	s.diskUsageCache = usage
	s.diskMeasuredAt = time.Now()
	return usage, s.diskMeasuredAt.UnixMilli(), nil
}

func (s *Server) storageReport(trackedBytes int64) (diskReport, error) {
	usage, measuredAt, err := s.diskUsage()
	if err != nil {
		return diskReport{}, err
	}

	return diskReport{
		Root:          s.blobs.Root(),
		Files:         usage.Files,
		Bytes:         usage.Bytes,
		TrackedBytes:  trackedBytes,
		OrphanBytes:   max(usage.Bytes-trackedBytes, 0),
		DatabaseBytes: s.databaseBytes(),
		MeasuredAt:    measuredAt,
	}, nil
}

func (s *Server) routeStorage(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	totals, err := s.store.StorageTotals()
	if err != nil {
		return err
	}
	repositories, err := s.store.StorageByRepository()
	if err != nil {
		return err
	}
	extensions, err := s.store.StorageByExtension()
	if err != nil {
		return err
	}
	largest, err := s.store.LargestComponents(20)
	if err != nil {
		return err
	}
	disk, err := s.storageReport(totals.Bytes)
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"totals":       totals,
		"repositories": repositories,
		"extensions":   extensions,
		"largest":      largest,
		"disk":         disk,
	})
	return nil
}

func (s *Server) routeComponents(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	query := request.URL.Query()

	repositoryID := ""
	if name := query.Get("repository"); name != "" {
		repository, err := s.store.RepositoryByName(name)
		if err != nil {
			return fail(http.StatusNotFound, "Repository not found")
		}
		repositoryID = repository.ID
	}

	components, err := s.store.ComponentsByRepository(repositoryID, query.Get("q"), 200)
	if err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, map[string]any{"components": components})
	return nil
}

func (s *Server) routeHealth(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	totals, err := s.store.StorageTotals()
	if err != nil {
		return err
	}
	users, err := s.store.CountUsers()
	if err != nil {
		return err
	}
	disk, err := s.storageReport(totals.Bytes)
	if err != nil {
		return err
	}

	day := 24 * time.Hour.Milliseconds()
	now := store.NowMillis()
	today, err := s.store.TrafficTotals(now-day, now)
	if err != nil {
		return err
	}

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	writeJSON(writer, http.StatusOK, map[string]any{
		"instanceName": s.store.InstanceName(),
		"startedAt":    s.started.UnixMilli(),
		"uptime":       time.Since(s.started).Milliseconds(),
		"goVersion":    runtime.Version(),
		"goroutines":   runtime.NumGoroutine(),
		"heapBytes":    int64(memory.HeapAlloc),
		"users":        users,
		"totals":       totals,
		"disk":         disk,
		"lastDay":      today,
	})
	return nil
}
