package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
)

// registryPromptHashes hashes the cascade's prompts as the evaluation harness
// registers prompt versions, so a report artifact names the versions it ran.
func registryPromptHashes() (confirmation, questions string, err error) {
	raw, err := json.Marshal(piopenrouter.PrefilterQuestions())
	if err != nil {
		return "", "", fmt.Errorf("marshal prefilter questions: %w", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", "", fmt.Errorf("decode prefilter questions: %w", err)
	}
	var sorted strings.Builder
	if err := writePythonJSON(&sorted, decoded); err != nil {
		return "", "", err
	}
	confirmationSum := sha256.Sum256([]byte(piopenrouter.SystemPrompt + "\n" + piopenrouter.WindowInstructions))
	questionsSum := sha256.Sum256([]byte(sorted.String()))
	return fmt.Sprintf("%x", confirmationSum), fmt.Sprintf("%x", questionsSum), nil
}

// writePythonJSON renders decoded JSON as Python's json.dumps(v,
// sort_keys=True) does: ", " and ": " separators, sorted keys, and every
// character outside printable ASCII escaped. It rejects numbers, which the
// prompts never contain and whose float formatting differs between the two.
func writePythonJSON(b *strings.Builder, v any) error {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		writePythonString(b, v)
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := writePythonJSON(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		b.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writePythonString(b, key)
			b.WriteString(": ")
			if err := writePythonJSON(b, v[key]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("python json: unsupported value %T", v)
	}
	return nil
}

func writePythonString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r >= ' ' && r <= '~':
			b.WriteRune(r)
		case r > 0xffff:
			high, low := utf16.EncodeRune(r)
			fmt.Fprintf(b, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(b, `\u%04x`, r)
		}
	}
	b.WriteByte('"')
}
