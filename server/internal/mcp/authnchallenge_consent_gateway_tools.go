package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// consentMemberOutcome is the discovery result both consent views display.
type consentMemberOutcome struct {
	// Slug identifies the source independently of its display name.
	Slug string `json:"slug"`
	// Name is the source's display name.
	Name string `json:"name"`
	// Status distinguishes verified access, sign-in issues, and outages.
	Status gatewayServerStatus `json:"status"`
	// Label is shared by the server list and the tool picker.
	Label string `json:"label"`
}

// captureGatewayConsentInventory captures one complete attempt before rendering
// either the server list or tool picker.
//
// The challenge has one gateway capture, so publication is ordered: each
// capture reserves the slot before discovering and publishes only over its
// own reservation. A capture a later page load replaced while it was still
// discovering fails instead of overwriting the newer one.
func (s *Service) captureGatewayConsentInventory(r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, view *gatewayConsentView) (string, error) {
	ctx := r.Context()
	reservation := consentToolInventory{
		CompletedAt: time.Time{}, BackendHash: "", StateID: state.ID, Attempt: uuid.NewString(), Resource: endpointToolSelectionResource(endpoint), Tools: []consentInventoryTool{}, ExpectedCursor: "", Complete: false, McpSessionID: "", MemberOutcomes: nil, MemberBindings: nil,
	}
	if err := s.consentToolInventoryCache.Store(ctx, reservation); err != nil {
		return "", fmt.Errorf("%w: reserve gateway capture: %w", errConsentInventoryUnavailable, err)
	}
	published := false
	defer func() {
		if published {
			return
		}
		// A failed capture leaves no capture behind; a newer one is kept.
		if _, err := s.consentToolInventoryCache.CompareAndDelete(context.WithoutCancel(ctx), reservation); err != nil {
			s.logger.WarnContext(ctx, "release gateway capture reservation", attr.SlogError(err))
		}
	}()
	tools, members, outcomes, err := s.enumerateGatewayConsentInventory(r, endpoint, state, view)
	if err != nil {
		return "", err
	}
	draft := reservation
	draft.MemberOutcomes = outcomes
	draft.MemberBindings = members
	draft, err = mergeConsentInventoryPage(draft, "", tools, "")
	if err != nil {
		return "", fmt.Errorf("cannot approve this gateway because its tool inventory could not be captured: %w", err)
	}
	published, err = s.consentToolInventoryCache.CompareAndSwap(ctx, reservation, draft)
	if err != nil {
		return "", fmt.Errorf("%w: publish gateway capture: %w", errConsentInventoryUnavailable, err)
	}
	if !published {
		return "", errConsentCaptureSuperseded
	}
	view.applyOutcomes(outcomes)
	// Definitions stay server-side; the picker receives only selection evidence.
	pickerTools := make([]consentInventoryTool, len(tools))
	for i, tool := range tools {
		tool.Definition = nil
		tool.DefinitionHash = ""
		pickerTools[i] = tool
	}
	raw, err := json.Marshal(struct {
		Attempt string                 `json:"attempt"`
		Tools   []consentInventoryTool `json:"tools"`
	}{Attempt: draft.Attempt, Tools: pickerTools})
	if err != nil {
		return "", fmt.Errorf("encode gateway picker: %w", err)
	}
	return string(raw), nil
}

// enumerateGatewayConsentInventory resolves the gateway's tools as the
// consenting subject's session will see them: the subject's upstream
// credentials, RBAC, and member visibility all apply, with no tool selection.
// Only members with complete successful discovery become eligible for
// approval; the outcomes explain every excluded member.
func (s *Service) enumerateGatewayConsentInventory(r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, view *gatewayConsentView) ([]consentInventoryTool, map[string]connectionCatalogMember, []consentMemberOutcome, error) {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	subject := *challengeState.Subject

	private := !endpoint.IsPublic
	if private && subject.Kind == urn.SessionSubjectKindAnonymous {
		return nil, nil, nil, oops.E(oops.CodeUnauthorized, nil, "anonymous subject cannot enumerate a private MCP server").LogWarn(ctx, logger)
	}

	organizationGateway := endpoint.IsOrganizationGateway()
	sessionID := "consent:" + challengeState.ID
	ctx, err := s.contextForSessionSubject(ctx, endpoint, subject, sessionID, challengeState.ClientID)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "stamp consent subject context").LogError(ctx, logger)
	}
	if private {
		if _, ok := contextvalues.GetAuthContext(ctx); !ok {
			return nil, nil, nil, oops.E(oops.CodeUnauthorized, nil, "consent subject has no authenticated context").LogWarn(ctx, logger)
		}
	}
	ctx = s.consentDiscoveryContext(ctx, challengeState)

	if s.authz != nil {
		if ctx, err = s.authz.PrepareContext(ctx); err != nil {
			return nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load access grants for consent inventory").LogError(ctx, logger)
		}
	}
	// The organization gateway has no grant of its own: a subject may consent
	// when it reaches at least one member, as its token exchange requires.
	if s.authz != nil && private && !organizationGateway {
		connectResourceID := endpoint.connectResourceID()
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, connectResourceID.String(), endpoint.ProjectID.String())); err != nil {
			return nil, nil, nil, fmt.Errorf("authorize consent inventory access: %w", mcpaccess.ServerPermissionDenied(err, s.requestAccessURL(ctx, connectResourceID.String(), endpoint.Slug)))
		}
	}

	gate := &metaGateContext{
		projectID:    endpoint.ProjectID,
		metaServerID: endpoint.MetaMcpServerID.UUID,
		// Agent gateways reach no consent page.
		agentID:             uuid.Nil,
		organizationGateway: organizationGateway,
		organizationID:      endpoint.OrganizationID,
		tokens:              nil,
		projectTokens:       nil,
		memberChoices:       nil,
		userSessionIssuerID: endpoint.UserSessionIssuerID,
		connectRoot:         "",
		chainUpstream:       nil,
		catalog:             nil,
		catalogRef:          nil,
		authentication:      nil,
		unapprovedMembers:   nil,
		execute:             false,
		discoveryMode:       "",
		authenticated:       subject.Kind != urn.SessionSubjectKindAnonymous,
		sessionID:           sessionID,
		chatID:              "",
		userID:              "",
		externalUserID:      "",
		apiKeyID:            "",
		protocolVersion:     mcpversions.Resolution{Declared: "", InEffect: mcpversions.DefaultInEffect},
		connectURL:          "",
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		gate.userID = subject.ID
	case urn.SessionSubjectKindAPIKey:
		gate.apiKeyID = subject.ID
	case urn.SessionSubjectKindAgent, urn.SessionSubjectKindAnonymous, urn.SessionSubjectKindWorkload:
		// The agent or workload actor rides on ctx; the gate has no field for it.
	}

	members := view.members
	if organizationGateway && len(members) == 0 {
		return nil, nil, nil, fmt.Errorf("authorize consent inventory access: %w", mcpaccess.ServerPermissionDenied(oops.E(oops.CodeForbidden, authz.ErrDenied, "subject cannot access any gateway members"), ""))
	}
	picks := chooseConsentUpstreamClientPicks(endpoint, members, view.cards)
	memberAuth, err := s.gatewayConsentAuthChoices(ctx, r, endpoint, subject, members, view.cards)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeUnavailable, err, "read gateway credential choices").LogError(ctx, logger)
	}
	view.auth = memberAuth
	for i := range members {
		member := &members[i]
		if member.backend != metaMemberBackendProxied {
			continue
		}
		if memberAuth != nil {
			res, resolveErr := s.resolveMemberAuthChoice(ctx, endpoint, *member, memberAuth[member.serverID], subject)
			s.applyMemberAuthClient(ctx, member, res, resolveErr)
			// As at runtime, a resolved client was checked against the member,
			// which authorizes its registration without a sign-in link.
			member.typedAuthClient = member.upstreamUnavailable == nil && res.outcome == authClientResolved && member.chosenClientID.Valid
			continue
		}
		clientID, chosen := picks[member.serverID]
		res, resolveErr := s.resolveAuthClientForConsent(ctx, endpoint.OrganizationID, member.serverID, member.projectID, uuid.NullUUID{UUID: clientID, Valid: chosen}, subject)
		s.applyMemberAuthClient(ctx, member, res, resolveErr)
	}
	gate.memberChoices = &memberChoices{selector: nil, enforced: false, subject: subject, profile: false, metaServerID: endpoint.MetaMcpServerID, resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
		if member.typedAuthClient {
			return s.remoteChallengeMgr.ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
				ProjectID: member.projectID, OrganizationID: endpoint.OrganizationID, UserSessionIssuerID: endpoint.UserSessionIssuerID,
				Subject: subject, RegistrationID: member.chosenClientID.UUID, SkipUnusable: true,
			})
		}
		return s.remoteChallengeMgr.ResolveClientAccessTokens(ctx, member.projectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject, member.chosenClientID.UUID, true)
	}, mu: sync.Mutex{}, byServer: map[uuid.UUID]*memberTokens{}}
	var signInID uuid.UUID
	if challengeState.SignInID != nil {
		signInID = *challengeState.SignInID
	}
	gate.chainUpstream = s.gatewayMemberChainer(gate, mcpidentity.KindConsentDiscovery, signInID)
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.serverID)
	}
	backendHashes, err := s.catalogMemberConfigurations(ctx, endpoint.OrganizationID, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	unavailableMembers := []unavailableMember{}
	captured := map[string]*memberCatalog{}
	inventory, err := s.collectGatewayTools(ctx, logger, gate, members, &unavailableMembers, captured)
	if err != nil {
		return nil, nil, nil, err
	}
	configured := view
	configuredStatus := make(map[string]gatewayServerStatus, len(members))
	for _, server := range configured.Servers {
		configuredStatus[server.Slug] = server.Status
	}
	outcomes := make([]consentMemberOutcome, 0, len(members))
	failures := make(map[string]error, len(unavailableMembers))
	for _, failed := range unavailableMembers {
		failures[failed.member.slug] = failed.err
	}
	for _, member := range members {
		status := gatewayServerUnavailable
		if captured[member.slug] != nil {
			status = gatewayServerConnected
			if configuredStatus[member.slug] == gatewayServerNoSignIn {
				status = gatewayServerNoSignIn
			}
			if configuredStatus[member.slug] == gatewayServerChained || configuredStatus[member.slug] == gatewayServerChainPending {
				status = gatewayServerChained
			}
		} else if member.authClient != nil && member.authClient.reason == authClientReasonAmbiguous {
			status = gatewayServerNotSetUp
		} else if failure := failures[member.slug]; failure != nil {
			if _, credential := errors.AsType[memberCredentialError](failure); credential {
				switch configuredStatus[member.slug] {
				case gatewayServerConnected, gatewayServerReconnect:
					status = gatewayServerReconnect
				case gatewayServerNoSignIn, gatewayServerNotSetUp:
					status = gatewayServerNotSetUp
				case gatewayServerNotConnected:
					status = gatewayServerNotConnected
				case gatewayServerChained, gatewayServerChainPending:
					status = gatewayServerChainPending
				default:
					status = gatewayServerUnavailable
				}
			} else if denied, ok := errors.AsType[*oops.ShareableError](failure); ok && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound) {
				status = gatewayServerNoAccess
			}
		}
		outcomes = append(outcomes, consentMemberOutcome{Slug: member.slug, Name: conv.Default(member.name, member.slug), Status: status, Label: (gatewayServer{Slug: member.slug, Name: member.name, Status: status, ConnectClientID: "", cardIDs: nil, Auth: nil}).StatusLabel()})
	}
	tools := make([]consentInventoryTool, 0, len(inventory))
	toolMembers := map[string]connectionCatalogMember{}
	for _, member := range members {
		if captured[member.slug] == nil {
			continue
		}

		var authBinding *connectionCatalogAuth
		if member.backend == metaMemberBackendProxied {
			clientID := uuid.Nil
			if member.authClient != nil {
				clientID = member.authClient.clientID
			}
			authBinding = &connectionCatalogAuth{ClientID: clientID, RegistrationID: member.chosenClientID}
		}
		toolMembers[member.slug] = connectionCatalogMember{ServerID: member.serverID, ProjectID: member.projectID, BackendHash: backendHashes[member.serverID], Auth: authBinding}
	}
	for _, tool := range inventory {
		var hints []string
		if a := tool.entry.Annotations; a != nil {
			hints = trueAnnotationValues(&types.ToolAnnotations{Title: nil, ReadOnlyHint: a.ReadOnlyHint, DestructiveHint: a.DestructiveHint, IdempotentHint: a.IdempotentHint, OpenWorldHint: a.OpenWorldHint})
		}
		tools = append(tools, consentInventoryTool{
			Name:           tool.entry.Name,
			Definition:     tool.entry,
			DefinitionHash: consentEvidenceHash(tool.entry),
			Annotations:    hints,
			Source:         conv.Default(tool.member.name, tool.member.slug),
		})
	}
	return tools, toolMembers, outcomes, nil
}
