package judgemessage

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const (
	maxDecodedViewBytes    = 16 * 1024
	maxBase64DecodedBytes  = 16 * 1024
	maxDecodeDepth         = 2
	minBase64CandidateSize = 16
)

func decodedView(input string) string {
	current := input
	changed := false
	for range maxDecodeDepth {
		next, percentChanged := decodePercentRuns(current)
		next, punycodeChanged := decodePunycodeLabels(next)
		next, base64Changed := decodeBase64Tokens(next)
		if !percentChanged && !punycodeChanged && !base64Changed {
			break
		}
		changed = true
		current = next
	}
	if !changed || current == input {
		return ""
	}
	return truncateUTF8Bytes(current, maxDecodedViewBytes)
}

func decodePercentRuns(input string) (string, bool) {
	var out strings.Builder
	changed := false
	for i := 0; i < len(input); {
		if i+2 >= len(input) || input[i] != '%' || !isHex(input[i+1]) || !isHex(input[i+2]) {
			out.WriteByte(input[i])
			i++
			continue
		}

		start := i
		decoded := make([]byte, 0, 8)
		for i+2 < len(input) && input[i] == '%' && isHex(input[i+1]) && isHex(input[i+2]) {
			decoded = append(decoded, fromHex(input[i+1])<<4|fromHex(input[i+2]))
			i += 3
		}
		if !utf8.Valid(decoded) {
			out.WriteString(input[start:i])
			continue
		}
		out.Write(decoded)
		changed = true
	}
	if !changed {
		return input, false
	}
	return out.String(), true
}

func decodePunycodeLabels(input string) (string, bool) {
	var out strings.Builder
	changed := false
	for i := 0; i < len(input); {
		if !isDomainByte(input[i]) {
			out.WriteByte(input[i])
			i++
			continue
		}

		start := i
		for i < len(input) && isDomainByte(input[i]) {
			i++
		}
		candidate := input[start:i]
		if !strings.Contains(strings.ToLower(candidate), "xn--") {
			out.WriteString(candidate)
			continue
		}
		decoded, err := idna.Lookup.ToUnicode(candidate)
		if err != nil || decoded == candidate {
			out.WriteString(candidate)
			continue
		}
		out.WriteString(decoded)
		changed = true
	}
	if !changed {
		return input, false
	}
	return out.String(), true
}

func decodeBase64Tokens(input string) (string, bool) {
	var out strings.Builder
	changed := false
	for i := 0; i < len(input); {
		if !isBase64Byte(input[i]) {
			out.WriteByte(input[i])
			i++
			continue
		}

		start := i
		for i < len(input) && isBase64Byte(input[i]) {
			i++
		}
		padding := 0
		for i < len(input) && input[i] == '=' && padding < 2 && i-start < maxBase64EncodedBytes() {
			i++
			padding++
		}
		candidate := input[start:i]
		decoded, ok := decodeBase64Candidate(candidate)
		if !ok {
			out.WriteString(candidate)
			continue
		}
		out.WriteString(decoded)
		changed = true
	}
	if !changed {
		return input, false
	}
	return out.String(), true
}

func decodeBase64Candidate(candidate string) (string, bool) {
	if len(candidate) < minBase64CandidateSize || len(candidate) > maxBase64EncodedBytes() {
		return "", false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if encoding.DecodedLen(len(candidate)) > maxBase64DecodedBytes {
			continue
		}
		decoded, err := encoding.DecodeString(candidate)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) || !printableUTF8(decoded) {
			continue
		}
		return string(decoded), true
	}
	return "", false
}

func printableUTF8(value []byte) bool {
	for _, r := range string(value) {
		if unicode.IsPrint(r) || r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		return false
	}
	return true
}

func truncateUTF8Bytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const marker = "\n…[decoded view truncated]…\n"
	budget := limit - len(marker)
	if budget <= 0 {
		return ""
	}
	headEnd := budget * 3 / 5
	for headEnd > 0 && !utf8.ValidString(value[:headEnd]) {
		headEnd--
	}
	tailStart := len(value) - (budget - headEnd)
	for tailStart < len(value) && !utf8.RuneStart(value[tailStart]) {
		tailStart++
	}
	return value[:headEnd] + marker + value[tailStart:]
}

func maxBase64EncodedBytes() int {
	return base64.StdEncoding.EncodedLen(maxBase64DecodedBytes)
}

func isBase64Byte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '+' || value == '/' || value == '-' || value == '_'
}

func isDomainByte(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '-' || value == '.'
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'f' ||
		value >= 'A' && value <= 'F'
}

func fromHex(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	default:
		return value - 'A' + 10
	}
}
