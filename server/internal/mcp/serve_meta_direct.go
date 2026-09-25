package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// listDirectGatewayTools exposes qualified member tools through the same
// permission-filtered catalogs used by Progressive discovery. An incomplete
// inventory is an error, so clients never cache a partial catalog as complete.
func (s *Service) listDirectGatewayTools(ctx context.Context, logger *slog.Logger, endpoint *mcpendpointsrepo.McpEndpoint, server *metamcprepo.MetaMcpServer, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	var params struct {
		Cursor string `json:"cursor"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid tools/list parameters")
		}
	}
	if len(params.Cursor) > 512 {
		return nil, oops.E(oops.CodeInvalid, nil, "invalid tool listing cursor")
	}
	ctx, members, err := s.resolveMetaMemberSnapshot(ctx, logger, server.ID, endpoint.ProjectID)
	if err != nil {
		return nil, err
	}
	tools := make([]*toolListEntry, 0)
	names := map[string]bool{}
	for _, member := range members {
		catalog, err := s.describeMetaMember(ctx, logger, gate, member)
		if err != nil {
			return nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
		}
		for _, entry := range catalog.entries {
			qualified := *entry
			qualified.Name = metamcp.QualifyName(member.slug, entry.Name)
			if names[qualified.Name] {
				return nil, oops.E(oops.CodeConflict, nil, "gateway contains an ambiguous tool name")
			}
			names[qualified.Name] = true
			tools = append(tools, &qualified)
			if len(tools) > 10000 {
				return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 10000 tools")
			}
		}
	}
	slices.SortFunc(tools, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	catalogBytes, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("encode gateway inventory: %w", err)
	}
	if len(catalogBytes) > 16<<20 {
		return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 16 MiB")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(catalogBytes))
	type cursor struct {
		Offset      int    `json:"offset"`
		Fingerprint string `json:"fingerprint"`
	}
	offset := 0
	if params.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(params.Cursor)
		var decoded cursor
		if err != nil || json.Unmarshal(raw, &decoded) != nil || decoded.Fingerprint != fingerprint || decoded.Offset <= 0 || decoded.Offset >= len(tools) {
			return nil, oops.E(oops.CodeInvalid, nil, "gateway inventory changed or cursor is invalid; restart tools/list")
		}
		offset = decoded.Offset
	}
	end := min(offset+100, len(tools))
	nextCursor := ""
	if end < len(tools) {
		raw, err := json.Marshal(cursor{Offset: end, Fingerprint: fingerprint})
		if err != nil {
			return nil, fmt.Errorf("encode gateway cursor: %w", err)
		}
		nextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	type page struct {
		Tools      []*toolListEntry `json:"tools"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}
	encoded, err := json.Marshal(&result[page]{
		ID:             req.ID,
		Result:         page{Tools: tools[offset:end], NextCursor: nextCursor},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Direct gateway catalog: %w", err)
	}
	return encoded, nil
}
