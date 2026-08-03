package chat

import (
	"context"
	"sort"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// redactionPlaceholder replaces a detected secret. It is deliberately short and
// obviously non-sensitive so the summarizing model reads it as "a credential
// went here" rather than as content.
const redactionPlaceholder = "[redacted]"

// redactFindings rewrites content with every finding's span replaced by
// redactionPlaceholder. Findings are byte-offset spans produced by the gitleaks
// scanner; they may arrive unsorted and, across rules, may overlap, so spans are
// sorted and merged before rewriting.
func redactFindings(content string, findings []scanners.Finding) string {
	if len(findings) == 0 {
		return content
	}

	spans := make([][2]int, 0, len(findings))
	for _, f := range findings {
		start, end := f.StartPos, f.EndPos
		if start < 0 {
			start = 0
		}
		if end > len(content) {
			end = len(content)
		}
		if start >= end {
			continue
		}
		spans = append(spans, [2]int{start, end})
	}
	if len(spans) == 0 {
		return content
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })

	var b strings.Builder
	prev := 0
	for _, span := range spans {
		if span[0] < prev {
			// Overlaps a span already redacted; extend it rather than emitting a
			// second placeholder over text that is no longer there.
			if span[1] > prev {
				prev = span[1]
			}
			continue
		}
		b.WriteString(content[prev:span[0]])
		b.WriteString(redactionPlaceholder)
		prev = span[1]
	}
	b.WriteString(content[prev:])
	return b.String()
}

// scrubToolArguments removes detected secrets from a tool call's arguments so
// they can be shown to the summarizing model. Arguments are what make an
// otherwise opaque tool call ("compose") describable, but they are also where
// API keys and bearer tokens end up, so every value is scanned first.
//
// It fails closed: with no scanner, or when scanning fails, the arguments are
// dropped entirely rather than forwarded unscanned.
func (s *Service) scrubToolArguments(ctx context.Context, args string) string {
	if args == "" {
		return ""
	}
	if s.secretScanner == nil {
		return ""
	}

	findings, err := s.secretScanner.Scan(ctx, args)
	if err != nil {
		s.logger.WarnContext(ctx, "dropping tool arguments: secret scan failed")
		return ""
	}

	return redactFindings(args, findings)
}
