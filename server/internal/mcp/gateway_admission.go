package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
	return gatewayCaller{
		kind:         0,
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

	authedCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, r, authToken, baseURL, endpoint)
	if err != nil {
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
	target, err := gatewayTargetFromEndpoint(endpoint)
	if err != nil {
		return ctx, nil, err
	}
	storedMachineAccess := caller.kind != gatewayCallerPerson && s.storedGatewayMachineAccessEnabled(ctx, logger, target)
	admitted, err := target.admits(caller, endpoint.ProjectID, storedMachineAccess)
	if err != nil {
		return ctx, nil, fmt.Errorf("evaluate gateway target: %w", err)
	}
	if !admitted {
		return ctx, nil, WriteAuthenticateChallenge(w, authentication.protectedResourceURL, "credential cannot enter this gateway")
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

func (s *Service) storedGatewayMachineAccessEnabled(ctx context.Context, logger *slog.Logger, target gatewayTarget) bool {
	if target.kind != gatewayKindStored || s.features == nil {
		return false
	}
	enabled, err := s.features.IsFlagEnabledLocal(ctx, feature.FlagStoredGatewayMachineAccess, target.organizationID, nil, nil)
	if err != nil {
		logger.WarnContext(ctx, "check stored gateway machine access rollout", attr.SlogError(err))
		return false
	}
	return enabled
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
