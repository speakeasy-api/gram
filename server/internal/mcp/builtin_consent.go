package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Credential-consuming built-ins use ordinary MCP auth client selection. Other
// first-party backends, including approvals, retain their existing consent path.
func credentialConsumingBuiltin(key string) bool {
	definition, ok := builtinmcp.Lookup(key)
	return ok && len(definition.CredentialVariables) > 0
}

func consentMemberHasAuthChoices(member metaMember) bool {
	return member.backend == metaMemberBackendProxied ||
		(member.backend == metaMemberBackendBuiltin && credentialConsumingBuiltin(member.builtinKey))
}

func (s *Service) serveConsentCredentialBuiltinMCP(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, draft consentToolInventory, key, postedAuthClient string) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	definition, ok := builtinmcp.Lookup(key)
	if !ok || !endpoint.McpServerID.Valid || endpoint.McpServerID.UUID != builtinmcp.ID(endpoint.ProjectID, key) {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
	if state.Subject == nil || state.Subject.Kind == urn.SessionSubjectKindAnonymous {
		return oops.E(oops.CodeBadRequest, nil, "anonymous subject cannot enumerate a private MCP server")
	}
	// Reuse the mode-gated choice resolver: legacy/shadow remain legacy, while
	// enforced previews bind exactly the client approval will capture. Inventory
	// itself never loads an environment or calls the Slack API.
	auth, err := s.consentDirectPreviewAuth(ctx, endpoint, state, postedAuthClient)
	if err != nil {
		if errors.Is(err, errConsentConfigurationChanged) {
			return oops.E(oops.CodeFailedPrecondition, err, "The credential you chose can no longer be used. Reload this page to choose again.").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnavailable, err, "resolve consent transport credential").LogError(ctx, logger)
	}
	if err := bindDirectConsentInventory(&draft, auth); err != nil {
		return oops.E(oops.CodeFailedPrecondition, err, "The credential changed; reload the tool preview.").LogWarn(ctx, logger)
	}
	if auth != nil {
		if err := s.consentToolInventoryCache.Store(ctx, draft); err != nil {
			return oops.E(oops.CodeUnavailable, err, "store consent credential binding").LogError(ctx, logger)
		}
	}
	return s.serveConsentLocalMCP(w, r, endpoint, draft, func(ctx context.Context) ([]consentInventoryTool, []string, error) {
		return s.authorizedBuiltinConsentInventory(ctx, endpoint, state, definition)
	})
}

func (s *Service) authorizedBuiltinConsentInventory(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, definition builtinmcp.Definition) ([]consentInventoryTool, []string, error) {
	if state.Subject == nil || state.Subject.Kind == urn.SessionSubjectKindAnonymous {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "anonymous subject cannot enumerate a private MCP server")
	}
	ctx, err := s.contextForSessionSubject(ctx, endpoint, *state.Subject, "consent:"+state.ID, state.ClientID)
	if err != nil {
		return nil, nil, fmt.Errorf("stamp built-in consent subject context: %w", err)
	}
	ctx, err = s.authz.PrepareContext(withMandatoryAdmission(ctx))
	if err != nil {
		return nil, nil, fmt.Errorf("load built-in consent access grants: %w", err)
	}
	toolset, err := s.builtinToolset(ctx, definition, endpoint.ProjectID)
	if err != nil {
		return nil, nil, fmt.Errorf("authorize built-in consent inventory: %w", err)
	}
	tools := make([]consentInventoryTool, 0, len(toolset.Tools))
	visible := make(map[string]bool, len(toolset.Tools))
	for _, tool := range toolset.Tools {
		desc := tool.Executor.Descriptor()
		visible[desc.Name] = true
		tools = append(tools, consentInventoryTool{
			Name: desc.Name, Definition: nil, DefinitionHash: consentEvidenceHash(desc.ToTool(endpoint.ProjectID)),
			Annotations: trueAnnotationValues(desc.Annotations), Source: "",
		})
	}
	var hidden []string
	for _, tool := range definition.Tools {
		if !visible[tool.Name] {
			hidden = append(hidden, tool.Name)
		}
	}
	return tools, hidden, nil
}
