package authz

import (
	"context"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// ChallengeKeyMCPServerID names, in a recorded challenge's selector, the MCP
// server a refused auth client was used for. Grants never match on it: the
// mcp:auth check names the client, and the server only says where it was used.
const ChallengeKeyMCPServerID = "mcp_server_id"

// LogMandatoryDenial records a refused mandatory check as a denied
// authorization challenge, so the caller's failed checks list it beside the
// checks Require records. An allowed decision records nothing.
//
// The recorded check keeps the client, its type and project, and adds the
// server it was used for. It leaves out the connection owner and the upstream
// OAuth client: anyone who can read the organization's challenges sees the
// row, and a personal connection's owner and upstream account are its owner's.
func (e *Engine) LogMandatoryDenial(ctx context.Context, authorization MandatoryAuthorization, decision MandatoryDecision, check Check, serverID string) {
	if decision.Allowed {
		return
	}

	recorded := check
	recorded.Dimensions = make(map[string]string, len(check.Dimensions)+1)
	for key, value := range check.Dimensions {
		if key == SelectorKeyConnectionOwner || key == SelectorKeyUpstreamClient {
			continue
		}
		recorded.Dimensions[key] = value
	}
	if serverID != "" {
		recorded.Dimensions[ChallengeKeyMCPServerID] = serverID
	}

	challengeLogger{
		Operation:            authzrepo.OperationRequire,
		Outcome:              authzrepo.OutcomeDeny,
		Reason:               mandatoryChallengeReason(authorization, decision, check),
		Checks:               []Check{recorded},
		Focus:                &recorded,
		Matches:              nil,
		EvaluatedGrantCount:  uint32(authorization.authorization.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
}

// mandatoryChallengeReason names a refusal in the terms challenges use. A
// personal connection used by anyone other than its owner is refused for that
// reason, whatever grants the caller holds, unless a grant explicitly blocks
// it.
func mandatoryChallengeReason(authorization MandatoryAuthorization, decision MandatoryDecision, check Check) authzrepo.Reason {
	switch decision.Reason {
	case MandatoryDenyReasonBlocked:
		return authzrepo.ReasonDenyGrant
	case MandatoryDenyReasonNoPolicy, MandatoryDenyReasonNotGranted:
		owner := check.Dimensions[SelectorKeyConnectionOwner]
		if check.isAuthClientCheck() && owner != "" && owner != authorization.RepresentedUserID() {
			return authzrepo.ReasonConnectionOwnerMismatch
		}
		if decision.Reason == MandatoryDenyReasonNoPolicy {
			return authzrepo.ReasonNoGrants
		}
		return authzrepo.ReasonScopeUnsatisfied
	default:
		return authzrepo.ReasonScopeUnsatisfied
	}
}
