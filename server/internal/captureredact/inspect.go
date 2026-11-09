package captureredact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// DecodedJSONField marks offsets into a decoded JSON string rather than the
// original capture. Consumers must not treat its safe ordinal path as a JSON
// pointer or use these offsets to reveal original content.
const DecodedJSONField = "capture.decoded_json"

// InspectText runs the same complete inspection as redaction and returns raw
// findings only for immediate, trusted in-process preparation. Callers must
// erase Match before publishing or persisting any returned finding. Literal
// matches retain original offsets; decoded-only matches use DecodedJSONField.
func (r *Redactor) InspectText(ctx context.Context, text string) (scanners.Result, error) {
	op := newOperation(r, text)
	op.redactText(ctx, text, 0, nil, nil)
	return scanners.Result{Findings: op.findings, STokens: op.stokens, Completed: op.err == nil}, op.err
}

type matchLocation struct {
	rule       string
	start, end int
}

// operation owns transient raw evidence for one capture only.
type operation struct {
	scanner    Scanner
	original   string
	findings   []scanners.Finding
	report     Report
	seen       map[matchLocation]struct{}
	nextString int
	stokens    int64
	err        error
}

func newOperation(r *Redactor, text string) *operation {
	var scanner Scanner
	if r != nil {
		scanner = r.scanner
	}
	return &operation{
		scanner: scanner, original: text, findings: nil,
		report: Report{Findings: nil, Incomplete: false},
		seen:   make(map[matchLocation]struct{}), nextString: 0, stokens: 0, err: nil,
	}
}

func (o *operation) fail() string {
	if o.err == nil {
		o.err = errors.New("capture credential inspection is incomplete")
	}
	o.report.Incomplete = true
	return Replacement
}

func (o *operation) scan(ctx context.Context, text string) ([]scanners.Finding, bool) {
	if ctx.Err() != nil || o.scanner == nil {
		o.fail()
		return nil, false
	}
	result, err := o.scanner.Scan(ctx, text)
	o.stokens += result.STokens
	if err != nil || !result.Completed || ctx.Err() != nil {
		o.fail()
		return nil, false
	}
	findings := make([]scanners.Finding, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if !RedactsRule(finding.RuleID) {
			continue
		}
		if !validSpan(text, finding) {
			o.fail()
			return nil, false
		}
		findings = append(findings, finding)
	}
	return findings, true
}

func validSpan(text string, finding scanners.Finding) bool {
	return finding.StartPos >= 0 && finding.EndPos > finding.StartPos && finding.EndPos <= len(text) &&
		runeBoundary(text, finding.StartPos) && runeBoundary(text, finding.EndPos) &&
		finding.Match == text[finding.StartPos:finding.EndPos]
}

func runeBoundary(text string, offset int) bool {
	return offset == len(text) || utf8.RuneStart(text[offset])
}

func (o *operation) redactText(ctx context.Context, text string, depth int, origin *textOrigin, frame *scanFrame) string {
	if text == "" {
		return text
	}
	if ctx.Err() != nil || depth > maxNesting {
		return o.fail()
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '"') && json.Valid([]byte(text)) {
		return string(o.redactJSON(ctx, []byte(text), depth, origin, frame))
	}
	index := o.nextString
	o.nextString++
	findings, ok := o.scan(ctx, text)
	if !ok {
		return Replacement
	}
	findings = projectContextFindings(text, findings, origin, frame)
	for _, finding := range findings {
		if !o.record(finding, origin, index) {
			return o.fail()
		}
	}
	return replaceMatches(text, findings)
}

// scanFrame keeps context-dependent detections (such as AWS credentials whose
// identifier is in a sibling field) available while individual JSON strings are
// visited. Every detection must be removed even if no one string contains it.
type scanFrame struct {
	parent   *scanFrame
	origin   *textOrigin
	findings []contextFinding
	consumed []bool
}

// contextFinding keeps detector offsets in its scanned surface separate from
// the corresponding byte span in the containing, still-encoded JSON document.
type contextFinding struct {
	finding scanners.Finding
	start   int
	end     int
}

func (o *operation) redactJSON(ctx context.Context, data []byte, depth int, origin *textOrigin, parent *scanFrame) []byte {
	if ctx.Err() != nil || depth > maxNesting || !json.Valid(data) {
		return []byte(`"` + o.fail() + `"`)
	}
	findings, ok := o.scan(ctx, string(data))
	if !ok {
		return []byte(`"` + Replacement + `"`)
	}
	contextFindings := make([]contextFinding, 0, len(findings))
	for _, finding := range findings {
		contextFindings = append(contextFindings, contextFinding{finding: finding, start: finding.StartPos, end: finding.EndPos})
	}
	// Contextual rules need the decoded value and its sibling identifier or
	// label in one scan. Scanning each decoded string alone loses that context.
	decoded, ok := decodeJSONContext(data, depth)
	if !ok {
		return []byte(`"` + o.fail() + `"`)
	}
	if decoded.text != string(data) {
		findings, ok := o.scan(ctx, decoded.text)
		if !ok {
			return []byte(`"` + Replacement + `"`)
		}
		for _, finding := range findings {
			start, startOK := decoded.mapBoundary(finding.StartPos)
			end, endOK := decoded.mapBoundary(finding.EndPos)
			if !startOK || !endOK || start >= end {
				return []byte(`"` + o.fail() + `"`)
			}
			contextFindings = append(contextFindings, contextFinding{finding: finding, start: start, end: end})
		}
	}
	frame := &scanFrame{parent: parent, origin: origin, findings: contextFindings, consumed: make([]bool, len(contextFindings))}
	var output bytes.Buffer
	last := 0
	for pos := 0; pos < len(data); pos++ {
		if data[pos] != '"' {
			continue
		}
		start := pos
		pos++
		for data[pos] != '"' {
			if data[pos] == '\\' {
				pos++
			}
			pos++
		}
		end := pos + 1
		var decoded string
		if err := json.Unmarshal(data[start:end], &decoded); err != nil {
			return []byte(`"` + o.fail() + `"`)
		}
		child := &textOrigin{parent: origin, encoded: data[start:end], decoded: decoded, start: start}
		redacted := o.redactText(ctx, decoded, depth+1, child, frame)
		if redacted == decoded {
			continue
		}
		encoded, err := json.Marshal(redacted)
		if err != nil {
			return []byte(`"` + o.fail() + `"`)
		}
		output.Write(data[last:start])
		output.Write(encoded)
		last = end
	}
	wholeDocument := false
	for i, consumed := range frame.consumed {
		if !consumed {
			// A match crossing JSON structure cannot be replaced inside one string.
			// Drop the document while retaining its content-free detection metadata.
			finding := frame.findings[i]
			if !o.recordMapped(finding.finding, origin, finding.start, finding.end, o.nextString) {
				o.fail()
			}
			wholeDocument = true
		}
	}
	if wholeDocument {
		return []byte(`"` + Replacement + `"`)
	}
	if last == 0 {
		return bytes.Clone(data)
	}
	output.Write(data[last:])
	return output.Bytes()
}

func projectContextFindings(text string, findings []scanners.Finding, origin *textOrigin, frame *scanFrame) []scanners.Finding {
	for current := frame; current != nil; current = current.parent {
		for i, contextual := range current.findings {
			finding := contextual.finding
			for offset := 0; offset < len(text); {
				relative := strings.Index(text[offset:], finding.Match)
				if relative < 0 {
					break
				}
				start := offset + relative
				end := start + len(finding.Match)
				offset = end
				mappedStart, mappedEnd, ok := origin.mapTo(current.origin, start, end)
				if !ok || mappedStart != contextual.start || mappedEnd != contextual.end {
					continue
				}
				current.consumed[i] = true
				finding.StartPos, finding.EndPos = start, end
				findings = append(findings, finding)
				break
			}
		}
	}
	return findings
}

func (o *operation) record(finding scanners.Finding, origin *textOrigin, index int) bool {
	return o.recordMapped(finding, origin, finding.StartPos, finding.EndPos, index)
}

func (o *operation) recordMapped(finding scanners.Finding, origin *textOrigin, mappedStart, mappedEnd, index int) bool {
	start, end, ok := origin.mapTo(nil, mappedStart, mappedEnd)
	if !ok || start < 0 || end > len(o.original) || start >= end {
		return false
	}
	location := matchLocation{rule: finding.RuleID, start: start, end: end}
	if _, exists := o.seen[location]; exists {
		return true
	}
	o.seen[location] = struct{}{}
	path := fmt.Sprintf("$strings[%d]", index)
	o.report.Findings = append(o.report.Findings, Finding{
		RuleID: finding.RuleID, StartPos: finding.StartPos, EndPos: finding.EndPos, Path: path,
	})
	if o.original[start:end] == finding.Match {
		finding.StartPos, finding.EndPos = start, end
	} else {
		finding.Field, finding.Path = DecodedJSONField, path
	}
	o.findings = append(o.findings, finding)
	return true
}

func replaceMatches(text string, findings []scanners.Finding) string {
	if len(findings) == 0 {
		return text
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].StartPos < findings[j].StartPos })
	var output strings.Builder
	last, start, end := 0, findings[0].StartPos, findings[0].EndPos
	for _, finding := range findings[1:] {
		if finding.StartPos <= end {
			end = max(end, finding.EndPos)
			continue
		}
		output.WriteString(text[last:start])
		output.WriteString(Replacement)
		last, start, end = end, finding.StartPos, finding.EndPos
	}
	output.WriteString(text[last:start])
	output.WriteString(Replacement)
	output.WriteString(text[end:])
	return output.String()
}

// textOrigin maps a decoded JSON string back through its containing strings.
// It retains source bytes only for the lifetime of this in-process operation.
type textOrigin struct {
	parent  *textOrigin
	encoded []byte
	decoded string
	start   int
}

func (o *textOrigin) mapTo(ancestor *textOrigin, start, end int) (int, int, bool) {
	for current := o; current != ancestor; current = current.parent {
		if current == nil {
			return 0, 0, false
		}
		left, ok := jsonDecodedBoundary(current.encoded, current.decoded, start)
		if !ok {
			return 0, 0, false
		}
		right, ok := jsonDecodedBoundary(current.encoded, current.decoded, end)
		if !ok {
			return 0, 0, false
		}
		start, end = current.start+left, current.start+right
	}
	return start, end, true
}

func jsonDecodedBoundary(encoded []byte, decoded string, offset int) (int, bool) {
	if offset < 0 || offset > len(decoded) {
		return 0, false
	}
	position := 1 // Skip the opening JSON quote.
	for decodedPosition := 0; decodedPosition < len(decoded); {
		if decodedPosition == offset {
			return position, true
		}
		_, decodedWidth := utf8.DecodeRuneInString(decoded[decodedPosition:])
		_, encodedWidth := utf8.DecodeRune(encoded[position:])
		if encoded[position] == '\\' {
			encodedWidth = 2
			if encoded[position+1] == 'u' {
				encodedWidth = 6
				if decodedWidth == 4 {
					encodedWidth = 12 // A decoded astral rune consumes a surrogate pair.
				}
			}
		}
		position += encodedWidth
		decodedPosition += decodedWidth
	}
	return position, offset == len(decoded)
}
