package indexer

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/schollz/progressbar/v3"
)

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

var (
	headlineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#bd93f9")).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272a4"))

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8be9fd"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50fa7b")).
			Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f1fa8c")).
			Bold(true)

	dangerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Bold(true)

	repoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff79c6")).
			Bold(true)

	slugStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8f8f2"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#bd93f9")).
			Padding(0, 1)
)

// RepoResult captures per-repo outcome for JSON summary.
type RepoResult struct {
	Path           string `json:"path"`
	CollectionSlug string `json:"collection_slug"`
	DefaultBranch  string `json:"default_branch,omitempty"`
	CheckoutOK     *bool  `json:"checkout_ok,omitempty"`
	PullOK         *bool  `json:"pull_ok,omitempty"`
	CodexRan       bool   `json:"codex_ran"`
	CodexExitCode  *int   `json:"codex_exit_code,omitempty"`
	Error          string `json:"error,omitempty"`
	DryRun         bool   `json:"dry_run"`
}

// Run executes the indexing workflow for the provided directory.
func Run(rootDir string, dryRun bool, summaryJSON string) error {
	infoHeader := headlineStyle.Render("🔍 Codex Repo Indexer") + " " + mutedStyle.Render("(Dracula mode)")
	fmt.Println(infoHeader)
	fmt.Println(infoStyle.Render("Root Directory: ") + rootDir)
	fmt.Println(infoStyle.Render("Dry Run Mode: ") + fmt.Sprintf("%v", dryRun))
	fmt.Println()

	repos, err := findGitRepos(rootDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, dangerStyle.Render("Error scanning for git repos: "), err)
		return fmt.Errorf("scan git repos: %w", err)
	}
	if len(repos) == 0 {
		fmt.Println(dangerStyle.Render("No git repositories found."))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Found %d git repos under %s", len(repos), rootDir)))
	fmt.Println()

	bar := progressbar.NewOptions(len(repos),
		progressbar.OptionSetDescription("Indexing repos…"),
		progressbar.OptionSetWriter(os.Stdout),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(30),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionShowIts(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)

	var results []RepoResult

	for _, repo := range repos {
		if err := bar.Add(1); err != nil {
			// Not fatal, just continue
		}
		fmt.Println()
		res := processRepo(repo, rootDir, dryRun)
		results = append(results, res)
	}

	fmt.Println()
	fmt.Println(headlineStyle.Render("📊 Summary"))
	fmt.Println()

	printSummaryTable(results)

	if err := writeSummaryJSON(summaryJSON, rootDir, dryRun, results); err != nil {
		fmt.Fprintln(os.Stderr, dangerStyle.Render("Error writing JSON summary: "), err)
		return fmt.Errorf("write summary json: %w", err)
	}

	fmt.Println(mutedStyle.Render("📝 JSON summary written to " + summaryJSON))
	return nil
}

// findGitRepos walks root and returns directories containing a .git folder.
func findGitRepos(root string) ([]string, error) {
	var repos []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			repos = append(repos, filepath.Dir(path))
			// Do not SkipDir on parent; if nested repos exist deeper we still want them.
			return nil
		}
		return nil
	})
	return repos, err
}

func processRepo(repoDir, rootDir string, dryRun bool) RepoResult {
	header := panelStyle.Render("📦 Repository: " + repoStyle.Render(repoDir))
	fmt.Println(header)

	slug := computeCollectionSlug(rootDir, repoDir)
	fmt.Println(mutedStyle.Render("  Path:       ") + slugStyle.Render(repoDir))
	fmt.Println(mutedStyle.Render("  Collection: ") + slugStyle.Render(slug))

	var (
		defaultBranch string
		checkoutOK    *bool
		pullOK        *bool
		codexRan      bool
		codexExit     *int
		errMsg        string
	)

	// detect default branch
	db, err := detectDefaultBranch(repoDir)
	if err != nil {
		fmt.Println(warnStyle.Render("  ⚠ Could not detect default branch — ") + mutedStyle.Render(err.Error()))
	} else if db == "" {
		fmt.Println(warnStyle.Render("  ⚠ Could not detect default branch — skipping checkout/pull"))
	} else {
		defaultBranch = db
		fmt.Println(successStyle.Render("  ✓ Default branch detected: ") + slugStyle.Render(defaultBranch))
	}

	// checkout & pull
	if defaultBranch != "" {
		if dryRun {
			fmt.Println(warnStyle.Render("  🧪 Dry-run: ") +
				infoStyle.Render("would run ") +
				mutedStyle.Render(fmt.Sprintf("git -C %q checkout %s", repoDir, defaultBranch)))
			fmt.Println(warnStyle.Render("  🧪 Dry-run: ") +
				infoStyle.Render("would run ") +
				mutedStyle.Render(fmt.Sprintf("git -C %q pull --ff-only", repoDir)))
		} else {
			cOK, pOK := checkoutAndPull(repoDir, defaultBranch)
			checkoutOK = &cOK
			pullOK = &pOK
		}
	}

	fmt.Println()

	// codex exec
	ran, exitCode, codexErr := runCodex(repoDir, slug, dryRun)
	codexRan = ran
	if exitCode != nil {
		codexExit = exitCode
	}
	if codexErr != nil {
		if errMsg != "" {
			errMsg += "; "
		}
		errMsg += codexErr.Error()
	}

	fmt.Println(mutedStyle.Render("──────────────────────────────────────────────"))
	fmt.Println()

	return RepoResult{
		Path:           repoDir,
		CollectionSlug: slug,
		DefaultBranch:  defaultBranch,
		CheckoutOK:     checkoutOK,
		PullOK:         pullOK,
		CodexRan:       codexRan,
		CodexExitCode:  codexExit,
		Error:          errMsg,
		DryRun:         dryRun,
	}
}

func computeCollectionSlug(rootDir, repoDir string) string {
	rel, err := filepath.Rel(rootDir, repoDir)
	if err != nil || rel == "." {
		rel = "root"
	}
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "_")
	return rel
}

func detectDefaultBranch(repoDir string) (string, error) {
	// origin/HEAD
	cmd := exec.Command("git", "-C", repoDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if strings.HasPrefix(branch, "origin/") {
			branch = strings.TrimPrefix(branch, "origin/")
		}
		return branch, nil
	}

	// fallback main / master
	if err := exec.Command("git", "-C", repoDir, "show-ref", "--verify", "--quiet",
		"refs/heads/main").Run(); err == nil {
		return "main", nil
	}
	if err := exec.Command("git", "-C", repoDir, "show-ref", "--verify", "--quiet",
		"refs/heads/master").Run(); err == nil {
		return "master", nil
	}
	return "", nil
}

func checkoutAndPull(repoDir, branch string) (bool, bool) {
	fmt.Println(infoStyle.Render("  → Checking out default branch…"))
	co := exec.Command("git", "-C", repoDir, "checkout", branch)
	if err := co.Run(); err != nil {
		fmt.Println(dangerStyle.Render("  ✗ git checkout failed — continuing on current branch"))
		return false, false
	}

	fmt.Println(infoStyle.Render("  → Pulling latest changes…"))
	pl := exec.Command("git", "-C", repoDir, "pull", "--ff-only")
	if err := pl.Run(); err != nil {
		fmt.Println(warnStyle.Render("  ⚠ git pull failed — using local state"))
		ok := true
		return ok, false
	}

	fmt.Println(successStyle.Render("  ✓ Repository updated to latest"))
	return true, true
}

func runCodex(repoDir, slug string, dryRun bool) (bool, *int, error) {
	cmd := exec.Command("codex", "exec", "--cd", repoDir, "--sandbox", "danger-full-access", codexPrompt)
	env := os.Environ()
	env = append(env, "COLLECTION_SLUG="+slug)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if dryRun {
		fmt.Println(warnStyle.Render("  🧪 Dry-run: ") +
			infoStyle.Render("would run ") +
			mutedStyle.Render(fmt.Sprintf("COLLECTION_SLUG=%q codex exec --cd %q --sandbox danger-full-access '<PROMPT>'",
				slug, repoDir)))
		return false, nil, nil
	}

	fmt.Println(infoStyle.Render("  🧠 Running Codex indexing…"))
	if err := cmd.Run(); err != nil {
		// get exit code if possible
		exitCode := 1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		fmt.Println(dangerStyle.Render(fmt.Sprintf("  ❌ Codex exited with code %d", exitCode)))
		return true, &exitCode, err
	}

	fmt.Println(successStyle.Render("  ✅ Codex indexing completed"))
	return true, nil, nil
}

func printSummaryTable(results []RepoResult) {
	// simple text summary table
	fmt.Println(mutedStyle.Render("Repo\tCollection\tBranch\tGit\tCodex\tStatus"))

	okCount, warnCount, errCount := 0, 0, 0

	for _, r := range results {
		gitStatus := "-"
		if r.DefaultBranch != "" {
			parts := []string{"⚑ " + r.DefaultBranch}
			if r.CheckoutOK != nil && !*r.CheckoutOK {
				parts = append(parts, "checkout❌")
			}
			if r.PullOK != nil && !*r.PullOK {
				parts = append(parts, "pull⚠")
			}
			gitStatus = strings.Join(parts, ", ")
		}

		codexStatus := "-"
		if r.DryRun {
			codexStatus = "🧪 dry-run"
		} else if !r.CodexRan {
			codexStatus = "not run"
		} else if r.CodexExitCode == nil {
			codexStatus = "✅ ok"
		} else {
			codexStatus = fmt.Sprintf("❌ exit %d", *r.CodexExitCode)
		}

		status := successStyle.Render("ok")
		if r.Error != "" || (r.CodexRan && r.CodexExitCode != nil) {
			status = dangerStyle.Render("error")
			errCount++
		} else if (r.CheckoutOK != nil && !*r.CheckoutOK) || (r.PullOK != nil && !*r.PullOK) {
			status = warnStyle.Render("warn")
			warnCount++
		} else {
			okCount++
		}

		repoName := filepath.Base(r.Path)
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s",
			repoName,
			r.CollectionSlug,
			orDash(r.DefaultBranch),
			gitStatus,
			codexStatus,
			status,
		)
		fmt.Println(line)
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("OK: %d", okCount)) +
		"    " + warnStyle.Render(fmt.Sprintf("Warn: %d", warnCount)) +
		"    " + dangerStyle.Render(fmt.Sprintf("Error: %d", errCount)))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func writeSummaryJSON(path, rootDir string, dryRun bool, results []RepoResult) error {
	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"root_dir":     rootDir,
		"dry_run":      dryRun,
		"repos":        results,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
