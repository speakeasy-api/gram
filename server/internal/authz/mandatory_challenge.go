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
// checks Require records, and reports whether it recorded one. An allowed
// decision records nothing, and neither does a refusal of the caller's
// credential rather than of this use, which no grant could change.
//
// The recorded check keeps the client, its type and project, and adds the
// server it was used for. It leaves out the connection owner and the upstream
// OAuth client: anyone who can read the organization's challenges sees the
// row, and a personal connection's owner and upstream account are its owner's.
func (e *Engine) LogMandatoryDenial(ctx context.Context, authorization MandatoryAuthorization, decision MandatoryDecision, check Check, serverID string) bool {
	if decision.Allowed {
		return false
	}
	reason, recordable := mandatoryChallengeReason(authorization, decision, check)
	if !recordable {
		return false
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

	row := &challengeRow{
		Operation:            authzrepo.OperationRequire,
		Outcome:              authzrepo.OutcomeDeny,
		Reason:               reason,
		Checks:               []Check{recorded},
		Focus:                &recorded,
		Matches:              nil,
		EvaluatedGrantCount:  uint32(authorization.authorization.grantCount()), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}
	return e.emitAndWrite(ctx, row)
}

// mandatoryChallengeReason names a refusal in the terms challenges use, and
// reports whether it is recorded at all. Only refusals the caller's grants
// decided are: a personal connection used by anyone other than its owner is
// refused for that reason, whatever grants the caller holds, unless a grant
// explicitly blocks it. A refusal of the credential itself (inactive, outside
// its key's limits, from another organization) is the credential's problem,
// and recording it as a missing permission would point at the wrong fix.
func mandatoryChallengeReason(authorization MandatoryAuthorization, decision MandatoryDecision, check Check) (authzrepo.Reason, bool) {
	switch decision.Reason {
	case MandatoryDenyReasonBlocked:
		return authzrepo.ReasonDenyGrant, true
	case MandatoryDenyReasonNoPolicy, MandatoryDenyReasonNotGranted:
		owner := check.Dimensions[SelectorKeyConnectionOwner]
		if check.isAuthClientCheck() && owner != "" && owner != authorization.authClientRepresentedUserID() {
			return authzrepo.ReasonConnectionOwnerMismatch, true
		}
		if decision.Reason == MandatoryDenyReasonNoPolicy {
			return authzrepo.ReasonNoGrants, true
		}
		return authzrepo.ReasonScopeUnsatisfied, true
	default:
		return "", false
	}
}
