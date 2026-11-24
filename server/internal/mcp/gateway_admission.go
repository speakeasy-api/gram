package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// gatewayCallerKind is the principal class admitted to a gateway.
type gatewayCallerKind uint8

const (
	gatewayCallerPerson gatewayCallerKind = iota + 1
	gatewayCallerAgent
	gatewayCallerWorkload
)

func (k gatewayCallerKind) String() string {
	switch k {
	case gatewayCallerPerson:
		return "person"
	case gatewayCallerAgent:
		return "agent"
	case gatewayCallerWorkload:
		return "workload"
	default:
		return "unknown"
	}
}

// gatewayCaller is the request-scoped result of gateway caller admission.
// ceiling contains only the immutable policy of the credential itself; live
// principal and owner policies remain separate and are rechecked per request.
type gatewayCaller struct {
	kind         gatewayCallerKind
	principal    urn.Principal
	owner        string
	ceiling      []authz.Grant
	projectScope *uuid.UUID
}

func emptyGatewayCaller() gatewayCaller {
	return newGatewayCaller(0)
}

func personGatewayCaller() gatewayCaller {
	return newGatewayCaller(gatewayCallerPerson)
}

func newGatewayCaller(kind gatewayCallerKind) gatewayCaller {
	return gatewayCaller{
		kind:         kind,
		principal:    urn.Principal{Type: "", ID: ""},
		owner:        "",
		ceiling:      nil,
		projectScope: nil,
	}
}

type gatewayAdmission struct {
	caller         gatewayCaller
	agentID        uuid.UUID
	authentication *issuerGateAuthentication
	toolSelection  *toolfilter.SessionPolicy
}

// admitGatewayCaller is the common caller boundary for custom, project, and
// organization gateways. Credential authentication performs live principal,
// owner, and workload admission before this function captures the caller.
func (s *Service) admitGatewayCaller(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	authToken string,
	baseURL string,
	endpoint *ResolvedMcpEndpoint,
) (context.Context, *gatewayAdmission, error) {
	lockedDown, err := s.endpointCustomDomainLockdown(ctx, logger, endpoint)
	if err != nil {
		return ctx, nil, err
	}
	if lockedDown {
		return ctx, nil, oops.E(oops.CodeForbidden, nil, "this MCP server is only accessible via its custom domain")
	}

	target, targetErr := gatewayTargetFromEndpoint(endpoint)
	if targetErr != nil {
		return ctx, nil, targetErr
	}
	authedCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, r, authToken, baseURL, endpoint)
	if err != nil {
		if target.kind == gatewayKindOrganization {
			if callerKind, ok := s.rejectedMachineCallerKind(ctx, authToken, endpoint.OrganizationID); ok {
				decision, decisionErr := target.admits(newGatewayCaller(callerKind), endpoint.ProjectID, false)
				if decisionErr == nil {
					s.metrics.RecordGatewayAdmission(ctx, callerKind.String(), target.kind.String(), decision.admitted, string(decision.refusalReason))
				}
			}
		}
		return ctx, nil, err
	}
	// Anonymous sessions have no acting principal. Preserve their existing
	// behavior without inventing another caller kind or a false principal.
	if authentication.subject.Kind == urn.SessionSubjectKindAnonymous {
		return authedCtx, &gatewayAdmission{
			caller:         emptyGatewayCaller(),
			agentID:        uuid.Nil,
			authentication: authentication,
			toolSelection:  toolSelection,
		}, nil
	}
	caller, err := gatewayCallerFromContext(authedCtx, authentication.subject)
	if err != nil {
		return ctx, nil, err
	}
	machineAccess := false
	if caller.kind != gatewayCallerPerson {
		machineAccess, err = s.gatewayMachineAccess(ctx, target)
		if err != nil {
			return ctx, nil, fmt.Errorf("check gateway machine access rollout: %w", err)
		}
	}
	decision, err := target.admits(caller, endpoint.ProjectID, machineAccess)
	if err != nil {
		return ctx, nil, fmt.Errorf("evaluate gateway target: %w", err)
	}
	s.metrics.RecordGatewayAdmission(authedCtx, caller.kind.String(), target.kind.String(), decision.admitted, string(decision.refusalReason))
	if !decision.admitted {
		if reach, ok := workloadSessionReachFromContext(authedCtx); caller.kind == gatewayCallerWorkload && ok && reach == workloadSessionReachIssuer {
			return ctx, nil, writeInsufficientScopeChallenge(w, authentication.protectedResourceURL, "access token does not grant access to this gateway")
		}
		return ctx, nil, WriteAuthenticateChallenge(w, authentication.protectedResourceURL, "credential cannot enter this gateway")
	}
	if caller.kind == gatewayCallerWorkload {
		authedCtx = authz.WithoutCredentialCeiling(authedCtx)
	}

	agentID := uuid.Nil
	if caller.kind == gatewayCallerAgent || caller.kind == gatewayCallerWorkload {
		var ok bool
		agentID, ok = contextvalues.EffectiveAgentID(authedCtx)
		if !ok {
			return ctx, nil, fmt.Errorf("admitted %s caller has no effective agent", authentication.subject.Kind)
		}
	}

	return authedCtx, &gatewayAdmission{
		caller:         caller,
		agentID:        agentID,
		authentication: authentication,
		toolSelection:  toolSelection,
	}, nil
}

// rejectedMachineCallerKind attributes a valid machine credential that the
// people-only organization gateway rejects before shared admission runs.
func (s *Service) rejectedMachineCallerKind(ctx context.Context, token, organizationID string) (gatewayCallerKind, bool) {
	if principalcredential.IsToken(token) {
		authed, err := s.principalCredentials.Authenticate(ctx, token)
		if err != nil {
			return 0, false
		}
		credential, ok := principalcredential.FromContext(authed)
		if !ok || credential.Credential.OrganizationID != organizationID {
			return 0, false
		}
		switch credential.Credential.Principal.Type {
		case urn.PrincipalTypeAgent:
			return gatewayCallerAgent, true
		case urn.PrincipalTypeWorkload:
			return gatewayCallerWorkload, true
		default:
			return 0, false
		}
	}
	if s.isWorkloadSessionBearer(token) {
		return gatewayCallerWorkload, true
	}
	if !strings.HasPrefix(token, "gram_") {
		return 0, false
	}
	keyCtx, err := s.auth.Authorize(ctx, token, &security.APIKeyScheme{
		Name: constants.KeySecurityScheme, Scopes: nil, RequiredScopes: []string{"consumer"},
	})
	if err != nil {
		return 0, false
	}
	actor, actorOK := contextvalues.AuthenticatedActor(keyCtx)
	authCtx, authOK := contextvalues.GetAuthContext(keyCtx)
	if !actorOK || actor.Type != urn.PrincipalTypeAgent || !authOK || authCtx == nil || authCtx.ActiveOrganizationID != organizationID {
		return 0, false
	}
	return gatewayCallerAgent, true
}

func (s *Service) gatewayMachineAccess(ctx context.Context, target gatewayTarget) (bool, error) {
	if s.features == nil {
		return false, nil
	}
	var flag feature.Flag
	switch target.kind {
	case gatewayKindStored:
		flag = feature.FlagStoredGatewayMachineAccess
	case gatewayKindProject:
		flag = feature.FlagProjectGatewayMachineAccess
	default:
		return false, nil
	}
	enabled, err := s.features.IsFlagEnabledLocal(ctx, flag, target.organizationID, nil, nil)
	if err != nil {
		return false, fmt.Errorf("evaluate %s rollout: %w", flag, err)
	}
	return enabled, nil
}

func gatewayCallerFromContext(ctx context.Context, subject urn.SessionSubject) (gatewayCaller, error) {
	caller := emptyGatewayCaller()
	principal, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok {
		return gatewayCaller{}, fmt.Errorf("gateway caller has no authenticated principal")
	}
	caller.principal = principal

	switch subject.Kind {
	case urn.SessionSubjectKindUser, urn.SessionSubjectKindAPIKey:
		caller.kind = gatewayCallerPerson
		return caller, nil
	case urn.SessionSubjectKindAgent:
		caller.kind = gatewayCallerAgent
		authorizer, _, ok := contextvalues.PrincipalCredentialProvenance(ctx)
		if !ok || authorizer == "" {
			return gatewayCaller{}, fmt.Errorf("admitted agent caller has no credential authorizer")
		}
		caller.owner = authorizer
	case urn.SessionSubjectKindWorkload:
		caller.kind = gatewayCallerWorkload
		owner, ok := authz.AdmittedWorkloadOwner(ctx)
		if !ok {
			return gatewayCaller{}, fmt.Errorf("admitted workload caller has no live owner")
		}
		caller.owner = owner
	default:
		return gatewayCaller{}, fmt.Errorf("unsupported gateway caller subject kind %q", subject.Kind)
	}

	ceiling, ok := authz.CredentialCeilingFromContext(ctx)
	if !ok {
		return gatewayCaller{}, fmt.Errorf("admitted %s caller has no credential ceiling", subject.Kind)
	}
	caller.ceiling = ceiling
	projectScope, err := gatewayCallerProjectScope(ceiling)
	if err != nil {
		return gatewayCaller{}, err
	}
	caller.projectScope = projectScope
	return caller, nil
}

// gatewayCallerProjectScope returns the one project every grant in a
// credential ceiling is limited to. Any unscoped grant or multiple project
// values means the credential is not limited to one project.
func gatewayCallerProjectScope(ceiling []authz.Grant) (*uuid.UUID, error) {
	if len(ceiling) == 0 {
		return nil, nil
	}

	var projectID uuid.UUID
	for _, grant := range ceiling {
		value, ok := grant.Selector[authz.SelectorKeyProjectID]
		if !ok || value == "" || value == authz.WildcardResource {
			return nil, nil
		}
		parsed, err := uuid.Parse(value)
		if err != nil || parsed == uuid.Nil || parsed.String() != value {
			return nil, fmt.Errorf("credential ceiling has invalid project scope %q", value)
		}
		if projectID != uuid.Nil && projectID != parsed {
			return nil, nil
		}
		projectID = parsed
	}
	return &projectID, nil
}
