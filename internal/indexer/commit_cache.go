package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type commitCache struct {
	data map[string]map[string]string
	path string
}

func loadCommitCache(path string) (*commitCache, error) {
	cache := &commitCache{
		path: path,
		data: make(map[string]map[string]string),
	}
	if path == "" {
		return cache, nil
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cache, nil
		}
		return nil, fmt.Errorf("read commit cache: %w", err)
	}
	if len(bytes) == 0 {
		return cache, nil
	}

	if err := json.Unmarshal(bytes, &cache.data); err != nil {
		return nil, fmt.Errorf("decode commit cache: %w", err)
	}
	return cache, nil
}

func (c *commitCache) Save() error {
	if c == nil || c.path == "" {
		return nil
	}

	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode commit cache: %w", err)
	}

	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write commit cache: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		return fmt.Errorf("persist commit cache: %w", err)
	}

	return nil
}

func (c *commitCache) LastCommit(repoSlug, branch string) (string, bool) {
	if c == nil || repoSlug == "" || branch == "" {
		return "", false
	}

	branches, ok := c.data[repoSlug]
	if !ok {
		return "", false
	}

	commit, ok := branches[branch]
	return commit, ok
}

func (c *commitCache) Update(repoSlug, branch, commit string) {
	if c == nil || repoSlug == "" || branch == "" || commit == "" {
		return
	}

	branches, ok := c.data[repoSlug]
	if !ok {
		branches = make(map[string]string)
		c.data[repoSlug] = branches
	}

	branches[branch] = commit
}
