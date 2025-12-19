package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommitCacheUpdateAndLastCommit(t *testing.T) {
	cache := &commitCache{
		data: make(map[string]map[string]string),
	}

	cache.Update("repo-one", "main", "abc123")

	commit, ok := cache.LastCommit("repo-one", "main")
	if !ok {
		t.Fatalf("expected commit to be present")
	}
	if commit != "abc123" {
		t.Fatalf("expected commit abc123, got %q", commit)
	}

	tests := map[string]struct {
		repo   string
		branch string
		ok     bool
	}{
		"missing repo": {
			repo:   "repo-two",
			branch: "main",
			ok:     false,
		},
		"missing branch": {
			repo:   "repo-one",
			branch: "dev",
			ok:     false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := cache.LastCommit(tc.repo, tc.branch)
			if ok != tc.ok {
				t.Fatalf("expected ok=%t, got %t", tc.ok, ok)
			}
		})
	}
}

func TestCommitCacheUpdateIgnoresEmptyInputs(t *testing.T) {
	cache := &commitCache{
		data: make(map[string]map[string]string),
	}

	cache.Update("", "main", "abc123")
	cache.Update("repo", "", "abc123")
	cache.Update("repo", "main", "")

	if len(cache.data) != 0 {
		t.Fatalf("expected empty cache data, got %v", cache.data)
	}
}

func TestCommitCacheSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")

	cache := &commitCache{
		path: path,
		data: map[string]map[string]string{
			"repo": {
				"main": "abc123",
			},
		},
	}

	if err := cache.Save(); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("expected cache file to be user-only, got mode %o", info.Mode().Perm())
	}

	loaded, err := loadCommitCache(path)
	if err != nil {
		t.Fatalf("load cache: %v", err)
	}

	commit, ok := loaded.LastCommit("repo", "main")
	if !ok {
		t.Fatalf("expected commit after load")
	}
	if commit != "abc123" {
		t.Fatalf("expected commit abc123, got %q", commit)
	}
}

func TestCommitCacheSaveNoPath(t *testing.T) {
	cache := &commitCache{
		data: make(map[string]map[string]string),
	}

	if err := cache.Save(); err != nil {
		t.Fatalf("save cache: %v", err)
	}
}

func TestLoadCommitCacheMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	cache, err := loadCommitCache(path)
	if err != nil {
		t.Fatalf("load missing cache: %v", err)
	}
	if cache == nil {
		t.Fatalf("expected cache to be initialized")
	}
	if len(cache.data) != 0 {
		t.Fatalf("expected empty cache, got %v", cache.data)
	}
}
