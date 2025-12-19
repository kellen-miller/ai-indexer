package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func writeSummaryJSON(path, rootDir string, dryRun bool, results []RepoResult) error {
	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"root_dir":     rootDir,
		"dry_run":      dryRun,
		"repos":        results,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal summary json: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write summary json: %w", err)
	}

	return nil
}
