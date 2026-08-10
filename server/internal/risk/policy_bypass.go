package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	riskPolicyBypassRequestStatusRequested = "requested"
	riskPolicyBypassRequestStatusApproved  = "approved"
	riskPolicyBypassRequestStatusDenied    = "denied"
	riskPolicyBypassRequestStatusRevoked   = "revoked"

	// PolicyBypassTargetKindShadowMCPServer identifies a Shadow MCP server target.
	PolicyBypassTargetKindShadowMCPServer = "shadow_mcp_server"
	// PolicyBypassWholePolicyTargetKey identifies a whole-policy target.
	PolicyBypassWholePolicyTargetKey = "policy"
)

func (s *Service) ListRiskPolicyBypassRequests(ctx context.Context, payload *gen.ListRiskPolicyBypassRequestsPayload) (*gen.ListRiskPolicyBypassRequestsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := validateRiskPolicyBypassRequestStatus(payload.Status); err != nil {
		return nil, err
	}

	policyID, err := conv.PtrToNullUUID(payload.PolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy id")
	}
	rows, err := s.repo.ListRiskPolicyBypassRequests(ctx, repo.ListRiskPolicyBypassRequestsParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
		Status:       conv.PtrToPGText(payload.Status),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policy bypass requests").LogError(ctx, s.logger)
	}

	requests := make([]*gen.RiskPolicyBypassRequest, 0, len(rows))
	for _, row := range rows {
		req, err := riskPolicyBypassRequestView(row)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	return &gen.ListRiskPolicyBypassRequestsResult{Requests: requests}, nil
}

func (s *Service) CreateRiskPolicyBypassRequest(ctx context.Context, payload *gen.CreateRiskPolicyBypassRequestPayload) (*gen.PolicyBypassRedemption, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if strings.TrimSpace(s.jwtSecret) == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "risk policy bypass request tokens are not configured").LogError(ctx, s.logger)
	}

	claims, err := parsePolicyBypassRequestToken(ctx, s.cache, s.jwtSecret, payload.RequestToken)
	if err != nil {
		// A failure to reach the cache backing the link is an infrastructure
		// problem, not a bad token: surface it as a server error and log it
		// rather than telling the user their link is invalid.
		if errors.Is(err, errPolicyBypassRequestStoreUnavailable) {
			return nil, oops.E(oops.CodeUnexpected, err, "load risk policy bypass request").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request token")
	}
	// The token is only a bearer reference; these checks are what actually
	// bind a redemption to the right caller, so a leaked link can't be cashed
	// in by someone else. The org must match the active session's org.
	if claims.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}
	// Bind to the original requester when we know who they are. Note this is
	// conditional: when the block hook couldn't resolve a user, RequesterUserID
	// is empty and any authenticated user in the org can redeem. That residual
	// exposure is exactly why the token still travels in the URL fragment (see
	// GeneratePolicyBypassRequestURL) rather than anywhere it could be logged.
	if claims.RequesterUserID != "" && claims.RequesterUserID != authCtx.UserID {
		return nil, oops.C(oops.CodeForbidden)
	}

	requestID, err := uuid.Parse(claims.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request token id")
	}
	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request project id")
	}
	policyID, err := uuid.Parse(claims.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request policy id")
	}

	target, err := riskPolicyBypassTargetFromClaims(claims)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request target")
	}

	// A shadow-MCP block on a URL-identified server redeems into the MCP
	// approval workflow when it is available: the ask attaches as a requester
	// on the server's single review — deduplicated by canonical URL, evidence
	// gathered — instead of minting a per-user bypass row. The legacy bypass
	// request remains only for what the workflow cannot key: identity-only
	// servers with no observed URL, and organizations without the approval
	// feature (a forbidden intake error is that signal, not a failure).
	if s.approvalIntake != nil && target.Kind == PolicyBypassTargetKindShadowMCPServer {
		if serverURL := target.Dimensions[authz.SelectorKeyServerURL]; serverURL != "" {
			// The legacy path below re-derives these bindings inside its
			// transaction, but a successful admission returns before reaching
			// it. Re-check them here: the token's project must belong to the
			// caller's organization before its id is trusted as a write
			// target, and a token whose project or policy has since been
			// deleted must fail like any other stale link instead of opening
			// an approval request for it.
			if _, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
				ID:             projectID,
				OrganizationID: claims.OrganizationID,
			}); err != nil {
				return nil, oops.E(oops.CodeNotFound, err, "project not found").LogError(ctx, s.logger)
			}
			if _, err := repo.New(s.db).GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
				ID:        policyID,
				ProjectID: projectID,
			}); err != nil {
				return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
			}

			admittedID, admittedStatus, err := s.approvalIntake.AdmitBlockedServer(
				ctx,
				claims.OrganizationID,
				projectID,
				serverURL,
				authCtx.UserID,
				conv.PtrValOrEmpty(authCtx.Email, ""),
				strings.TrimSpace(conv.PtrValOr(claims.BlockReason, "")),
			)
			switch {
			case err == nil:
				return &gen.PolicyBypassRedemption{
					Kind:   "approval_request",
					ID:     admittedID,
					Status: admittedStatus,
				}, nil
			case oopsCodeIs(err, oops.CodeForbidden):
				// The approval workflow is not enabled for this organization;
				// fall through to the legacy bypass request.
			default:
				return nil, oops.E(oops.CodeUnexpected, err, "admit blocked server into approval workflow").LogError(ctx, s.logger)
			}
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass request").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: claims.OrganizationID,
	}); err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "project not found").LogError(ctx, s.logger)
	}
	q := repo.New(dbtx)
	policy, err := q.GetRiskPolicyForUpdate(ctx, repo.GetRiskPolicyForUpdateParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	row, err := q.UpsertRiskPolicyBypassRequest(ctx, repo.UpsertRiskPolicyBypassRequestParams{
		ID:               requestID,
		OrganizationID:   claims.OrganizationID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		TargetKind:       conv.ToPGTextEmpty(target.Kind),
		TargetLabel:      conv.ToPGTextEmpty(target.Label),
		TargetKey:        conv.ToPGText(target.Key),
		TargetDimensions: target.dimensions,
		RequesterUserID:  authCtx.UserID,
		RequesterEmail:   conv.ToPGTextEmpty(conv.PtrValOrEmpty(authCtx.Email, "")),
		Note:             conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(claims.BlockReason, ""))),
		Status:           riskPolicyBypassRequestStatusRequested,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy bypass request").LogError(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestCreate, authCtx, policy.ID, policy.ProjectID, policy.Name, nil, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request create").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass request").LogError(ctx, s.logger)
	}

	return &gen.PolicyBypassRedemption{
		Kind:   "bypass_request",
		ID:     row.ID.String(),
		Status: row.Status,
	}, nil
}

// oopsCodeIs reports whether err carries the given shareable error code.
func oopsCodeIs(err error, code oops.Code) bool {
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && shareable.Code == code
}

func (s *Service) ApproveRiskPolicyBypassRequest(ctx context.Context, payload *gen.ApproveRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass approval").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").LogError(ctx, s.logger)
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}

	var principalURNs []string
	if effectiveShadowMCPDisposition(policy.ShadowMcpDisposition, policy.Sources, policy.Action) == ShadowMCPDispositionAllowAll {
		// Approval on an allow_all policy unblocks the server for the whole
		// project by revoking its risk_policy:block grant. No principal-scoped
		// bypass grants are minted — those are a block_all concept.
		serverURL, err := riskPolicyBypassTargetServerURL(current)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read risk policy bypass target").LogError(ctx, s.logger)
		}
		if serverURL == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "risk policy bypass request has no server url target to unblock")
		}
		if err := policybypass.RevokePolicyURL(ctx, dbtx, authCtx.ActiveOrganizationID, authz.ScopeRiskPolicyBlock, policy.ID.String(), serverURL); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "unblock shadow mcp server").LogError(ctx, s.logger)
		}
		principalURNs = []string{}
	} else {
		principals, urns, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, payload.GrantedPrincipalUrns)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass grant principals")
		}
		principalURNs = urns
		if err := validateRiskPolicyBypassGrantPrincipals(ctx, dbtx, authCtx.ActiveOrganizationID, principals); err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass grant principals")
		}
		selector, err := riskPolicyBypassGrantSelector(current)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").LogError(ctx, s.logger)
		}
		if current.Status == riskPolicyBypassRequestStatusApproved {
			currentPrincipals, _, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, current.GrantedPrincipalUrns)
			if err != nil {
				return nil, oops.E(oops.CodeInvalid, err, "invalid current risk policy bypass grant principals")
			}
			principalsToRevoke := riskPolicyBypassGrantPrincipalDifference(currentPrincipals, principals)
			if len(principalsToRevoke) > 0 {
				if err := authz.RevokeResourceFromPrincipals(ctx, dbtx, authz.ResourceGrant{
					Resource: authz.Resource{
						OrganizationID: authCtx.ActiveOrganizationID,
						Scope:          authz.ScopeRiskPolicyBypass,
						ResourceID:     current.RiskPolicyID.String(),
					},
					Principals: principalsToRevoke,
					Selector:   selector,
				}); err != nil {
					return nil, oops.E(oops.CodeUnexpected, err, "revoke replaced risk policy bypass grants").LogError(ctx, s.logger)
				}
			}
		}
		if err := authz.GrantResourceToPrincipals(ctx, dbtx, authz.ResourceGrant{
			Resource: authz.Resource{
				OrganizationID: authCtx.ActiveOrganizationID,
				Scope:          authz.ScopeRiskPolicyBypass,
				ResourceID:     current.RiskPolicyID.String(),
			},
			Principals: principals,
			Selector:   selector,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "grant risk policy bypass").LogError(ctx, s.logger)
		}
	}

	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusApproved,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: principalURNs,
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "approve risk policy bypass request").LogError(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestApprove, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request approval").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass approval").LogError(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) DenyRiskPolicyBypassRequest(ctx context.Context, payload *gen.DenyRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass denial").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").LogError(ctx, s.logger)
	}
	if current.Status != riskPolicyBypassRequestStatusRequested {
		return nil, oops.E(oops.CodeInvalid, nil, "risk policy bypass request must be pending")
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}
	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusDenied,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").LogError(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestDeny, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request denial").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass denial").LogError(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) RevokeRiskPolicyBypassRequest(ctx context.Context, payload *gen.RevokeRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass revocation").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").LogError(ctx, s.logger)
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").LogError(ctx, s.logger)
	}
	if effectiveShadowMCPDisposition(policy.ShadowMcpDisposition, policy.Sources, policy.Action) == ShadowMCPDispositionAllowAll {
		// Revoking an allow_all approval re-blocks the server for the whole
		// project by restoring its risk_policy:block grant; there is no
		// bypass grant to revoke.
		serverURL, err := riskPolicyBypassTargetServerURL(current)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read risk policy bypass target").LogError(ctx, s.logger)
		}
		if serverURL == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "risk policy bypass request has no server url target to re-block")
		}
		if err := policybypass.ReplacePolicyURLAudience(ctx, dbtx, authCtx.ActiveOrganizationID, authz.ScopeRiskPolicyBlock, policy.ID.String(), serverURL, []urn.Principal{authz.AllUsersPrincipal()}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "re-block shadow mcp server").LogError(ctx, s.logger)
		}
	} else {
		principals, _, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, current.GrantedPrincipalUrns)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid granted risk policy bypass principals")
		}
		selector, err := riskPolicyBypassGrantSelector(current)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").LogError(ctx, s.logger)
		}
		if err := authz.RevokeResourceFromPrincipals(ctx, dbtx, authz.ResourceGrant{
			Resource: authz.Resource{
				OrganizationID: authCtx.ActiveOrganizationID,
				Scope:          authz.ScopeRiskPolicyBypass,
				ResourceID:     current.RiskPolicyID.String(),
			},
			Principals: principals,
			Selector:   selector,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "revoke risk policy bypass").LogError(ctx, s.logger)
		}
	}

	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusRevoked,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke risk policy bypass request").LogError(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestRevoke, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request revocation").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass revocation").LogError(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) logRiskPolicyBypassRequestAudit(
	ctx context.Context,
	dbtx auditrepo.DBTX,
	action audit.Action,
	authCtx *contextvalues.AuthContext,
	policyID uuid.UUID,
	projectID uuid.UUID,
	policyName string,
	beforeRow *repo.RiskPolicyBypassRequest,
	afterRow *repo.RiskPolicyBypassRequest,
) error {
	var before *audit.RiskPolicyBypassRequestSnapshot
	var err error
	if beforeRow != nil {
		before, err = riskPolicyBypassRequestSnapshot(*beforeRow)
		if err != nil {
			return err
		}
	}

	var after *audit.RiskPolicyBypassRequestSnapshot
	if afterRow != nil {
		after, err = riskPolicyBypassRequestSnapshot(*afterRow)
		if err != nil {
			return err
		}
	}

	metadata, err := riskPolicyBypassRequestAuditMetadata(beforeRow, afterRow)
	if err != nil {
		return err
	}

	event := audit.LogRiskPolicyBypassRequestEvent{
		OrganizationID:                    authCtx.ActiveOrganizationID,
		ProjectID:                         projectID,
		Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                  authCtx.Email,
		ActorSlug:                         nil,
		RiskPolicyID:                      policyID,
		RiskPolicyName:                    policyName,
		PolicyBypassRequestSnapshotBefore: before,
		PolicyBypassRequestSnapshotAfter:  after,
		Metadata:                          metadata,
	}

	switch action {
	case audit.ActionRiskPolicyBypassRequestCreate:
		if err := s.audit.LogRiskPolicyBypassRequestCreate(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request create: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestApprove:
		if err := s.audit.LogRiskPolicyBypassRequestApprove(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request approve: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestDeny:
		if err := s.audit.LogRiskPolicyBypassRequestDeny(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request deny: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestRevoke:
		if err := s.audit.LogRiskPolicyBypassRequestRevoke(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request revoke: %w", err)
		}
	default:
		return fmt.Errorf("unsupported risk policy bypass request audit action %q", action)
	}

	return nil
}

func riskPolicyBypassRequestSnapshot(row repo.RiskPolicyBypassRequest) (*audit.RiskPolicyBypassRequestSnapshot, error) {
	view, err := riskPolicyBypassRequestView(row)
	if err != nil {
		return nil, err
	}

	return &audit.RiskPolicyBypassRequestSnapshot{
		ID:                   view.ID,
		PolicyID:             view.PolicyID,
		TargetKind:           view.TargetKind,
		TargetLabel:          view.TargetLabel,
		TargetKey:            view.TargetKey,
		TargetDimensions:     maps.Clone(view.TargetDimensions),
		RequesterUserID:      view.RequesterUserID,
		RequesterEmail:       view.RequesterEmail,
		Note:                 view.Note,
		Status:               view.Status,
		DecidedBy:            view.DecidedBy,
		GrantedPrincipalURNs: slices.Clone(view.GrantedPrincipalUrns),
		DecidedAt:            view.DecidedAt,
		CreatedAt:            view.CreatedAt,
		UpdatedAt:            view.UpdatedAt,
	}, nil
}

func riskPolicyBypassRequestAuditMetadata(beforeRow *repo.RiskPolicyBypassRequest, afterRow *repo.RiskPolicyBypassRequest) (*audit.RiskPolicyBypassRequestMetadata, error) {
	source := afterRow
	if source == nil {
		source = beforeRow
	}
	if source == nil {
		return nil, nil
	}

	dimensions, err := riskPolicyBypassDimensions(source.TargetDimensions)
	if err != nil {
		return nil, err
	}

	previousStatus := ""
	if beforeRow != nil {
		previousStatus = beforeRow.Status
	}

	return &audit.RiskPolicyBypassRequestMetadata{
		RequestID:            source.ID.String(),
		TargetKind:           conv.FromPGTextOrEmpty[string](source.TargetKind),
		TargetKey:            conv.FromPGTextOrEmpty[string](source.TargetKey),
		TargetDimensions:     dimensions,
		RequesterUserID:      source.RequesterUserID,
		GrantedPrincipalURNs: slices.Clone(source.GrantedPrincipalUrns),
		PreviousStatus:       previousStatus,
		CurrentStatus:        source.Status,
	}, nil
}

func validateRiskPolicyBypassRequestStatus(status *string) error {
	if status == nil || *status == "" {
		return nil
	}
	switch *status {
	case riskPolicyBypassRequestStatusRequested, riskPolicyBypassRequestStatusApproved, riskPolicyBypassRequestStatusDenied, riskPolicyBypassRequestStatusRevoked:
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "invalid bypass request status")
	}
}

type riskPolicyBypassRequestTarget struct {
	PolicyBypassTarget
	dimensions []byte
}

func riskPolicyBypassTargetFromClaims(claims *policyBypassRequestClaims) (riskPolicyBypassRequestTarget, error) {
	evidence := shadowmcp.AccessEvidence{
		FullURL:        conv.PtrValOr(claims.ObservedFullURL, ""),
		URLHost:        conv.PtrValOr(claims.ObservedURLHost, ""),
		ServerIdentity: conv.PtrValOr(claims.ObservedServerIdentity, ""),
	}
	target := ShadowMCPPolicyBypassTarget(evidence, conv.PtrValOr(claims.ToolName, ""))
	if target == nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("policy bypass request target evidence is required")
	}
	if observedName := strings.TrimSpace(conv.PtrValOr(claims.ObservedName, "")); observedName != "" && target.Kind == PolicyBypassTargetKindShadowMCPServer {
		target.Label = observedName
	}

	dimensions, err := json.Marshal(target.Dimensions)
	if err != nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("marshal dimensions: %w", err)
	}

	return riskPolicyBypassRequestTarget{
		PolicyBypassTarget: *target,
		dimensions:         dimensions,
	}, nil
}

// riskPolicyBypassTargetServerURL returns the canonical server URL the
// request targets, or "" when the target carries no URL dimension (e.g. an
// identity-only stdio target).
func riskPolicyBypassTargetServerURL(row repo.RiskPolicyBypassRequest) (string, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return "", err
	}
	return dimensions[authz.SelectorKeyServerURL], nil
}

func riskPolicyBypassGrantSelector(row repo.RiskPolicyBypassRequest) (authz.Selector, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, err
	}

	targetKind := conv.FromPGTextOrEmpty[string](row.TargetKind)
	switch targetKind {
	case "":
	case PolicyBypassTargetKindShadowMCPServer:
		if dimensions[authz.SelectorKeyServerURL] == "" && dimensions[authz.SelectorKeyServerIdentity] == "" {
			return nil, fmt.Errorf("shadow mcp server bypass target missing server_url or server_identity dimension")
		}
	default:
		return nil, fmt.Errorf("unsupported risk policy bypass target kind %q", targetKind)
	}

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, row.RiskPolicyID.String())
	maps.Copy(selector, dimensions)

	return selector, nil
}

func riskPolicyBypassGrantPrincipals(requesterUserID string, principalURNs []string) ([]urn.Principal, []string, error) {
	if len(principalURNs) == 0 {
		principalURNs = []string{urn.NewPrincipal(urn.PrincipalTypeUser, requesterUserID).String()}
	}

	principals := make([]urn.Principal, 0, len(principalURNs))
	grantedPrincipalURNs := make([]string, 0, len(principalURNs))
	seen := make(map[string]struct{}, len(principalURNs))
	hasAllUsers := false

	for _, rawPrincipalURN := range principalURNs {
		principalURN := strings.TrimSpace(rawPrincipalURN)
		if principalURN == "" {
			continue
		}

		principal, err := urn.ParsePrincipal(principalURN)
		if err != nil {
			return nil, nil, fmt.Errorf("parse principal urn %q: %w", principalURN, err)
		}
		switch principal.Type {
		case urn.PrincipalTypeUser, urn.PrincipalTypeRole:
		default:
			return nil, nil, fmt.Errorf("unsupported principal type %q", principal.Type)
		}

		key := principal.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		principals = append(principals, principal)
		grantedPrincipalURNs = append(grantedPrincipalURNs, key)
		hasAllUsers = hasAllUsers || key == authz.AllUsersPrincipal().String()
	}

	if len(principals) == 0 {
		return nil, nil, fmt.Errorf("at least one principal is required")
	}
	if hasAllUsers && len(principals) > 1 {
		return nil, nil, fmt.Errorf("user:all cannot be combined with narrower principals")
	}

	return principals, grantedPrincipalURNs, nil
}

func validateRiskPolicyBypassGrantPrincipals(ctx context.Context, db accessrepo.DBTX, organizationID string, principals []urn.Principal) error {
	for _, principal := range principals {
		if err := authz.ValidatePrincipal(ctx, db, organizationID, principal); err != nil {
			return fmt.Errorf("validate bypass grant principal %q: %w", principal.String(), err)
		}
	}

	return nil
}

func riskPolicyBypassGrantPrincipalDifference(currentPrincipals []urn.Principal, nextPrincipals []urn.Principal) []urn.Principal {
	next := make(map[string]struct{}, len(nextPrincipals))
	for _, principal := range nextPrincipals {
		next[principal.String()] = struct{}{}
	}

	diff := make([]urn.Principal, 0, len(currentPrincipals))
	for _, principal := range currentPrincipals {
		if _, ok := next[principal.String()]; ok {
			continue
		}
		diff = append(diff, principal)
	}

	return diff
}

func riskPolicyBypassRequestView(row repo.RiskPolicyBypassRequest) (*gen.RiskPolicyBypassRequest, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode risk policy bypass target dimensions")
	}

	decidedAt := conv.FromPGTimestamptz(row.DecidedAt)
	var decidedAtPtr *string
	if decidedAt != "" {
		decidedAtPtr = &decidedAt
	}

	return &gen.RiskPolicyBypassRequest{
		ID:                   row.ID.String(),
		PolicyID:             row.RiskPolicyID.String(),
		TargetKind:           conv.FromPGText[string](row.TargetKind),
		TargetLabel:          conv.FromPGText[string](row.TargetLabel),
		TargetKey:            conv.FromPGText[string](row.TargetKey),
		TargetDimensions:     dimensions,
		RequesterUserID:      row.RequesterUserID,
		RequesterEmail:       conv.FromPGText[string](row.RequesterEmail),
		Note:                 conv.FromPGText[string](row.Note),
		Status:               row.Status,
		DecidedBy:            conv.FromPGText[string](row.DecidedBy),
		GrantedPrincipalUrns: slices.Clone(row.GrantedPrincipalUrns),
		DecidedAt:            decidedAtPtr,
		CreatedAt:            conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(row.UpdatedAt),
	}, nil
}

func riskPolicyBypassDimensions(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	var dimensions map[string]string
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, fmt.Errorf("unmarshal dimensions: %w", err)
	}

	if dimensions == nil {
		return map[string]string{}, nil
	}

	return dimensions, nil
}
