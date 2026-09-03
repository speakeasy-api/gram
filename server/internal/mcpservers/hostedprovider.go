package mcpservers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	toolsrepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/security"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// HostedOAuthProvider is the one upstream OAuth provider a hosted server's
// materialized external MCP tools authenticate with. TokenEndpoint identifies
// the authorization server, so a stored provider issuer can be checked against
// the tools currently deployed.
type HostedOAuthProvider struct {
	// Name is the external MCP server's name, for labeling the provider.
	Name          string
	ResourceURL   string
	TokenEndpoint string
}

// HostedProviderError is a toolset OAuth shape a gateway cannot serve with one
// credential: dispatch injects one token per hosted member into every
// OAuth-shaped tool, so the tools must agree on a single https upstream and no
// OpenAPI or function tool may take OAuth alongside them.
type HostedProviderError struct{ Reason string }

func (e *HostedProviderError) Error() string { return e.Reason }

// ResolveHostedOAuthProvider inspects the toolset's latest version against the
// project's active deployment. Nil when no materialized external MCP tool
// requires OAuth (the meta surface never dispatches passthrough ones).
func ResolveHostedOAuthProvider(ctx context.Context, db *pgxpool.Pool, projectID, toolsetID uuid.UUID) (*HostedOAuthProvider, error) {
	version, err := toolsetsrepo.New(db).GetLatestToolsetVersion(ctx, toolsetID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && len(version.ToolUrns) == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load toolset version: %w", err)
	}
	deploymentID, err := deploymentsrepo.New(db).GetActiveDeploymentID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active deployment: %w", err)
	}
	oauthTools, err := externalmcprepo.New(db).GetExternalMCPToolsRequiringOAuth(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("load external mcp tools requiring oauth: %w", err)
	}

	urns := make([]string, 0, len(version.ToolUrns))
	for _, toolURN := range version.ToolUrns {
		urns = append(urns, toolURN.String())
	}
	var provider *HostedOAuthProvider
	for _, tool := range oauthTools {
		resource := strings.TrimRight(tool.RemoteUrl, "/")
		if tool.Type == "proxy" || resource == "" || !slices.Contains(urns, tool.ToolUrn) {
			continue
		}
		// Discovery output decides where Gram registers a client; never trust
		// it over cleartext.
		if !secureURL(resource) {
			return nil, &HostedProviderError{Reason: fmt.Sprintf("external MCP tool upstream %s must use https", resource)}
		}
		tokenEndpoint := strings.TrimRight(tool.OauthTokenEndpoint.String, "/")
		if tokenEndpoint == "" {
			return nil, &HostedProviderError{Reason: fmt.Sprintf("external MCP tool upstream %s has no OAuth token endpoint recorded; redeploy the toolset", resource)}
		}
		switch {
		case provider == nil:
			provider = &HostedOAuthProvider{Name: tool.RegistryServerName, ResourceURL: resource, TokenEndpoint: tokenEndpoint}
		case provider.ResourceURL != resource || provider.TokenEndpoint != tokenEndpoint:
			return nil, &HostedProviderError{Reason: fmt.Sprintf("external MCP tools span several OAuth upstreams (%s, %s); a gateway routes one credential per member", provider.ResourceURL, resource)}
		}
	}
	if provider == nil {
		return nil, nil
	}

	mixed, err := toolsetTakesOAuthElsewhere(ctx, db, projectID, urns)
	if err != nil {
		return nil, err
	}
	if mixed {
		return nil, &HostedProviderError{Reason: fmt.Sprintf("OpenAPI or function tools take OAuth alongside external MCP tools on %s; a gateway routes one credential per member", provider.ResourceURL)}
	}
	return provider, nil
}

// toolsetTakesOAuthElsewhere reports whether a selected OpenAPI or function
// tool would also receive the member's token (see resolveUserConfiguration).
func toolsetTakesOAuthElsewhere(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, urns []string) (bool, error) {
	httpTools, err := toolsrepo.New(db).FindHttpToolsByUrn(ctx, toolsrepo.FindHttpToolsByUrnParams{ProjectID: projectID, Urns: urns})
	if err != nil {
		return false, fmt.Errorf("load http tools: %w", err)
	}
	for _, row := range httpTools {
		def := row.HttpToolDefinition
		keys, _, err := security.ParseHTTPToolSecurityKeys(def.Security)
		if err != nil {
			return false, fmt.Errorf("parse http tool security: %w", err)
		}
		if len(keys) == 0 {
			continue
		}
		var docIDs []uuid.UUID
		if def.Openapiv3DocumentID.Valid {
			docIDs = []uuid.UUID{def.Openapiv3DocumentID.UUID}
		}
		entries, err := toolsetsrepo.New(db).GetHTTPSecurityDefinitions(ctx, toolsetsrepo.GetHTTPSecurityDefinitionsParams{SecurityKeys: keys, DeploymentIds: []uuid.UUID{def.DeploymentID}, Openapiv3DocumentIds: docIDs})
		if err != nil {
			return false, fmt.Errorf("load http security definitions: %w", err)
		}
		for _, entry := range entries {
			if entry.Type.String == "openIdConnect" || slices.Contains(entry.OauthTypes, "authorization_code") {
				return true, nil
			}
		}
	}

	functionTools, err := toolsrepo.New(db).FindFunctionToolsByUrn(ctx, toolsrepo.FindFunctionToolsByUrnParams{ProjectID: projectID, Urns: urns})
	if err != nil {
		return false, fmt.Errorf("load function tools: %w", err)
	}
	for _, row := range functionTools {
		if len(row.FunctionToolDefinition.AuthInput) == 0 {
			continue
		}
		var authInput functions.ManifestAuthInputAttributeV0
		if err := json.Unmarshal(row.FunctionToolDefinition.AuthInput, &authInput); err != nil {
			return false, fmt.Errorf("parse function tool auth input: %w", err)
		}
		if authInput.Type == "oauth2" {
			return true, nil
		}
	}
	return false, nil
}

func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" || (u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1"))
}
