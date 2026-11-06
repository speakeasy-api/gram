package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
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

// memberToolSchemas records the tools a member last listed in full, so a
// gateway call can send the Mcp-Param headers its tool requires without
// listing first.
type memberToolSchemas struct {
	// Destination is the member's routed backend, so a swapped backend
	// never reuses another backend's schemas.
	Destination string `json:"destination"`

	// Tools holds every listed tool by name.
	Tools map[string]memberToolSchema `json:"tools"`
}

// memberToolSchema is one listed tool.
type memberToolSchema struct {
	// InputSchema is set only when the schema declares x-mcp-header annotations.
	InputSchema json.RawMessage `json:"input_schema,omitempty"`

	// Oversized marks an annotated schema over the record's byte limits,
	// which a call lists instead.
	Oversized bool `json:"oversized,omitempty"`
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
// record the gateway holds: the member's last complete listing, then the
// consented catalog. A listing that omits the tool still makes it known, with
// no schema. known is false when neither record covers the tool.
func (s *Service) knownMemberToolSchema(ctx context.Context, gate *metaGateContext, member metaMember, toolName string) (schema json.RawMessage, known bool) {
	destination := codeMemberDestination(member)
	key := memberToolSchemasKey(destination)
	record, err := s.memberToolSchemaCache.Get(ctx, key)
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		// The shared cache is unavailable; this process's copy avoids a
		// listing per call.
		local, ok := s.memberToolSchemaLocal.Get(key)
		record, err = local, nil
		if !ok {
			err = errors.New("no local member tool schemas")
		}
	}
	if err == nil && record.Destination == destination {
		tool, listed := record.Tools[toolName]
		if !tool.Oversized && (listed || len(record.Tools) > 0) {
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

// rememberMemberToolSchemas replaces a member's record with a complete
// listing. Only annotated schemas within the byte limits are kept; an
// annotated tool over them is marked, so a call lists it instead.
func (s *Service) rememberMemberToolSchemas(ctx context.Context, logger *slog.Logger, member metaMember, catalog *memberCatalog) {
	if catalog.incomplete {
		return
	}
	destination := codeMemberDestination(member)
	record := memberToolSchemas{Destination: destination, Tools: make(map[string]memberToolSchema, len(catalog.byName))}
	total := 0
	for name, entry := range catalog.byName {
		if !declaresParameterHeaders(entry.InputSchema) {
			record.Tools[name] = memberToolSchema{InputSchema: nil, Oversized: false}
			continue
		}
		if len(entry.InputSchema) > maxMemberToolSchemaBytes || total+len(entry.InputSchema) > maxMemberToolSchemasBytes {
			record.Tools[name] = memberToolSchema{InputSchema: nil, Oversized: true}
			continue
		}
		total += len(entry.InputSchema)
		record.Tools[name] = memberToolSchema{InputSchema: entry.InputSchema, Oversized: false}
	}
	key := record.CacheKey()
	s.memberToolSchemaLocal.Add(key, record)
	if previous, err := s.memberToolSchemaCache.Get(ctx, key); err == nil && sameMemberToolSchemas(previous, record) {
		return
	}
	if err := s.memberToolSchemaCache.Store(ctx, record); err != nil {
		logger.WarnContext(ctx, "store member tool schemas", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	}
}

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
