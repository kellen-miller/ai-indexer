package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type indexer struct {
	stdout       io.Writer
	stderr       io.Writer
	cache        *commitCache
	skip         []string
	codexTimeout time.Duration
	workerCount  int
}

func newIndexer(
	stdout io.Writer,
	stderr io.Writer,
	cache *commitCache,
	skip []string,
	codexTimeout time.Duration,
	workerCount int,
) *indexer {
	return &indexer{
		stdout:       stdout,
		stderr:       stderr,
		cache:        cache,
		skip:         skip,
		codexTimeout: codexTimeout,
		workerCount:  workerCount,
	}
}

func (ix *indexer) outln(args ...any) {
	if _, err := fmt.Fprintln(ix.stdout, args...); err != nil {
		fmt.Fprintf(os.Stderr, "stdout write error: %v\n", err)
	}
}

func (ix *indexer) errln(args ...any) {
	if _, err := fmt.Fprintln(ix.stderr, args...); err != nil {
		fmt.Fprintf(os.Stderr, "stderr write error: %v\n", err)
	}
}

func (ix *indexer) persistCache() error {
	if ix.cache == nil {
		return nil
	}
	return ix.cache.Save()
}

// RepoResult captures per-repo outcome for JSON summary.
type RepoResult struct {
	CheckoutOK     *bool  `json:"checkout_ok,omitempty"`
	PullOK         *bool  `json:"pull_ok,omitempty"`
	CodexExitCode  *int   `json:"codex_exit_code,omitempty"`
	Path           string `json:"path"`
	CollectionSlug string `json:"collection_slug"`
	DefaultBranch  string `json:"default_branch,omitempty"`
	Error          string `json:"error,omitempty"`
	SkipReason     string `json:"skip_reason,omitempty"`
	IndexedCommit  string `json:"indexed_commit,omitempty"`
	CachedCommit   string `json:"cached_commit,omitempty"`
	DiffBaseCommit string `json:"diff_base_commit,omitempty"`
	DiffFileCount  int    `json:"diff_file_count,omitempty"`
	CodexRan       bool   `json:"codex_ran"`
	DryRun         bool   `json:"dry_run"`
}

// Run executes the indexing workflow for the provided directory.
func Run(
	rootDir string,
	dryRun bool,
	summaryJSON, cachePath string,
	skipRepos []string,
	codexTimeout time.Duration,
	workerCount int,
) error {
	cache, err := loadCommitCache(cachePath)
	if err != nil {
		return err
	}

	if workerCount <= 0 {
		workerCount = 1
	}

	outputMu := &sync.Mutex{}
	stdout := io.Writer(os.Stdout)
	stderr := io.Writer(os.Stderr)
	if workerCount > 1 {
		stdout = &lockedWriter{mu: outputMu, w: os.Stdout}
		stderr = &lockedWriter{mu: outputMu, w: os.Stderr}
	}

	ix := newIndexer(stdout, stderr, cache, skipRepos, codexTimeout, workerCount)
	err = ix.run(rootDir, dryRun, summaryJSON)
	saveErr := cache.Save()
	if err != nil {
		if saveErr != nil {
			return fmt.Errorf("%w (cache save failed: %w)", err, saveErr)
		}
		return err
	}
	if saveErr != nil {
		return saveErr
	}
	return nil
}

func (ix *indexer) run(rootDir string, dryRun bool, summaryJSON string) error {
	ctx := context.Background()

	ix.outln(colorize(colorCyan, "Codex Repo Indexer"))
	ix.outln(colorize(colorMuted, "Root Directory: %s", rootDir))
	ix.outln(colorize(colorMuted, "Dry Run Mode: %t", dryRun))
	ix.outln()

	repos, err := findGitRepos(rootDir)
	if err != nil {
		ix.errln("Error scanning for git repos:", err)
		return fmt.Errorf("scan git repos: %w", err)
	}
	if len(repos) == 0 {
		ix.outln("No git repositories found.")
		return nil
	}

	workerCount := ix.workerCount
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(repos) {
		workerCount = len(repos)
	}

	ix.outln(fmt.Sprintf("Found %d git repos under %s", len(repos), rootDir))
	ix.outln(colorize(colorMuted, "Parallel Workers: %d", workerCount))
	ix.outln()

	results := make([]RepoResult, len(repos))

	if workerCount == 1 {
		for idx, repo := range repos {
			results[idx] = ix.processRepo(ctx, repo, rootDir, dryRun)
		}
	} else {
		type repoJob struct {
			path  string
			index int
		}

		jobs := make(chan repoJob)
		var wg sync.WaitGroup

		for range workerCount {
			wg.Go(func() {
				for job := range jobs {
					results[job.index] = ix.processRepo(ctx, job.path, rootDir, dryRun)
				}
			})
		}

		for idx, repo := range repos {
			jobs <- repoJob{
				index: idx,
				path:  repo,
			}
		}
		close(jobs)
		wg.Wait()
	}

	ix.outln(colorize(colorCyan, "==> Summary"))
	ix.outln("")

	ix.printSummaryTable(results)

	if err := writeSummaryJSON(summaryJSON, rootDir, dryRun, results); err != nil {
		ix.errln("Error writing JSON summary:", err)
		return fmt.Errorf("write summary json: %w", err)
	}

	ix.outln("JSON summary written to " + summaryJSON)
	return nil
}

func (ix *indexer) repoHeader(repoDir, slug string) {
	ix.outln("")
	ix.outln(colorize(colorMagenta, "==> %s", repoDir))
	ix.outln(colorize(colorMuted, "    collection: %s", slug))
}

func (ix *indexer) repoInfof(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	ix.outln(colorize(colorBlue, "    - %s", msg))
}

func (ix *indexer) repoWarnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	ix.outln(colorize(colorYellow, "    ! %s", msg))
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	written, err := lw.w.Write(p)
	if err != nil {
		return 0, fmt.Errorf("write to locked writer: %w", err)
	}

	return written, nil
}
