package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

const (
	// memberToolSchemasTTL bounds how long a member's listed schemas stand in
	// for a fresh listing. A member that adds or changes x-mcp-header
	// annotations sooner refuses the call, and the gateway lists again and
	// retries once.
	memberToolSchemasTTL = 6 * time.Hour

	// maxMemberToolSchemaBytes skips recording one annotated schema. Real
	// annotated schemas are a few KiB; a larger one is listed per call instead.
	maxMemberToolSchemaBytes = 64 << 10 // 64 KiB

	// maxMemberToolSchemasBytes bounds one member's recorded schemas in total,
	// so a member with many large annotated tools cannot grow its record past
	// what a cache read should carry per call.
	maxMemberToolSchemasBytes = 1 << 20 // 1 MiB

	// memberToolSchemasLocalSize bounds the in-process copies kept for when the
	// shared cache is unavailable, one per member backend.
	memberToolSchemasLocalSize = 1024
)

// parameterHeaderAnnotation marks a schema that may declare parameter headers.
var parameterHeaderAnnotation = []byte(`"x-mcp-header"`)

// memberToolSchemas records the tools the member's recent listings included,
// so a gateway call can send the Mcp-Param headers its tool requires without
// listing first. Listings pass through per-caller filters, so a tool absent
// from the record is unknown, never known to need no headers.
type memberToolSchemas struct {
	// Destination is the member's routed backend, so a swapped backend
	// never reuses another backend's schemas.
	Destination string `json:"destination"`

	// StoredAt is when the record was last written.
	StoredAt time.Time `json:"stored_at"`

	// Tools holds each recently listed tool by name.
	Tools map[string]memberToolSchema `json:"tools"`
}

// memberToolSchema is one listed tool.
type memberToolSchema struct {
	// InputSchema is set only when the schema declares x-mcp-header annotations.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// Oversized marks an annotated schema over the record's byte limits,
	// which a call lists instead.
	Oversized bool `json:"oversized,omitempty"`

	// SeenAt is when a listing last included the tool. Tools unseen for
	// memberToolSchemasTTL are dropped, so removed tools do not accumulate.
	SeenAt time.Time `json:"seen_at"`
}

func (m memberToolSchemas) CacheKey() string { return memberToolSchemasKey(m.Destination) }

func (m memberToolSchemas) TTL() time.Duration { return memberToolSchemasTTL }

func memberToolSchemasKey(destination string) string {
	sum := sha256.Sum256([]byte(destination))
	return "member_tool_schemas:" + hex.EncodeToString(sum[:])
}

// declaresParameterHeaders reports whether schema may carry x-mcp-header
// annotations. Schemas without one never need headers, so their size and
// shape are never checked.
func declaresParameterHeaders(schema json.RawMessage) bool {
	return bytes.Contains(schema, parameterHeaderAnnotation)
}

// knownMemberToolSchema returns the tool's input schema from the freshest
// record the gateway holds: the member's recent listings, then the consented
// catalog. known is false when neither covers the tool.
func (s *Service) knownMemberToolSchema(ctx context.Context, gate *metaGateContext, member metaMember, toolName string) (schema json.RawMessage, known bool) {
	destination := codeMemberDestination(member)
	if record, ok := s.readMemberToolSchemas(ctx, destination); ok {
		if tool, listed := record.Tools[toolName]; listed && !tool.Oversized && time.Since(tool.SeenAt) < memberToolSchemasTTL {
			return tool.InputSchema, true
		}
	}
	if gate.catalog != nil && gate.catalogRef != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached {
		qualified := metamcp.QualifyName(member.slug, toolName)
		for _, tool := range gate.catalog.Tools {
			if tool.Name == qualified {
				return tool.InputSchema, true
			}
		}
	}
	return nil, false
}

// readMemberToolSchemas reads the shared record, or this process's copy when
// the shared cache is unavailable, so an outage does not cost a listing per call.
func (s *Service) readMemberToolSchemas(ctx context.Context, destination string) (memberToolSchemas, bool) {
	key := memberToolSchemasKey(destination)
	record, err := s.memberToolSchemaCache.Get(ctx, key)
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss):
		return memberToolSchemas{Destination: "", StoredAt: time.Time{}, Tools: nil}, false
	case err != nil:
		local, ok := s.memberToolSchemaLocal.Get(key)
		if !ok {
			return memberToolSchemas{Destination: "", StoredAt: time.Time{}, Tools: nil}, false
		}
		record = local
	}
	return record, record.Destination == destination
}

// rememberMemberToolSchemas merges a listing into the member's record. Concurrent
// merges are not locked: a lost update only costs a later listing. Only
// annotated schemas within the byte limits are kept, spent in tool-name
// order; an annotated tool over them is marked, so a call lists it instead.
// An unchanged record is rewritten once it is half its lifetime old.
func (s *Service) rememberMemberToolSchemas(ctx context.Context, logger *slog.Logger, member metaMember, catalog *memberCatalog, now time.Time) {
	destination := codeMemberDestination(member)
	previous, found := s.readMemberToolSchemas(ctx, destination)
	record := memberToolSchemas{Destination: destination, StoredAt: now, Tools: make(map[string]memberToolSchema, len(previous.Tools)+len(catalog.byName))}
	for name, tool := range previous.Tools {
		if now.Sub(tool.SeenAt) < memberToolSchemasTTL {
			record.Tools[name] = tool
		}
	}
	for name, entry := range catalog.byName {
		tool := memberToolSchema{InputSchema: nil, Oversized: false, SeenAt: now}
		if declaresParameterHeaders(entry.InputSchema) {
			tool.InputSchema = entry.InputSchema
		}
		record.Tools[name] = tool
	}
	total := 0
	for _, name := range slices.Sorted(maps.Keys(record.Tools)) {
		tool := record.Tools[name]
		if len(tool.InputSchema) == 0 {
			continue
		}
		if len(tool.InputSchema) > maxMemberToolSchemaBytes || total+len(tool.InputSchema) > maxMemberToolSchemasBytes {
			record.Tools[name] = memberToolSchema{InputSchema: nil, Oversized: true, SeenAt: tool.SeenAt}
			continue
		}
		total += len(tool.InputSchema)
	}
	key := record.CacheKey()
	s.memberToolSchemaLocal.Add(key, record)
	if found && sameMemberToolSchemas(previous, record) && now.Sub(previous.StoredAt) < memberToolSchemasTTL/2 && seenRecently(previous, catalog, now) {
		return
	}
	if err := s.memberToolSchemaCache.Store(ctx, record); err != nil {
		logger.WarnContext(ctx, "store member tool schemas", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	}
}

// seenRecently reports whether every tool in the listing was seen in the stored
// record within half its lifetime, so skipping the write cannot expire it.
func seenRecently(previous memberToolSchemas, catalog *memberCatalog, now time.Time) bool {
	for name := range catalog.byName {
		if now.Sub(previous.Tools[name].SeenAt) >= memberToolSchemasTTL/2 {
			return false
		}
	}
	return true
}

// sameMemberToolSchemas compares the tools and schemas, not when they were seen.
func sameMemberToolSchemas(a, b memberToolSchemas) bool {
	if a.Destination != b.Destination || len(a.Tools) != len(b.Tools) {
		return false
	}
	for name, tool := range b.Tools {
		previous, ok := a.Tools[name]
		if !ok || previous.Oversized != tool.Oversized || !bytes.Equal(previous.InputSchema, tool.InputSchema) {
			return false
		}
	}
	return true
}
