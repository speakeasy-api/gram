// Package codemode defines the bounded host API available to gateway Python.
package codemode

import (
	"context"
	"encoding/json"
)

const (
	// MaxCatalogTools bounds a live discovery operation across all members.
	MaxCatalogTools = 10000
	// MaxCatalogBytes limits retained definitions to 16 MiB per operation.
	MaxCatalogBytes = 16 << 20
	// MaxResultBytes bounds an individual callback response to 1 MiB.
	MaxResultBytes = 1 << 20
	// MaxPageSize keeps model-visible discovery pages small.
	MaxPageSize = 50
	// DefaultPageSize is the number of search candidates returned by default.
	DefaultPageSize = 10
)

// Host performs discovery and invocation under the admitted gateway identity.
type Host interface {
	// Servers lists the currently permitted gateway members.
	Servers(context.Context, PageArgs) (*ServerPage, error)
	// Search loads and ranks current authorized catalogs.
	Search(context.Context, SearchArgs) (*SearchPage, error)
	// Describe returns the original definition of one exact qualified name.
	Describe(context.Context, string) (*Description, error)
	// Call invokes one member tool without changing the execution's identity.
	Call(context.Context, string, json.RawMessage) (*ToolResult, error)
}

// PageArgs bounds a discovery response; cursors never grant access.
type PageArgs struct {
	// Limit is the requested item count, or zero for the default.
	Limit int `json:"limit,omitempty"`
	// Cursor identifies an offset in an unchanged live inventory.
	Cursor string `json:"cursor,omitempty"`
}

// SearchArgs controls lexical discovery over the permitted catalog.
type SearchArgs struct {
	// Query is a natural-language query or exact qualified tool path.
	Query string `json:"query"`
	// Server optionally narrows discovery to an exact member slug.
	Server string `json:"server,omitempty"`
	// Limit is the requested candidate count, or zero for the default.
	Limit int `json:"limit,omitempty"`
	// Cursor continues an unchanged live search.
	Cursor string `json:"cursor,omitempty"`
}

// Server is the public projection of a permitted gateway member.
type Server struct {
	// Slug is the exact member prefix, without the qualification separator.
	Slug string `json:"slug"`
	// Name is the configured display name.
	Name string `json:"name"`
}

// ServerPage is a bounded member inventory.
type ServerPage struct {
	// Items contains the members on this page.
	Items []Server `json:"items"`
	// NextCursor is empty when the inventory is exhausted.
	NextCursor string `json:"next_cursor,omitempty"`
}

// Candidate is a small model-visible tool search result.
type Candidate struct {
	// Path is the exact gateway-qualified tool name.
	Path string `json:"path"`
	// Description comes directly from the member's tool definition.
	Description string `json:"description"`
}

// Document is an authorized tool's ephemeral search input.
type Document struct {
	// Candidate is the public projection.
	Candidate Candidate `json:"candidate"`
	// Name is the unqualified upstream tool name.
	Name string `json:"name"`
	// ServerName supplies member display-name context for ranking.
	ServerName string `json:"server_name"`
	// Fingerprint binds the complete definition and routing identity.
	Fingerprint string `json:"fingerprint"`
}

// FailedMember records incomplete discovery without exposing internal errors.
type FailedMember struct {
	// Server is a permitted member slug.
	Server string `json:"server"`
	// Reason is a bounded public error code.
	Reason string `json:"reason"`
}

// SearchPage reports partial discovery explicitly.
type SearchPage struct {
	// Items contains ranked summaries.
	Items []Candidate `json:"items"`
	// NextCursor is empty on the final page.
	NextCursor string `json:"next_cursor,omitempty"`
	// Incomplete indicates one or more unavailable or truncated catalogs.
	Incomplete bool `json:"incomplete"`
	// FailedMembers identifies the affected permitted members.
	FailedMembers []FailedMember `json:"failed_members,omitempty"`
}

// Description preserves authoritative schemas rather than inventing a SDK.
type Description struct {
	// Path is the exact gateway-qualified tool name.
	Path string `json:"path"`
	// Definition is the original MCP definition with its qualified name.
	Definition json.RawMessage `json:"definition"`
	// Fingerprint detects changes during the current execution.
	Fingerprint string `json:"fingerprint"`
	// CallTemplate shows the Python calling convention using an escaped name.
	CallTemplate string `json:"call_template"`
}

// ToolResult is the Python view of a full MCP tool result.
type ToolResult struct {
	// Outcome records whether dispatch completed or its external outcome is unknown.
	Outcome string `json:"outcome"`

	// Warnings report post-dispatch validation problems without suggesting a retry.
	Warnings []string `json:"warnings,omitempty"`

	// OK is false for an upstream tool error.
	OK bool `json:"ok"`
	// Data preserves structuredContent exactly, including empty objects.
	Data json.RawMessage `json:"data"`
	// Content preserves bounded MCP content blocks without private metadata.
	Content json.RawMessage `json:"content"`
	// Error is a client-safe explanation when OK is false.
	Error string `json:"error,omitempty"`
}
