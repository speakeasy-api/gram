package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

// codeDescriptionLimit is what Claude Code keeps of a tool description it
// loads at session start.
const codeDescriptionLimit = 4096

// codeServerListBudget bounds the server list in the execute description, so
// the list and the usage text around it fit codeDescriptionLimit. The reserve
// covers the notes about hidden servers that can follow the list.
const codeServerListBudget = codeDescriptionLimit - len(codeExecuteLead) - len(codeExecuteUsage) - 256

// codeServerToolNames is the most tool names one server contributes.
const codeServerToolNames = 8

// memberDescriptionLimit is the most characters of a member's own description
// that discovery repeats.
const memberDescriptionLimit = 120

// codeSummaryToolName admits only names that cannot carry prose into the
// description: a member's tool names are third-party text.
var codeSummaryToolName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]{0,63}$`)

// codeServerSummary is one server's line in the execute description.
type codeServerSummary struct {
	slug        string
	name        string
	description string
	tools       []string
}

// upstreamDescription takes a proxied member's description of itself from its
// initialize result: the server's declared description, else the first
// sentence of its instructions.
func upstreamDescription(result *mcp.InitializeResult) string {
	if result == nil {
		return ""
	}
	if result.ServerInfo != nil && strings.TrimSpace(result.ServerInfo.Description) != "" {
		return memberDescription(result.ServerInfo.Description)
	}
	return memberDescription(result.Instructions)
}

// memberDescription reduces a member's description to one bounded line. The
// text is third-party, so it is cut to its first sentence, stripped of control
// characters and line breaks, and capped; callers quote it wherever it is shown.
func memberDescription(text string) string {
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}), " ")
	if end := strings.Index(text, ". "); end >= 0 {
		text = text[:end+1]
	}
	if utf8.RuneCountInString(text) <= memberDescriptionLimit {
		return text
	}
	runes := []rune(text)[:memberDescriptionLimit-1]
	for cut := len(runes) - 1; cut > memberDescriptionLimit/2; cut-- {
		if runes[cut] == ' ' {
			runes = runes[:cut]
			break
		}
	}
	return string(runes) + "…"
}

// summarizeCodeMember describes a member for discovery without contacting it:
// its own description, and the tool names this caller may use. A frozen
// connection reads its approved definitions and a hosted member reads its
// toolset. Any other member contributes only the description recorded at
// consent.
func (s *Service) summarizeCodeMember(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) codeServerSummary {
	summary := codeServerSummary{slug: member.slug, name: member.name, description: "", tools: nil}
	if gate.catalog != nil {
		summary.description = gate.catalog.Members[member.slug].Description
	}
	var catalog *memberCatalog
	var err error
	switch {
	case gate.catalog != nil && gate.catalogRef != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached:
		catalog, err = s.describeCachedMember(ctx, gate, member)
	case member.backend == metaMemberBackendHosted:
		catalog, err = s.describeMemberToolset(ctx, logger, gate, member)
	default:
		return summary
	}
	if err != nil || catalog == nil {
		// The list still names the server; only its tool sample is lost.
		return summary
	}
	if summary.description == "" {
		summary.description = catalog.description
	}
	for _, entry := range catalog.entries {
		summary.tools = append(summary.tools, entry.Name)
	}
	return summary
}

// pickSummaryTools chooses up to limit tool names that together cover the most
// distinct words, so the sample shows what a server does rather than its first
// few names in alphabetical order. The result keeps the input order.
func pickSummaryTools(names []string, limit int) []string {
	valid := make([]string, 0, len(names))
	for _, name := range names {
		if codeSummaryToolName.MatchString(name) {
			valid = append(valid, name)
		}
	}
	if len(valid) <= limit {
		return valid
	}
	seen := map[string]bool{}
	picked := make([]bool, len(valid))
	for range limit {
		best, bestScore := -1, -1
		for i, name := range valid {
			if picked[i] {
				continue
			}
			score := 0
			for _, word := range summaryWords(name) {
				if !seen[word] {
					score++
				}
			}
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		picked[best] = true
		for _, word := range summaryWords(valid[best]) {
			seen[word] = true
		}
	}
	out := make([]string, 0, limit)
	for i, name := range valid {
		if picked[i] {
			out = append(out, name)
		}
	}
	return out
}

// summaryWords splits a tool name into lowercase words at separators and
// camelCase boundaries.
func summaryWords(name string) []string {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	var previous rune
	for _, r := range name {
		switch {
		case r == '_' || r == '-' || r == '.':
			flush()
		case unicode.IsUpper(r) && unicode.IsLower(previous):
			flush()
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
		previous = r
	}
	flush()
	return words
}

// renderCodeServers lists servers with their descriptions and a sample of
// their tools in at most budget characters. It shortens every sample, then
// drops the descriptions, before it leaves a server out, and it counts the
// servers it could not name.
func renderCodeServers(servers []codeServerSummary, budget int) string {
	for _, described := range []bool{true, false} {
		for limit := codeServerToolNames; limit >= 0; limit-- {
			var b strings.Builder
			for _, server := range servers {
				b.WriteString(renderCodeServer(server, limit, described))
			}
			if utf8.RuneCountInString(b.String()) <= budget {
				return b.String()
			}
		}
	}
	var b strings.Builder
	used := 0
	for i, server := range servers {
		line := renderCodeServer(server, 0, false)
		rest := fmt.Sprintf("%d more servers are listed by tools.servers().\n", len(servers)-i)
		if used+utf8.RuneCountInString(line)+utf8.RuneCountInString(rest) > budget {
			b.WriteString(rest)
			break
		}
		b.WriteString(line)
		used += utf8.RuneCountInString(line)
	}
	return b.String()
}

func renderCodeServer(server codeServerSummary, limit int, described bool) string {
	// Quoting keeps labels and third-party text from starting a new
	// instruction line.
	line := "- " + strconv.Quote(server.slug+"--") + " " + strconv.Quote(server.name)
	if described && server.description != "" {
		line += " " + strconv.Quote(server.description)
	}
	tools := pickSummaryTools(server.tools, limit)
	if len(tools) > 0 {
		line += ": " + strings.Join(tools, ", ")
		if more := len(server.tools) - len(tools); more > 0 {
			line += fmt.Sprintf(", +%d more", more)
		}
	}
	return line + "\n"
}
