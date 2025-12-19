package indexer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	shortCommitLen              = 7
	codexInputKeepAliveInterval = 30 * time.Second
)

func findGitRepos(root string) ([]string, error) {
	var repos []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			repos = append(repos, filepath.Dir(path))
			return nil
		}
		return nil
	})
	if err != nil {
		return repos, fmt.Errorf("walk repos in %s: %w", root, err)
	}

	return repos, nil
}

func (ix *indexer) shouldSkipRepo(rootDir, repoDir, slug string) (bool, string) {
	if len(ix.skip) == 0 {
		return false, ""
	}

	repoAbs := filepath.Clean(repoDir)
	repoAbsLower := strings.ToLower(repoAbs)
	repoBaseLower := strings.ToLower(filepath.Base(repoAbs))

	rel, err := filepath.Rel(rootDir, repoAbs)
	if err != nil {
		rel = repoAbs
	}
	rel = strings.TrimPrefix(rel, "./")
	rel = filepath.ToSlash(rel)
	relLower := strings.ToLower(rel)

	slugLower := strings.ToLower(slug)

	for _, raw := range ix.skip {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}

		rawLower := strings.ToLower(pattern)
		if rawLower == slugLower || rawLower == repoBaseLower || rawLower == relLower {
			return true, fmt.Sprintf("repo excluded via --skip-repo %q", raw)
		}

		cleaned := filepath.Clean(pattern)
		cleanLower := strings.ToLower(cleaned)
		if cleanLower == repoAbsLower {
			return true, fmt.Sprintf("repo excluded via --skip-repo %q", raw)
		}

		cleanSlashLower := strings.ToLower(filepath.ToSlash(cleaned))
		if cleanSlashLower == relLower {
			return true, fmt.Sprintf("repo excluded via --skip-repo %q", raw)
		}

		if !filepath.IsAbs(cleaned) {
			abs := filepath.Join(rootDir, cleaned)
			if strings.ToLower(filepath.Clean(abs)) == repoAbsLower {
				return true, fmt.Sprintf("repo excluded via --skip-repo %q", raw)
			}
		}
	}

	return false, ""
}

func (ix *indexer) processRepo(ctx context.Context, repoDir, rootDir string, dryRun bool) RepoResult {
	slug := computeCollectionSlug(rootDir, repoDir)
	ix.repoHeader(repoDir, slug)

	result := RepoResult{
		Path:           repoDir,
		CollectionSlug: slug,
		DryRun:         dryRun,
	}

	if skip, reason := ix.shouldSkipRepo(rootDir, repoDir, slug); skip {
		result.SkipReason = reason
		ix.repoInfof("skipping indexing: %s", reason)
		ix.outln("")
		return result
	}

	defaultBranch := ix.reportDefaultBranch(ctx, repoDir)
	result.DefaultBranch = defaultBranch

	indexDir := repoDir
	idxDir, checkoutOK, pullOK, cleanup := ix.prepareIndexWorkspace(ctx, repoDir, slug, defaultBranch, dryRun)
	if cleanup != nil {
		defer cleanup()
	}
	if idxDir != "" {
		indexDir = idxDir
	}
	result.CheckoutOK = checkoutOK
	result.PullOK = pullOK

	indexBranch := ix.selectIndexBranch(ctx, indexDir, defaultBranch)
	if indexBranch != "" && result.DefaultBranch == "" {
		result.DefaultBranch = indexBranch
	}

	result.IndexedCommit = ix.detectIndexedCommit(ctx, indexDir)
	result.SkipReason, result.CachedCommit = ix.evaluateSkip(slug, indexBranch, result.IndexedCommit)

	if result.SkipReason != "" {
		ix.repoInfof("skipping indexing: %s", result.SkipReason)
		ix.outln("")
		return result
	}

	var diffFiles []string
	if result.CachedCommit != "" {
		result.DiffBaseCommit = result.CachedCommit
		files, err := diffFilesSince(ctx, indexDir, result.CachedCommit)
		if err != nil {
			ix.repoWarnf("could not compute diff vs %s: %v — falling back to full indexing",
				shortCommit(result.CachedCommit), err)
		} else {
			diffFiles = files
			result.DiffFileCount = len(files)
			ix.repoInfof("incremental indexing: %d files changed since %s",
				len(files), shortCommit(result.CachedCommit))
		}
	}

	ran, exitCode, codexErr := ix.runCodex(ctx, indexDir, slug, result.CachedCommit, diffFiles, dryRun)
	result.CodexRan = ran
	if exitCode != nil {
		result.CodexExitCode = exitCode
	}
	if codexErr != nil {
		result.Error = codexErr.Error()
	} else if !dryRun && ix.cache != nil && indexBranch != "" && result.IndexedCommit != "" {
		ix.cache.Update(slug, indexBranch, result.IndexedCommit)
		if err := ix.persistCache(); err != nil {
			ix.repoWarnf("commit cache save failed: %v", err)
		}
	}

	ix.outln("")
	return result
}

func computeCollectionSlug(rootDir, repoDir string) string {
	rel, err := filepath.Rel(rootDir, repoDir)
	if err != nil || rel == "." {
		rel = "root"
	}
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "_")
	return rel
}

func detectDefaultBranch(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "symbolic-ref", "--quiet", "--short",
		"refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		branch = strings.TrimPrefix(branch, "origin/")
		return branch, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return "", fmt.Errorf("detect origin head: %w", err)
	}

	mainErr := exec.CommandContext(ctx, "git", "-C", repoDir, "show-ref", "--verify", "--quiet",
		"refs/heads/main").Run()
	if mainErr == nil {
		return "main", nil
	}
	var mainExitErr *exec.ExitError
	if !errors.As(mainErr, &mainExitErr) {
		return "", fmt.Errorf("check main branch: %w", mainErr)
	}

	masterErr := exec.CommandContext(ctx, "git", "-C", repoDir, "show-ref", "--verify", "--quiet",
		"refs/heads/master").Run()
	if masterErr == nil {
		return "master", nil
	}
	var masterExitErr *exec.ExitError
	if !errors.As(masterErr, &masterExitErr) {
		return "", fmt.Errorf("check master branch: %w", masterErr)
	}
	return "", nil
}

func (ix *indexer) runCodex(
	ctx context.Context,
	repoDir, slug, baseCommit string,
	diffFiles []string,
	dryRun bool,
) (bool, *int, error) {
	cmdCtx := ctx
	var cancel context.CancelFunc
	if ix.codexTimeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, ix.codexTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(cmdCtx, "codex", "exec",
		"--cd", repoDir,
		"--sandbox", "danger-full-access",
		"--dangerously-bypass-approvals-and-sandbox",
		codexPrompt)
	env := os.Environ()
	env = append(env, "COLLECTION_SLUG="+slug)
	if baseCommit != "" {
		env = append(env, "INDEX_BASE_COMMIT="+baseCommit)
	}
	if len(diffFiles) > 0 {
		env = append(env, "INDEX_DIFF_FILES="+strings.Join(diffFiles, "\n"))
	}
	cmd.Env = env
	cmd.Stdout = ix.stdout
	cmd.Stderr = ix.stderr

	if dryRun {
		desc := fmt.Sprintf(
			"[dry-run] COLLECTION_SLUG=%q codex exec --cd %q --sandbox danger-full-access --dangerously-bypass-approvals-and-sandbox '<PROMPT>'",
			slug,
			repoDir,
		)
		if baseCommit != "" {
			desc += fmt.Sprintf(" (incremental from %s)", shortCommit(baseCommit))
		}
		ix.repoInfof("%s", desc)
		return false, nil, nil
	}

	feeder := newNewlineFeeder(codexInputKeepAliveInterval)
	defer func() {
		if err := feeder.Close(); err != nil {
			ix.repoWarnf("codex input feeder close failed: %v", err)
		}
	}()
	cmd.Stdin = feeder

	ix.repoInfof("running Codex indexing")
	err := cmd.Run()
	if err == nil {
		ix.repoInfof("Codex indexing completed")
		return true, nil, nil
	}

	exitCode := 1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}

	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		if ix.codexTimeout > 0 {
			ix.repoWarnf("Codex timed out after %s", ix.codexTimeout)
		} else {
			ix.repoWarnf("Codex timed out (context deadline exceeded)")
		}
		timeoutErr := fmt.Errorf("codex exec deadline exceeded: %w", err)
		return true, &exitCode, timeoutErr
	}

	ix.repoWarnf("Codex exited with code %d", exitCode)
	return true, &exitCode, fmt.Errorf("codex exec: %w", err)
}

func (ix *indexer) reportDefaultBranch(ctx context.Context, repoDir string) string {
	db, err := detectDefaultBranch(ctx, repoDir)
	if err != nil {
		ix.repoWarnf("could not detect default branch: %v", err)
		return ""
	}
	if db == "" {
		ix.repoWarnf("could not detect default branch — skipping checkout/pull")
		return ""
	}
	ix.repoInfof("default branch: %s", db)
	return db
}

func (ix *indexer) selectIndexBranch(ctx context.Context, repoDir, defaultBranch string) string {
	if defaultBranch != "" {
		return defaultBranch
	}
	branch, err := currentBranch(ctx, repoDir)
	if err != nil {
		ix.repoWarnf("could not determine current branch: %v", err)
		return ""
	}
	if branch != "" {
		ix.repoInfof("using current branch: %s", branch)
	}
	return branch
}

func (ix *indexer) detectIndexedCommit(ctx context.Context, repoDir string) string {
	commit, err := headCommit(ctx, repoDir)
	if err != nil {
		ix.repoWarnf("could not determine HEAD commit: %v", err)
		return ""
	}
	return commit
}

func (ix *indexer) evaluateSkip(slug, branch, commit string) (string, string) {
	if ix.cache == nil || branch == "" || commit == "" {
		return "", ""
	}
	last, ok := ix.cache.LastCommit(slug, branch)
	if !ok {
		return "", ""
	}
	if last == commit {
		msg := fmt.Sprintf("commit %s on %s already indexed", shortCommit(commit), branch)
		return msg, last
	}
	return "", last
}

func boolPtr(b bool) *bool {
	return &b
}

func shortCommit(commit string) string {
	if len(commit) > shortCommitLen {
		return commit[:shortCommitLen]
	}
	return commit
}

func diffFilesSince(ctx context.Context, repoDir, baseCommit string) ([]string, error) {
	if baseCommit == "" {
		return nil, errors.New("base commit is required to compute a diff")
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "diff", "--name-only", baseCommit, "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s HEAD: %w", baseCommit, err)
	}

	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	return files, nil
}

type newlineFeeder struct {
	done     chan struct{}
	interval time.Duration
	once     sync.Once
	first    bool
}

func newNewlineFeeder(interval time.Duration) *newlineFeeder {
	return &newlineFeeder{
		interval: interval,
		first:    true,
		done:     make(chan struct{}),
	}
}

func (nf *newlineFeeder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if !nf.first {
		timer := time.NewTimer(nf.interval)
		defer timer.Stop()
		select {
		case <-nf.done:
			return 0, io.EOF
		case <-timer.C:
		}
	} else {
		nf.first = false
	}

	select {
	case <-nf.done:
		return 0, io.EOF
	default:
	}

	p[0] = '\n'
	return 1, nil
}

func (nf *newlineFeeder) Close() error {
	nf.once.Do(func() {
		close(nf.done)
	})
	return nil
}
