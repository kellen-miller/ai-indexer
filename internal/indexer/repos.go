package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const shortCommitLen = 7

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
	return repos, err
}

func (ix *indexer) processRepo(ctx context.Context, repoDir, rootDir string, dryRun bool) RepoResult {
	slug := computeCollectionSlug(rootDir, repoDir)
	ix.repoHeader(repoDir, slug)

	result := RepoResult{
		Path:           repoDir,
		CollectionSlug: slug,
		DryRun:         dryRun,
	}

	defaultBranch := ix.reportDefaultBranch(ctx, repoDir)
	result.DefaultBranch = defaultBranch

	result.CheckoutOK, result.PullOK = ix.maybeSynchronizeRepo(ctx, repoDir, defaultBranch, dryRun)

	indexBranch := ix.selectIndexBranch(ctx, repoDir, defaultBranch)
	if indexBranch != "" && result.DefaultBranch == "" {
		result.DefaultBranch = indexBranch
	}

	result.IndexedCommit = ix.detectIndexedCommit(ctx, repoDir)
	result.SkipReason, result.CachedCommit = ix.evaluateSkip(slug, indexBranch, result.IndexedCommit)

	if result.SkipReason != "" {
		ix.repoInfof("skipping indexing: %s", result.SkipReason)
		ix.outln("")
		return result
	}

	ran, exitCode, codexErr := ix.runCodex(ctx, repoDir, slug, dryRun)
	result.CodexRan = ran
	if exitCode != nil {
		result.CodexExitCode = exitCode
	}
	if codexErr != nil {
		result.Error = codexErr.Error()
	} else if !dryRun && ix.cache != nil && indexBranch != "" && result.IndexedCommit != "" {
		ix.cache.Update(slug, indexBranch, result.IndexedCommit)
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

func (ix *indexer) checkoutAndPull(ctx context.Context, repoDir, branch string) (bool, bool) {
	ix.repoInfof("checking out %s", branch)
	co := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", branch)
	if err := co.Run(); err != nil {
		ix.repoWarnf("git checkout failed — continuing on current branch")
		return false, false
	}

	ix.repoInfof("pulling latest changes")
	pl := exec.CommandContext(ctx, "git", "-C", repoDir, "pull", "--ff-only")
	if err := pl.Run(); err != nil {
		ix.repoWarnf("git pull failed — using local state")
		ok := true
		return ok, false
	}

	ix.repoInfof("repository updated to latest")
	return true, true
}

func (ix *indexer) runCodex(ctx context.Context, repoDir, slug string, dryRun bool) (bool, *int, error) {
	cmd := exec.CommandContext(ctx, "codex", "exec",
		"--cd", repoDir,
		"--sandbox", "danger-full-access",
		codexPrompt)
	// Force Codex to read EOF immediately so it doesn't wait for user input
	// after the first turn, which previously left the indexer hanging.
	cmd.Stdin = strings.NewReader("")
	env := os.Environ()
	env = append(env, "COLLECTION_SLUG="+slug)
	cmd.Env = env
	cmd.Stdout = ix.stdout
	cmd.Stderr = ix.stderr

	if dryRun {
		ix.repoInfof("[dry-run] COLLECTION_SLUG=%q codex exec --cd %q --sandbox danger-full-access '<PROMPT>'",
			slug,
			repoDir)
		return false, nil, nil
	}

	ix.repoInfof("running Codex indexing")
	if err := cmd.Run(); err != nil {
		exitCode := 1
		ee := &exec.ExitError{}
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
		ix.repoWarnf("Codex exited with code %d", exitCode)
		return true, &exitCode, err
	}

	ix.repoInfof("Codex indexing completed")
	return true, nil, nil
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

func (ix *indexer) maybeSynchronizeRepo(ctx context.Context, repoDir, branch string, dryRun bool) (*bool, *bool) {
	if branch == "" {
		return nil, nil
	}
	if dryRun {
		ix.repoInfof("[dry-run] git -C %q checkout %s", repoDir, branch)
		ix.repoInfof("[dry-run] git -C %q pull --ff-only", repoDir)
		return nil, nil
	}
	cOK, pOK := ix.checkoutAndPull(ctx, repoDir, branch)
	return boolPtr(cOK), boolPtr(pOK)
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
