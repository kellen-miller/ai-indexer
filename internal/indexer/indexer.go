package indexer

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

type indexer struct {
	stdout       io.Writer
	stderr       io.Writer
	cache        *commitCache
	skip         []string
	codexTimeout time.Duration
}

func newIndexer(stdout, stderr io.Writer, cache *commitCache, skip []string, codexTimeout time.Duration) *indexer {
	return &indexer{
		stdout:       stdout,
		stderr:       stderr,
		cache:        cache,
		skip:         skip,
		codexTimeout: codexTimeout,
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
func Run(rootDir string, dryRun bool, summaryJSON, cachePath string, skipRepos []string, codexTimeout time.Duration) error {
	cache, err := loadCommitCache(cachePath)
	if err != nil {
		return err
	}

	ix := newIndexer(os.Stdout, os.Stderr, cache, skipRepos, codexTimeout)
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

	ix.outln(fmt.Sprintf("Found %d git repos under %s", len(repos), rootDir))
	ix.outln()

	results := make([]RepoResult, 0, len(repos))

	for _, repo := range repos {
		res := ix.processRepo(ctx, repo, rootDir, dryRun)
		results = append(results, res)
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
