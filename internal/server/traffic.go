package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/store"
	"arca/internal/store/models"
)

const (
	trafficBuffer    = 2048
	trafficBatchSize = 256
	trafficFlushWait = 2 * time.Second
	trafficRetention = 90 * 24 * time.Hour
)

// message carries either an event to buffer or a request to write the buffer
// out. Both travel the same channel so a flush always follows the events that
// were recorded before it.
type message struct {
	event   models.TrafficEvent
	flushed chan struct{}
}

type trafficRecorder struct {
	store    *store.Store
	messages chan message
	stopped  chan struct{}
}

func newTrafficRecorder(database *store.Store) *trafficRecorder {
	recorder := &trafficRecorder{
		store:    database,
		messages: make(chan message, trafficBuffer),
		stopped:  make(chan struct{}),
	}
	go recorder.run()
	return recorder
}

// record never blocks the request that produced the event: a full buffer drops
// the sample rather than slowing artifact delivery down.
func (r *trafficRecorder) record(event models.TrafficEvent) {
	event.At = store.NowMillis()

	select {
	case r.messages <- message{event: event}:
	default:
	}
}

func (r *trafficRecorder) run() {
	defer close(r.stopped)

	ticker := time.NewTicker(trafficFlushWait)
	defer ticker.Stop()

	batch := make([]models.TrafficEvent, 0, trafficBatchSize)

	for {
		select {
		case next, open := <-r.messages:
			if !open {
				r.flush(batch)
				return
			}
			if next.flushed != nil {
				batch = r.flush(batch)
				close(next.flushed)
				continue
			}
			batch = append(batch, next.event)
			if len(batch) >= trafficBatchSize {
				batch = r.flush(batch)
			}
		case <-ticker.C:
			batch = r.flush(batch)
		}
	}
}

func (r *trafficRecorder) flush(batch []models.TrafficEvent) []models.TrafficEvent {
	if len(batch) == 0 {
		return batch
	}
	if err := r.store.RecordTraffic(batch); err != nil {
		log.Errorf("recording traffic failed: %v", err)
	}
	return batch[:0]
}

func (r *trafficRecorder) sync() {
	flushed := make(chan struct{})

	select {
	case r.messages <- message{flushed: flushed}:
		<-flushed
	case <-r.stopped:
	}
}

func (r *trafficRecorder) close() {
	close(r.messages)
	<-r.stopped
}

func userIDFrom(request *http.Request) *string {
	user := currentUser(request)
	if user == nil {
		return nil
	}
	return &user.ID
}

func (s *Server) recordAssetTraffic(request *http.Request, kind string, repository *models.Repository, asset *models.Asset) {
	coordinates, _ := layoutFor(repository).Coordinates(asset.Path)

	s.traffic.record(models.TrafficEvent{
		Kind:         kind,
		RepositoryID: asset.RepositoryID,
		Namespace:    coordinates.Namespace,
		Name:         coordinates.Name,
		Version:      coordinates.Version,
		Bytes:        asset.Size,
		UserID:       userIDFrom(request),
	})
}

// visitMiddleware counts document loads of the single page application, which
// stands in for a browser visit without tracking every asset the page fetches.
func (s *Server) visitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && strings.Contains(request.Header.Get("Accept"), "text/html") {
			s.traffic.record(models.TrafficEvent{Kind: models.TrafficVisit, UserID: userIDFrom(request)})
		}
		next.ServeHTTP(writer, request)
	})
}
