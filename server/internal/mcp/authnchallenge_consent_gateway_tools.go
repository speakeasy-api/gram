package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// consentToolSourceMetaKey labels each gateway tool with the member server
// it comes from, so the picker can group a gateway's tools by source.
const consentToolSourceMetaKey = "gram.dev/source"

// consentUnavailableSourcesMetaKey names the gateway members whose tools
// could not be listed, usually because their upstream is not connected yet.
const consentUnavailableSourcesMetaKey = "gram.dev/unavailableSources"

// consentManagedSourcesMetaKey names the gateway members reached through
// identity chaining. Consent cannot list them: a chained token is bound to
// the session grant, which does not exist until consent completes.
const consentManagedSourcesMetaKey = "gram.dev/managedSources"

// serveConsentGatewayMCP answers the consent method surface for a gateway:
// Speakeasy is the MCP server, so the whole inventory is one page of
// member-qualified tools, resolved through the same member catalogs the
// runtime serves and snapshotted before the response is written.
func (s *Service) serveConsentGatewayMCP(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, draft consentToolInventory) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusOK)
		return nil
	}

	req, err := decodeConsentJSONRPCRequest(w, r)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid jsonrpc request").LogWarn(ctx, logger)
	}
	if !consentAllowedMethods[req.Method] {
		return writeConsentJSONRPCError(w, req.ID, proxy.RejectCodeMethodNotFound, "method is not available on the consent transport")
	}

	switch req.Method {
	case mcpversions.MethodInitialize:
		protocolVersion, ok := consentProtocolVersion(req.Params)
		if !ok {
			return writeConsentJSONRPCError(w, req.ID, proxy.RejectCodeMethodNotFound, "method is not available on the consent transport")
		}
		sessionID := uuid.NewString()
		draft.McpSessionID = sessionID
		if err := s.consentToolInventoryCache.Store(ctx, draft); err != nil {
			return oops.E(oops.CodeUnavailable, err, "service temporarily unavailable").LogError(ctx, logger)
		}
		w.Header().Set(proxy.McpSessionIDHeader, sessionID)
		return writeConsentJSONRPCResult(w, req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      serverInfoMetaServer,
		}, nil)
	case mcpversions.MethodNotificationsInitialized:
		w.WriteHeader(http.StatusAccepted)
		return nil
	case mcpversions.MethodPing:
		return writeConsentJSONRPCResult(w, req.ID, map[string]any{}, nil)
	case mcpversions.MethodToolsList:
		tools, members, unavailable, managed, err := s.enumerateGatewayConsentInventory(w, r, endpoint, challengeState)
		if err != nil {
			return err
		}
		draft.Members = members
		if _, aerr := s.appendConsentInventoryPage(ctx, draft, consentRequestCursor(req.Params), tools, ""); aerr != nil {
			if errors.Is(aerr, errConsentInventoryUnavailable) {
				return oops.E(oops.CodeUnavailable, aerr, "service temporarily unavailable").LogError(ctx, logger)
			}
			return oops.E(oops.CodeInvalid, aerr, "the tool inventory could not be captured").LogWarn(ctx, logger)
		}
		result := map[string]any{"tools": consentWireTools(tools)}
		meta := map[string]any{}
		if len(unavailable) > 0 {
			meta[consentUnavailableSourcesMetaKey] = unavailable
		}
		if len(managed) > 0 {
			meta[consentManagedSourcesMetaKey] = managed
		}
		if len(meta) > 0 {
			result["_meta"] = meta
		}
		return writeConsentJSONRPCResult(w, req.ID, result, cacheHintsCallerVarying)
	default:
		return writeConsentJSONRPCError(w, req.ID, proxy.RejectCodeMethodNotFound, "method is not available on the consent transport")
	}
}

// enumerateGatewayConsentInventory resolves the gateway's tools as the
// consenting subject's session will see them: the subject's upstream
// credentials, RBAC, and member visibility all apply, with no tool selection.
// Members that cannot be listed yet are named rather than failing the page,
// so their tools are simply not offered; members identity chaining would
// reach are named separately. It also returns the server id of each member
// that contributed tools.
func (s *Service) enumerateGatewayConsentInventory(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) ([]consentInventoryTool, map[string]uuid.UUID, []string, []string, error) {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	subject := *challengeState.Subject

	private := !endpoint.IsPublic
	if private && subject.Kind == urn.SessionSubjectKindAnonymous {
		return nil, nil, nil, nil, oops.E(oops.CodeUnauthorized, nil, "anonymous subject cannot enumerate a private MCP server").LogWarn(ctx, logger)
	}

	// Members route credentials by resource, so a provider the subject has
	// not connected leaves only its own member unavailable.
	tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject)
	if err != nil {
		return nil, nil, nil, nil, oops.E(oops.CodeUnexpected, err, "resolve upstream tokens for consent transport").LogError(ctx, logger)
	}

	sessionID := "consent:" + challengeState.ID
	ctx, err = s.contextForSessionSubject(ctx, endpoint, subject, sessionID, challengeState.ClientID)
	if err != nil {
		return nil, nil, nil, nil, oops.E(oops.CodeUnexpected, err, "stamp consent subject context").LogError(ctx, logger)
	}
	if private {
		if _, ok := contextvalues.GetAuthContext(ctx); !ok {
			return nil, nil, nil, nil, oops.E(oops.CodeUnauthorized, nil, "consent subject has no authenticated context").LogWarn(ctx, logger)
		}
	}
	// Discovery identity belongs to the user who authenticated this challenge.
	ctx = mcpidentity.WithoutIdentity(ctx)
	if subject.Kind == urn.SessionSubjectKindUser &&
		challengeState.AuthorizerImpersonated != nil && !*challengeState.AuthorizerImpersonated &&
		challengeState.AuthorizerUserID != "" && subject.ID == challengeState.AuthorizerUserID &&
		challengeState.Federation == nil && !challengeState.CreatedAt.IsZero() && !challengeState.CreatedAt.After(time.Now().Add(time.Minute)) {
		ctx = s.identityValidator.StampConsentDiscovery(ctx, subject.ID, challengeState.CreatedAt.Add(challengeState.TTL()))
	}
	if s.authz != nil && private {
		if ctx, err = s.authz.PrepareContext(ctx); err != nil {
			return nil, nil, nil, nil, oops.E(oops.CodeUnexpected, err, "load access grants for consent inventory").LogError(ctx, logger)
		}
		connectResourceID := endpoint.connectResourceID()
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, connectResourceID.String(), endpoint.ProjectID.String())); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("authorize consent inventory access: %w", mcpaccess.ServerPermissionDenied(err, s.requestAccessURL(ctx, connectResourceID.String(), endpoint.Slug)))
		}
	}

	gate := &metaGateContext{
		projectID:    endpoint.ProjectID,
		metaServerID: endpoint.MetaMcpServerID.UUID,
		// Consent lists a stored gateway; agent gateways reach no consent page.
		agentID:             uuid.Nil,
		organizationGateway: false,
		organizationID:      endpoint.OrganizationID,
		tokens:              tokens,
		projectTokens:       nil,
		// Consent never acquires credentials by identity chaining.
		userSessionIssuerID: uuid.Nil,
		connectRoot:         "",
		chainUpstream:       nil,
		toolSelection:       nil,
		selectionMembers:    nil,
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

	unavailableMembers := []metaMember{}
	inventory, err := s.collectGatewayTools(ctx, logger, endpoint.MetaMcpServerID.UUID, endpoint.ProjectID, gate, &unavailableMembers)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	unavailable, managed := []string{}, []string{}
	for _, member := range unavailableMembers {
		name := conv.Default(member.name, member.slug)
		if s.consentChainedMember(ctx, logger, endpoint, subject, member) {
			managed = append(managed, name)
		} else {
			unavailable = append(unavailable, name)
		}
	}
	tools := make([]consentInventoryTool, 0, len(inventory))
	members := map[string]uuid.UUID{}
	for _, tool := range inventory {
		members[tool.member.slug] = tool.member.serverID
		tools = append(tools, consentInventoryTool{
			Name:        tool.entry.Name,
			Annotations: consentAnnotationsFromEntry(tool.entry.Annotations),
			Source:      conv.Default(tool.member.name, tool.member.slug),
		})
	}
	return tools, members, unavailable, managed, nil
}

// consentChainedMember reports whether a tools/call would reach member by
// identity chaining, judged with the request metaMemberChainer builds. It
// reads configuration only and reports false on any lookup fault.
func (s *Service) consentChainedMember(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, member metaMember) bool {
	if s.identityChainer == nil || endpoint.UserSessionIssuerID == uuid.Nil || subject.Kind != urn.SessionSubjectKindUser || member.projectID != endpoint.ProjectID {
		return false
	}
	var resource string
	switch {
	case member.remoteServerID.Valid:
		remote, err := remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{ID: member.remoteServerID.UUID, ProjectID: member.projectID})
		if err != nil {
			logger.WarnContext(ctx, "load gateway member upstream for consent identity chaining", attr.SlogError(err))
			return false
		}
		resource = remote.Url
	case member.tunneledServerID.Valid:
		resource = member.tunneledResourceIdentifier
	default:
		return false
	}
	req, ok := identitychaining.NewRequest(endpoint.OrganizationID, member.projectID, endpoint.UserSessionIssuerID, subject.ID, resource, member.tunneledServerID.Valid, member.remoteSessionIssuerID)
	if !ok {
		return false
	}
	_, served := s.identityChainer.Serves(ctx, req)
	return served
}

// consentAnnotationsFromEntry keeps only explicitly true hints, the same
// fail-closed axis toolfilter matches on.
func consentAnnotationsFromEntry(annotations *externalmcp.ToolAnnotations) []string {
	if annotations == nil {
		return nil
	}
	hintTrue := func(hint *bool) bool { return hint != nil && *hint }
	var values []string
	if hintTrue(annotations.ReadOnlyHint) {
		values = append(values, toolfilter.AnnotationReadOnly)
	}
	if hintTrue(annotations.DestructiveHint) {
		values = append(values, toolfilter.AnnotationDestructive)
	}
	if hintTrue(annotations.IdempotentHint) {
		values = append(values, toolfilter.AnnotationIdempotent)
	}
	if hintTrue(annotations.OpenWorldHint) {
		values = append(values, toolfilter.AnnotationOpenWorld)
	}
	return values
}
