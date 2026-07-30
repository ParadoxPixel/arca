package server

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"arca/internal/format"
	"arca/internal/migrate"
	"arca/internal/store"
	"arca/internal/store/models"
)

const (
	MigrationIdle      = "idle"
	MigrationRunning   = "running"
	MigrationDone      = "done"
	MigrationCancelled = "cancelled"
	MigrationFailed    = "failed"
)

const (
	repositoryPending = "pending"
	repositoryRunning = "running"
	repositoryDone    = "done"
	repositoryFailed  = "failed"
	repositorySkipped = "skipped"
)

type migrationRepository struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Action         string `json:"action"`
	Format         string `json:"format"`
	Type           string `json:"type"`
	Reason         string `json:"reason"`
	State          string `json:"state"`
	Existed        bool   `json:"existed"`
	Copied         int    `json:"copied"`
	Present        int    `json:"present"`
	Failed         int    `json:"failed"`
	Untranslatable int    `json:"untranslatable"`
	CopyingAssets  bool   `json:"copyingAssets"`
}

type migrationStatus struct {
	State        string                `json:"state"`
	Source       string                `json:"source"`
	StartedAt    int64                 `json:"startedAt"`
	FinishedAt   int64                 `json:"finishedAt"`
	Current      string                `json:"current"`
	Message      string                `json:"message"`
	Repositories []migrationRepository `json:"repositories"`
	Errors       []string              `json:"errors"`
}

// migrationRun holds the one migration an instance can be running. It lives in
// memory: a restart loses the progress view, not the work, because a migration
// resumes by skipping what is already stored.
type migrationRun struct {
	mutex  sync.Mutex
	status migrationStatus
	cancel context.CancelFunc
}

func newMigrationRun() *migrationRun {
	return &migrationRun{status: migrationStatus{State: MigrationIdle, Repositories: []migrationRepository{}, Errors: []string{}}}
}

func (m *migrationRun) snapshot() migrationStatus {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	copied := m.status
	copied.Repositories = append([]migrationRepository{}, m.status.Repositories...)
	copied.Errors = append([]string{}, m.status.Errors...)
	return copied
}

func (m *migrationRun) running() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.status.State == MigrationRunning
}

// begin claims the run. It reports false when one is already going, which is
// what stops two administrators starting the same migration twice.
func (m *migrationRun) begin(source string, plan []migrate.Decision, cancel context.CancelFunc) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.status.State == MigrationRunning {
		return false
	}

	repositories := make([]migrationRepository, 0, len(plan))
	for _, decision := range plan {
		state := repositoryPending
		if decision.Action == migrate.ActionSkip {
			state = repositorySkipped
		}
		repositories = append(repositories, migrationRepository{
			Name:          decision.Name(),
			Source:        decision.SourceName(),
			Action:        decision.Action,
			Format:        decision.Format,
			Type:          decision.Type,
			Reason:        decision.Reason,
			State:         state,
			CopyingAssets: decision.CopyAssets,
		})
	}

	m.status = migrationStatus{
		State:        MigrationRunning,
		Source:       source,
		StartedAt:    store.NowMillis(),
		Repositories: repositories,
		Errors:       []string{},
	}
	m.cancel = cancel
	return true
}

func (m *migrationRun) finish(state string, message string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.status.State = state
	m.status.Message = message
	m.status.FinishedAt = store.NowMillis()
	m.status.Current = ""
	m.cancel = nil
}

func (m *migrationRun) stop() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.status.State != MigrationRunning || m.cancel == nil {
		return false
	}
	m.cancel()
	return true
}

func (m *migrationRun) update(name string, mutate func(*migrationRepository)) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for index := range m.status.Repositories {
		if m.status.Repositories[index].Name == name {
			mutate(&m.status.Repositories[index])
			return
		}
	}
}

// Repository, Progress and Problem satisfy migrate.Reporter.
func (m *migrationRun) Repository(decision migrate.Decision) {
	if decision.Action == migrate.ActionSkip {
		return
	}

	m.mutex.Lock()
	m.status.Current = decision.Name()
	m.mutex.Unlock()

	m.update(decision.Name(), func(entry *migrationRepository) { entry.State = repositoryRunning })
}

func (m *migrationRun) Progress(repository string, copied, present, failed int) {
	m.update(repository, func(entry *migrationRepository) {
		entry.Copied, entry.Present, entry.Failed = copied, present, failed
	})
}

func (m *migrationRun) Problem(repository, path string, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// The list is capped so a repository that fails wholesale cannot grow the
	// status payload without bound.
	if len(m.status.Errors) < 200 {
		m.status.Errors = append(m.status.Errors, fmt.Sprintf("%s/%s: %v", repository, path, err))
	}
}

func (m *migrationRun) record(results []migrate.Result) {
	for _, result := range results {
		if result.Decision.Action == migrate.ActionSkip {
			continue
		}
		m.update(result.Decision.Name(), func(entry *migrationRepository) {
			entry.Existed = result.Existed
			entry.Copied, entry.Present, entry.Failed = result.Copied, result.Skipped, result.Failed
			entry.Untranslatable = result.Untranslatable
			entry.State = repositoryDone
			if result.Failed > 0 {
				entry.State = repositoryFailed
			}
		})
	}
}

// localDestination writes migrated content straight into this instance, so a
// migration driven from the admin UI never has to authenticate back into the
// server it is already running in.
type localDestination struct {
	server       *Server
	repositories sync.Map
}

func (d *localDestination) CreateRepository(_ context.Context, decision migrate.Decision, visibility string) error {
	if _, err := d.server.store.RepositoryByName(decision.Name()); err == nil {
		return migrate.ErrRepositoryExists
	}

	remoteURL, err := normalizeRemote(decision.Type, decision.RemoteURL)
	if err != nil {
		return err
	}
	if err := validateP2(decision.Format, decision.Type, remoteURL); err != nil {
		return err
	}

	policy := decision.Policy
	if policy == "" {
		policy = models.PolicyRelease
	}

	return d.server.store.CreateRepository(&models.Repository{
		Name:               decision.Name(),
		Format:             decision.Format,
		Type:               decision.Type,
		Policy:             policy,
		Visibility:         visibility,
		AllowRedeploy:      policy == models.PolicySnapshot,
		RemoteURL:          remoteURL,
		MetadataTTLSeconds: models.DefaultMetadataTTLSeconds,
		NegativeTTLSeconds: models.DefaultNegativeTTLSeconds,
	})
}

func (d *localDestination) repository(name string) (*models.Repository, error) {
	if cached, ok := d.repositories.Load(name); ok {
		return cached.(*models.Repository), nil
	}

	repository, err := d.server.store.RepositoryByName(name)
	if err != nil {
		return nil, err
	}
	d.repositories.Store(name, repository)
	return repository, nil
}

func (d *localDestination) HasAsset(_ context.Context, repository, path string) (bool, error) {
	stored, err := d.repository(repository)
	if err != nil {
		return false, err
	}
	_, err = d.server.store.FindAsset(stored.ID, path)
	return err == nil, nil
}

func (d *localDestination) PutAsset(_ context.Context, repository, path string, _ int64, body io.Reader) error {
	if !format.IsSafe(path) {
		return fmt.Errorf("%q is not a path this server will store", path)
	}

	stored, err := d.repository(repository)
	if err != nil {
		return err
	}

	_, err = d.server.storeUpload(stored, path, body, uploadDetails{})
	return err
}

// Settled builds the metadata a format cannot index as its files arrive. Only docker
// needs it, and only after every blob is present: a manifest's platform and labels come
// from a config blob the copy order gives no guarantee of having seen yet.
func (d *localDestination) Settled(_ context.Context, decision migrate.Decision) error {
	if decision.Format != format.Docker {
		return nil
	}

	stored, err := d.repository(decision.Name())
	if err != nil {
		return err
	}

	result, err := d.server.reindexDockerRepository(stored)
	if err != nil {
		return err
	}
	if result.Failed > 0 {
		return fmt.Errorf("%d of %d manifests could not be indexed", result.Failed, result.Failed+result.Manifests)
	}
	return nil
}

func sanitizeSource(url string) string { return strings.TrimSpace(url) }
