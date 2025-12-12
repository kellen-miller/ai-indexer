package indexer

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

const summaryTabPadding = 2

func (ix *indexer) printSummaryTable(results []RepoResult) {
	counts := summaryCounts{}
	tw := tabwriter.NewWriter(ix.stdout, 0, 0, summaryTabPadding, ' ', 0)
	if _, err := fmt.Fprintln(tw, colorize(colorMuted, "Repo\tCollection\tBranch\tGit\tCodex\tStatus")); err != nil {
		ix.errln("summary header write failed:", err)
		return
	}
	for i := range results {
		r := &results[i]
		status := ix.renderStatus(r, &counts)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			filepath.Base(r.Path),
			r.CollectionSlug,
			orDash(r.DefaultBranch),
			formatGitStatus(r),
			formatCodexStatus(r),
			colorStatus(status),
		); err != nil {
			ix.errln("summary row write failed:", err)
			return
		}
	}
	if err := tw.Flush(); err != nil {
		ix.errln("summary table flush failed:", err)
		return
	}

	ix.outln("")
	ix.outln(fmt.Sprintf("OK: %d    Warn: %d    Error: %d", counts.ok, counts.warn, counts.err))
}

type summaryCounts struct {
	ok   int
	warn int
	err  int
}

func formatGitStatus(r *RepoResult) string {
	if r.DefaultBranch == "" {
		return "unknown"
	}

	parts := []string{r.DefaultBranch}
	if r.CheckoutOK != nil && !*r.CheckoutOK {
		parts = append(parts, "checkout failed")
	}
	if r.PullOK != nil && !*r.PullOK {
		parts = append(parts, "pull failed")
	}
	return strings.Join(parts, ", ")
}

func formatCodexStatus(r *RepoResult) string {
	switch {
	case r.SkipReason != "":
		return "skipped"
	case r.DryRun:
		return "dry-run"
	case !r.CodexRan:
		return "not run"
	case r.CodexExitCode == nil:
		return "ok"
	default:
		return fmt.Sprintf("exit %d", *r.CodexExitCode)
	}
}

func (ix *indexer) renderStatus(r *RepoResult, counts *summaryCounts) string {
	switch {
	case r.Error != "" || (r.CodexRan && r.CodexExitCode != nil):
		counts.err++
		return "error"
	case (r.CheckoutOK != nil && !*r.CheckoutOK) || (r.PullOK != nil && !*r.PullOK):
		counts.warn++
		return "warn"
	default:
		counts.ok++
		return "ok"
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func colorStatus(status string) string {
	switch status {
	case "ok":
		return colorize(colorGreen, "%s", status)
	case "warn":
		return colorize(colorYellow, "%s", status)
	case "error":
		return colorize(colorRed, "%s", status)
	default:
		return status
	}
}
