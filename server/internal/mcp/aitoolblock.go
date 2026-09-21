// Shadow AI gateway blocking: refusing an AI tool an organization has decided
// may not reach its MCP servers.
//
// The check lives at the OAuth boundary rather than at tools/call because
// that is the last point where the caller's identity is a credential Gram
// verified rather than a name the client reported about itself. A blocked
// tool is turned away before a token exists, and the token endpoint re-checks
// so a token minted before the block dies on its next use.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// ErrAIToolBlockCheckUnavailable reports that the organization has blocked at
// least one AI tool but the catalog needed to tell whether this caller is one
// of them could not be read. Callers turn it into a retryable failure.
var ErrAIToolBlockCheckUnavailable = errors.New("ai tool gateway block check unavailable")

// AIToolBlockedError reports that the presented client belongs to an AI tool
// the organization has blocked. Like admission.DenialError it owns only why
// the client was refused and what to tell the end user; the OAuth error code
// and HTTP status stay with the transport.
type AIToolBlockedError struct {
	// TargetID is the scan target the client matched.
	TargetID string

	// DisplayName is that target's human-readable name.
	DisplayName string
}

func (e *AIToolBlockedError) Error() string {
	return "ai tool blocked at the gateway: " + e.TargetID
}

// Description is the client-facing explanation.
//
// It names the tool and says plainly that an administrator has to act,
// because this failure is a dead end for the person hitting it: MCP clients
// commit to a client id at metadata-discovery time and do not fall back to
// dynamic registration when /authorize refuses them.
//
// It carries no request-access link, deliberately and by scope. Self-service
// access requests are a Shadow MCP feature; Shadow AI has no equivalent flow
// yet and is not getting one here. The only link available is the MCP RBAC
// request, which grants a role on an MCP server and cannot clear
// ai_scan_targets.status for the blocked tool, so offering it sent the user to
// a form whose successful submission changed nothing about why they were
// refused.
//
// So the text stops at naming the tool and saying an administrator must act.
// If Shadow AI gains a decision-request flow of its own, this is where its
// link belongs; until then there is nothing honest to point at. This is why
// the dashboard warns an admin before they record the block.
func (e *AIToolBlockedError) Description() string {
	return fmt.Sprintf(
		"%s is not permitted to connect to this organization's MCP servers; an organization administrator must approve it",
		e.DisplayName,
	)
}

// aiToolBlockReads are the two database reads behind checkAIToolGatewayBlock.
// They are function values rather than direct calls so a test can fail one
// read while the other succeeds, which is the only way to reach the fail-open
// and fail-closed branches against a real database. NewService installs
// defaultAIToolBlockReads; nothing at runtime replaces them.
type aiToolBlockReads struct {
	// blockedTargetIDs lists the scan-target ids the organization has
	// blocked: the cheap "is anything blocked at all?" read.
	blockedTargetIDs func(ctx context.Context, queries *agentrepo.Queries, organizationID string) ([]string, error)

	// catalog loads the organization's scan-target catalog, which is what
	// matches the caller to a target.
	catalog func(ctx context.Context, queries *agentrepo.Queries, organizationID string) (*aitargets.OrganizationList, error)
}

// defaultAIToolBlockReads returns the production reads.
func defaultAIToolBlockReads() aiToolBlockReads {
	return aiToolBlockReads{
		blockedTargetIDs: func(ctx context.Context, queries *agentrepo.Queries, organizationID string) ([]string, error) {
			ids, err := queries.ListBlockedAITargetIDs(ctx, organizationID)
			if err != nil {
				return nil, fmt.Errorf("list blocked ai targets: %w", err)
			}
			return ids, nil
		},
		catalog: aitargets.LoadOrganizationList,
	}
}

// checkAIToolGatewayBlock refuses clientID when the organization has blocked
// the AI tool it belongs to. Nil means not blocked, the usual answer.
//
// The first query asks only whether the organization blocks anything at all,
// so the common case never touches the scan-target catalog.
//
// The two failure modes are answered differently, because they know different
// things. If the first query fails there is no evidence the organization
// blocks anything, and refusing every MCP client in an organization that has
// never used the feature is a worse outage than the one it would prevent, so
// it allows. Once that query says a block IS in force, a failure to load the
// catalog means the answer is unknown rather than absent, and the request is
// refused as retryable instead of quietly bypassing the control.
func (s *Service) checkAIToolGatewayBlock(ctx context.Context, logger *slog.Logger, organizationID string, clientID string) error {
	if organizationID == "" || clientID == "" {
		return nil
	}

	queries := agentrepo.New(s.db)
	blocked, err := s.aiToolBlockReads.blockedTargetIDs(ctx, queries, organizationID)
	if err != nil {
		logger.WarnContext(ctx, "ai tool gateway block unavailable; allowing the connection", attr.SlogError(err))
		return nil
	}
	if len(blocked) == 0 {
		return nil
	}

	list, err := s.aiToolBlockReads.catalog(ctx, queries, organizationID)
	if err != nil {
		logger.ErrorContext(ctx, "ai scan targets unavailable while a block is in force", attr.SlogError(err))
		return fmt.Errorf("%w: %w", ErrAIToolBlockCheckUnavailable, err)
	}

	caller := aitargets.GatewayCaller{
		OAuthClientID:  clientID,
		CIMDVendorKey:  "",
		CIMDCatalogURL: "",
	}
	// A CIMD client_id is also a catalog entry, and the entry is the only
	// stable handle on a vendor that mints one document per server.
	if preset, known := admission.CatalogPreset(clientID); known {
		caller.CIMDVendorKey = preset.VendorKey
		caller.CIMDCatalogURL = preset.URL
	}

	target, matched := aitargets.MatchGatewayCaller(list.Snapshot.Targets(), caller)
	if !matched {
		return nil
	}
	blockedIDs := make(map[string]struct{}, len(blocked))
	for _, id := range blocked {
		blockedIDs[id] = struct{}{}
	}
	if _, isBlocked := blockedIDs[target.ID]; !isBlocked {
		return nil
	}

	logger.InfoContext(ctx, "ai tool blocked at the gateway",
		attr.SlogOAuthClientID(truncateClientIDForLog(clientID)),
	)
	return &AIToolBlockedError{
		TargetID:    target.ID,
		DisplayName: target.DisplayName,
	}
}
