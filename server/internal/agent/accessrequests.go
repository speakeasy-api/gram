package agent

import (
	"context"
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AccessRequestsRate bounds listPendingAccessRequests per member. The device
// agent polls it every few seconds at most; the limit stops a leaked per-user
// key from hammering the organization's requests.
var AccessRequestsRate = ratelimit.PerMinute(60)

// WithAccessRequests sets the access service behind
// listPendingAccessRequests. Nil answers it not found.
func (s *Service) WithAccessRequests(service *access.Service) *Service {
	s.accessRequests = service
	return s
}

// WithAccessRequestsLimiter sets the per-member limit on
// listPendingAccessRequests. Nil leaves it unlimited.
func (s *Service) WithAccessRequestsLimiter(limiter *ratelimit.Limiter) *Service {
	s.accessRequestsLimiter = limiter
	return s
}

// accessRequestMember resolves the member an access request endpoint acts
// for. Only a per-user key is accepted: whether the member may see the
// organization's requests rests on who they are, so the org install key
// (shared by every device, with an email anyone can vouch), device-agent
// tokens, workload sessions and agent identities are refused. While access
// requests are off for the organization the endpoints answer not found.
func (s *Service) accessRequestMember(ctx context.Context) (*contextvalues.AuthContext, agentMember, error) {
	authCtx, err := s.accessRequestCaller(ctx)
	if err != nil {
		return nil, agentMember{}, err
	}

	member, err := s.memberFor(ctx, nil, "access requests")
	if err != nil {
		return nil, agentMember{}, err
	}
	// The key's owner must still be a member under the same identity the key
	// was minted for.
	if member.userID == "" || member.userID != authCtx.UserID {
		return nil, agentMember{}, oops.E(oops.CodeForbidden, nil, "the enrolled user is not a member of this organization")
	}
	if err := allowMemberCall(ctx, s.logger, s.accessRequestsLimiter, member, "pending access requests"); err != nil {
		return nil, agentMember{}, err
	}
	return authCtx, member, nil
}

// deviceCanRequestAccess reports whether the caller could file an access
// request from the device agent: access requests are on and the caller holds
// a per-user key.
func (s *Service) deviceCanRequestAccess(ctx context.Context) bool {
	_, err := s.accessRequestCaller(ctx)
	return err == nil
}

// accessRequestCaller checks the caller of an access request endpoint without
// resolving the member: the flag, then the credential.
func (s *Service) accessRequestCaller(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	// The flag comes first, so every caller reads the endpoint as absent
	// while access requests are off.
	if s.accessRequests == nil || !s.accessRequests.AccessRequestsEnabled(ctx, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String()) {
		return nil, oops.E(oops.CodeForbidden, nil, "access requests require a per-user agent key; the org install key cannot act for a member")
	}
	if _, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		return nil, oops.E(oops.CodeForbidden, nil, "access requests belong to a person; an agent identity has none")
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return nil, oops.E(oops.CodeForbidden, nil, "access requests belong to a person; an agent identity has none")
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type != urn.PrincipalTypeUser {
		return nil, oops.E(oops.CodeForbidden, nil, "access requests belong to a person; %s has none", actor.Type)
	}
	if authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

// ListPendingAccessRequests lists the organization's pending requests for an
// administrator, each with the dashboard link that decides it. Anyone else
// gets an empty list.
func (s *Service) ListPendingAccessRequests(ctx context.Context, _ *gen.ListPendingAccessRequestsPayload) (*gen.ListPendingAccessRequestsResult, error) {
	_, member, err := s.accessRequestMember(ctx)
	if err != nil {
		return nil, err
	}
	pending, err := s.accessRequests.ListPendingForAdmin(ctx, member.organizationID, member.userID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing pending access requests").LogError(ctx, s.logger)
	}
	result := &gen.ListPendingAccessRequestsResult{Requests: make([]*gen.AgentPendingAccessRequest, 0, len(pending))}
	for _, item := range pending {
		request := item.Request
		result.Requests = append(result.Requests, &gen.AgentPendingAccessRequest{
			ID:                       request.ID,
			RequesterDisplayName:     request.RequesterName,
			Scope:                    request.Scope,
			PermissionLabel:          item.PermissionLabel,
			ResourceName:             request.ResourceName,
			Message:                  request.Message,
			RequestedDurationSeconds: request.RequestedDurationSeconds,
			RequestedUntil:           request.RequestedUntil,
			CreatedAt:                request.CreatedAt,
			ExpiresAt:                request.ExpiresAt,
			DecideURL:                item.DecideURL,
		})
	}
	return result, nil
}
