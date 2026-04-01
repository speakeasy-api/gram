package mcp

import (
	"testing"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

func TestDescribeToolSecurity_APIKey(t *testing.T) {
	t.Parallel()
	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-1",
			Type:         new("apiKey"),
			Name:         "X-API-Key",
			InPlacement:  "header",
			Scheme:       "",
			EnvVariables: []string{"X_API_KEY"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeAPIKey, schemes[0].Kind)
	require.Equal(t, "sv-1", schemes[0].Key)
	require.Equal(t, []string{"X_API_KEY"}, schemes[0].EnvVariables)
}

func TestDescribeToolSecurity_Bearer(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-2",
			Type:         new("http"),
			Name:         "Authorization",
			InPlacement:  "header",
			Scheme:       "bearer",
			EnvVariables: []string{"BEARER_TOKEN"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeHTTPBearer, schemes[0].Kind)
}

func TestDescribeToolSecurity_OAuth2AuthCode(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-3",
			Type:         new("oauth2"),
			Name:         "OAuth",
			Scheme:       "",
			OauthTypes:   []string{"authorization_code"},
			EnvVariables: []string{"GITHUB_ACCESS_TOKEN"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeOAuth2AuthCode, schemes[0].Kind)
}

func TestDescribeToolSecurity_ClientCredentials(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-4",
			Type:         new("oauth2"),
			Name:         "ClientCreds",
			Scheme:       "",
			OauthTypes:   []string{"client_credentials"},
			EnvVariables: []string{"MY_CLIENT_ID", "MY_CLIENT_SECRET"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeOAuth2ClientCredentials, schemes[0].Kind)
}

func TestDescribeToolSecurity_OpenIDConnect(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-5",
			Type:         new("openIdConnect"),
			Name:         "OIDC",
			Scheme:       "",
			EnvVariables: []string{"OIDC_ACCESS_TOKEN"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeOpenIDConnect, schemes[0].Kind)
}

func TestDescribeToolSecurity_MultipleSchemes(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-1",
			Type:         new("apiKey"),
			Name:         "X-API-Key",
			InPlacement:  "header",
			Scheme:       "",
			EnvVariables: []string{"X_API_KEY"},
		},
		{
			ID:           "sv-2",
			Type:         new("oauth2"),
			Name:         "OAuth",
			Scheme:       "",
			OauthTypes:   []string{"authorization_code"},
			EnvVariables: []string{"ACCESS_TOKEN"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 2)
	require.Equal(t, securitySchemeAPIKey, schemes[0].Kind)
	require.Equal(t, securitySchemeOAuth2AuthCode, schemes[1].Kind)
}

func TestDescribeToolSecurity_Empty(t *testing.T) {
	t.Parallel()

	require.Empty(t, describeToolSecurity(nil))
	require.Empty(t, describeToolSecurity([]*types.SecurityVariable{}))
}

func TestDescribeToolSecurity_NilType(t *testing.T) {
	t.Parallel()

	secVars := []*types.SecurityVariable{
		{
			ID:           "sv-x",
			Type:         nil,
			Name:         "Unknown",
			Scheme:       "",
			EnvVariables: []string{"SOME_VAR"},
		},
	}

	schemes := describeToolSecurity(secVars)
	require.Len(t, schemes, 1)
	require.Equal(t, securitySchemeAPIKey, schemes[0].Kind)
}

func TestSchemeSatisfied_APIKeyPresent(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("x_api_key", "my-secret")

	scheme := securityRequirement{
		Kind:         securitySchemeAPIKey,
		EnvVariables: []string{"X_API_KEY"},
	}

	require.True(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_APIKeyMissing(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	scheme := securityRequirement{
		Kind:         securitySchemeAPIKey,
		EnvVariables: []string{"X_API_KEY"},
	}

	require.False(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_BearerPresent(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("bearer_token", "tok123")

	scheme := securityRequirement{
		Kind:         securitySchemeHTTPBearer,
		EnvVariables: []string{"BEARER_TOKEN"},
	}

	require.True(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_BasicBothPresent(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("username", "user")
	env.Set("password", "pass")

	scheme := securityRequirement{
		Kind:         securitySchemeHTTPBasic,
		EnvVariables: []string{"USERNAME", "PASSWORD"},
	}

	require.True(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_BasicMissingPassword(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("username", "user")

	scheme := securityRequirement{
		Kind:         securitySchemeHTTPBasic,
		EnvVariables: []string{"USERNAME", "PASSWORD"},
	}

	require.False(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_OAuth2WithAccessToken(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("github_access_token", "ghp_xxx")

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2AuthCode,
		EnvVariables: []string{"GITHUB_ACCESS_TOKEN"},
	}

	require.True(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_OAuth2WithOAuthTokenFallback(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2AuthCode,
		EnvVariables: []string{"GITHUB_ACCESS_TOKEN"},
	}

	require.True(t, schemeSatisfied(scheme, env, "some-oauth-token"))
}

func TestSchemeSatisfied_OAuth2NoEnvVarsWithOAuthToken(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2AuthCode,
		EnvVariables: []string{},
	}

	require.True(t, schemeSatisfied(scheme, env, "some-oauth-token"))
}

func TestSchemeSatisfied_OAuth2NoEnvVarsNoOAuthToken(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2AuthCode,
		EnvVariables: []string{},
	}

	require.False(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_ClientCredentialsBothPresent(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("my_client_id", "id123")
	env.Set("my_client_secret", "secret456")

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2ClientCredentials,
		EnvVariables: []string{"MY_CLIENT_ID", "MY_CLIENT_SECRET"},
	}

	require.True(t, schemeSatisfied(scheme, env, ""))
}

func TestSchemeSatisfied_ClientCredentialsMissingSecret(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("my_client_id", "id123")

	scheme := securityRequirement{
		Kind:         securitySchemeOAuth2ClientCredentials,
		EnvVariables: []string{"MY_CLIENT_ID", "MY_CLIENT_SECRET"},
	}

	require.False(t, schemeSatisfied(scheme, env, ""))
}

func TestAnySchemeSatisfied_OneOfTwoMet(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()
	env.Set("x_api_key", "my-key")

	schemes := []securityRequirement{
		{Kind: securitySchemeAPIKey, EnvVariables: []string{"X_API_KEY"}},
		{Kind: securitySchemeOAuth2AuthCode, EnvVariables: []string{"ACCESS_TOKEN"}},
	}

	require.True(t, anySchemeSatisfied(schemes, env, ""))
}

func TestAnySchemeSatisfied_NoneMet(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	schemes := []securityRequirement{
		{Kind: securitySchemeAPIKey, EnvVariables: []string{"X_API_KEY"}},
		{Kind: securitySchemeOAuth2AuthCode, EnvVariables: []string{"ACCESS_TOKEN"}},
	}

	require.False(t, anySchemeSatisfied(schemes, env, ""))
}

func TestAnySchemeSatisfied_EmptySchemes(t *testing.T) {
	t.Parallel()

	env := toolconfig.NewCaseInsensitiveEnv()

	require.True(t, anySchemeSatisfied(nil, env, ""))
	require.True(t, anySchemeSatisfied([]securityRequirement{}, env, ""))
}
