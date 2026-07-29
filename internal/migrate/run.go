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
}

// Result records what happened to one repository.
type Result struct {
	Decision Decision
	Created  bool
	Existed  bool
	Copied   int
	Skipped  int
	Failed   int
	Errors   []string
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
					copied, err := r.copyAsset(ctx, decision.Name(), asset)

					mutex.Lock()
					switch {
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

// copyAsset reports whether anything was transferred. An asset already in arca
// is left alone so a rerun costs one HEAD instead of a download.
func (r *Runner) copyAsset(ctx context.Context, repository string, asset nexus.Asset) (bool, error) {
	present, err := r.Target.HasAsset(ctx, repository, asset.Path)
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

	if err := r.Target.PutAsset(ctx, repository, asset.Path, asset.FileSize, body); err != nil {
		return false, err
	}
	return true, nil
}
