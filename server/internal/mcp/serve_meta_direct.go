package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
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

	// minMemberRetryBudget is the least listing time a retry needs left to be
	// worth starting: a fresh dial, initialize and tools/list. A retry started
	// with less would likely time out and replace the upstream's own failure,
	// such as a rate limit, with a timeout.
	minMemberRetryBudget = 500 * time.Millisecond

	// maxDirectGatewayCatalogBytes bounds the encoded tool definitions of one
	// Direct listing.
	maxDirectGatewayCatalogBytes = 16 << 20

	// omittedMemberLogTimeout bounds recording the members a listing left out,
	// so a slow telemetry store cannot hold the listing's answer.
	omittedMemberLogTimeout = 500 * time.Millisecond
)

type memberDescribeResult struct {
	catalog *memberCatalog
	err     error
}

// describeMetaMembers describes members concurrently, retrying transient
// failures, and returns results in member order. Every describe and retry
// shares one listing deadline.
func (s *Service) describeMetaMembers(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember) []memberDescribeResult {
	ctx, cancelAll := context.WithTimeout(ctx, min(memberDescribeBudget*s.metaRuntime.MemberCallTimeout, maxGatewayListingTime))
	defer cancelAll()
	results := make([]memberDescribeResult, len(members))
	slots := semaphore.NewWeighted(maxConcurrentMemberDescribes)
	var group sync.WaitGroup
	for i, member := range members {
		group.Go(func() {
			results[i] = s.describeMetaMemberWithRetry(ctx, logger, gate, member, slots)
			if ctx.Err() != nil {
				results[i] = expiredListingResult(member, results[i])
			}
		})
	}
	group.Wait()
	return results
}

// describeMetaMemberWithRetry describes a member, and describes it again after
// a retryable failure once its Retry-After, or a jittered backoff, has passed,
// up to maxMemberDescribeAttempts in all. A retry that would start with less
// than minMemberRetryBudget before ctx's deadline is not attempted, so the
// last failure is returned instead.
// Each attempt holds one of slots and is bounded by the member call timeout;
// a member waiting to retry holds no slot. The result is empty when ctx ended
// before the first attempt got a slot.
func (s *Service) describeMetaMemberWithRetry(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember, slots *semaphore.Weighted) memberDescribeResult {
	var result memberDescribeResult
	for attempt := 1; ; attempt++ {
		if slots.Acquire(ctx, 1) != nil {
			return result
		}
		memberCtx, cancel := context.WithTimeout(ctx, s.metaRuntime.MemberCallTimeout)
		result.catalog, result.err = s.describeMetaMember(memberCtx, logger, gate, member)
		cancel()
		slots.Release(1)
		failure, ok := errors.AsType[memberDiscoveryError](result.err)
		if !ok || !failure.retryable || attempt == maxMemberDescribeAttempts {
			return result
		}
		delay := failure.retryAfter
		if delay == 0 {
			delay = memberRetryBaseDelay << (attempt - 1)
		}
		delay += randomDuration(delay / memberRetryJitterDivisor)
		if deadline, bounded := ctx.Deadline(); bounded && time.Now().Add(delay+minMemberRetryBudget).After(deadline) {
			return result
		}
		logger.InfoContext(ctx, "retrying gateway member tool listing", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(failure))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
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
	return memberDescribeResult{catalog: nil, err: memberDiscoveryError{metaMemberError: &metaMemberError{message: fmt.Sprintf("server %q did not answer within the gateway listing time", member.slug)}, status: 0, retryable: false, retryAfter: 0}}
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
// the caller cannot see, member failures, and malformed catalogs. A failure
// outside a member's classification fails the whole inventory unless
// tolerateAnyFailure is set, and a canceled request always does.
func (s *Service) collectGatewayTools(ctx context.Context, logger *slog.Logger, gate *metaGateContext, members []metaMember, tolerateAnyFailure bool, captured map[string]*memberCatalog) ([]gatewayTool, []unavailableMember, error) {
	tools := make([]gatewayTool, 0)
	unavailable := []unavailableMember{}
	names := map[string]bool{}
	described := s.describeMetaMembers(ctx, logger, gate, members)
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("describe gateway members: %w", err)
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
			return nil, nil, oops.E(oops.CodeUnavailable, err, "gateway tool inventory is incomplete; try again").LogWarn(ctx, logger)
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
				return nil, nil, oops.E(oops.CodeConflict, nil, "gateway contains an ambiguous tool name")
			}
			names[qualified.Name] = true
			tools = append(tools, gatewayTool{entry: &qualified, member: member})
			if len(tools) > 10000 {
				return nil, nil, oops.E(oops.CodeRequestTooLarge, nil, "gateway inventory exceeds 10000 tools")
			}
		}
	}
	return tools, unavailable, nil
}

// listDirectGatewayTools exposes qualified member tools through the same
// permission-filtered catalogs used by Progressive discovery. The inventory is
// already bounded, so it is returned whole: a cursor would have to name a
// listing that live members can change between pages.
func (s *Service) listDirectGatewayTools(ctx context.Context, logger *slog.Logger, endpoint *mcpendpointsrepo.McpEndpoint, server *metamcprepo.MetaMcpServer, gate *metaGateContext, req *rawRequest) (json.RawMessage, error) {
	start := time.Now()
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
	inventory, unavailable, err := s.collectGatewayTools(ctx, logger, gate, members, false, nil)
	if err != nil {
		return nil, err
	}
	// Each member a failure left out is recorded as gateway discovery, so the
	// gateway's diagnostics show what the listing is missing; exclusions such
	// as visibility, credentials, and consent are not failures. Rows carry the
	// upstream's status and fixed text, never the member's message, which can
	// quote its upstream's response.
	omittedLogs := make([]tm.LogParams, 0, len(unavailable))
	for _, omitted := range unavailable {
		status := http.StatusBadGateway
		reason := "returned tool definitions the gateway could not list"
		if omitted.err != nil {
			upstream, ok := errors.AsType[memberDiscoveryError](omitted.err)
			if !ok {
				continue
			}
			if upstream.status != 0 {
				status = upstream.status
			}
			switch {
			case upstream.status == http.StatusTooManyRequests:
				reason = "was rate limited"
			case upstream.status >= http.StatusInternalServerError:
				reason = "was unavailable"
			case upstream.status != 0:
				reason = "rejected the tool listing"
			default:
				reason = "could not be listed"
			}
		}
		row := metaDiscoveryLogParams(ctx, gate, "tools/list", start, nil)
		tm.HTTPLogAttributes(row.Attributes).RecordStatusCode(status)
		row.Attributes[attr.ErrorMessageKey] = fmt.Sprintf("server %q %s and was left out of the tool listing", omitted.member.slug, reason)
		omittedLogs = append(omittedLogs, row)
	}
	if len(omittedLogs) > 0 {
		logCtx, cancel := context.WithTimeout(ctx, omittedMemberLogTimeout)
		err := s.telemLogger.LogBulkBounded(logCtx, omittedLogs)
		cancel()
		if err != nil {
			logger.ErrorContext(ctx, "record gateway members left out of tools/list", attr.SlogError(err))
		}
	}
	tools := make([]*toolListEntry, 0, len(inventory))
	for _, tool := range inventory {
		tools = append(tools, tool.entry)
	}
	slices.SortFunc(tools, func(a, b *toolListEntry) int { return strings.Compare(a.Name, b.Name) })
	return encodeDirectGatewayToolList(req.ID, tools)
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
