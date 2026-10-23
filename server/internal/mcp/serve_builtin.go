package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const BuiltinRoute = "/mcp/builtin/{organizationSlug}/{projectSlug}/{builtinKey}"

// ServeBuiltin dispatches a code-owned backend after shared ingress admission.
func (s *Service) ServeBuiltin(w http.ResponseWriter, r *http.Request) error {
	endpoint, err := s.loadBuiltinEndpoint(r.Context(), builtinRequestSlug(r))
	if err != nil {
		return err
	}
	projectID := endpoint.ProjectID
	parts := strings.Split(endpoint.Slug, "/")
	key, projectSlug := parts[2], parts[1]
	definition, _ := builtinmcp.Lookup(key)
	prepared, handled, err := s.prepareTerminatedMCPRequest(w, r, s.logger, platformToolsetMaxBodyBytes, mcpversions.SupportedPlatformToolset(), mcpmetrics.SurfacePlatform)
	if err != nil || handled {
		return err
	}
	ctx, _, policy, err := s.authenticateIssuerGate(r.Context(), w, httpheaders.AuthorizationBearerToken(r), s.BaseURLForRequest(r), endpoint)
	if err != nil {
		return fmt.Errorf("serve built-in MCP: %w", err)
	}
	ctx = withMandatoryAdmission(ctx)
	id := endpoint.BuiltinID.UUID
	if err := s.authz.RequireBuiltinConnect(ctx, id.String(), projectID.String()); err != nil {
		return fmt.Errorf("authorize built-in MCP connection: %w", err)
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	bound := *authCtx
	bound.ProjectID = &projectID
	bound.ProjectSlug = &projectSlug
	ctx = contextvalues.SetAuthContext(ctx, &bound)
	metering.AttributeMCPBandwidth(ctx, endpoint.OrganizationID, projectID)
	metering.AttributeMCPBandwidthServer(ctx, metering.MCPServerTypePlatformToolset, id.String(), "builtin-"+key)
	if prepared.empty() {
		return nil
	}
	if err := validateMCPRequestEnvelope(ctx, s.logger, prepared, oops.CodeRequestTooLarge, "MCP request body exceeds 1 MiB"); err != nil {
		return err
	}
	req := prepared.request
	version := prepared.protocolVersion
	tools, err := s.builtinToolset(ctx, definition, projectID)
	if err != nil {
		return fmt.Errorf("serve built-in MCP: %w", err)
	}
	if policy != nil {
		if policy.Resource != "builtin_mcp:"+id.String() || policy.Catalog != nil || policy.Gateway != nil {
			return oops.C(oops.CodeForbidden)
		}
		selected := tools.Tools[:0]
		for _, tool := range tools.Tools {
			model := tool.Executor.Descriptor().ToTool(projectID)
			if len(toolfilter.FilterToolsBySelection([]*types.Tool{model}, policy.Selection)) != 0 {
				selected = append(selected, tool)
			}
		}
		tools.Tools = selected
	}
	s.metrics.RecordMCPRequest(ctx, version.Declared, req.Method, mcpmetrics.SurfacePlatform)
	var body json.RawMessage
	if !methodAvailable(&req, version, mcpversions.SupportedPlatformToolset()) {
		return writeMCPError(ctx, s.logger, w, req.ID, version.InEffect, unavailableMethod(&req))
	}
	if req.Method == mcpversions.MethodToolsCall {
		environment, envErr := s.builtinEnvironment(ctx, projectID, key, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, r.Header.Get("Gram-Environment"), parseMcpEnvVariables(r, nil))
		if envErr != nil {
			err = envErr
		} else {
			body, err = s.callPlatformToolsetToolWithConfig(ctx, &bound, tools, &req, r.Header.Get("Gram-Chat-ID"), &platformCallConfig{environment: environment, serverID: id.String(), metaServerID: ""})
		}
	} else {
		description := describePlatformServer()
		description.identity = serverInfoBuiltin
		switch req.Method {
		case mcpversions.MethodInitialize:
			body, err = handleCodeOwnedInitialize(ctx, s.logger, s.metrics, &req, &version, description)
		case mcpversions.MethodServerDiscover:
			body, err = handleServerDiscover(ctx, s.logger, req.ID, description, mcpversions.SupportedPlatformToolset())
		case mcpversions.MethodPing:
			body, err = handlePing(ctx, s.logger, req.ID, serverInfoBuiltin)
		case mcpversions.MethodToolsList:
			body, err = s.listCodeOwnedToolsetTools(ctx, &bound, tools, &req, serverInfoBuiltin)
		case mcpversions.MethodNotificationsInitialized, mcpversions.MethodNotificationsCancelled:
		default:
			err = unavailableMethod(&req)
		}
	}
	if err != nil {
		return writeMCPError(ctx, s.logger, w, req.ID, version.InEffect, err)
	}
	if body == nil {
		return respondWithNoContent(true, w)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err = w.Write(body); err != nil {
		return fmt.Errorf("write built-in MCP response: %w", err)
	}
	return nil
}

// builtinToolset pins only catalog-approved executors. Tool-level restrictions
// filter listing and are checked again by the shared execution pipeline.
func (s *Service) builtinToolset(ctx context.Context, definition builtinmcp.Definition, projectID uuid.UUID) (platformtools.Toolset, error) {
	id := builtinmcp.ID(projectID, definition.Key)
	if err := s.authz.RequireBuiltinConnect(ctx, id.String(), projectID.String()); err != nil {
		return platformtools.Toolset{}, fmt.Errorf("authorize built-in MCP: %w", err)
	}
	tools := platformtools.Toolset{Slug: "builtin-" + definition.Key, Tools: make([]platformtools.ExternalTool, 0, len(definition.Tools))}
	for _, desc := range definition.Tools {
		executor := s.builtinExecutors[desc.ToolURN().String()]
		if executor == nil {
			return platformtools.Toolset{}, fmt.Errorf("missing built-in executor %s", desc.Name)
		}
		check := authz.MCPToolCallCheck(id.String(), authz.MCPToolCallDimensions{Tool: desc.Name, Disposition: conv.DispositionFromAnnotations(desc.Annotations), ProjectID: projectID.String()})
		if err := s.authz.RequireBuiltinConnect(ctx, id.String(), projectID.String(), check); err != nil {
			continue
		}
		tools.Tools = append(tools.Tools, platformtools.ExternalTool{Executor: executor, RequiredFeature: "", RequiredScope: ""})
	}
	return tools, nil
}

// builtinEnvironment resolves only the member project's declared credential
// variables. Missing overrides fail rather than silently using another account.
func (s *Service) builtinEnvironment(ctx context.Context, projectID uuid.UUID, key string, override uuid.NullUUID, headerEnvironment string, headers map[string]string) (toolconfig.ToolCallEnv, error) {
	environment := toolconfig.ToolCallEnv{UserConfig: toolconfig.NewCaseInsensitiveEnv(), SystemEnv: toolconfig.NewCaseInsensitiveEnv(), OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}}
	definition, ok := builtinmcp.Lookup(key)
	if !ok {
		return environment, oops.C(oops.CodeNotFound)
	}
	settings, err := builtinmcp.LoadSettings(ctx, s.db, projectID, key)
	if err != nil {
		return environment, fmt.Errorf("load built-in settings: %w", err)
	}
	return resolveBuiltinEnvironment(ctx, s.env, projectID, definition, settings, override, headerEnvironment, headers)
}

func resolveBuiltinEnvironment(ctx context.Context, loader toolconfig.EnvironmentLoader, projectID uuid.UUID, definition builtinmcp.Definition, settings builtinmcp.Settings, override uuid.NullUUID, headerEnvironment string, headers map[string]string) (toolconfig.ToolCallEnv, error) {
	environment := toolconfig.ToolCallEnv{UserConfig: toolconfig.NewCaseInsensitiveEnv(), SystemEnv: toolconfig.NewCaseInsensitiveEnv(), OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}}
	// Project creation provisions this standard environment. An unset built-in
	// override means inherit it, not execute without project credentials.
	selected := toolconfig.Slug("default")
	explicitEnvironment := settings.EnvironmentID.Valid || override.Valid || headerEnvironment != ""
	if settings.EnvironmentID.Valid {
		selected = toolconfig.ID(settings.EnvironmentID.UUID)
	}
	if override.Valid {
		selected = toolconfig.ID(override.UUID)
	}
	if headerEnvironment != "" {
		if id, err := uuid.Parse(headerEnvironment); err == nil {
			selected = toolconfig.ID(id)
		} else {
			selected = toolconfig.Slug(headerEnvironment)
		}
	}
	headerValues := toolconfig.NewCaseInsensitiveEnv()
	for name, value := range headers {
		headerValues.Set(name, value)
	}
	hasHeaderCredential := false
	for _, variable := range definition.CredentialVariables {
		if headerValues.Get(variable) != "" {
			hasHeaderCredential = true
			break
		}
	}
	values, err := loader.Load(ctx, projectID, selected)
	if err != nil {
		// Direct callers may supply their own Slack header credentials when a
		// legacy project has no standard environment. Never mask a broken
		// explicit selection or an environment-store failure this way.
		if explicitEnvironment || !hasHeaderCredential || !errors.Is(err, toolconfig.ErrNotFound) {
			return environment, oops.E(oops.CodeBadRequest, err, "built-in environment is unavailable")
		}
		values = nil
	}
	stored := toolconfig.NewCaseInsensitiveEnv()
	for key, value := range values {
		stored.Set(key, value)
	}
	for key, value := range headers {
		stored.Set(key, value)
	}
	found := false
	for _, variable := range definition.CredentialVariables {
		if value := stored.Get(variable); value != "" {
			environment.UserConfig.Set(variable, value)
			found = true
		}
	}
	if !found {
		return environment, oops.E(oops.CodeBadRequest, nil, "configure a Slack credential in the built-in environment")
	}
	return environment, nil
}
