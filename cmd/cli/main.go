package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ai-index/internal/indexer"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return fmt.Sprint([]string(*s))
}

func (s *stringSliceFlag) Set(value string) error {
	if value == "" {
		return nil
	}
	*s = append(*s, value)
	return nil
}

const defaultCommitCacheFile = "codex_commit_cache.json"

func main() {
	var (
		dryRun       bool
		summaryJSON  string
		cachePath    string
		noCache      bool
		skipRepos    stringSliceFlag
		codexTimeout time.Duration
		parallel     int
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Do everything except actually run codex exec.")
	flag.BoolVar(&dryRun, "n", false, "Alias for --dry-run.")
	flag.StringVar(&summaryJSON, "summary-json", "codex_index_summary.json", "Path to JSON summary output.")
	flag.StringVar(&cachePath, "commit-cache", defaultCommitCacheFile,
		fmt.Sprintf("Path to commit cache file (default %s). Use --no-commit-cache to disable.",
			defaultCommitCacheFile))
	flag.BoolVar(&noCache, "no-commit-cache", false, "Disable commit cache.")
	flag.Var(&skipRepos, "skip-repo", "Path, slug, or name of a repository to skip (repeatable).")
	flag.DurationVar(&codexTimeout, "codex-timeout", 45*time.Minute,
		"Maximum duration to allow Codex indexing per repository (0 disables the timeout).")
	flag.IntVar(&parallel, "parallel", 1, "Number of repositories to index concurrently.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <root-directory>\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	rootArg := flag.Arg(0)
	rootDir, err := filepath.Abs(rootArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving root directory:", err)
		os.Exit(1)
	}

	if noCache {
		cachePath = ""
	} else if cachePath == "" {
		cachePath = defaultCommitCacheFile
	}

	if err := indexer.Run(rootDir, dryRun, summaryJSON, cachePath, []string(skipRepos), codexTimeout, parallel); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
