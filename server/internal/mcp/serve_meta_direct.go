package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// maxConcurrentMemberDescribes bounds how many members one gateway inventory
// describes at once.
const maxConcurrentMemberDescribes = 6

// memberDescribeBudget bounds a whole inventory, in member call timeouts, so a
// large gateway with many slow members cannot hold a request for one timeout
// per batch. Members not described in time count as unreachable.
const memberDescribeBudget = 2

type memberDescribeResult struct {
	catalog *memberCatalog
	err     error
}

// describeMetaMembers describes members concurrently and returns results in
// member order. Each describe gets its own deadline from when it starts, so
// members queued behind slow ones are not starved, within an overall budget.
func (s *Service) describeMetaMembers(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember) []memberDescribeResult {
	ctx, cancelAll := context.WithTimeout(ctx, memberDescribeBudget*s.metaRuntime.MemberCallTimeout)
	defer cancelAll()
	results := make([]memberDescribeResult, len(members))
	var group errgroup.Group
	group.SetLimit(maxConcurrentMemberDescribes)
	for i, member := range members {
		group.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
			defer cancel()
			results[i].catalog, results[i].err = s.describeMetaMember(ctx, logger, gate, member)
			return nil
		})
	}
	_ = group.Wait()
	return results
}

// listDirectGatewayTools exposes qualified member tools through the same
// permission-filtered catalogs used by Progressive discovery. A member that
// cannot be reached or is not connected for the caller is left out, as
// Progressive does; a malformed member catalog fails the whole listing.
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
	described := s.describeMetaMembers(ctx, logger, gate, members)
	for i, member := range members {
		catalog, err := described[i].catalog, described[i].err
		if err != nil {
			var denied *oops.ShareableError
			if errors.As(err, &denied) && (denied.Code == oops.CodeNotFound || denied.Code == oops.CodeForbidden) {
				continue
			}
			if _, ok := errors.AsType[*metaMemberError](err); ok {
				logger.WarnContext(ctx, "gateway member tools unavailable", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(err))
				continue
			}
			return nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
		}
		if catalog.incomplete {
			return nil, oops.E(oops.CodeUnavailable, nil, "gateway tool inventory contains invalid definitions; try again").LogWarn(ctx, logger)
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
