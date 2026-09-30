// Command rewritelogs is a local hop for `mise run demo:agent-activity`.
//
// Harnesses report the signed-in account as the log attribute `user.email`
// and offer no override. They do honour OTEL_RESOURCE_ATTRIBUTES, so the
// demo task marks each run with `gram.demo.user_email` on the resource.
// Gram's hooks ingest reads the log attribute, not that resource key.
// Copying the resource value inside the server would let any API-key holder
// spoof attribution, so the copy happens only in this process, which the
// demo task starts for one run and points only the demo harnesses at.
//
// The payload is handled as a generic JSON object. A partial OTLP struct
// would drop every field this process does not know, including log bodies.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

const (
	demoEmailKey  = "gram.demo.user_email"
	hooksEmailKey = "user.email"

	// claudeScopePrefix matches Claude Code's log instrumentation scopes.
	claudeScopePrefix = "com.anthropic.claude_code"

	// codexScopeName is Codex's agent-event log scope. Codex also exports
	// its own tracing on the same connection; those records are not a
	// dialect hooks can read.
	codexScopeName = "codex_otel.log_only"
)

// rewriteDemoLogs copies gram.demo.user_email onto user.email for log
// records the demo dialects can read, and drops everything else. kept is
// the number of records left in the payload.
func rewriteDemoLogs(payload map[string]any) (map[string]any, int) {
	rawLogs, _ := payload["resourceLogs"].([]any)
	keptResources := make([]any, 0, len(rawLogs))
	kept := 0
	for _, rawResource := range rawLogs {
		resourceLog, ok := rawResource.(map[string]any)
		if !ok {
			continue
		}
		email, ok := resourceEmail(resourceLog)
		if !ok {
			continue
		}
		rawScopes, _ := resourceLog["scopeLogs"].([]any)
		keptScopes := make([]any, 0, len(rawScopes))
		for _, rawScope := range rawScopes {
			scopeLog, ok := rawScope.(map[string]any)
			if !ok || !demoScope(scopeName(scopeLog)) {
				continue
			}
			rawRecords, _ := scopeLog["logRecords"].([]any)
			keptRecords := make([]any, 0, len(rawRecords))
			for _, rawRecord := range rawRecords {
				record, ok := rawRecord.(map[string]any)
				if !ok {
					continue
				}
				setStringAttr(record, hooksEmailKey, email)
				keptRecords = append(keptRecords, record)
				kept++
			}
			if len(keptRecords) == 0 {
				continue
			}
			scopeLog["logRecords"] = keptRecords
			keptScopes = append(keptScopes, scopeLog)
		}
		if len(keptScopes) == 0 {
			continue
		}
		resourceLog["scopeLogs"] = keptScopes
		keptResources = append(keptResources, resourceLog)
	}
	payload["resourceLogs"] = keptResources
	return payload, kept
}

func resourceEmail(resourceLog map[string]any) (string, bool) {
	resource, ok := resourceLog["resource"].(map[string]any)
	if !ok {
		return "", false
	}
	attrs, _ := resource["attributes"].([]any)
	return attrString(attrs, demoEmailKey)
}

func scopeName(scopeLog map[string]any) string {
	scope, ok := scopeLog["scope"].(map[string]any)
	if !ok {
		return ""
	}
	name, _ := scope["name"].(string)
	return name
}

func demoScope(name string) bool {
	return strings.HasPrefix(name, claudeScopePrefix) || name == codexScopeName
}

func attrString(attrs []any, key string) (string, bool) {
	for _, raw := range attrs {
		attr, ok := raw.(map[string]any)
		if !ok || attr["key"] != key {
			continue
		}
		val, _ := attr["value"].(map[string]any)
		for _, field := range []string{"stringValue", "string_value"} {
			s, ok := val[field].(string)
			if ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

// setStringAttr replaces every existing attribute named key, then appends
// one. Hooks keep the last user.email in a record, so leaving an earlier
// copy of the signed-in account would undo the rewrite.
func setStringAttr(record map[string]any, key, value string) {
	var kept []any
	if attrs, ok := record["attributes"].([]any); ok {
		for _, raw := range attrs {
			attr, ok := raw.(map[string]any)
			if ok && attr["key"] == key {
				continue
			}
			kept = append(kept, raw)
		}
	}
	record["attributes"] = append(kept, map[string]any{
		"key":   key,
		"value": map[string]any{"stringValue": value},
	})
}

func decodePayload(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, err
	}
	return payload, nil
}
