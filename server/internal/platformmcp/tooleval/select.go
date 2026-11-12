package tooleval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"reflect"
	"slices"
	"strings"
)

// Catalogue is what clients are shown: each surface's server instructions and
// every tool. It is committed as catalogue.json, so a pull request's diff shows
// exactly which descriptions and schemas changed, and the eval can run only
// the cases a change could move.
type Catalogue struct {
	Instructions map[string]string `json:"instructions"`
	Tools        []Tool            `json:"tools"`
}

// Marshal renders the catalogue the way catalogue.json stores it.
func (c Catalogue) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode catalogue: %w", err)
	}
	return append(data, '\n'), nil
}

// ChangedTools lists the tools added, removed, or given a new description,
// schema or annotation between two catalogues.
func ChangedTools(base, head Catalogue) []string {
	before := make(map[string]Tool, len(base.Tools))
	for _, tool := range base.Tools {
		before[tool.Name] = tool
	}
	var changed []string
	for _, tool := range head.Tools {
		old, ok := before[tool.Name]
		delete(before, tool.Name)
		if !ok || old.Description != tool.Description || old.ReadOnly != tool.ReadOnly || !sameJSON(old.InputSchema, tool.InputSchema) {
			changed = append(changed, tool.Name)
		}
	}
	changed = append(changed, slices.Collect(maps.Keys(before))...)
	slices.Sort(changed)
	return changed
}

// sameJSON compares decoded values, so formatting and escaping do not count
// as a change.
func sameJSON(a, b json.RawMessage) bool {
	var decodedA, decodedB any
	if json.Unmarshal(a, &decodedA) != nil || json.Unmarshal(b, &decodedB) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(decodedA, decodedB)
}

// SelectChanged returns the cases a change from base to head could move, and
// why. Changed server instructions reach every tool, so they select every
// case. Otherwise a case is selected when it is new or edited, or when it
// names a changed tool in expect or allow. When anything is selected, the
// negative cases come too, since a reworded tool can start answering requests
// it should not.
func SelectChanged(base, head Catalogue, baseCases, headCases []Case) ([]Case, string) {
	if !maps.Equal(base.Instructions, head.Instructions) {
		return headCases, "server instructions changed, so every case runs"
	}
	tools := ChangedTools(base, head)
	before := make(map[string]Case, len(baseCases))
	for _, c := range baseCases {
		before[c.ID] = c
	}
	var selected []Case
	edited := 0
	for _, c := range headCases {
		old, ok := before[c.ID]
		old.File, c.File = "", ""
		caseChanged := !ok || !reflect.DeepEqual(old, c)
		if caseChanged {
			edited++
		}
		if caseChanged || slices.ContainsFunc(slices.Concat(c.Expect, c.Allow), func(tool string) bool { return slices.Contains(tools, tool) }) {
			selected = append(selected, c)
		}
	}
	if len(selected) == 0 {
		return nil, "no Platform MCP tool or eval case changed"
	}
	for _, c := range headCases {
		if c.Negative() && !slices.ContainsFunc(selected, func(s Case) bool { return s.ID == c.ID }) {
			selected = append(selected, c)
		}
	}
	reason := fmt.Sprintf("%d cases selected: %d new or edited", len(selected), edited)
	if len(tools) > 0 {
		reason += fmt.Sprintf(", and the cases for %d changed tools (%s)", len(tools), strings.Join(tools, ", "))
	}
	return selected, reason + ", plus the negative cases"
}

// SampleCases keeps at most n cases, chosen by a hash of their IDs so the
// same change samples the same cases on every run.
func SampleCases(cases []Case, n int) []Case {
	if n <= 0 || len(cases) <= n {
		return cases
	}
	hashOf := func(id string) uint64 {
		h := fnv.New64a()
		_, _ = h.Write([]byte(id))
		return h.Sum64()
	}
	sampled := slices.Clone(cases)
	slices.SortFunc(sampled, func(a, b Case) int {
		ha, hb := hashOf(a.ID), hashOf(b.ID)
		switch {
		case ha < hb:
			return -1
		case ha > hb:
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return sampled[:n]
}

// ProviderSchemaProblem says why a model provider would reject a tool's input
// schema, or returns "" when none would. Anthropic requires an object schema
// without oneOf, anyOf or allOf at the top level, and rejects the whole
// request, not just the tool, when one breaks the rule.
func ProviderSchemaProblem(schema json.RawMessage) string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(schema, &top); err != nil {
		return "input schema is not a JSON object"
	}
	var kind string
	if err := json.Unmarshal(top["type"], &kind); err != nil || kind != "object" {
		return `top-level type is not "object"`
	}
	var combinators []string
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := top[key]; ok {
			combinators = append(combinators, key)
		}
	}
	if len(combinators) > 0 {
		return "top-level " + strings.Join(combinators, " and ")
	}
	return ""
}
