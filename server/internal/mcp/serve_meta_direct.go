package mcp

import (
	"context"
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
// per batch. Members not described in time count as unanswered.
const memberDescribeBudget = 2

type memberDescribeResult struct {
	catalog *memberCatalog
	err     error
}

// describeMetaMembers describes members concurrently and returns results in
// member order. Each describe gets its own deadline from when it starts, so
// members queued behind slow ones are not starved, within an overall budget.
func (s *Service) describeMetaMembers(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember) []memberDescribeResult {
	requestCtx := ctx
	ctx, cancelAll := context.WithTimeout(ctx, memberDescribeBudget*s.metaRuntime.MemberCallTimeout)
	defer cancelAll()
	results := make([]memberDescribeResult, len(members))
	var group errgroup.Group
	group.SetLimit(maxConcurrentMemberDescribes)
	for i, member := range members {
		group.Go(func() error {
			if ctx.Err() == nil {
				memberCtx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
				results[i].catalog, results[i].err = s.describeMetaMember(memberCtx, logger, gate, member)
				cancel()
			}
			// Past the budget, any failure (including never starting) is the
			// budget's, not the member's, so it is skipped like a slow member.
			// A canceled request fails the listing instead.
			if ctx.Err() != nil && (results[i].err != nil || results[i].catalog == nil) {
				if err := requestCtx.Err(); err != nil {
					results[i] = memberDescribeResult{catalog: nil, err: err}
				} else {
					results[i] = memberDescribeResult{catalog: nil, err: memberUnansweredError{&metaMemberError{message: fmt.Sprintf("server %q did not answer within the gateway listing time", member.slug)}}}
				}
			}
			return nil
		})
	}
	_ = group.Wait()
	return results
}

// unavailableMember is a member collectGatewayTools could not list; err is
// nil when its catalog was malformed.
type unavailableMember struct {
	member metaMember
	err    error
}

// gatewayTool is one member tool under its gateway-qualified name.
type gatewayTool struct {
	entry  *toolListEntry
	member metaMember
}

// collectGatewayTools describes members' permission-filtered catalogs under
// gateway-qualified names. Members the caller cannot see are skipped, as are
// members it has not connected or that answer with a failure excluding them.
// A member that did not answer, or any unclassified failure, fails the whole
// inventory rather than listing it without that member, unless unavailable
// is non-nil: then a member that cannot be listed right now is skipped and
// appended there with its failure.
func (s *Service) collectGatewayTools(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, unavailable *[]unavailableMember, captured map[string]*memberCatalog) ([]gatewayTool, error) {
	tools := make([]gatewayTool, 0)
	names := map[string]bool{}
	described := s.describeMetaMembers(ctx, logger, gate, members)
	for i, member := range members {
		catalog, err := described[i].catalog, described[i].err
		if err != nil {
			var denied *oops.ShareableError
			if errors.As(err, &denied) && (denied.Code == oops.CodeNotFound || denied.Code == oops.CodeForbidden) {
				if unavailable != nil {
					*unavailable = append(*unavailable, unavailableMember{member: member, err: err})
				}
				continue
			}
			if unavailable == nil {
				if unanswered, ok := errors.AsType[memberUnansweredError](err); ok {
					return nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete because %s; try again", unanswered.message).LogWarn(ctx, logger)
				}
			}
			if _, memberErr := errors.AsType[*metaMemberError](err); memberErr || unavailable != nil {
				logger.WarnContext(ctx, "gateway member tools unavailable", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(err))
				if unavailable != nil {
					*unavailable = append(*unavailable, unavailableMember{member: member, err: err})
				}
				continue
			}
			return nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
		}
		if catalog.incomplete {
			if unavailable != nil {
				*unavailable = append(*unavailable, unavailableMember{member: member, err: nil})
				continue
			}
			return nil, oops.E(oops.CodeUnavailable, nil, "gateway tool inventory contains invalid definitions; try again").LogWarn(ctx, logger)
		}
		if captured != nil {
			captured[member.slug] = catalog
		}
		for _, entry := range catalog.entries {
			qualified := *entry
			qualified.Name = metamcp.QualifyName(member.slug, entry.Name)
			if names[qualified.Name] {
				return nil, oops.E(oops.CodeConflict, nil, "gateway contains an ambiguous tool name")
			}
			names[qualified.Name] = true
			tools = append(tools, gatewayTool{entry: &qualified, member: member})
			if len(tools) > 10000 {
				return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 10000 tools")
			}
		}
	}
	return tools, nil
}

// listDirectGatewayTools exposes qualified member tools through the same
// permission-filtered catalogs used by Progressive discovery. The inventory is
// already bounded, so it is returned whole: a cursor would have to name a
// listing that live members can change between pages.
func (s *Service) listDirectGatewayTools(ctx context.Context, logger *slog.Logger, endpoint *mcpendpointsrepo.McpEndpoint, server *metamcprepo.MetaMcpServer, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	var params struct {
		Cursor string `json:"cursor"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid tools/list parameters")
		}
	}
	// No page carries a cursor, so one can only come from an earlier paginated
	// listing; the client restarts rather than appending a whole inventory.
	if params.Cursor != "" {
		return nil, oops.E(oops.CodeInvalid, nil, "gateway inventory changed or cursor is invalid; restart tools/list")
	}
	ctx, members, err := s.resolveGateMemberSnapshot(ctx, logger, gate, server.ID, endpoint.ProjectID)
	if err != nil {
		return nil, err
	}
	inventory, err := s.collectGatewayTools(ctx, logger, gate, members, nil, nil)
	if err != nil {
		return nil, err
	}
	tools := make([]*toolListEntry, 0, len(inventory))
	for _, tool := range inventory {
		tools = append(tools, tool.entry)
	}
	slices.SortFunc(tools, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	type page struct {
		Tools []*toolListEntry `json:"tools"`
	}
	encoded, err := json.Marshal(&result[page]{
		ID:             req.ID,
		Result:         page{Tools: tools},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Direct gateway catalog: %w", err)
	}
	if len(encoded) > 16<<20 {
		return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 16 MiB")
	}
	return encoded, nil
}
