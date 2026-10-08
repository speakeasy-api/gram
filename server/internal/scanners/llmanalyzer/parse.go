package llmanalyzer

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrParse marks a completion that could not be read as a verdict. Callers
// treat it like any other analyzer failure: the sync lane denies, the async
// lane publishes nothing.
var ErrParse = errors.New("risk llm: unparsable verdict")

// maxReasoningRunes caps each stored reasoning, matching the rationale cap of
// the OpenRouter judge so both flow into Finding.Description under the same
// bound.
const maxReasoningRunes = 500

// riskKeys lists the four model risk keys in canonical order. Every verdict
// must carry all of them.
var riskKeys = []string{
	KeySecretsLeak,
	KeyPersonalDataLeak,
	KeyPromptInjection,
	KeyDestructiveToolCall,
}

// RiskVerdict is the model's judgement on one risk key.
type RiskVerdict struct {
	// Score is 1 when the risk is present and 0 otherwise.
	Score int

	// Reasoning is the model's short justification, capped at 500 runes. It
	// may paraphrase the evaluated content.
	Reasoning string
}

// Verdict is one parsed model reply.
type Verdict struct {
	// Risks holds one entry per model risk key.
	Risks map[string]RiskVerdict

	// Raw is the completion text the verdict was parsed from.
	Raw string
}

// Flagged returns the risk keys scored 1, in canonical order: secrets_leak,
// personal_data_leak, prompt_injection, destructive_tool_call.
func (v Verdict) Flagged() []string {
	flagged := make([]string, 0, len(riskKeys))
	for _, key := range riskKeys {
		if v.Risks[key].Score == 1 {
			flagged = append(flagged, key)
		}
	}
	return flagged
}

// maxVerdictCandidates bounds how many JSON objects findVerdictObject decodes
// from one completion. A reply that buries the verdict behind more stray
// objects than this is treated as unparsable rather than scanned up to the
// response size cap.
const maxVerdictCandidates = 64

// cleanShorthandKey is the single key of the v4 clean verdict {"risk": 0}.
const cleanShorthandKey = "risk"

// ParseVerdict reads the model reply. It takes the first top-level JSON
// object in the text that is a verdict, so prose or code fences around the
// object are tolerated even when they contain braces of their own. A verdict
// is either the clean shorthand {"risk": 0}, which scores every risk 0, or an
// object carrying all four risk keys, each valued as {"score": 0|1,
// "reasoning": "..."} or as a bare score. Scores may be numbers, numeric
// strings or booleans. When no object decodes, the scores are salvaged from
// the text itself (see salvageScores) so a reply cut off or garbled inside a
// reasoning string still yields its verdict, without reasoning. Any other
// shape, including {"risk": 1} (a flag with nothing to attribute it to),
// yields an error wrapping ErrParse.
//
// The flat reply format (risk-judge-9b, v3 and v4) uses bare scores plus one
// optional top-level "reasoning" string covering the flagged risks, written
// as "<key>: <sentence> <key>: <sentence>". That string is split by key and
// attached to the matching risks; a reasoning that names no key is attached
// to every flagged risk. Per-risk reasoning from the nested format wins when
// both are present.
func ParseVerdict(text string) (Verdict, error) {
	object, err := findVerdictObject(text)
	if err != nil {
		if risks, ok := salvageScores(text); ok {
			return Verdict{Risks: risks, Raw: text}, nil
		}
		return Verdict{}, fmt.Errorf("%w: %w", ErrParse, err)
	}

	if isCleanShorthand(object) {
		score, err := parseScore(object[cleanShorthandKey])
		if err != nil {
			return Verdict{}, fmt.Errorf("%w: key %q: %w", ErrParse, cleanShorthandKey, err)
		}
		if score != 0 {
			return Verdict{}, fmt.Errorf("%w: key %q is %d but no risk key is present", ErrParse, cleanShorthandKey, score)
		}
		risks := make(map[string]RiskVerdict, len(riskKeys))
		for _, key := range riskKeys {
			risks[key] = RiskVerdict{Score: 0, Reasoning: ""}
		}
		return Verdict{Risks: risks, Raw: text}, nil
	}

	risks := make(map[string]RiskVerdict, len(riskKeys))
	for _, key := range riskKeys {
		raw, ok := object[key]
		if !ok {
			return Verdict{}, fmt.Errorf("%w: missing key %q", ErrParse, key)
		}
		risk, err := parseRiskVerdict(raw)
		if err != nil {
			return Verdict{}, fmt.Errorf("%w: key %q: %w", ErrParse, key, err)
		}
		risks[key] = risk
	}

	if raw, ok := object["reasoning"]; ok {
		attachTopLevelReasoning(risks, raw)
	}

	return Verdict{Risks: risks, Raw: text}, nil
}

// attachTopLevelReasoning fills empty Reasoning fields of flagged risks from
// the compact format's top-level "reasoning" string. Advisory like per-risk
// reasoning: a non-string value is ignored, never an error.
func attachTopLevelReasoning(risks map[string]RiskVerdict, raw json.RawMessage) {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	byKey := splitReasoningByKey(text)
	for _, key := range riskKeys {
		risk := risks[key]
		if risk.Score != 1 || risk.Reasoning != "" {
			continue
		}
		reasoning, ok := byKey[key]
		if !ok {
			if len(byKey) > 0 {
				continue
			}
			reasoning = text // no key markers at all: the whole string applies
		}
		risks[key] = RiskVerdict{Score: 1, Reasoning: capReasoning(reasoning)}
	}
}

// splitReasoningByKey breaks "secrets_leak: a b. prompt_injection: c d." into
// {"secrets_leak": "a b.", "prompt_injection": "c d."}. A marker counts at the
// start of the string or after whitespace or sentence punctuation (the model
// sometimes drops the space after a period), so a key name that is part of a
// longer word or quoted inside a sentence is not taken as a new segment.
// Returns an empty map when no marker is found.
func splitReasoningByKey(text string) map[string]string {
	type marker struct {
		key   string
		start int // index of the key
		body  int // index just past "key:"
	}
	var markers []marker
	for _, key := range riskKeys {
		from := 0
		for {
			i := strings.Index(text[from:], key+":")
			if i < 0 {
				break
			}
			i += from
			if i == 0 || isMarkerBoundary(text[i-1]) {
				markers = append(markers, marker{key: key, start: i, body: i + len(key) + 1})
			}
			from = i + len(key)
		}
	}
	if len(markers) == 0 {
		return map[string]string{}
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].start < markers[j].start })

	out := make(map[string]string, len(markers))
	for i, m := range markers {
		end := len(text)
		if i+1 < len(markers) {
			end = markers[i+1].start
		}
		segment := strings.TrimSpace(text[m.body:end])
		if segment == "" {
			continue
		}
		if prev, ok := out[m.key]; ok { // repeated key: keep both sentences
			segment = prev + " " + segment
		}
		out[m.key] = segment
	}
	return out
}

// isMarkerBoundary reports whether a "<key>:" marker may start right after c.
func isMarkerBoundary(c byte) bool {
	switch c {
	case ' ', '\n', '\t', '\r', '.', ';', '!', '?', ',':
		return true
	}
	return false
}

// capReasoning trims and bounds a reasoning string to maxReasoningRunes.
func capReasoning(reasoning string) string {
	reasoning = strings.TrimSpace(reasoning)
	if utf8.RuneCountInString(reasoning) > maxReasoningRunes {
		reasoning = string([]rune(reasoning)[:maxReasoningRunes])
	}
	return reasoning
}

// findVerdictObject returns the first JSON object in text that is a verdict:
// the clean shorthand or an object carrying every risk key. nextObjectSpan
// delimits each candidate by brace depth and the candidate is decoded exactly
// once. A candidate that decodes but is neither (a stray {} in surrounding
// prose, say) is skipped and the walk resumes after it. A candidate that fails to decode is usually prose with an unmatched '{'
// that swallowed the real object, so the walk resumes at the next '{' inside
// that span instead. Every candidate is one linear pass and at most
// maxVerdictCandidates are tried, which bounds a brace-heavy malformed reply
// to a small constant number of passes over the text. The error of the last
// candidate is reported when none qualifies.
func findVerdictObject(text string) (map[string]json.RawMessage, error) {
	lastErr := errors.New("no json object in completion")
	from := 0
	for range maxVerdictCandidates {
		start, end, ok := nextObjectSpan(text, from)
		if !ok {
			break
		}

		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text[start:end]), &object); err != nil {
			lastErr = err
			from = start + 1
		} else if key, ok := missingRiskKey(object); ok && !isCleanShorthand(object) {
			lastErr = fmt.Errorf("missing key %q", key)
			from = end
		} else {
			return object, nil
		}
	}
	return nil, lastErr
}

// scorePattern matches one `"<key>": <score>` pair as the model writes it in
// any of the reply shapes: a bare score, a quoted score, or the nested
// `{"score": <score>` opener. Only 0 and 1 count; anything else is left to
// the error path.
var scorePattern = regexp.MustCompile(
	`"(` + strings.Join(append(append([]string{}, riskKeys...), cleanShorthandKey), "|") + `)"\s*:\s*(?:\{\s*"score"\s*:\s*)?"?([01])"?`)

// salvageScores is the last resort for a reply in which no JSON object
// decodes as a verdict: one cut off inside a reasoning string, or with a stray
// quote or brace in it. It takes the first `"<key>": <score>` pair of each
// key straight from the text and returns a verdict when all four risk keys
// were found, or an all-clear when none was and `"risk": 0` was. Reasoning is
// dropped: the string it lived in is what failed to decode.
func salvageScores(text string) (map[string]RiskVerdict, bool) {
	scores := make(map[string]int, len(riskKeys)+1)
	for _, m := range scorePattern.FindAllStringSubmatch(text, -1) {
		if _, seen := scores[m[1]]; !seen {
			scores[m[1]] = int(m[2][0] - '0')
		}
	}

	risks := make(map[string]RiskVerdict, len(riskKeys))
	found := 0
	for _, key := range riskKeys {
		if score, ok := scores[key]; ok {
			found++
			risks[key] = RiskVerdict{Score: score, Reasoning: ""}
		}
	}
	switch {
	case found == len(riskKeys):
		return risks, true
	case found == 0 && scores[cleanShorthandKey] == 0 && hasKey(scores, cleanShorthandKey):
		for _, key := range riskKeys {
			risks[key] = RiskVerdict{Score: 0, Reasoning: ""}
		}
		return risks, true
	}
	return nil, false
}

func hasKey(m map[string]int, key string) bool {
	_, ok := m[key]
	return ok
}

// isCleanShorthand reports whether object has the shape of the v4 clean
// verdict: the "risk" key and nothing else. The value is checked by the
// caller, so {"risk": 1} is recognised here and then rejected with a clear
// error rather than skipped as a stray object.
func isCleanShorthand(object map[string]json.RawMessage) bool {
	_, ok := object[cleanShorthandKey]
	return ok && len(object) == 1
}

// nextObjectSpan locates the first '{' at or after from and returns the span
// of the object it opens: end is the index just past the brace that returns
// the depth to zero, or len(text) when the object never closes. Braces inside
// JSON strings are skipped (with backslash escapes honoured), so a reasoning
// value containing "}" neither ends the span early nor opens a new one.
// Quotes outside an object are prose and carry no state.
func nextObjectSpan(text string, from int) (start, end int, ok bool) {
	offset := strings.IndexByte(text[from:], '{')
	if offset < 0 {
		return 0, 0, false
	}
	start = from + offset

	depth, inString, escaped := 0, false, false
	for i := start; i < len(text); i++ {
		c := text[i]
		switch {
		case escaped:
			escaped = false
		case inString:
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
		case c == '"':
			inString = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return start, i + 1, true
			}
		}
	}
	return start, len(text), true
}

// missingRiskKey reports the first risk key absent from object.
func missingRiskKey(object map[string]json.RawMessage) (string, bool) {
	for _, key := range riskKeys {
		if _, ok := object[key]; !ok {
			return key, true
		}
	}
	return "", false
}

func parseRiskVerdict(raw json.RawMessage) (RiskVerdict, error) {
	trimmed := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(trimmed, "{") {
		score, err := parseScore(raw)
		if err != nil {
			return RiskVerdict{}, err
		}
		return RiskVerdict{Score: score, Reasoning: ""}, nil
	}

	var nested struct {
		Score     json.RawMessage `json:"score"`
		Reasoning json.RawMessage `json:"reasoning"`
	}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return RiskVerdict{}, fmt.Errorf("decode risk object: %w", err)
	}
	if len(nested.Score) == 0 {
		return RiskVerdict{}, errors.New("missing score")
	}
	score, err := parseScore(nested.Score)
	if err != nil {
		return RiskVerdict{}, err
	}

	// Reasoning is advisory: a non-string value (an array, an object) drops
	// to empty rather than discarding a parsed score.
	var reasoning string
	if err := json.Unmarshal(nested.Reasoning, &reasoning); err != nil {
		reasoning = ""
	}
	return RiskVerdict{Score: score, Reasoning: capReasoning(reasoning)}, nil
}

// parseScore coerces a JSON number, numeric string or boolean into 0 or 1.
func parseScore(raw json.RawMessage) (int, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("decode score: %w", err)
	}

	var score float64
	switch v := value.(type) {
	case float64:
		score = v
	case bool:
		if v {
			score = 1
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("score %q is not numeric", v)
		}
		score = parsed
	default:
		return 0, fmt.Errorf("score has unsupported type %T", value)
	}

	switch score {
	case 0:
		return 0, nil
	case 1:
		return 1, nil
	default:
		return 0, fmt.Errorf("score %v is not 0 or 1", score)
	}
}
