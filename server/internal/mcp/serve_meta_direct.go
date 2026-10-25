package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// maxConcurrentMemberDescribes bounds how many member describes one gateway
// inventory runs at once. A member waiting to retry holds no slot.
const maxConcurrentMemberDescribes = 6

// memberDescribeBudget bounds a whole inventory, in member call timeouts, when
// a deployment configures member calls shorter than maxGatewayListingTime
// allows for, so a listing never waits past two of them.
const memberDescribeBudget = 2

const (
	// maxMemberDescribeAttempts bounds how often one listing describes a member
	// whose upstream failed retryably: the first try plus two retries.
	maxMemberDescribeAttempts = 3

	// maxGatewayListingTime bounds a whole inventory, retries included. A
	// listing answers an agent's startup, so a rate-limited or slow member is
	// left out after a few seconds rather than holding every other tool.
	maxGatewayListingTime = 3 * time.Second

	// memberRetryBaseDelay is the first retry's delay when the upstream sent no
	// Retry-After; each later retry doubles it.
	memberRetryBaseDelay = 500 * time.Millisecond

	// memberRetryJitterDivisor adds up to a fifth of each delay, so members
	// rate-limited together do not retry in lockstep.
	memberRetryJitterDivisor = 5

	// maxDirectGatewayCatalogBytes bounds the encoded tool definitions of one
	// Direct listing.
	maxDirectGatewayCatalogBytes = 16 << 20
)

type memberDescribeResult struct {
	catalog *memberCatalog
	err     error

	// calls is how often the listing described the member: 0 when its
	// deadline passed before the member got a slot.
	calls int
}

// describeMetaMembers describes members concurrently and returns results in
// member order. Every describe and retry shares one listing deadline, and each
// attempt is also bounded by the member call timeout. A retryable failure is
// described again after its Retry-After, or a jittered backoff, when that
// retry can still start before the deadline.
func (s *Service) describeMetaMembers(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember) []memberDescribeResult {
	ctx, cancelAll := context.WithTimeout(ctx, min(memberDescribeBudget*s.metaRuntime.MemberCallTimeout, maxGatewayListingTime))
	defer cancelAll()
	results := make([]memberDescribeResult, len(members))
	slots := semaphore.NewWeighted(maxConcurrentMemberDescribes)
	var group sync.WaitGroup
	for i, member := range members {
		group.Go(func() {
			for attempt := 1; ; attempt++ {
				if slots.Acquire(ctx, 1) != nil {
					break
				}
				memberCtx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
				results[i].calls++
				results[i].catalog, results[i].err = s.describeMetaMember(memberCtx, logger, gate, member)
				cancel()
				slots.Release(1)
				failure, ok := errors.AsType[memberDiscoveryError](results[i].err)
				if !ok || !failure.retryable || attempt == maxMemberDescribeAttempts {
					break
				}
				delay := failure.retryAfter
				if delay == 0 {
					delay = memberRetryBaseDelay << (attempt - 1)
				}
				delay += randomDuration(delay / memberRetryJitterDivisor)
				if deadline, _ := ctx.Deadline(); time.Now().Add(delay).After(deadline) {
					break
				}
				logger.InfoContext(ctx, "retrying gateway member tool listing", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(failure))
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
				case <-timer.C:
				}
				timer.Stop()
			}
			if ctx.Err() != nil {
				results[i] = expiredListingResult(member, results[i])
			}
		})
	}
	group.Wait()
	return results
}

// expiredListingResult classifies a member's result once the listing time ran
// out. A member that never finished, or failed outside its own classification,
// is the budget's failure, so it is skipped like a slow member. A catalog, a
// member failure, and an access denial keep their own meaning.
func expiredListingResult(member metaMember, result memberDescribeResult) memberDescribeResult {
	if result.catalog != nil || memberAccessDenied(result.err) {
		return result
	}
	if _, memberFailure := errors.AsType[*metaMemberError](result.err); memberFailure {
		return result
	}
	// A member that never got a slot is the gateway's queue, not its upstream.
	reason := reasonListingTimeout
	if result.calls == 0 {
		reason = reasonListingBudgetExhausted
	}
	return memberDescribeResult{catalog: nil, err: memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q did not answer within the gateway listing time", member.slug), reason: reason}, status: 0, retryable: false, retryAfter: 0}, calls: result.calls}
}

// memberAccessDenied reports a member the caller may not describe, which a
// listing leaves out as an access exclusion rather than a failure.
func memberAccessDenied(err error) bool {
	denied, ok := errors.AsType[*oops.ShareableError](err)
	return ok && (denied.Code == oops.CodeNotFound || denied.Code == oops.CodeForbidden)
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
// gateway-qualified names and returns the members it skipped with why: those
// the caller cannot see, member failures, and malformed catalogs. It also
// returns every member's describe result, in member order. A failure outside
// a member's classification fails the whole inventory unless
// tolerateAnyFailure is set, and a canceled request always does.
func (s *Service) collectGatewayTools(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, tolerateAnyFailure bool, captured map[string]*memberCatalog) ([]gatewayTool, []unavailableMember, []memberDescribeResult, error) {
	tools := make([]gatewayTool, 0)
	unavailable := []unavailableMember{}
	names := map[string]bool{}
	described := s.describeMetaMembers(ctx, logger, gate, members)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("describe gateway members: %w", err)
	}
	for i, member := range members {
		catalog, err := described[i].catalog, described[i].err
		if err != nil {
			if memberAccessDenied(err) {
				unavailable = append(unavailable, unavailableMember{member: member, err: err})
				continue
			}
			if _, memberErr := errors.AsType[*metaMemberError](err); memberErr || tolerateAnyFailure {
				logger.WarnContext(ctx, "gateway member tools unavailable", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(err))
				unavailable = append(unavailable, unavailableMember{member: member, err: err})
				continue
			}
			return nil, nil, nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
		}
		if catalog.incomplete {
			unavailable = append(unavailable, unavailableMember{member: member, err: nil})
			continue
		}
		if captured != nil {
			captured[member.slug] = catalog
		}
		for _, entry := range catalog.entries {
			qualified := *entry
			qualified.Name = metamcp.QualifyName(member.slug, entry.Name)
			if names[qualified.Name] {
				return nil, nil, nil, oops.E(oops.CodeConflict, nil, "gateway contains an ambiguous tool name")
			}
			names[qualified.Name] = true
			tools = append(tools, gatewayTool{entry: &qualified, member: member})
			if len(tools) > 10000 {
				return nil, nil, nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 10000 tools")
			}
		}
	}
	return tools, unavailable, described, nil
}

// listDirectGatewayTools exposes qualified member tools through the same
// permission-filtered catalogs used by Progressive discovery. The inventory is
// already bounded, so it is returned whole: a cursor would have to name a
// listing that live members can change between pages. Every call records one
// listing attempt and, once members are known, one attempt per visible member.
func (s *Service) listDirectGatewayTools(ctx context.Context, logger *slog.Logger, endpoint *mcpendpointsrepo.McpEndpoint, server *metamcprepo.MetaMcpServer, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	start := time.Now()
	var params struct {
		Cursor string `json:"cursor"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.recordDirectListing(ctx, gate, start, listingFailure(ctx, classConfiguration, reasonInvalidRequest), nil)
			return nil, oops.E(oops.CodeInvalid, err, "invalid tools/list parameters")
		}
	}
	// No page carries a cursor, so one can only come from an earlier paginated
	// listing; the client restarts rather than appending a whole inventory.
	if params.Cursor != "" {
		s.recordDirectListing(ctx, gate, start, listingFailure(ctx, classConfiguration, reasonStaleCursor), nil)
		return nil, oops.E(oops.CodeInvalid, nil, "gateway inventory changed or cursor is invalid; restart tools/list")
	}
	ctx, members, err := s.resolveGateMemberSnapshot(ctx, logger, gate, server.ID, endpoint.ProjectID)
	if err != nil {
		s.recordDirectListing(ctx, gate, start, listingFailure(ctx, classInternal, reasonMemberSnapshotFailed), nil)
		return nil, err
	}
	inventory, _, described, err := s.collectGatewayTools(ctx, logger, gate, members, false, nil)
	if err != nil {
		s.recordDirectListing(ctx, gate, start, inventoryFailure(ctx, err), nil)
		return nil, err
	}
	attempts := make([]memberAttempt, 0, len(members)+len(gate.unapprovedMembers))
	for i, member := range members {
		if attempt, recorded := memberAttemptFor(member, described[i]); recorded {
			attempts = append(attempts, attempt)
		}
	}
	for _, member := range gate.unapprovedMembers {
		attempts = append(attempts, unapprovedMemberAttempt(member))
	}
	tools := make([]*toolListEntry, 0, len(inventory))
	for _, tool := range inventory {
		tools = append(tools, tool.entry)
	}
	slices.SortFunc(tools, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	encoded, err := encodeDirectGatewayToolList(req.ID, tools)
	if err != nil {
		s.recordDirectListing(ctx, gate, start, encodingFailure(ctx, err), attempts)
		return nil, err
	}
	s.recordDirectListing(ctx, gate, start, listingResult(attempts), attempts)
	return encoded, nil
}

// encodeDirectGatewayToolList encodes a whole Direct listing. The size bound
// applies to the tool definitions, as it did when listings were paged, so the
// response envelope never rejects an inventory whose definitions fit.
func encodeDirectGatewayToolList(id mcpjsonrpc.ID, tools []*toolListEntry) (json.RawMessage, error) {
	catalog, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("encode Direct gateway catalog: %w", err)
	}
	if len(catalog) > maxDirectGatewayCatalogBytes {
		return nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 16 MiB")
	}
	type page struct {
		Tools json.RawMessage `json:"tools"`
	}
	encoded, err := json.Marshal(&result[page]{
		ID:             id,
		Result:         page{Tools: catalog},
		serverIdentity: serverInfoMetaServer,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Direct gateway tools/list: %w", err)
	}
	return encoded, nil
}
