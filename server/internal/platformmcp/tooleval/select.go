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
	// AgentPrompt is the eval's own framing and environment, before any
	// server instructions.
	AgentPrompt  string            `json:"agent_prompt"`
	Instructions map[string]string `json:"instructions"`
	// Discovery is the progressive surface's own search, describe and call
	// tools, and FirstRun the catalogue tools it lists next to them.
	Discovery []Tool   `json:"discovery"`
	FirstRun  []string `json:"first_run"`
	Tools     []Tool   `json:"tools"`
}

// sharedChanged reports what every case reads that changed, or "".
func sharedChanged(base, head Catalogue) string {
	switch {
	case base.AgentPrompt != head.AgentPrompt:
		return "the eval's agent prompt or environment changed"
	case !maps.Equal(base.Instructions, head.Instructions):
		return "server instructions changed"
	case !slices.Equal(base.FirstRun, head.FirstRun) || len(ChangedTools(Catalogue{Tools: base.Discovery}, Catalogue{Tools: head.Discovery})) > 0:
		return "the progressive discovery surface changed"
	}
	return ""
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
// schema or annotations between two catalogues.
func ChangedTools(base, head Catalogue) []string {
	before := make(map[string]Tool, len(base.Tools))
	for _, tool := range base.Tools {
		before[tool.Name] = tool
	}
	var changed []string
	for _, tool := range head.Tools {
		old, ok := before[tool.Name]
		delete(before, tool.Name)
		if !ok || old.Description != tool.Description || old.ReadOnly != tool.ReadOnly || !sameJSON(old.InputSchema, tool.InputSchema) || !sameJSON(old.Annotations, tool.Annotations) {
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
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var decodedA, decodedB any
	if json.Unmarshal(a, &decodedA) != nil || json.Unmarshal(b, &decodedB) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(decodedA, decodedB)
}

// SelectChanged returns the cases a change from base to head could move, the
// negative cases to run beside them, and why. A change every case reads (the
// agent prompt, server instructions, or the progressive discovery surface)
// selects every case. Otherwise a case is selected when it is new or edited,
// or when it names a changed tool in expect or allow. When anything is
// selected, the negative cases come too, since a reworded tool can start
// answering requests it should not.
//
// A reworded tool can also take requests meant for a neighbouring tool. That
// neighbour's cases run only if they name the changed tool; a full run
// (without --changed-since) covers the rest.
func SelectChanged(base, head Catalogue, baseCases, headCases []Case) (changed, negatives []Case, reason string) {
	if why := sharedChanged(base, head); why != "" {
		return headCases, nil, why + ", so every case runs"
	}
	tools := ChangedTools(base, head)
	before := make(map[string]Case, len(baseCases))
	for _, c := range baseCases {
		before[c.ID] = c
	}
	edited := 0
	for _, c := range headCases {
		old, ok := before[c.ID]
		old.File, c.File = "", ""
		caseChanged := !ok || !reflect.DeepEqual(old, c)
		if caseChanged {
			edited++
		}
		if caseChanged || slices.ContainsFunc(slices.Concat(c.Expect, c.Allow), func(tool string) bool { return slices.Contains(tools, tool) }) {
			changed = append(changed, c)
		}
	}
	if len(changed) == 0 {
		return nil, nil, "no Platform MCP tool or eval case changed"
	}
	for _, c := range headCases {
		if c.Negative() && !slices.ContainsFunc(changed, func(s Case) bool { return s.ID == c.ID }) {
			negatives = append(negatives, c)
		}
	}
	reason = fmt.Sprintf("%d cases selected: %d new or edited", len(changed), edited)
	if len(tools) > 0 {
		reason += fmt.Sprintf(", and the cases for %d changed tools (%s)", len(tools), strings.Join(tools, ", "))
	}
	return changed, negatives, reason + fmt.Sprintf(", plus %d negative cases", len(negatives))
}

// CapCases keeps at most n cases, the selected ones first: when they alone
// exceed n, a stable sample of them; otherwise all of them and a stable sample
// of the filler for the rest. Zero means no cap.
func CapCases(selected, filler []Case, n int) []Case {
	if n <= 0 || len(selected)+len(filler) <= n {
		return slices.Concat(selected, filler)
	}
	if len(selected) >= n {
		return SampleCases(selected, n)
	}
	return slices.Concat(selected, SampleCases(filler, n-len(selected)))
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
