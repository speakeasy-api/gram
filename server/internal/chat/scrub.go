package chat

import (
	"context"
	"regexp"
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

// sensitiveArgumentKey matches JSON keys whose value is a credential whatever
// it looks like. Pattern detection alone is not credential protection: gitleaks
// recognizes known token shapes, so `"password": "hunter2"` or an opaque
// in-house key would pass straight through. The key name is the other half.
var sensitiveArgumentKey = regexp.MustCompile(
	`(?i)"[^"]*(secret|token|password|passwd|credential|api[_-]?key|access[_-]?key|private[_-]?key|authorization|auth|cookie|session|signature|bearer)[^"]*"\s*:\s*"(?:[^"\\]|\\.)*"`,
)

// redactSensitiveKeys blanks the value of any JSON key whose name says it holds
// a credential, leaving the key visible so the model still sees the shape of
// the call.
func redactSensitiveKeys(content string) string {
	return sensitiveArgumentKey.ReplaceAllStringFunc(content, func(match string) string {
		colon := strings.Index(match, ":")
		if colon < 0 {
			return match
		}
		return match[:colon+1] + `"` + redactionPlaceholder + `"`
	})
}

// scrubToolArguments removes secrets from a tool call's arguments so they can be
// shown to the summarizing model. Arguments are what make an otherwise opaque
// tool call ("compose") describable, but they are also where API keys and bearer
// tokens end up, so values are redacted two ways before anything is forwarded:
// by key name (a credential need not look like one) and by gitleaks detection (a
// credential need not be under a telling key).
//
// maxRunes bounds the work: arguments are truncated to what the prompt will
// actually carry before scanning, so a large blob can't spend scanner time on
// bytes that get dropped anyway.
//
// It fails closed: with no scanner, or when scanning fails, the arguments are
// dropped entirely rather than forwarded unscanned.
func (s *Service) scrubToolArguments(ctx context.Context, args string, maxRunes int) string {
	args = truncateRunes(args, maxRunes)
	if args == "" {
		return ""
	}
	if s.secretScanner == nil {
		return ""
	}

	args = redactSensitiveKeys(args)

	findings, err := s.secretScanner.Scan(ctx, args)
	if err != nil {
		s.logger.WarnContext(ctx, "dropping tool arguments: secret scan failed")
		return ""
	}

	return redactFindings(args, findings)
}
