package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
)

// jsStackFrame matches a single frame of a JavaScript stack trace, e.g.
// "    at handleToolCall (file:///var/task/functions.js:515:40340)". A function
// bundle is minified before it is deployed, so every frame names a generated
// symbol at a byte offset into a file the caller has never seen.
var jsStackFrame = regexp.MustCompile(`(?m)^[ \t]+at \S`)

// maxFailureBodyDepth bounds how far into a failure body the trim walks. Error
// bodies are shallow in practice; the bound only keeps a pathological payload
// from costing an unbounded amount of work on a path that is already failing.
const maxFailureBodyDepth = 8

// trimFunctionFailureBody strips a failed Gram Function's JSON error body down
// to what the tool's caller can act on. Two things bloat it today:
//
// Stack traces. `@gram-ai/functions` before 0.16.2 attached one to every
// `ctx.fail()`, and the runner still attaches one when user code throws. Both
// name minified frames inside the deployed bundle, so a caller can do nothing
// with them, and an MCP client pays for them in context on every turn that
// follows the failed call.
//
// A duplicated validation dump. The same versions of the SDK set `error` to
// Zod's `ZodError.message`, which is the `issues` array re-serialized as
// pretty-printed JSON — the failure is then reported twice in one body, once
// unreadably. When `error` is exactly that, it is replaced by a one-line
// summary and the structured `issues` are kept as they are.
//
// The untrimmed body is what telemetry records, so a function's author still
// sees the stack in their tool call logs.
//
// The body is returned unchanged unless it is a JSON object with one of those
// two things in it.
func trimFunctionFailureBody(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return body
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return body
	}

	changed := dropStackTraces(payload, 0)
	if collapseValidationDump(payload) {
		changed = true
	}
	if !changed {
		return body
	}

	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}

	return out
}

// dropStackTraces removes every "stack" entry holding a JavaScript stack trace
// from value and reports whether it removed any. The value has to look like a
// stack trace: "stack" is an ordinary word and a tool is free to report, say,
// which deployment stack refused a request.
func dropStackTraces(value any, depth int) bool {
	if depth >= maxFailureBodyDepth {
		return false
	}

	changed := false

	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if trace, ok := child.(string); ok && key == "stack" && jsStackFrame.MatchString(trace) {
				delete(typed, key)
				changed = true
				continue
			}

			if dropStackTraces(child, depth+1) {
				changed = true
			}
		}
	case []any:
		for _, child := range typed {
			if dropStackTraces(child, depth+1) {
				changed = true
			}
		}
	}

	return changed
}

// collapseValidationDump replaces payload's `error` with a one-line summary
// when it is nothing but the sibling `issues` array serialized as JSON, and
// reports whether it did.
func collapseValidationDump(payload map[string]any) bool {
	message, ok := payload["error"].(string)
	if !ok {
		return false
	}

	issues, ok := payload["issues"]
	if !ok {
		return false
	}

	decoder := json.NewDecoder(strings.NewReader(message))
	decoder.UseNumber()

	var encoded any
	if err := decoder.Decode(&encoded); err != nil {
		return false
	}
	if !reflect.DeepEqual(encoded, issues) {
		return false
	}

	summary := summarizeValidationIssues(issues)
	if summary == "" {
		return false
	}

	payload["error"] = summary

	return true
}

// summarizeValidationIssues renders a Zod issue list as one line, each issue
// prefixed by the input path it concerns. It returns an empty string for
// anything that is not a non-empty list of issues carrying a message, leaving
// the caller with the original text rather than a lossy rewrite of a shape
// this does not recognize.
func summarizeValidationIssues(issues any) string {
	list, ok := issues.([]any)
	if !ok || len(list) == 0 {
		return ""
	}

	parts := make([]string, 0, len(list))
	for _, entry := range list {
		issue, ok := entry.(map[string]any)
		if !ok {
			return ""
		}

		message, ok := issue["message"].(string)
		if !ok || message == "" {
			return ""
		}

		if path := joinValidationPath(issue["path"]); path != "" {
			message = path + ": " + message
		}

		parts = append(parts, message)
	}

	return strings.Join(parts, "; ")
}

// joinValidationPath renders a Zod issue path — a list of object keys and
// array indices — the way it would be written in JavaScript, e.g. "a.b[0].c".
func joinValidationPath(path any) string {
	segments, ok := path.([]any)
	if !ok {
		return ""
	}

	var out strings.Builder
	for _, segment := range segments {
		switch typed := segment.(type) {
		case string:
			if out.Len() > 0 {
				out.WriteString(".")
			}
			out.WriteString(typed)
		case json.Number:
			out.WriteString("[")
			out.WriteString(typed.String())
			out.WriteString("]")
		default:
			return ""
		}
	}

	return out.String()
}
