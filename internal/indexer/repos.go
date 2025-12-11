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

	var (
		defaultBranch string
		checkoutOK    *bool
		pullOK        *bool
		codexRan      bool
		codexExit     *int
		errMsg        string
	)

	db, err := detectDefaultBranch(ctx, repoDir)
	if err != nil {
		ix.repoWarnf("could not detect default branch: %v", err)
	} else if db == "" {
		ix.repoWarnf("could not detect default branch — skipping checkout/pull")
	} else {
		defaultBranch = db
		ix.repoInfof("default branch: %s", defaultBranch)
	}

	if defaultBranch != "" {
		if dryRun {
			ix.repoInfof("[dry-run] git -C %q checkout %s", repoDir, defaultBranch)
			ix.repoInfof("[dry-run] git -C %q pull --ff-only", repoDir)
		} else {
			cOK, pOK := ix.checkoutAndPull(ctx, repoDir, defaultBranch)
			checkoutOK = &cOK
			pullOK = &pOK
		}
	}

	ran, exitCode, codexErr := ix.runCodex(ctx, repoDir, slug, dryRun)
	codexRan = ran
	if exitCode != nil {
		codexExit = exitCode
	}
	if codexErr != nil {
		if errMsg != "" {
			errMsg += "; "
		}
		errMsg += codexErr.Error()
	}

	ix.outln("")

	return RepoResult{
		Path:           repoDir,
		CollectionSlug: slug,
		DefaultBranch:  defaultBranch,
		CheckoutOK:     checkoutOK,
		PullOK:         pullOK,
		CodexRan:       codexRan,
		CodexExitCode:  codexExit,
		Error:          errMsg,
		DryRun:         dryRun,
	}
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
		"-c", "\"model_reasoning_effort=medium\"",
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
		ix.repoInfof("[dry-run] COLLECTION_SLUG=%q codex exec --cd %q --sandbox danger-full-access -c \"model_reasoning_effort=medium\" '<PROMPT>'",
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
