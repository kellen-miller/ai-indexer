package main

import (
	"ai-index/internal/indexer"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var (
		dryRun      bool
		summaryJSON string
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Do everything except actually run codex exec.")
	flag.BoolVar(&dryRun, "n", false, "Alias for --dry-run.")
	flag.StringVar(&summaryJSON, "summary-json", "codex_index_summary.json", "Path to JSON summary output.")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <root-directory>\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	rootArg := flag.Arg(0)
	rootDir, err := filepath.Abs(rootArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error resolving root directory:", err)
		os.Exit(1)
	}

	if err := indexer.Run(rootDir, dryRun, summaryJSON); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
