package indexer

import (
	"io"
	"strings"
	"testing"
)

func TestFormatGitStatus(t *testing.T) {
	tests := map[string]struct {
		result RepoResult
		want   string
	}{
		"unknown when no branch": {
			result: RepoResult{},
			want:   "unknown",
		},
		"clean branch": {
			result: RepoResult{DefaultBranch: "main"},
			want:   "main",
		},
		"checkout failed": {
			result: RepoResult{DefaultBranch: "main", CheckoutOK: boolPtr(false)},
			want:   "main, checkout failed",
		},
		"pull failed": {
			result: RepoResult{DefaultBranch: "main", PullOK: boolPtr(false)},
			want:   "main, pull failed",
		},
		"both failures": {
			result: RepoResult{DefaultBranch: "main", CheckoutOK: boolPtr(false), PullOK: boolPtr(false)},
			want:   "main, checkout failed, pull failed",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := formatGitStatus(&tc.result)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFormatCodexStatus(t *testing.T) {
	exitCode := 2

	tests := map[string]struct {
		result RepoResult
		want   string
	}{
		"skip reason wins": {
			result: RepoResult{SkipReason: "skip"},
			want:   "skipped",
		},
		"dry run": {
			result: RepoResult{DryRun: true},
			want:   "dry-run",
		},
		"not run": {
			result: RepoResult{CodexRan: false},
			want:   "not run",
		},
		"ok": {
			result: RepoResult{CodexRan: true},
			want:   "ok",
		},
		"exit code": {
			result: RepoResult{CodexRan: true, CodexExitCode: &exitCode},
			want:   "exit 2",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := formatCodexStatus(&tc.result)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestRenderStatus(t *testing.T) {
	ix := newIndexer(io.Discard, io.Discard, nil, nil, 0, 1)
	var exitCode int

	tests := map[string]struct {
		result     RepoResult
		wantStatus string
		wantCounts summaryCounts
	}{
		"error by codex exit": {
			result:     RepoResult{CodexRan: true, CodexExitCode: &exitCode},
			wantStatus: "error",
			wantCounts: summaryCounts{err: 1},
		},
		"error by error message": {
			result:     RepoResult{Error: "boom"},
			wantStatus: "error",
			wantCounts: summaryCounts{err: 1},
		},
		"warn by checkout": {
			result:     RepoResult{CheckoutOK: boolPtr(false)},
			wantStatus: "warn",
			wantCounts: summaryCounts{warn: 1},
		},
		"ok": {
			result:     RepoResult{},
			wantStatus: "ok",
			wantCounts: summaryCounts{ok: 1},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			counts := summaryCounts{}
			status := ix.renderStatus(&tc.result, &counts)
			if status != tc.wantStatus {
				t.Fatalf("expected status %q, got %q", tc.wantStatus, status)
			}
			if counts != tc.wantCounts {
				t.Fatalf("expected counts %+v, got %+v", tc.wantCounts, counts)
			}
		})
	}
}

func TestColorStatus(t *testing.T) {
	tests := map[string]struct {
		status string
		prefix string
	}{
		"ok": {
			status: "ok",
			prefix: colorGreen,
		},
		"warn": {
			status: "warn",
			prefix: colorYellow,
		},
		"error": {
			status: "error",
			prefix: colorRed,
		},
		"other": {
			status: "other",
			prefix: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := colorStatus(tc.status)
			if tc.prefix == "" {
				if got != tc.status {
					t.Fatalf("expected %q, got %q", tc.status, got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.prefix) {
				t.Fatalf("expected %q to start with %q", got, tc.prefix)
			}
			if !strings.HasSuffix(got, colorReset) {
				t.Fatalf("expected %q to end with reset", got)
			}
		})
	}
}
