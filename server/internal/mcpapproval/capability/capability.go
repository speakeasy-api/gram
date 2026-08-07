// Package capability summarises what an MCP tool says it can do.
//
// This answers the most concrete question an approver asks — whether a server
// only reads or also acts on their behalf — from two sources: the MCP
// annotations a server publishes, and the shape of each tool's input schema.
//
// Everything here is a **declaration**, never an observation — including the
// schema-derived half. Both inputs are documents the server wrote about
// itself: it may declare readOnlyHint and write anyway, and it may accept a
// shell command through a parameter it named `message`. The MCP specification
// is explicit on the point: "clients MUST consider tool annotations to be
// untrusted unless they come from trusted servers".
//
// So an assessment is useful in one direction only. A tool declaring
// destructive capability, or advertising a parameter that takes a command, is
// telling you what authority it wants — which is what lets an admin grant
// narrowly. A tool declaring nothing has told you nothing, and must never be
// read as declaring itself harmless.
package capability

import (
	"encoding/json"
	"slices"
	"strings"
)

// Hint is one MCP annotation. A server may declare it true, declare it false,
// or omit it entirely, and the three are kept distinct.
//
// Omitted is not treated as either value. The specification assigns defaults,
// but reading an absent annotation as a positive claim would manufacture
// reassurance a server never offered — and an approval surface must show
// undeclared as undeclared.
type Hint int

const (
	// HintUndeclared means the server published no value for the annotation.
	HintUndeclared Hint = iota

	// HintTrue means the server declared the annotation true.
	HintTrue

	// HintFalse means the server declared the annotation false.
	HintFalse
)

// DeclaresDestructive reports that a server annotated a tool destructive.
//
// The single reading of `destructiveHint` in the codebase, so a definition-
// driven caller assembling approval evidence and a call-driven scanner
// classifying recorded traffic cannot drift on what the annotation means.
// An absent annotation is not a declaration, and is not treated as one.
func DeclaresDestructive(destructiveHint *bool) bool {
	return HintOf(destructiveHint) == HintTrue
}

// HintOf converts an optional annotation into a Hint.
func HintOf(value *bool) Hint {
	switch {
	case value == nil:
		return HintUndeclared
	case *value:
		return HintTrue
	default:
		return HintFalse
	}
}

// Declaration is everything one tool publishes about itself.
type Declaration struct {
	// Name is the tool's identifier.
	Name string

	// Description is the tool's published description. It is untrusted text
	// that reaches the model verbatim, not documentation about the tool.
	Description string

	// InputSchema is the raw JSON Schema for the tool's arguments, or empty
	// when the server published none.
	InputSchema string

	// ReadOnly is the readOnlyHint annotation.
	ReadOnly *bool

	// Destructive is the destructiveHint annotation.
	Destructive *bool

	// Idempotent is the idempotentHint annotation.
	Idempotent *bool

	// OpenWorld is the openWorldHint annotation, which a server sets when the
	// tool reaches entities outside its own control.
	OpenWorld *bool
}

// Capability is one thing a tool declares, or its schema implies, that it can
// do.
type Capability string

const (
	// CapabilityDestructive is declared through destructiveHint.
	CapabilityDestructive Capability = "destructive"

	// CapabilityOpenWorld is declared through openWorldHint: the tool reaches
	// entities outside the server's own control, which is the annotation
	// closest to "this can send your data somewhere".
	CapabilityOpenWorld Capability = "open_world"

	// CapabilityArbitraryCommand is implied by a parameter that accepts a
	// shell command or script.
	CapabilityArbitraryCommand Capability = "arbitrary_command"

	// CapabilityFilesystemPath is implied by a parameter that accepts a file
	// or directory path.
	CapabilityFilesystemPath Capability = "filesystem_path"

	// CapabilityArbitraryURL is implied by a parameter that accepts a URL,
	// which lets a caller choose where the tool connects.
	CapabilityArbitraryURL Capability = "arbitrary_url"

	// CapabilityCredentialInput is implied by a parameter that accepts a
	// token, key, or password — a tool asking to be handed a secret directly.
	//
	//nolint:gosec // G101: a capability name, not a credential.
	CapabilityCredentialInput Capability = "credential_input"
)

// Assessment is what one tool declares, separated by how confident the source
// is.
type Assessment struct {
	// Tool is the tool's name.
	Tool string

	// Declared holds capabilities the server stated through annotations.
	Declared []Capability

	// SchemaImplied holds capabilities inferred from parameter shape.
	//
	// Inferred from the *declaration*, not from behaviour: the schema is
	// another document the server wrote, so a tool is free to take a shell
	// command through a parameter called `message`. These are heuristics over
	// names and formats, they improve how legible an honest server is, and
	// they offer no protection against a dishonest one.
	SchemaImplied []Capability

	// ActsOnBehalf reports whether the tool declares it does more than read.
	// This is the approver's question — read-only or acting for me — and it is
	// true only when the server itself said so.
	ActsOnBehalf bool

	// Unannotated reports that the server published no annotations at all, so
	// nothing about this tool's authority was declared either way. It must
	// surface as unknown rather than as an absence of capability.
	Unannotated bool
}

// Assess summarises one tool's declared capabilities.
func Assess(declaration Declaration) Assessment {
	readOnly := HintOf(declaration.ReadOnly)
	destructive := HintOf(declaration.Destructive)
	openWorld := HintOf(declaration.OpenWorld)

	var declared []Capability
	if destructive == HintTrue {
		declared = append(declared, CapabilityDestructive)
	}
	if openWorld == HintTrue {
		declared = append(declared, CapabilityOpenWorld)
	}

	return Assessment{
		Tool:          declaration.Name,
		Declared:      declared,
		SchemaImplied: schemaImplied(declaration.InputSchema),
		// readOnlyHint false is the server saying this tool does more than
		// read; destructiveHint true says the same more strongly. An
		// undeclared readOnlyHint is not a claim either way.
		ActsOnBehalf: readOnly == HintFalse || destructive == HintTrue,
		Unannotated: readOnly == HintUndeclared &&
			destructive == HintUndeclared &&
			openWorld == HintUndeclared &&
			declaration.Idempotent == nil,
	}
}

// parameterSignals maps a capability to the parameter-name substrings and JSON
// Schema formats that suggest it.
//
// Deliberately a small, legible list rather than an exhaustive one. It exists
// to draw an approver's eye to a tool that accepts a command or a path, not to
// prove anything: a server can accept the same input under any name it likes,
// so a miss here is expected and a hit is a prompt to look closer.
//
// Generic names are a known blind spot, and chasing them is not worth much.
// The reference filesystem server's move_file takes `source` and `destination`
// and matches nothing here, but adding those two words would flag every data
// source and message destination in the catalogue. Since the whole signal is
// server-controlled anyway, wider matching buys legibility on honest servers
// rather than protection against dishonest ones.
var parameterSignals = []struct {
	capability Capability
	substrings []string
	formats    []string
}{
	{
		capability: CapabilityArbitraryCommand,
		substrings: []string{"command", "cmd", "script", "shell", "exec", "argv"},
		formats:    nil,
	},
	{
		capability: CapabilityFilesystemPath,
		substrings: []string{"path", "filename", "filepath", "directory", "dirname", "folder"},
		formats:    []string{"path"},
	},
	{
		capability: CapabilityArbitraryURL,
		substrings: []string{"url", "uri", "endpoint", "webhook", "callback"},
		formats:    []string{"uri", "url", "iri"},
	},
	{
		capability: CapabilityCredentialInput,
		substrings: []string{"password", "secret", "token", "apikey", "api_key", "credential", "passphrase", "private_key"},
		formats:    nil,
	},
}

// schemaImplied walks a tool's input schema and reports the capabilities its
// parameters suggest.
//
// A schema that does not parse yields nothing: an unreadable schema is not
// evidence of safety, and the caller distinguishes an empty result from an
// absent one through the declaration itself.
func schemaImplied(schema string) []Capability {
	if strings.TrimSpace(schema) == "" {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		return nil
	}

	var found []Capability
	for name, definition := range walkProperties(parsed) {
		lowered := strings.ToLower(name)
		format := ""
		if object, ok := definition.(map[string]any); ok {
			if value, ok := object["format"].(string); ok {
				format = strings.ToLower(value)
			}
		}

		for _, signal := range parameterSignals {
			if slices.Contains(found, signal.capability) {
				continue
			}
			if matchesSignal(lowered, format, signal.substrings, signal.formats) {
				found = append(found, signal.capability)
			}
		}
	}

	return found
}

// matchesSignal reports whether a parameter's name or declared format matches
// one of a signal's markers.
func matchesSignal(name string, format string, substrings []string, formats []string) bool {
	for _, substring := range substrings {
		if strings.Contains(name, substring) {
			return true
		}
	}

	return format != "" && slices.Contains(formats, format)
}

// walkProperties yields every property name and definition in a JSON Schema,
// descending through nested objects and array items so a dangerous parameter
// cannot hide one level down.
func walkProperties(schema map[string]any) map[string]any {
	found := map[string]any{}
	collectProperties(schema, found, 0)

	return found
}

// maxSchemaDepth bounds recursion. Schemas come from the server under review,
// so a deeply nested or self-referential one must not be able to exhaust the
// stack.
const maxSchemaDepth = 12

func collectProperties(node map[string]any, into map[string]any, depth int) {
	if depth > maxSchemaDepth {
		return
	}

	if properties, ok := node["properties"].(map[string]any); ok {
		for name, definition := range properties {
			into[name] = definition
			if object, ok := definition.(map[string]any); ok {
				collectProperties(object, into, depth+1)
			}
		}
	}

	if items, ok := node["items"].(map[string]any); ok {
		collectProperties(items, into, depth+1)
	}

	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := node[key].([]any)
		if !ok {
			continue
		}
		for _, branch := range branches {
			if object, ok := branch.(map[string]any); ok {
				collectProperties(object, into, depth+1)
			}
		}
	}
}
