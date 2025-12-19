package indexer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSummaryJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.json")

	results := []RepoResult{
		{
			Path:           "/tmp/repo",
			CollectionSlug: "repo",
			CodexRan:       true,
		},
	}

	if err := writeSummaryJSON(path, "/tmp", true, results); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}

	var payload struct {
		GeneratedAt string       `json:"generated_at"`
		RootDir     string       `json:"root_dir"`
		DryRun      bool         `json:"dry_run"`
		Repos       []RepoResult `json:"repos"`
	}

	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if payload.GeneratedAt == "" {
		t.Fatalf("expected generated_at to be set")
	}
	if payload.RootDir != "/tmp" {
		t.Fatalf("expected root_dir /tmp, got %q", payload.RootDir)
	}
	if !payload.DryRun {
		t.Fatalf("expected dry_run true, got %t", payload.DryRun)
	}
	if len(payload.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(payload.Repos))
	}
	if payload.Repos[0].CollectionSlug != "repo" {
		t.Fatalf("expected repo slug to be repo, got %q", payload.Repos[0].CollectionSlug)
	}
}
