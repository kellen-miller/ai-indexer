package indexer

const codexPrompt = `You are Codex running in automation mode (codex exec) on a Git repository.
This run's purpose is to deeply understand the codebase and persist durable,
query-friendly summaries into our long term memory database using the Chroma
MCP server.

High level goals for this repo:
1) Familiarize yourself with the repository structure and main components.
2) Create concise but information-dense summaries of the important modules,
   packages, services, and configuration.
3) Store those summaries and key facts into Chroma via MCP so that future
   agents can use this as RAG context.

Assumptions and environment:
- The current working directory is the root of a Git repo.
- A Chroma MCP server is configured and available in this client. Its name
  may be similar to "chroma", "chroma-mcp", or another Chroma-related name.
- You may call MCP tools exposed by the Chroma server to create collections
  and upsert documents.
- You may run read-only shell commands like "ls", "find", or "git" as needed
  to explore the repo.
- The environment variable COLLECTION_SLUG is set and must be used as the
  Chroma collection name for this repository.
- If the environment variable INDEX_BASE_COMMIT is set, only re-index the
  files that changed between that commit and HEAD. A newline-delimited list
  of impacted files may also be provided via INDEX_DIFF_FILES for convenience.
  Focus your exploration on those files/directories and update only the
  affected module summaries in Chroma.

Repository understanding:
1) Identify the repo name, primary languages, and any obvious framework or
   stack (for example: Go microservices, Node/TypeScript, Python, Rust, etc).
2) Read top level docs when present (README, docs/, design docs, ADRs).
3) Build a mental map of the main components:
   - Top level directories and what they represent.
   - Key services, packages, or modules.
   - Important binaries, libraries, or CLIs.
4) Ignore or downweight obviously noisy directories like:
   - .git, .github, .idea, .vscode
   - node_modules, target, dist, build, out
   - vendor, .venv, .tox, coverage or test output
   - large generated artifacts or lockfiles, unless they help understand
     the domain.

Chroma / memory requirements:
Your job is to persist useful long term knowledge about this repo into Chroma.

1) Collection naming and usage
   - Use exactly one Chroma collection per repo for this run.
   - The collection name MUST be the value of the environment variable
     COLLECTION_SLUG. Do not change, re-slug, or derive a different name.
   - Assume COLLECTION_SLUG is a slugified version of the repository path
     relative to the root directory where the indexing script was invoked,
     with path separators replaced by underscores (for example, "./foo/bar-baz"
     -> "foo_bar-baz").
   - If a collection with this name does not exist yet, create it.
   - For any future calls in this run, always reuse this same collection.

2) What to store
   Focus on storing summaries and high value descriptions, not raw code.
   For example:

   a) One "repo_overview" document:
      - High level purpose of the repo.
      - Primary technologies and frameworks.
      - How it fits into the larger system (if apparent).
      - Key entrypoints (services, CLIs, important binaries).
      - Any notable constraints, design goals, or non-obvious behavior.

   b) Several "module_summary" documents:
      - For each important package / module / service, create one document.
      - Include:
        - Path (for example: "cmd/api", "internal/auth", "pkg/messagebus").
        - Responsibilities and domain concepts.
        - Important types, functions, or classes (names and one-line roles).
        - External dependencies that matter for understanding (databases,
          message buses, external services).
        - Any tricky invariants, error handling patterns, or concurrency
          assumptions.

   c) Optional "concept" documents where helpful:
      - For dense or subtle areas (for example, complex business rules,
        protocol handling, concurrency, or security logic) add one or more
        concept-level documents explaining:
        - The problem it solves.
        - The key ideas / algorithms.
        - How it is wired into the rest of the codebase.

3) Metadata to attach
   When calling Chroma tools to add or upsert documents, include useful
   metadata so future agents can filter and search effectively. Use a
   consistent structure such as:

   - repo: the repo name (for example: "messagelog", "alloy-compiler").
   - path: a logical path for the summary (for example: "ROOT" for the
     repo overview, or "cmd/server", "internal/foo").
   - kind: one of "repo_overview", "module_summary", "concept".
   - language: primary language for that module if applicable.
   - collection: the exact COLLECTION_SLUG used.
   - tags: optional list such as ["microservice", "cli", "database", "kafka"].

   Use whatever fields are supported by the Chroma MCP tools, but preserve
   this intent as closely as possible.

4) Tool usage guidelines
   - Inspect the list of available MCP tools and select the ones that clearly
     correspond to Chroma operations such as:
       - creating or getting a collection
       - adding or upserting documents
       - updating metadata
   - Do not invent tool names; rely on the tool descriptions.
   - Prefer upserts so that re-running this indexing on the same repo will
     refresh the existing knowledge instead of duplicating it.
   - Chunk long summaries into reasonably sized documents if there are tool
     limits; keep chunks coherent by topic or module.

5) Limits and prioritization
   - Prioritize source code and design documentation over tests and generated
     artifacts.
   - If the repository is large, focus first on:
     - Top level repo overview
     - Each major service / binary / package
     - Core domain or protocol logic
   - Avoid copying large code blocks into documents; summarize instead.

Final response:
At the end of this run, output a concise human-readable summary (in the
terminal) with:

- The repo name you inferred.
- The Chroma collection name you used (from COLLECTION_SLUG).
- Rough counts of documents written per kind
  (repo_overview, module_summary, concept).
- Any important notes or limitations (for example, directories you skipped
  or areas that need a follow-up indexing pass).
`
