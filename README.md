# ai-indexer

Batch-run `codex exec` across many Git repositories and persist durable
summaries into Chroma collections. The indexer scans a root directory for
Git repos, computes a stable collection slug per repo, and runs Codex with
incremental indexing when possible.

## Features

- Discovers repositories by walking for `.git` directories.
- Uses a per-repo collection slug derived from the root-relative path.
- Supports incremental indexing with a commit cache and file diffs.
- Runs indexing in parallel with a configurable worker count.
- Produces a colored summary table and a JSON report.

## Requirements

- Go 1.25+
- `git` on your PATH
- `codex` CLI on your PATH
- Chroma MCP server configured in Codex

## Install

```bash
go build -o build/indexer ./cmd/cli
```

Or run directly:

```bash
go run ./cmd/cli <root-directory>
```

## Usage

```bash
indexer [flags] <root-directory>
```

### Common examples

Dry run:

```bash
go run ./cmd/cli --dry-run ~/development
```

Parallel indexing (4 workers):

```bash
go run ./cmd/cli --parallel 4 ~/development
```

Skip a repo by name, slug, or path:

```bash
go run ./cmd/cli --skip-repo my-repo --skip-repo tools/legacy ~/development
```

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--dry-run`, `-n` | `false` | Print actions but do not run Codex. |
| `--summary-json` | `codex_index_summary.json` | Path to JSON summary output. |
| `--commit-cache` | `codex_commit_cache.json` | Commit cache path (use `--no-commit-cache` to disable). |
| `--no-commit-cache` | `false` | Disable the commit cache. |
| `--skip-repo` | `[]` | Skip repo by slug, basename, or path (repeatable). |
| `--codex-timeout` | `45m` | Max duration per repo (0 disables timeout). |
| `--parallel` | `1` | Number of repositories to index concurrently. |

## How it works

### Collection slug

Each repo gets a collection slug computed from the root-relative path:

- `.` becomes `root`
- path separators are replaced with `_`

For example, `~/development/tools/legacy` becomes `tools_legacy`.

### Incremental indexing

The commit cache stores the last indexed commit per repo and branch. If the
current `HEAD` matches the cached commit, the repo is skipped. When the commit
differs, the indexer computes `git diff --name-only <cached> HEAD` and passes:

- `INDEX_BASE_COMMIT` with the cached commit
- `INDEX_DIFF_FILES` as a newline-delimited file list

If diff computation fails, the indexer falls back to a full indexing run.

### Default branch worktree

When possible, the indexer fetches `origin/<default-branch>` and adds a
temporary worktree under `$TMPDIR/codex-indexer-worktrees` to ensure indexing
the latest default branch. If fetch or worktree add fails, it indexes the
current working tree instead.

### Parallelism

Set `--parallel` to run multiple repos at once. Output is serialized to avoid
garbled logs, but repo sections can still interleave. Start small (2-4) if
your machine or network is constrained.

## Output

- A colored summary table printed to stdout.
- A JSON report written to `--summary-json`, including per-repo status, commit
  info, and Codex exit codes.

## Development

Run tests:

```bash
go test ./...
```

Using Taskfile:

```bash
task build
task test
task check
```

## Safety

Codex runs with `--sandbox danger-full-access` and
`--dangerously-bypass-approvals-and-sandbox`. Use this tool only on
repositories you trust.
