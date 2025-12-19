package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/quick"
	"unicode"
)

func TestRunParallelIndexing(t *testing.T) {
	rootDir := t.TempDir()
	repoOne := filepath.Join(rootDir, "repo-one")
	repoTwo := filepath.Join(rootDir, "repo-two")

	initGitRepo(t, repoOne)
	initGitRepo(t, repoTwo)

	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("make bin dir: %v", err)
	}

	codexPath := filepath.Join(binDir, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write codex stub: %v", err)
	}

	pathEnv := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathEnv)

	var (
		summaryPath = filepath.Join(rootDir, "summary.json")
		cachePath   = filepath.Join(rootDir, "cache.json")
	)

	if err := Run(rootDir, false, summaryPath, cachePath, nil, 0, 2); err != nil {
		t.Fatalf("run indexer: %v", err)
	}

	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary json: %v", err)
	}

	var payload struct {
		Repos []RepoResult `json:"repos"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode summary json: %v", err)
	}

	if len(payload.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(payload.Repos))
	}

	slugs := make(map[string]bool)
	for _, repo := range payload.Repos {
		if !repo.CodexRan {
			t.Fatalf("expected codex to run for %s", repo.Path)
		}
		slugs[repo.CollectionSlug] = true
	}

	if !slugs["repo-one"] || !slugs["repo-two"] {
		t.Fatalf("unexpected slugs: %v", slugs)
	}
}

func TestSanitizePathComponentProperty(t *testing.T) {
	check := func(input string) bool {
		output := sanitizePathComponent(input)
		if output == "" {
			return false
		}

		for _, r := range output {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
				continue
			}
			return false
		}

		return true
	}

	if err := quick.Check(check, nil); err != nil {
		t.Fatalf("property check failed: %v", err)
	}
}

func initGitRepo(t *testing.T, repoDir string) {
	t.Helper()

	const defaultBranch = "trunk"

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}

	if err := runGit(repoDir, "init", "-b", defaultBranch); err != nil {
		if err := runGit(repoDir, "init"); err != nil {
			t.Fatalf("git init: %v", err)
		}
		if err := runGit(repoDir, "checkout", "-b", defaultBranch); err != nil {
			t.Fatalf("git checkout -b %s: %v", defaultBranch, err)
		}
	}

	if err := runGit(repoDir, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := runGit(repoDir, "config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config name: %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := runGit(repoDir, "add", "README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := runGit(repoDir, "commit", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func runGit(repoDir string, args ...string) error {
	argv := slices.Concat([]string{"-C", repoDir}, args)
	cmd := exec.Command("git", argv...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
}
