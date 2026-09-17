package llmanalyzer

import (
	"encoding/json"
	"errors"
	"fmt"
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

// ParseVerdict reads the model reply. It takes the first JSON object in the
// text that carries all four risk keys, so prose or code fences around the
// object are tolerated even when they contain braces of their own, and
// accepts each value either as {"score": 0|1, "reasoning": "..."} or as a
// bare score. Scores may be numbers, numeric strings or booleans. Any other
// shape yields an error wrapping ErrParse.
func ParseVerdict(text string) (Verdict, error) {
	object, err := findVerdictObject(text)
	if err != nil {
		return Verdict{}, fmt.Errorf("%w: %w", ErrParse, err)
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

	return Verdict{Risks: risks, Raw: text}, nil
}

// findVerdictObject decodes a JSON object from each '{' in text in turn and
// returns the first one that carries every risk key. An object that decodes
// but lacks a key (a stray {} in surrounding prose, say) is skipped, and the
// error of the last candidate is reported when none qualifies.
func findVerdictObject(text string) (map[string]json.RawMessage, error) {
	var lastErr error = errors.New("no json object in completion")
	for start := strings.IndexByte(text, '{'); start >= 0; {
		var object map[string]json.RawMessage
		if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&object); err != nil {
			lastErr = err
		} else if key, ok := missingRiskKey(object); ok {
			lastErr = fmt.Errorf("missing key %q", key)
		} else {
			return object, nil
		}
		next := strings.IndexByte(text[start+1:], '{')
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return nil, lastErr
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
	reasoning = strings.TrimSpace(reasoning)
	if utf8.RuneCountInString(reasoning) > maxReasoningRunes {
		reasoning = string([]rune(reasoning)[:maxReasoningRunes])
	}
	return RiskVerdict{Score: score, Reasoning: reasoning}, nil
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
