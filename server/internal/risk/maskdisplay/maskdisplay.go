// Package maskdisplay produces the partial-mask display form of a risk
// finding's matched value — the string stored in the ClickHouse
// risk_findings.match_redacted column and rendered as the match by default in
// listings.
//
// Keeping the format in one function keeps the backfill
// (server/cmd/tools/migrations/riskfindings) and any live writer byte-identical
// for the same input, and keeps the Go code in lockstep with the column's
// schema comment (server/clickhouse/schema.sql), which documents the same
// tiers. Storing boundary characters of real matches is a deliberate,
// signed-off relaxation of the earlier no-plaintext rule per the
// reveal-from-ClickHouse design.
package maskdisplay

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
)

// Detection source constants, mirrored as literals (same precedent as the
// categories package) so this leaf package does not pull the scanner
// implementations in. Canonical definitions:
//
//	prompt_injection  server/internal/scanners/promptinjection.Source
//	llm_judge         server/internal/scanners/promptpolicy.Source
//	destructive_tool  server/internal/shadowmcp.SourceDestructiveTool
//	cli_destructive   server/internal/scanners/clidestructive.Source
//	shadow_mcp        server/internal/shadowmcp.SourceShadowMCP
//	account_identity  server/internal/scanners/accountidentity.Source
const (
	sourcePromptInjection = "prompt_injection"
	sourceLLMJudge        = "llm_judge"
	sourceDestructiveTool = "destructive_tool"
	sourceCLIDestructive  = "cli_destructive"
	sourceShadowMCP       = "shadow_mcp"
	sourceAccountIdentity = "account_identity"
)

// emailRuleID is the Presidio email rule; its match is an email address whose
// domain is safe to display.
const emailRuleID = "pii.email_address"

// Display returns the partial-mask display form of a raw match. All slicing is
// rune-based so multibyte matches are never cut mid-rune.
//
// Rules, in precedence order:
//
//  1. An empty match displays as "".
//  2. Judge/derived sources (prompt_injection, llm_judge, destructive_tool,
//     cli_destructive) display as "": their rationale/description carries the
//     signal and the match is a rendered artifact, not a value worth showing.
//  3. shadow_mcp passes through verbatim — a documented carve-out: the match is
//     a non-secret MCP server identifier the UI shows unmasked.
//  4. Emails (the pii.email_address rule, or any account_identity match) show
//     "***@" plus everything after the LAST "@". The three stars are fixed so
//     the local-part length does not leak. Matches without "@" fall through to
//     the general tiers.
//  5. Financial-category matches of at least 5 runes show "****" plus the last
//     4 runes, receipt style.
//  6. General tiers on rune count n:
//     n >= 8: first 4 + "*"×(n−6) + last 2 (e.g. AKIA**************LE)
//     5–7:    first 2 + "*"×(n−3) + last 1
//     3–4:    first 1 + "*"×(n−2) + last 1
//     n <= 2: "*"×n — a deliberate deviation from the literal first/last
//     rules, which at this length would disclose the entire value.
func Display(source, ruleID, match string) string {
	if match == "" {
		return ""
	}

	switch source {
	case sourcePromptInjection, sourceLLMJudge, sourceDestructiveTool, sourceCLIDestructive:
		return ""
	case sourceShadowMCP:
		return match
	}

	if ruleID == emailRuleID || source == sourceAccountIdentity {
		// "@" is ASCII, so the byte index is always a rune boundary.
		if i := strings.LastIndex(match, "@"); i >= 0 {
			return "***@" + match[i+1:]
		}
	}

	runes := []rune(match)
	n := len(runes)

	if categories.Classify(source, ruleID) == categories.CategoryFinancial && n >= 5 {
		return "****" + string(runes[n-4:])
	}

	switch {
	case n >= 8:
		return string(runes[:4]) + strings.Repeat("*", n-6) + string(runes[n-2:])
	case n >= 5:
		return string(runes[:2]) + strings.Repeat("*", n-3) + string(runes[n-1:])
	case n >= 3:
		return string(runes[:1]) + strings.Repeat("*", n-2) + string(runes[n-1:])
	default:
		return strings.Repeat("*", n)
	}
}
