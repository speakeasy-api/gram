// Per-member credential selection for gateway endpoints. A caller
// authenticates once at a gateway, so every upstream credential they hold
// hangs off the gateway's user_session_issuer and the per-client derivation
// that qualifies a normal endpoint's grants derives nothing. This resolves the
// member a connecting client belongs to at consent time — by probing each
// member's RFC 9728 protected-resource metadata for the client's authorization
// server — and records that member's URL as the grant's RFC 8707 resource, so
// routeUpstreamToken can route by it unchanged.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const (
	// gatewayResourceProbeTimeout bounds one member's probe. Tighter than the
	// discovery helper's own budget on purpose: this runs inside a consent
	// click, so a member that is slow is a member that does not resolve.
	gatewayResourceProbeTimeout = 3 * time.Second

	// gatewayResourceProbeBudget bounds the whole fan-out however many members
	// the gateway holds, so consent latency stays independent of membership
	// size. Members left unprobed when it expires are unknown, not absent.
	gatewayResourceProbeBudget = 6 * time.Second

	// gatewayResourceProbeConcurrency caps in-flight probes. A gateway's
	// members are distinct upstreams, so the cap bounds what a single consent
	// click reads as against other people's servers.
	gatewayResourceProbeConcurrency = 4
)

// gatewayMemberProbe is one member's outcome. A conclusive probe either found
// the member claiming the issuer (resource set) or proved it claims nothing;
// an inconclusive one leaves the member unknown, and one unknown member is
// enough that no other member's claim can be called unique.
type gatewayMemberProbe struct {
	resource     string
	inconclusive bool
}

// resolveGatewayMemberResource returns the upstream URL of the one gateway
// member whose protected-resource metadata names issuerURL as an authorization
// server, or "" when the member cannot be identified unambiguously.
//
// Fails closed, never guesses: no match, several matches, and any member whose
// metadata could not be read all derive "", which the connect arm treats
// exactly as an underivable resource. Metadata that could not be read is the
// load-bearing case — a member that is slow, unreachable, or simply never
// probed makes no claim only because nobody asked, so treating it as absent
// would hand a rival member a unique match it did not earn. A 404 is the
// opposite: the member answered, and answered that it speaks no OAuth.
//
// Two limits fall out of the discovery approach and are accepted for v1 —
// tunneled members advertise no metadata document and so are never resolvable
// this way, and one authorization server fronting two members of the same
// gateway is ambiguous by construction, because a grant records one resource
// per (subject, client).
//
// The returned error is a database or grant-load fault only; the connect arm
// turns it into a failed consent rather than minting an unqualified grant.
func (s *Service) resolveGatewayMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	issuerURL string,
) (string, error) {
	if strings.TrimSpace(issuerURL) == "" {
		return "", nil
	}

	rows, err := metamcprepo.New(s.db).ListServableMetaMCPMemberUpstreams(ctx, metamcprepo.ListServableMetaMCPMemberUpstreamsParams{
		MetaMcpServerID: endpoint.MetaMcpServerID.UUID,
		ProjectID:       endpoint.ProjectID,
	})
	if err != nil {
		return "", fmt.Errorf("list gateway member upstreams: %w", err)
	}

	candidates, err := s.authorizedGatewayMemberUpstreams(ctx, endpoint, rows)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", nil
	}

	budgetCtx, cancel := context.WithTimeout(ctx, gatewayResourceProbeBudget)
	defer cancel()

	// Index-addressed so each probe writes its own slot and one unreachable
	// member neither blocks nor discards another's result.
	probes := make([]gatewayMemberProbe, len(candidates))

	// A plain Group: a probe failure is one member not resolving, not a reason
	// to cancel the others.
	group := &errgroup.Group{}
	group.SetLimit(gatewayResourceProbeConcurrency)
	for i, row := range candidates {
		group.Go(func() error {
			if budgetCtx.Err() != nil {
				probes[i].inconclusive = true
				return nil
			}
			probeCtx, cancelOne := context.WithTimeout(budgetCtx, gatewayResourceProbeTimeout)
			defer cancelOne()

			doc, _, probeErr := wellknown.DiscoverProtectedResourceMetadata(probeCtx, s.guardianPolicy, row.UpstreamUrl)
			if probeErr != nil {
				probes[i].inconclusive = !conclusiveProbeMiss(probeErr)
				// Debug: a member without OAuth answers 404 here, which is the
				// common case and not a fault.
				logger.DebugContext(ctx, "gateway member advertises no usable protected resource metadata",
					attr.SlogMcpServerID(row.McpServerID.String()),
					attr.SlogResourceURI(row.UpstreamUrl),
					attr.SlogError(probeErr),
				)
				return nil
			}
			// RFC 9728 §3.3: a document naming some other resource is the
			// host's, not this member's — on a shared multi-tenant host the
			// path-style probe 404s and the origin-style fallback returns a
			// neighbour's document, whose authorization servers this member
			// never declared.
			if doc.Resource != "" && !remotesessions.IssuerURLsCanonicallyEqual(doc.Resource, row.UpstreamUrl) {
				logger.DebugContext(ctx, "gateway member protected resource metadata names another resource",
					attr.SlogMcpServerID(row.McpServerID.String()),
					attr.SlogResourceURI(row.UpstreamUrl),
					attr.SlogOAuthResource(doc.Resource),
				)
				return nil
			}
			for _, authorizationServer := range doc.AuthorizationServers {
				// Canonical, not literal: the entry is written by the upstream
				// while the issuer URL is Gram's stored spelling, so the two
				// disagree on trailing slash, default port, and host case for
				// one authorization server. A literal compare both misses real
				// members and lets two members behind one authorization server
				// slip past the ambiguity guard on a spelling difference.
				if remotesessions.IssuerURLsCanonicallyEqual(authorizationServer, issuerURL) {
					probes[i].resource = strings.TrimRight(row.UpstreamUrl, "/")
					break
				}
			}
			return nil
		})
	}
	_ = group.Wait()

	for _, probe := range probes {
		if probe.inconclusive {
			logger.WarnContext(ctx, "a gateway member's protected resource metadata could not be read; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogOAuthIssuer(issuerURL),
			)
			return "", nil
		}
	}

	resource := ""
	for _, probe := range probes {
		switch {
		// Two members may front one URL: remote_mcp_servers is unique on
		// (project_id, slug), not on url. routeUpstreamToken keys on the
		// destination URL, so a token minted for it serves either member.
		case probe.resource == "", probe.resource == resource:
		case resource == "":
			resource = probe.resource
		default:
			logger.WarnContext(ctx, "gateway members share an authorization server; credential cannot be qualified to one member",
				attr.SlogMetaMcpServerID(endpoint.MetaMcpServerID.UUID.String()),
				attr.SlogOAuthIssuer(issuerURL),
			)
			return "", nil
		}
	}
	return resource, nil
}

// authorizedGatewayMemberUpstreams drops the members the connecting subject
// holds no mcp:connect on, as resolveMetaMemberSnapshot does for the serving
// path. A member the caller cannot reach must neither claim their credential
// — the resolved URL is echoed to them and sent to the authorization server —
// nor contest the claim of a member they can.
func (s *Service) authorizedGatewayMemberUpstreams(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	rows []metamcprepo.ListServableMetaMCPMemberUpstreamsRow,
) ([]metamcprepo.ListServableMetaMCPMemberUpstreamsRow, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	ctx, err := s.authz.PrepareContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load access grants: %w", err)
	}

	authorized := make([]metamcprepo.ListServableMetaMCPMemberUpstreamsRow, 0, len(rows))
	for _, row := range rows {
		// Only proxy backends reach here, so mcp:connect is keyed on the
		// mcp_servers id (see grantResourceIdForMcpServer). Any visibility
		// other than the two known values fails closed.
		switch row.McpServerVisibility {
		case mcpservers.VisibilityPublic:
		case mcpservers.VisibilityPrivate:
			if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), endpoint.ProjectID.String())); err != nil {
				continue
			}
		default:
			continue
		}
		authorized = append(authorized, row)
	}
	return authorized, nil
}

// conclusiveProbeMiss reports whether a failed probe proves the member claims
// no authorization server at all. Only an answer does: a 404 (the common
// shape for a member without OAuth), a member URL that cannot be probed, a
// host network policy forbids, and a document that is not RFC 9728. Every
// other failure — timeout, transport, unexpected status, or anything the
// discovery helper did not type — leaves the member's claim unknown.
func conclusiveProbeMiss(err error) bool {
	discoveryErr, ok := errors.AsType[*wellknown.ProtectedResourceDiscoveryError](err)
	if !ok {
		return false
	}
	switch discoveryErr.Code() {
	case "not_found", "invalid_url", "host_blocked", "malformed":
		return true
	default:
		return false
	}
}
