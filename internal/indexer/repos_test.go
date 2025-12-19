package indexer

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestComputeCollectionSlug(t *testing.T) {
	rootDir := t.TempDir()
	repoDir := filepath.Join(rootDir, "services", "api")

	tests := map[string]struct {
		repoDir string
		want    string
	}{
		"root repo": {
			repoDir: rootDir,
			want:    "root",
		},
		"nested repo": {
			repoDir: repoDir,
			want:    "services_api",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := computeCollectionSlug(rootDir, tc.repoDir)
			if tc.want == "" {
				if got == "" {
					t.Fatalf("expected non-empty slug")
				}
				return
			}
			if got != tc.want {
				t.Fatalf("expected slug %q, got %q", tc.want, got)
			}
		})
	}
}

func TestShouldSkipRepo(t *testing.T) {
	rootDir := t.TempDir()
	repoDir := filepath.Join(rootDir, "services", "api")
	slug := computeCollectionSlug(rootDir, repoDir)

	tests := map[string]struct {
		skip        []string
		wantSkip    bool
		wantMention string
	}{
		"match slug": {
			skip:        []string{slug},
			wantSkip:    true,
			wantMention: slug,
		},
		"match basename": {
			skip:        []string{"api"},
			wantSkip:    true,
			wantMention: "api",
		},
		"match relative path": {
			skip:        []string{"services/api"},
			wantSkip:    true,
			wantMention: "services/api",
		},
		"match relative path cleaned": {
			skip:        []string{"./services/api"},
			wantSkip:    true,
			wantMention: "./services/api",
		},
		"match absolute path": {
			skip:        []string{repoDir},
			wantSkip:    true,
			wantMention: repoDir,
		},
		"no match": {
			skip:     []string{"other"},
			wantSkip: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ix := newIndexer(io.Discard, io.Discard, nil, tc.skip, 0, 1)
			skip, reason := ix.shouldSkipRepo(rootDir, repoDir, slug)
			if skip != tc.wantSkip {
				t.Fatalf("expected skip=%t, got %t", tc.wantSkip, skip)
			}
			if tc.wantSkip && !strings.Contains(reason, tc.wantMention) {
				t.Fatalf("expected reason to mention %q, got %q", tc.wantMention, reason)
			}
			if !tc.wantSkip && reason != "" {
				t.Fatalf("expected empty reason, got %q", reason)
			}
		})
	}
}

func TestDiffFilesSince(t *testing.T) {
	ctx := t.Context()
	repoDir := t.TempDir()

	initGitRepo(t, repoDir)

	baseCommit, err := headCommit(ctx, repoDir)
	if err != nil {
		t.Fatalf("head commit: %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := runGit(repoDir, "add", "README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := runGit(repoDir, "commit", "-m", "update readme"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	files, err := diffFilesSince(ctx, repoDir, baseCommit)
	if err != nil {
		t.Fatalf("diff files: %v", err)
	}
	if !slices.Contains(files, "README.md") {
		t.Fatalf("expected README.md in diff, got %v", files)
	}
}

func TestDiffFilesSinceRequiresBaseCommit(t *testing.T) {
	ctx := t.Context()
	repoDir := t.TempDir()

	initGitRepo(t, repoDir)

	_, err := diffFilesSince(ctx, repoDir, "")
	if err == nil {
		t.Fatalf("expected error for empty base commit")
	}
}

func TestNewlineFeeder(t *testing.T) {
	feeder := newNewlineFeeder(10 * time.Millisecond)
	buf := make([]byte, 1)

	n, err := feeder.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if n != 1 || buf[0] != '\n' {
		t.Fatalf("expected newline on first read, got %q", buf)
	}

	if err := feeder.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = feeder.Read(buf)
	if err == nil {
		t.Fatalf("expected EOF after close")
	}
}
