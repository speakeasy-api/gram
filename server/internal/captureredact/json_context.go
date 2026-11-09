package captureredact

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// jsonContext retains JSON punctuation around decoded strings so contextual
// detectors can see labels and sibling values without losing source locations.
// Its text is a scan surface, not a JSON document to persist or interpret.
type jsonContext struct {
	// text contains decoded strings surrounded by their original punctuation.
	text string
	// spans map each decoded string token back to the encoded document.
	spans []jsonContextSpan
	// size is the encoded document length, used for the trailing punctuation.
	size int
}

type jsonContextSpan struct {
	// start includes the opening quote in the decoded scan surface.
	start int
	// end follows the closing quote in the decoded scan surface.
	end int
	// origin contains the original quoted token and its document offset.
	origin textOrigin
	// nested maps a further decoded JSON document inside this string.
	nested *jsonContext
}

// decodeJSONContext accepts a validated JSON document. The mapping preserves
// byte boundaries even when escapes shorten or expand a decoded string.
func decodeJSONContext(data []byte, depth int) (jsonContext, bool) {
	if depth > maxNesting {
		return jsonContext{text: "", spans: nil, size: 0}, false
	}
	var output bytes.Buffer
	spans := make([]jsonContextSpan, 0)
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
			return jsonContext{text: "", spans: nil, size: 0}, false
		}
		text := decoded
		var nested *jsonContext
		trimmed := strings.TrimSpace(decoded)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '"') && json.Valid([]byte(decoded)) {
			context, ok := decodeJSONContext([]byte(decoded), depth+1)
			if !ok {
				return jsonContext{text: "", spans: nil, size: 0}, false
			}
			nested, text = &context, context.text
		}
		output.Write(data[last:start])
		decodedStart := output.Len()
		output.WriteByte('"')
		output.WriteString(text)
		output.WriteByte('"')
		spans = append(spans, jsonContextSpan{
			start: decodedStart, end: output.Len(),
			origin: textOrigin{parent: nil, encoded: data[start:end], decoded: decoded, start: start},
			nested: nested,
		})
		last = end
	}
	output.Write(data[last:])
	return jsonContext{text: output.String(), spans: spans, size: len(data)}, true
}

func (c jsonContext) mapBoundary(position int) (int, bool) {
	if position < 0 || position > len(c.text) {
		return 0, false
	}
	i := sort.Search(len(c.spans), func(i int) bool { return c.spans[i].end >= position })
	if i == len(c.spans) {
		return position + c.size - len(c.text), true
	}
	span := c.spans[i]
	if position <= span.start {
		return position + span.origin.start - span.start, true
	}
	if position == span.end {
		return span.origin.start + len(span.origin.encoded), true
	}
	relative := position - span.start - 1
	if span.nested != nil {
		var ok bool
		relative, ok = span.nested.mapBoundary(relative)
		if !ok {
			return 0, false
		}
	}
	offset, ok := jsonDecodedBoundary(span.origin.encoded, span.origin.decoded, relative)
	return span.origin.start + offset, ok
}
