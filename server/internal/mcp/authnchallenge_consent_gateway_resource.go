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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

const (
	// gatewayResourceProbeTimeout bounds one member's probe. Tighter than the
	// discovery helper's own budget on purpose: this runs inside a consent
	// click, so a member that is slow is a member that does not resolve.
	gatewayResourceProbeTimeout = 3 * time.Second

	// gatewayResourceProbeBudget bounds the whole fan-out however many members
	// the gateway holds, so consent latency stays independent of membership
	// size. Members left unprobed when it expires simply do not match.
	gatewayResourceProbeBudget = 6 * time.Second

	// gatewayResourceProbeConcurrency caps in-flight probes. A gateway's
	// members are distinct upstreams, so the cap bounds what a single consent
	// click reads as against other people's servers.
	gatewayResourceProbeConcurrency = 4
)

// resolveGatewayMemberResource returns the upstream URL of the one gateway
// member whose protected-resource metadata names issuerURL as an authorization
// server, or "" when the member cannot be identified unambiguously.
//
// Fails closed, never guesses: no match, several matches, unavailable metadata,
// and probe errors all derive "", which the connect arm treats exactly as an
// underivable resource. Two limits fall out of the discovery approach and are
// accepted for v1 — tunneled members advertise no metadata document and so are
// never resolvable this way, and one authorization server fronting two members
// of the same gateway is ambiguous by construction, because a grant records one
// resource per (subject, client).
//
// The returned error is a database fault only; the connect arm turns it into a
// failed consent rather than minting an unqualified grant.
func (s *Service) resolveGatewayMemberResource(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	issuerURL string,
) (string, error) {
	want := strings.TrimRight(issuerURL, "/")
	if want == "" {
		return "", nil
	}

	rows, err := metamcprepo.New(s.db).ListServableMetaMCPMemberUpstreams(ctx, metamcprepo.ListServableMetaMCPMemberUpstreamsParams{
		MetaMcpServerID: endpoint.MetaMcpServerID.UUID,
		ProjectID:       endpoint.ProjectID,
	})
	if err != nil {
		return "", fmt.Errorf("list gateway member upstreams: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}

	budgetCtx, cancel := context.WithTimeout(ctx, gatewayResourceProbeBudget)
	defer cancel()

	// Index-addressed so each probe writes its own slot and one unreachable
	// member neither blocks nor discards another's result.
	matches := make([]string, len(rows))

	// A plain Group: a probe failure is one member not resolving, not a reason
	// to cancel the others.
	group := &errgroup.Group{}
	group.SetLimit(gatewayResourceProbeConcurrency)
	for i, row := range rows {
		group.Go(func() error {
			if budgetCtx.Err() != nil {
				return nil
			}
			probeCtx, cancelOne := context.WithTimeout(budgetCtx, gatewayResourceProbeTimeout)
			defer cancelOne()

			doc, _, probeErr := wellknown.DiscoverProtectedResourceMetadata(probeCtx, s.guardianPolicy, row.UpstreamUrl)
			if probeErr != nil {
				// Debug: a member without OAuth answers 404 here, which is the
				// common case and not a fault.
				logger.DebugContext(ctx, "gateway member advertises no usable protected resource metadata",
					attr.SlogMcpServerID(row.McpServerID.String()),
					attr.SlogResourceURI(row.UpstreamUrl),
					attr.SlogError(probeErr),
				)
				return nil
			}
			for _, authorizationServer := range doc.AuthorizationServers {
				if strings.TrimRight(authorizationServer, "/") == want {
					matches[i] = strings.TrimRight(row.UpstreamUrl, "/")
					break
				}
			}
			return nil
		})
	}
	_ = group.Wait()

	resource := ""
	for _, match := range matches {
		switch {
		case match == "", match == resource:
		case resource == "":
			resource = match
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
