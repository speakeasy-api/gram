package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateRemoteSessionIssuerRejectsInsecureProviderURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	insecure := "http://identity.example.com/oauth"
	testCases := []struct {
		apply func(*issuersgen.CreateRemoteSessionIssuerPayload)
	}{
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.Issuer = insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.AuthorizationEndpoint = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.TokenEndpoint = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.RevocationEndpoint = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.RegistrationEndpoint = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.JwksURI = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.UserinfoEndpoint = &insecure }},
		{apply: func(payload *issuersgen.CreateRemoteSessionIssuerPayload) { payload.IntrospectionEndpoint = &insecure }},
	}

	for _, testCase := range testCases {
		payload := newIssuerPayload("insecure-" + uuid.NewString())
		testCase.apply(payload)

		_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestCreateRemoteSessionIssuerAcceptsHTTPSProviderURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := newIssuerPayload("secure-provider-urls")
	payload.RevocationEndpoint = conv.PtrEmpty("https://idp.example.com/revoke")
	payload.UserinfoEndpoint = conv.PtrEmpty("https://idp.example.com/userinfo")
	payload.IntrospectionEndpoint = conv.PtrEmpty("https://idp.example.com/introspect")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, payload.Issuer, created.Issuer)
}

func TestCreateRemoteSessionIssuerAcceptsLoopbackProviderURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuer := "http://localhost:8080/oauth"
	payload := newIssuerPayload("loopback-provider-urls")
	payload.Issuer = issuer
	payload.AuthorizationEndpoint = conv.PtrEmpty(issuer + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(issuer + "/token")
	payload.RevocationEndpoint = conv.PtrEmpty(issuer + "/revoke")
	payload.RegistrationEndpoint = conv.PtrEmpty(issuer + "/register")
	payload.JwksURI = conv.PtrEmpty(issuer + "/jwks")
	payload.UserinfoEndpoint = conv.PtrEmpty(issuer + "/userinfo")
	payload.IntrospectionEndpoint = conv.PtrEmpty(issuer + "/introspect")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, issuer, created.Issuer)
}

func TestCreateRemoteSessionIssuerNormalizesOmittedRequiredArrays(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := newIssuerPayload("omitted-required-arrays")
	payload.ScopesSupported = nil
	payload.GrantTypesSupported = nil
	payload.ResponseTypesSupported = nil
	payload.TokenEndpointAuthMethodsSupported = nil

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ScopesSupported)
	require.Empty(t, created.ScopesSupported)
	require.NotNil(t, created.GrantTypesSupported)
	require.Empty(t, created.GrantTypesSupported)
	require.NotNil(t, created.ResponseTypesSupported)
	require.Empty(t, created.ResponseTypesSupported)
	require.NotNil(t, created.TokenEndpointAuthMethodsSupported)
	require.Empty(t, created.TokenEndpointAuthMethodsSupported)
}

func TestOrganizationAndGlobalIssuerCreationValidateProviderURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	insecure := "http://identity.example.com/token"

	organizationPayload := newCreateIssuerPayload("org-insecure-provider-url", nil)
	organizationPayload.TokenEndpoint = &insecure
	_, err := ti.service.CreateIssuer(ctx, organizationPayload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	globalPayload := createGlobalIssuer(t, "global-insecure-provider-url")
	globalPayload.JwksURI = &insecure
	_, err = ti.service.CreateGlobalIssuer(withAdmin(t, ctx), globalPayload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationIssuerCreationNormalizesOmittedRequiredArrays(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := newCreateIssuerPayload("org-omitted-required-arrays", nil)
	payload.ScopesSupported = nil
	payload.GrantTypesSupported = nil
	payload.ResponseTypesSupported = nil
	payload.TokenEndpointAuthMethodsSupported = nil

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ScopesSupported)
	require.Empty(t, created.ScopesSupported)
	require.NotNil(t, created.GrantTypesSupported)
	require.Empty(t, created.GrantTypesSupported)
	require.NotNil(t, created.ResponseTypesSupported)
	require.Empty(t, created.ResponseTypesSupported)
	require.NotNil(t, created.TokenEndpointAuthMethodsSupported)
	require.Empty(t, created.TokenEndpointAuthMethodsSupported)
}

func TestProviderUpdatesRejectInsecureEffectiveURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	insecure := "http://identity.example.com/oauth"
	projectIssuer, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("project-update-provider-urls"))
	require.NoError(t, err)
	projectUpdates := []func(*issuersgen.UpdateRemoteSessionIssuerPayload){
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.Issuer = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.AuthorizationEndpoint = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.TokenEndpoint = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.RevocationEndpoint = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.RegistrationEndpoint = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.JwksURI = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.UserinfoEndpoint = &insecure },
		func(payload *issuersgen.UpdateRemoteSessionIssuerPayload) { payload.IntrospectionEndpoint = &insecure },
	}
	for _, apply := range projectUpdates {
		payload := &issuersgen.UpdateRemoteSessionIssuerPayload{ID: projectIssuer.ID}
		apply(payload)
		_, err := ti.service.UpdateRemoteSessionIssuer(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	organizationIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("organization-update-provider-urls", nil))
	require.NoError(t, err)
	_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{
		ID:                   organizationIssuer.ID,
		RegistrationEndpoint: &insecure,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	adminCtx := withAdmin(t, ctx)
	globalIssuer, err := ti.service.CreateGlobalIssuer(adminCtx, createGlobalIssuer(t, "global-update-provider-urls"))
	require.NoError(t, err)
	_, err = ti.service.UpdateGlobalIssuer(adminCtx, &adminrsgen.UpdateGlobalIssuerPayload{
		ID:      globalIssuer.ID,
		JwksURI: &insecure,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestProviderCreateAndUpdateValidateThePersistedTrimmedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createPayload := newIssuerPayload("trimmed-project-provider")
	createPayload.Issuer = "  https://identity.example.com/create  "
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)
	require.Equal(t, "https://identity.example.com/create", created.Issuer)

	updatedIssuer := "\thttps://identity.example.com/update\n"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{
		ID:     created.ID,
		Issuer: &updatedIssuer,
	})
	require.NoError(t, err)
	require.Equal(t, "https://identity.example.com/update", updated.Issuer)
}

func TestCommitServerUserIdentityCreateProviderValidatesURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "atomic-url-validation")
	insecure := "http://identity.example.com/token"

	for _, form := range []*sessionsgen.CreateRemoteSessionIssuerForm{
		serverIdentityProviderForm("atomic-insecure-issuer", nil, false),
		serverIdentityProviderForm("atomic-insecure-token", nil, false),
	} {
		if form.Slug == "atomic-insecure-issuer" {
			form.Issuer = insecure
		} else {
			form.TokenEndpoint = &insecure
		}

		_, err := ti.service.CommitServerUserIdentityConfiguration(ctx, manualServerIdentityPayload(targetID, form))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestCommitServerUserIdentityCreateProviderAcceptsLoopbackAndNormalizesArrays(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, "atomic-loopback")
	issuer := "http://127.0.0.1:8080/oauth"
	form := serverIdentityProviderForm("atomic-loopback-provider", conv.PtrEmpty(issuer+"/register"), false)
	form.Issuer = issuer
	form.AuthorizationEndpoint = conv.PtrEmpty(issuer + "/authorize")
	form.TokenEndpoint = conv.PtrEmpty(issuer + "/token")
	form.RevocationEndpoint = conv.PtrEmpty(issuer + "/revoke")
	form.JwksURI = conv.PtrEmpty(issuer + "/jwks")
	form.UserinfoEndpoint = conv.PtrEmpty(issuer + "/userinfo")
	form.IntrospectionEndpoint = conv.PtrEmpty(issuer + "/introspect")
	form.ScopesSupported = nil
	form.GrantTypesSupported = nil
	form.ResponseTypesSupported = nil
	form.TokenEndpointAuthMethodsSupported = nil

	result, err := ti.service.CommitServerUserIdentityConfiguration(ctx, manualServerIdentityPayload(targetID, form))
	require.NoError(t, err)
	require.Equal(t, issuer, result.Provider.Issuer)
	require.NotNil(t, result.Provider.ScopesSupported)
	require.Empty(t, result.Provider.ScopesSupported)
	require.NotNil(t, result.Provider.GrantTypesSupported)
	require.Empty(t, result.Provider.GrantTypesSupported)
	require.NotNil(t, result.Provider.ResponseTypesSupported)
	require.Empty(t, result.Provider.ResponseTypesSupported)
	require.NotNil(t, result.Provider.TokenEndpointAuthMethodsSupported)
	require.Empty(t, result.Provider.TokenEndpointAuthMethodsSupported)
}

func manualServerIdentityPayload(targetID uuid.UUID, form *sessionsgen.CreateRemoteSessionIssuerForm) *sessionsgen.CommitServerUserIdentityConfigurationPayload {
	return &sessionsgen.CommitServerUserIdentityConfigurationPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      targetID.String(),
		ProviderID:       nil,
		CreateProvider:   form,
		ClientMode:       "manual",
		ExistingClientID: nil,
		ClientConfiguration: &sessionsgen.ServerUserIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("manual-client"),
			ClientSecret:            nil,
			TokenEndpointAuthMethod: conv.PtrEmpty("none"),
			Scope:                   nil,
			Audience:                nil,
		},
	}
}
