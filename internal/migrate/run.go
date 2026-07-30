package migrate

import (
	"context"
	"errors"
	"io"
	"sync"

	"arca/internal/nexus"
)

const defaultWorkers = 4

// ErrRepositoryExists lets a destination report a name that is already taken
// without failing the run, which is what makes a rerun resume rather than stop.
var ErrRepositoryExists = errors.New("migrate: the repository already exists")

// Destination is where a migration writes. The CLI drives arca over HTTP; the
// server implements this against its own store so the admin UI does not have to
// call back into itself.
type Destination interface {
	CreateRepository(ctx context.Context, decision Decision, visibility string) error
	HasAsset(ctx context.Context, repository, path string) (bool, error)
	PutAsset(ctx context.Context, repository, path string, size int64, body io.Reader) error
	// Settled runs once a repository's content is all in place, for a format whose
	// metadata cannot be built as the files arrive. Docker needs it: a manifest
	// references blobs that the copy order gives no guarantee of having seen yet.
	Settled(ctx context.Context, decision Decision) error
}

// Result records what happened to one repository.
type Result struct {
	Decision Decision
	Created  bool
	Existed  bool
	Copied   int
	Skipped  int
	Failed   int
	// Untranslatable counts source paths this server has no place for. Reporting them
	// is what stops a copy claiming to be complete when it silently dropped content.
	Untranslatable int
	Errors         []string
}

type Reporter interface {
	Repository(decision Decision)
	Progress(repository string, copied, skipped, failed int)
	Problem(repository, path string, err error)
}

type Runner struct {
	Source   *nexus.Client
	Target   Destination
	Options  Options
	Workers  int
	Reporter Reporter
}

func (r *Runner) workers() int {
	if r.Workers > 0 {
		return r.Workers
	}
	return defaultWorkers
}

// Apply creates every planned repository and copies the assets of the hosted
// ones. A failure on one asset is recorded and the rest carry on, because a
// migration of thousands of files should not be lost to one bad blob.
func (r *Runner) Apply(ctx context.Context, plan []Decision) []Result {
	results := make([]Result, 0, len(plan))

	for _, decision := range plan {
		if r.Reporter != nil {
			r.Reporter.Repository(decision)
		}
		if decision.Action == ActionSkip {
			results = append(results, Result{Decision: decision})
			continue
		}
		results = append(results, r.applyOne(ctx, decision))
	}

	return results
}

func (r *Runner) applyOne(ctx context.Context, decision Decision) Result {
	result := Result{Decision: decision}

	switch err := r.Target.CreateRepository(ctx, decision, r.Options.visibility()); {
	case errors.Is(err, ErrRepositoryExists):
		result.Existed = true
	case err != nil:
		result.Failed++
		result.Errors = append(result.Errors, err.Error())
		return result
	default:
		result.Created = true
	}

	if !decision.CopyAssets {
		return result
	}

	r.copyAssets(ctx, decision, &result)
	if decision.CopyComponents() {
		r.copyComponents(ctx, decision, &result)
	}

	// Settling comes last for the same reason it exists: only now is every blob a
	// manifest might reference actually present.
	if err := r.Target.Settled(ctx, decision); err != nil {
		result.Failed++
		result.Errors = append(result.Errors, "building metadata: "+err.Error())
	}

	return result
}

func (r *Runner) copyAssets(ctx context.Context, decision Decision, result *Result) {
	var mutex sync.Mutex

	err := r.Source.Assets(ctx, decision.SourceName(), func(page []nexus.Asset) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		queue := make(chan nexus.Asset)
		var workers sync.WaitGroup

		for range r.workers() {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for asset := range queue {
					// The transfer happens outside the lock. Holding it across a
					// download would serialise the pool this exists to parallelise.
					copied, err := r.copyAsset(ctx, decision, asset)

					mutex.Lock()
					r.record(decision, asset, copied, err, result)
					mutex.Unlock()
				}
			}()
		}

		for _, asset := range page {
			queue <- asset
		}
		close(queue)
		workers.Wait()

		return nil
	})

	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err.Error())
	}
}

// copyComponents walks the source grouped by version. It exists for docker, whose tags
// appear in no other listing, and it copies only the assets the asset walk could not
// have seen.
func (r *Runner) copyComponents(ctx context.Context, decision Decision, result *Result) {
	err := r.Source.Components(ctx, decision.SourceName(), func(page []nexus.Component) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		for _, component := range page {
			for _, asset := range component.Assets {
				r.recordCopy(ctx, decision, asset, result)
			}
		}
		return nil
	})

	if err != nil {
		result.Failed++
		result.Errors = append(result.Errors, err.Error())
	}
}

// recordCopy transfers one asset and books the outcome. Only the components walk uses
// it, which runs on one goroutine, so it needs no lock of its own.
func (r *Runner) recordCopy(ctx context.Context, decision Decision, asset nexus.Asset, result *Result) {
	copied, err := r.copyAsset(ctx, decision, asset)
	r.record(decision, asset, copied, err, result)
}

// record books one outcome. Callers hold whatever lock the result needs.
func (r *Runner) record(decision Decision, asset nexus.Asset, copied bool, err error, result *Result) {
	switch {
	case errors.Is(err, errUntranslatable):
		result.Untranslatable++
	case err != nil:
		result.Failed++
		result.Errors = append(result.Errors, asset.Path+": "+err.Error())
		if r.Reporter != nil {
			r.Reporter.Problem(decision.Name(), asset.Path, err)
		}
	case copied:
		result.Copied++
	default:
		result.Skipped++
	}

	if r.Reporter != nil {
		r.Reporter.Progress(decision.Name(), result.Copied, result.Skipped, result.Failed)
	}
}

var errUntranslatable = errors.New("migrate: this server has no place for that path")

// copyAsset reports whether anything was transferred. An asset already in arca
// is left alone so a rerun costs one HEAD instead of a download.
func (r *Runner) copyAsset(ctx context.Context, decision Decision, asset nexus.Asset) (bool, error) {
	path, ok := decision.TranslatePath(asset.Path)
	if !ok {
		return false, errUntranslatable
	}

	repository := decision.Name()

	present, err := r.Target.HasAsset(ctx, repository, path)
	if err != nil {
		return false, err
	}
	if present {
		return false, nil
	}

	body, err := r.Source.Download(ctx, asset)
	if err != nil {
		return false, err
	}
	defer body.Close()

	if err := r.Target.PutAsset(ctx, repository, path, asset.FileSize, body); err != nil {
		return false, err
	}
	return true, nil
}
