// Package tooleval measures how well an agent uses the Platform MCP tools. A
// case is one plain-language request from an administrator; the agent sees a
// discovery surface (the full catalogue, or the progressive search, describe
// and call tools) and the eval scores the first Platform tool it reaches for
// and the arguments it sends. Tools never run: selection is scored before any
// side effect, so the eval needs no database and no seeded organization.
package tooleval

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed cases/*.yaml
var caseFiles embed.FS

// Case is one request and the tool calls that would serve it.
type Case struct {
	// ID is unique across every case file.
	ID string `yaml:"id"`

	// Prompt is what the user types. It names no tool.
	Prompt string `yaml:"prompt"`

	// Expect lists the tools that serve the request; reaching any one of
	// them first is a hit. An empty list marks a negative case, where the
	// right move is to answer or ask without calling a Platform tool.
	Expect []string `yaml:"expect"`

	// Allow lists reads that are a correct first step towards an expected
	// tool, such as reading a role before changing it. Reaching one counts as
	// on path and ends the attempt: the eval cannot fake the references a
	// real read returns, so it does not try to score what comes after.
	Allow []string `yaml:"allow"`

	// Args are values the expected tool's arguments must carry. A string
	// matches when the argument contains it, ignoring case, or, for an array
	// argument, when one element does. A list matches an array argument when
	// each listed string is in some element, in any order. Any other value
	// must be equal as JSON.
	Args map[string]any `yaml:"args"`

	// File is the case file the case came from.
	File string `yaml:"-"`
}

// Negative reports whether no Platform tool should be called.
func (c Case) Negative() bool { return len(c.Expect) == 0 }

// OrientationTools are reads an agent may make before any request without
// being marked wrong: they tell it who it is, where, and which servers exist.
// They get a stub result and the agent continues, since the environment in
// its instructions already answers them.
var OrientationTools = []string{"get_platform_context", "whoami", "list_projects", "find_mcp"}

// orients reports whether a call is an orientation read the agent continues
// past.
func (c Case) orients(tool string) bool {
	return !slices.Contains(c.Expect, tool) && !slices.Contains(c.Allow, tool) && slices.Contains(OrientationTools, tool)
}

// onPath reports whether a call is a correct first step short of an expected
// tool.
func (c Case) onPath(tool string) bool {
	return !slices.Contains(c.Expect, tool) && slices.Contains(c.Allow, tool)
}

// LoadCases reads every embedded case file.
func LoadCases() ([]Case, error) {
	cases, err := fs.Sub(caseFiles, "cases")
	if err != nil {
		return nil, fmt.Errorf("open case files: %w", err)
	}
	return LoadCasesFS(cases)
}

// LoadCasesFS reads every case file at the root of fsys, such as the case
// files of a base commit a change is compared against.
func LoadCasesFS(fsys fs.FS) ([]Case, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, fmt.Errorf("list case files: %w", err)
	}
	var cases []Case
	seen := make(map[string]string)
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		var fileCases []Case
		if err := yaml.Unmarshal(data, &fileCases); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, c := range fileCases {
			c.File = path.Base(name)
			if c.ID == "" || strings.TrimSpace(c.Prompt) == "" {
				return nil, fmt.Errorf("%s: every case needs an id and a prompt", c.File)
			}
			if other, ok := seen[c.ID]; ok {
				return nil, fmt.Errorf("case %q is in both %s and %s", c.ID, other, c.File)
			}
			seen[c.ID] = c.File
			cases = append(cases, c)
		}
	}
	return cases, nil
}

// argsMatch reports whether the arguments carry every expected value.
func argsMatch(expected map[string]any, arguments json.RawMessage) bool {
	if len(expected) == 0 {
		return true
	}
	var got map[string]any
	if err := json.Unmarshal(arguments, &got); err != nil {
		return false
	}
	for key, want := range expected {
		value, ok := got[key]
		if !ok {
			return false
		}
		if wantString, ok := want.(string); ok {
			if !containsString(value, wantString) {
				return false
			}
			continue
		}
		if wantList, ok := want.([]any); ok {
			if gotList, ok := value.([]any); ok && allStrings(wantList) {
				for _, item := range wantList {
					if !containsString(gotList, item.(string)) {
						return false
					}
				}
				continue
			}
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			return false
		}
		gotJSON, err := json.Marshal(value)
		if err != nil || string(wantJSON) != string(gotJSON) {
			return false
		}
	}
	return true
}

// containsString reports whether a string argument, or any string element of
// an array argument, contains want, ignoring case.
func containsString(value any, want string) bool {
	switch got := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(got), strings.ToLower(want))
	case []any:
		for _, item := range got {
			if text, ok := item.(string); ok && strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
				return true
			}
		}
	}
	return false
}

func allStrings(items []any) bool {
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return len(items) > 0
}
