package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

// memberToolSchemasTTL bounds how long a member's listed schemas stand in for
// a fresh listing. A member that adds or changes x-mcp-header annotations
// sooner refuses the call, and the gateway lists again and retries once.
const memberToolSchemasTTL = 6 * time.Hour

// memberToolSchemas records the tools a member last listed, so a gateway call
// can send the Mcp-Param headers its tool requires without listing first.
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
}

func (m memberToolSchemas) CacheKey() string { return memberToolSchemasKey(m.Destination) }

func (m memberToolSchemas) TTL() time.Duration { return memberToolSchemasTTL }

func memberToolSchemasKey(destination string) string {
	sum := sha256.Sum256([]byte(destination))
	return "member_tool_schemas:" + hex.EncodeToString(sum[:])
}

// knownMemberToolSchema returns the tool's input schema from the freshest
// record the gateway holds: the member's last listing, then the consented
// catalog. known is false when neither lists the tool.
func (s *Service) knownMemberToolSchema(ctx context.Context, gate *metaGateContext, member metaMember, toolName string) (schema json.RawMessage, known bool) {
	if listed, err := s.memberToolSchemaCache.Get(ctx, memberToolSchemasKey(codeMemberDestination(member))); err == nil {
		if tool, ok := listed.Tools[toolName]; ok {
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

// rememberMemberToolSchemas merges a member's listing into its record. Only
// annotated schemas are kept, so the record stays small.
func (s *Service) rememberMemberToolSchemas(ctx context.Context, logger *slog.Logger, member metaMember, catalog *memberCatalog) {
	destination := codeMemberDestination(member)
	record, err := s.memberToolSchemaCache.Get(ctx, memberToolSchemasKey(destination))
	if err != nil || record.Destination != destination {
		record = memberToolSchemas{Destination: destination, Tools: nil}
	}
	if record.Tools == nil {
		record.Tools = make(map[string]memberToolSchema, len(catalog.byName))
	}
	changed := false
	for name, entry := range catalog.byName {
		next := memberToolSchema{InputSchema: nil}
		if bytes.Contains(entry.InputSchema, []byte(`"x-mcp-header"`)) {
			next.InputSchema = entry.InputSchema
		}
		if previous, ok := record.Tools[name]; ok && bytes.Equal(previous.InputSchema, next.InputSchema) {
			continue
		}
		record.Tools[name] = next
		changed = true
	}
	if !changed {
		return
	}
	if err := s.memberToolSchemaCache.Store(ctx, record); err != nil {
		logger.WarnContext(ctx, "store member tool schemas", attr.SlogError(err), attr.SlogMcpServerID(member.serverID.String()))
	}
}
