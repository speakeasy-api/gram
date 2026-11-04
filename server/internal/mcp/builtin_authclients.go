package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// builtinDirectAuthEnvironment returns handled=false only when legacy environment
// resolution is allowed. Errors always return handled=true: callers must never
// fall back to environment or request credentials after an enforcement failure.
// Admission (including mcp:connect) must run before this helper.
func (s *Service) builtinDirectAuthEnvironment(ctx context.Context, authentication *issuerGateAuthentication, key string) (toolconfig.ToolCallEnv, bool, error) {
	var empty toolconfig.ToolCallEnv
	if authentication == nil || authentication.endpoint == nil {
		return empty, true, builtinAuthSetupError()
	}
	endpoint := authentication.endpoint
	mode, err := s.authClientMode(ctx, endpoint.OrganizationID)
	if err != nil {
		return empty, true, err
	}
	if mode != feature.AuthClientResolverEnforced {
		return empty, false, nil
	}
	if !endpoint.McpServerID.Valid {
		return empty, true, builtinAuthSetupError()
	}
	selector, err := s.upstreamSelectorFor(ctx, endpoint)
	if err != nil {
		return empty, true, err
	}
	if selector == nil {
		selector, err = s.agentKeySelectorFor(ctx, endpoint)
		if err != nil {
			return empty, true, err
		}
	}
	if selector == nil {
		selector = s.profileSelectorFor(ctx, endpoint)
	}
	if selector == nil {
		return empty, true, builtinAuthSetupError()
	}
	res, err := s.resolveEnforcedAuthClient(ctx, selector, endpoint.McpServerID.UUID, endpoint.ProjectID, connectionSubject(ctx, authentication.subject))
	if err != nil {
		return empty, true, err
	}
	authentication.authClient = &res
	if res.outcome == authClientNoAccess {
		return empty, true, s.authClientNoAccessError(ctx, endpoint.McpServerID.UUID, res)
	}
	env, err := builtinStaticAuthEnvironment(key, &res)
	return env, true, err
}

// builtinMemberAuthEnvironment consumes the result of memberChoices.apply. It
// never selects a client or acquires an unrelated environment. Use this for both
// gateway readiness and execution after the gateway has applied its choices.
func (s *Service) builtinMemberAuthEnvironment(ctx context.Context, organizationID string, member metaMember) (toolconfig.ToolCallEnv, bool, error) {
	var empty toolconfig.ToolCallEnv
	mode, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return empty, true, err
	}
	if mode != feature.AuthClientResolverEnforced {
		return empty, false, nil
	}
	if member.upstreamUnavailable != nil {
		return empty, true, member.upstreamUnavailable
	}
	if member.authClient != nil && member.authClient.outcome == authClientNoAccess {
		return empty, true, s.authClientNoAccessError(ctx, member.serverID, *member.authClient)
	}
	env, err := builtinStaticAuthEnvironment(member.builtinKey, member.authClient)
	return env, true, err
}

// builtinStaticAuthEnvironment maps only Slack's supported static token fields.
// The resolution must already have passed the ordinary typed-client permission
// check. No caller headers, environment values, or token-generating clients are
// accepted. Errors deliberately contain neither header names nor secret values.
func builtinStaticAuthEnvironment(key string, res *authClientResolution) (toolconfig.ToolCallEnv, error) {
	var empty toolconfig.ToolCallEnv
	if key != "slack" || res == nil || res.outcome != authClientResolved || res.clientID == uuid.Nil || res.clientType != authClientTypeStaticCredential {
		return empty, builtinAuthSetupError()
	}
	values := toolconfig.NewCaseInsensitiveEnv()
	found := false
	for _, header := range res.headers {
		if header.ValueFromRequestHeader != "" {
			return empty, builtinAuthSetupError()
		}
		name := strings.ToUpper(header.Name)
		switch name {
		case "SLACK_BOT_TOKEN", "SLACK_USER_TOKEN", "SLACK_TOKEN":
			if strings.TrimSpace(header.StaticValue) == "" || values.Get(name) != "" {
				return empty, builtinAuthSetupError()
			}
			values.Set(name, header.StaticValue)
			found = true
		default:
			// Unknown headers cannot become tool configuration. A required one
			// signals a configuration this backend cannot satisfy.
			if header.IsRequired {
				return empty, builtinAuthSetupError()
			}
		}
	}
	if !found {
		return empty, builtinAuthSetupError()
	}
	return toolconfig.ToolCallEnv{UserConfig: values, SystemEnv: toolconfig.NewCaseInsensitiveEnv(), OAuthToken: "", GramEmail: "", GramChatID: "", MCPClient: toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""}}, nil
}

var errBuiltinAuthSelectionUnavailable = errors.New("built-in credential selection is unavailable")

func builtinAuthSetupError() error {
	return oops.E(oops.CodeForbidden, errBuiltinAuthSelectionUnavailable, "select an available static Slack credential for this connection")
}

// typedMember includes credential-consuming built-ins only under enforcement;
// their legacy/shadow environment path must remain unchanged.
func (c *memberChoices) typedMember(member metaMember) bool {
	return member.backend == metaMemberBackendProxied || (c.enforced && member.backend == metaMemberBackendBuiltin)
}
