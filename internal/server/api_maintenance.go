package server

import (
	"context"
	"net/http"
	"time"

	"github.com/charmbracelet/log"

	"arca/internal/migrate"
	"arca/internal/nexus"
	"arca/internal/store/models"
)

const migrationTimeout = 60 * time.Second

type migrationRequest struct {
	URL        string   `json:"url"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	RawFormat  string   `json:"rawFormat"`
	Visibility string   `json:"visibility"`
	Only       []string `json:"only"`
	Workers    int      `json:"workers"`
}

func (b migrationRequest) options() migrate.Options {
	return migrate.Options{
		RawFormat:  b.RawFormat,
		Visibility: b.Visibility,
		Only:       b.Only,
	}
}

// plan reads the Nexus repository list and decides what would happen. It writes
// nothing, here or there.
func (s *Server) plan(ctx context.Context, body migrationRequest) ([]migrate.Decision, error) {
	if body.URL == "" {
		return nil, fail(http.StatusBadRequest, "url is required")
	}
	if body.Visibility != "" {
		if _, err := requireOneOf(body.Visibility, visibilities, "visibility"); err != nil {
			return nil, err
		}
	}
	if body.RawFormat != "" {
		if _, err := requireOneOf(body.RawFormat, formats, "rawFormat"); err != nil {
			return nil, err
		}
	}

	client, err := nexus.New(sanitizeSource(body.URL), body.Username, body.Password, migrationTimeout)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "url must be an absolute http or https URL")
	}

	repositories, err := client.Repositories(ctx)
	if err != nil {
		return nil, fail(http.StatusBadGateway, "%s", "Reading the Nexus repository list failed: "+err.Error())
	}
	return migrate.Plan(repositories, body.options()), nil
}

func (s *Server) routeMigrationPreview(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	var body migrationRequest
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	plan, err := s.plan(request.Context(), body)
	if err != nil {
		return err
	}

	entries := make([]migrationRepository, 0, len(plan))
	for _, decision := range plan {
		entries = append(entries, migrationRepository{
			Name:          decision.Name(),
			Source:        decision.SourceName(),
			Action:        decision.Action,
			Format:        decision.Format,
			Type:          decision.Type,
			Reason:        decision.Reason,
			CopyingAssets: decision.CopyAssets,
		})
	}

	writeJSON(writer, http.StatusOK, map[string]any{"repositories": entries})
	return nil
}

func (s *Server) routeMigrationStatus(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}

	writeJSON(writer, http.StatusOK, s.migration.snapshot())
	return nil
}

func (s *Server) routeStartMigration(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}
	if s.migration.running() {
		return fail(http.StatusConflict, "A migration is already running")
	}

	var body migrationRequest
	if err := decodeBody(request, &body); err != nil {
		return err
	}

	plan, err := s.plan(request.Context(), body)
	if err != nil {
		return err
	}

	client, err := nexus.New(sanitizeSource(body.URL), body.Username, body.Password, migrationTimeout)
	if err != nil {
		return fail(http.StatusBadRequest, "url must be an absolute http or https URL")
	}

	// The run outlives the request that started it, so it gets a context of its
	// own that only cancellation or shutdown ends.
	ctx, cancel := context.WithCancel(context.Background())
	if !s.migration.begin(sanitizeSource(body.URL), plan, cancel) {
		cancel()
		return fail(http.StatusConflict, "A migration is already running")
	}

	options := body.options()
	if options.Visibility == "" {
		options.Visibility = models.VisibilityPrivate
	}

	runner := &migrate.Runner{
		Source:   client,
		Target:   &localDestination{server: s},
		Options:  options,
		Workers:  body.Workers,
		Reporter: s.migration,
	}

	go s.runMigration(ctx, cancel, runner, plan)

	writeJSON(writer, http.StatusAccepted, s.migration.snapshot())
	return nil
}

func (s *Server) runMigration(ctx context.Context, cancel context.CancelFunc, runner *migrate.Runner, plan []migrate.Decision) {
	defer cancel()

	results := runner.Apply(ctx, plan)
	s.migration.record(results)

	failed := 0
	for _, result := range results {
		failed += result.Failed
	}

	switch {
	case ctx.Err() != nil:
		s.migration.finish(MigrationCancelled, "The migration was cancelled. Starting it again resumes where it stopped.")
		log.Warnf("migration cancelled after %d failures", failed)
	case failed > 0:
		s.migration.finish(MigrationFailed, "Some transfers failed. Running it again retries only those.")
		log.Warnf("migration finished with %d failures", failed)
	default:
		s.migration.finish(MigrationDone, "")
		log.Info("migration finished")
	}
}

func (s *Server) routeCancelMigration(writer http.ResponseWriter, request *http.Request) error {
	if _, err := requireAdmin(request); err != nil {
		return err
	}
	if !s.migration.stop() {
		return fail(http.StatusConflict, "No migration is running")
	}

	writeJSON(writer, http.StatusOK, map[string]string{"status": "cancelling"})
	return nil
}
