# Repository Guidelines

## Project Structure & Module Organization
- `cmd/cli/` holds the CLI entrypoint that builds the `indexer` binary.
- `internal/indexer/` contains the core indexing workflow (repo discovery, Codex runs, commit cache).
- `build/` is the local output directory for compiled binaries.
- Root configuration lives in `go.mod`, `Taskfile.yaml`, and `README.md`.
- Generated artifacts such as `codex_index_summary.json` and `codex_commit_cache.json` are gitignored.

## Build, Test, and Development Commands
- `go build -o build/indexer ./cmd/cli` builds the CLI.
- `go run ./cmd/cli <root-directory>` runs the indexer against a directory of repos.
- `go test ./...` runs all Go tests.
- Taskfile helpers:
  - `task build`, `task test`, `task check` (fmt + lint + test).
  - `task fmt` / `task lint` for formatting and linting.
  - `task dry-run` or `task index-dev` for common local runs.

## Coding Style & Naming Conventions
- Go 1.25 code with explicit parameter types (avoid `x, y int`).
- Struct literals: one field per line, build values first, then construct.
- Prefer early returns; wrap errors with `%w` and keep error strings lowercase.
- Naming: camelCase for locals, PascalCase for exported, acronyms in all caps (ID, HTTP).
- Formatting/linting runs via `golangci-lint` (`task fmt`, `task lint`).

## Testing Guidelines
- Use the Go `testing` package; tests live alongside code as `*_test.go` (see `internal/indexer/`).
- Prefer table-driven tests with map cases and avoid loop-variable rebinding.
- Default test command: `go test -v -race ./...` (or `task test`).

## Commit & Pull Request Guidelines
- Commit history favors Conventional Commits with the `indexer` scope (e.g., `feat(indexer): add parallel indexing`).
- Keep the subject under ~50 characters and include context in the body when behavior changes.
- PRs should include a short summary, test results, and README/flag updates for user-facing CLI changes.

## Security & Configuration Tips
- Requires `git`, the `codex` CLI, and a configured Chroma MCP server.
- Codex runs with dangerous sandbox flags—only index repositories you trust.
- Use CLI flags like `--parallel`, `--summary-json`, `--commit-cache`, and `--codex-timeout` to control behavior.
