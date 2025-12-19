package indexer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const worktreeRootDirName = "codex-indexer-worktrees"

func sanitizePathComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	out := b.String()
	if strings.Trim(out, "_") == "" {
		return "component"
	}
	return out
}

func (ix *indexer) prepareIndexWorkspace(ctx context.Context, repoDir, slug, branch string, dryRun bool) (string, *bool, *bool, func()) {
	if branch == "" {
		return repoDir, nil, nil, nil
	}

	safeSlug := sanitizePathComponent(slug)
	safeBranch := sanitizePathComponent(branch)
	worktreeBase := filepath.Join(os.TempDir(), worktreeRootDirName)
	worktreePath := filepath.Join(worktreeBase, safeSlug+"-"+safeBranch)

	if dryRun {
		ix.repoInfof("[dry-run] git -C %q fetch --prune origin %s", repoDir, branch)
		ix.repoInfof("[dry-run] git -C %q worktree add --force --detach %q origin/%s", repoDir, worktreePath, branch)
		return repoDir, nil, nil, nil
	}

	if err := os.RemoveAll(worktreePath); err != nil {
		ix.repoWarnf("could not clean worktree path %q: %v", worktreePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		ix.repoWarnf("could not prepare worktree parent dir %q: %v", filepath.Dir(worktreePath), err)
		return repoDir, boolPtr(false), boolPtr(false), nil
	}

	fetch := exec.CommandContext(ctx, "git", "-C", repoDir, "fetch", "--prune", "origin", branch)
	if err := fetch.Run(); err != nil {
		ix.repoWarnf("git fetch origin %s failed: %v — using current working tree", branch, err)
		return repoDir, boolPtr(false), boolPtr(false), nil
	}

	add := exec.CommandContext(ctx, "git", "-C", repoDir, "worktree", "add", "--force", "--detach", worktreePath, "origin/"+branch)
	if err := add.Run(); err != nil {
		ix.repoWarnf("git worktree add for %s failed: %v — using current working tree", branch, err)
		return repoDir, boolPtr(false), boolPtr(true), nil
	}

	ix.repoInfof("using temporary worktree for %s at %s", branch, worktreePath)

	cleanup := func() {
		rmCtx := context.Background()
		rm := exec.CommandContext(rmCtx, "git", "-C", repoDir, "worktree", "remove", "--force", worktreePath)
		if err := rm.Run(); err != nil {
			ix.repoWarnf("failed to remove worktree %q: %v", worktreePath, err)
		}
		if err := os.RemoveAll(worktreePath); err != nil {
			ix.repoWarnf("failed to delete worktree dir %q: %v", worktreePath, err)
		}
	}

	return worktreePath, boolPtr(true), boolPtr(true), cleanup
}
