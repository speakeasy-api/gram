package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	orgremotegen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	orgusergen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeInboundEMAUsers struct {
	mu        sync.Mutex
	issuers   map[string]*types.UserSessionIssuer
	updates   []*orgusergen.UpdateIssuerPayload
	updateErr error
}

func (f *fakeInboundEMAUsers) ListIssuers(_ context.Context, payload *orgusergen.ListIssuersPayload) (*orgusergen.ListOrganizationUserSessionIssuersResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if payload.Tier == nil || *payload.Tier != "organization" {
		return nil, errors.New("inbound EMA must list the organization tier")
	}
	items := []*types.UserSessionIssuer{}
	for _, issuer := range f.issuers {
		if issuer.ProjectID == "" {
			copied := *issuer
			items = append(items, &copied)
		}
	}
	return &orgusergen.ListOrganizationUserSessionIssuersResult{Items: items}, nil
}

func (f *fakeInboundEMAUsers) GetIssuer(_ context.Context, payload *orgusergen.GetIssuerPayload) (*types.UserSessionIssuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	issuer, ok := f.issuers[payload.ID]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "user session issuer not found")
	}
	copied := *issuer
	return &copied, nil
}

func (f *fakeInboundEMAUsers) UpdateIssuer(_ context.Context, payload *orgusergen.UpdateIssuerPayload) (*types.UserSessionIssuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, payload)
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	issuer := f.issuers[payload.ID]
	if payload.TrustedRemoteSessionIssuerID != nil {
		issuer.TrustedRemoteSessionIssuerID = nil
		if *payload.TrustedRemoteSessionIssuerID != "" {
			issuer.TrustedRemoteSessionIssuerID = new(*payload.TrustedRemoteSessionIssuerID)
		}
	}
	copied := *issuer
	return &copied, nil
}

type fakeInboundEMARemote struct {
	mu       sync.Mutex
	draft    *types.RemoteSessionIssuerDraft
	fetchErr error
	records  map[string]*types.RemoteSessionIssuer
	matches  []*types.RemoteSessionIssuerDuplicateMatch
	created  []*orgremotegen.CreateIssuerPayload
}

func (f *fakeInboundEMARemote) FetchIssuerMetadata(context.Context, *orgremotegen.FetchIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.draft, nil
}

func (f *fakeInboundEMARemote) GetIssuerDuplicatePreflight(_ context.Context, payload *orgremotegen.GetIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	matches := []*types.RemoteSessionIssuerDuplicateMatch{}
	for _, match := range f.matches {
		if match.Issuer == *payload.Issuer {
			matches = append(matches, match)
		}
	}
	return &types.RemoteSessionIssuerDuplicatePreflight{Matches: matches}, nil
}

func (f *fakeInboundEMARemote) GetIssuer(_ context.Context, payload *orgremotegen.GetIssuerPayload) (*types.RemoteSessionIssuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	record, ok := f.records[payload.ID]
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "remote session issuer not found")
	}
	return record, nil
}

func (f *fakeInboundEMARemote) CreateIssuer(_ context.Context, payload *orgremotegen.CreateIssuerPayload) (*types.RemoteSessionIssuer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, payload)
	record := &types.RemoteSessionIssuer{ID: uuid.NewString(), OrganizationID: "org", Slug: payload.Slug, Issuer: payload.Issuer, Name: payload.Name, JwksURI: payload.JwksURI}
	f.records[record.ID] = record
	f.matches = append(f.matches, &types.RemoteSessionIssuerDuplicateMatch{ID: record.ID, Issuer: record.Issuer, Tier: remoteIssuerTierOrganization})
	return record, nil
}

const inboundEMATestIDP = "https://idp.example.test"

type inboundEMAFixture struct {
	principal Principal
	service   *InboundEMATrustService
	users     *fakeInboundEMAUsers
	remote    *fakeInboundEMARemote
	issuerID  string
}

func newInboundEMAFixture(t *testing.T, directoryUsers int) inboundEMAFixture {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_inbound_ema")
	require.NoError(t, err)
	principal, _ := seedRegistrationLifecycle(t, ctx, conn)
	for range directoryUsers {
		userID := "inbound-ema-" + uuid.NewString()
		email := userID + "@example.test"
		seedAccessMember(t, ctx, conn, principal.OrganizationID, userID, email)
		seedAccessDirectoryProfile(t, ctx, conn, principal.OrganizationID, userID, email, `{}`)
	}
	// A directory user who never joined the organization resolves to nobody.
	seedAccessDirectoryProfile(t, ctx, conn, principal.OrganizationID, "", "unjoined-"+uuid.NewString()+"@example.test", `{}`)
	issuerID := uuid.NewString()
	users := &fakeInboundEMAUsers{issuers: map[string]*types.UserSessionIssuer{
		issuerID: {ID: issuerID, OrganizationID: principal.OrganizationID, Slug: "workforce", AuthorizationServerMode: "shared", IssuerURL: new("https://app.example.test/oauth/usi/" + issuerID)},
	}}
	remote := &fakeInboundEMARemote{
		draft:   &types.RemoteSessionIssuerDraft{Issuer: inboundEMATestIDP, JwksURI: new(inboundEMATestIDP + "/keys"), TokenEndpoint: new(inboundEMATestIDP + "/token")},
		records: map[string]*types.RemoteSessionIssuer{},
	}
	service := NewPostgresReader(testenv.NewLogger(t), conn).WithInboundEMATrust(users, remote).inboundEMATrust
	return inboundEMAFixture{principal: principal, service: service, users: users, remote: remote, issuerID: issuerID}
}

func TestInboundEMATrustToolContracts(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "inbound-ema", Version: "1"}, nil))
	registerInboundEMATrustTools(reg, nil)
	require.Empty(t, reg.For(AudienceAssistant), "trust is organization administration by a person")
	for _, name := range []string{getInboundEMATrustToolName, trustInboundEMAProviderToolName, untrustInboundEMAProviderToolName} {
		descriptor := descriptorByName(t, reg, name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeNone, descriptor.Meta.ProjectScope)
		require.Equal(t, []authz.Scope{authz.ScopeOrgAdmin}, descriptor.Meta.DiscoveryScopes)
		arguments := map[string]any{}
		if name != getInboundEMATrustToolName {
			arguments["user_session_issuer_id"] = uuid.NewString()
			arguments["confirmed"] = true
			var schema struct {
				Required []string `json:"required"`
			}
			require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
			require.Contains(t, schema.Required, "confirmed")
			require.Contains(t, schema.Required, "user_session_issuer_id")
		}
		if name == trustInboundEMAProviderToolName {
			arguments["issuer_url"] = inboundEMATestIDP
		}
		raw, err := json.Marshal(arguments)
		require.NoError(t, err)
		_, err = descriptor.Invoke(contextWithPrincipal(t.Context(), Principal{OrganizationID: "org", UserID: "user"}), raw)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Contains(t, refusal.Payload, unavailableCode)
	}
	require.True(t, descriptorByName(t, reg, getInboundEMATrustToolName).Annotations.ReadOnlyHint)
	require.True(t, *descriptorByName(t, reg, untrustInboundEMAProviderToolName).Annotations.DestructiveHint)
}

func TestInboundEMATrustGetReportsAudienceDirectoryAndProvider(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 2)
	providerID := uuid.NewString()
	f.remote.records[providerID] = &types.RemoteSessionIssuer{ID: providerID, Issuer: inboundEMATestIDP, JwksURI: new(inboundEMATestIDP + "/keys")}
	f.users.issuers[f.issuerID].TrustedRemoteSessionIssuerID = new(providerID)
	endpointID := uuid.NewString()
	f.users.issuers[endpointID] = &types.UserSessionIssuer{ID: endpointID, Slug: "endpoint", AuthorizationServerMode: "endpoint"}

	output, err := f.service.Get(t.Context(), f.principal)
	require.NoError(t, err)
	require.Equal(t, InboundEMADirectory{ResolvableDirectoryUsers: 2, Synced: true}, output.Directory)
	require.Len(t, output.Issuers, 2)
	for _, issuer := range output.Issuers {
		switch issuer.UserSessionIssuerID {
		case f.issuerID:
			require.Equal(t, "https://app.example.test/oauth/usi/"+f.issuerID, issuer.Audience)
			require.Equal(t, &InboundEMAIdentityProvider{RemoteSessionIssuerID: providerID, IssuerURL: inboundEMATestIDP, KeySetURL: inboundEMATestIDP + "/keys", Tier: "platform"}, issuer.TrustedIdentityProvider)
		case endpointID:
			require.Empty(t, issuer.Audience)
			require.NotEmpty(t, issuer.AudienceNote)
			require.Nil(t, issuer.TrustedIdentityProvider)
		}
	}
}

func TestInboundEMATrustGetWithoutDirectorySyncSaysEveryExchangeIsRefused(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 0)
	output, err := f.service.Get(t.Context(), f.principal)
	require.NoError(t, err)
	require.False(t, output.Directory.Synced)
	require.Contains(t, output.NextStep, "every ID-JAG exchange is refused")
}

func TestInboundEMATrustPreviewChangesNothing(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	output, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP})
	require.NoError(t, err)
	require.False(t, output.Applied)
	require.True(t, output.Changed)
	require.False(t, output.IdentityProviderReused)
	require.Equal(t, inboundEMATestIDP+"/keys", output.IdentityProvider.KeySetURL)
	require.Empty(t, f.remote.created)
	require.Empty(t, f.users.updates)
}

func TestInboundEMATrustCreatesProviderThroughManagementAPI(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	output, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	require.NoError(t, err)
	require.True(t, output.Applied)
	require.False(t, output.IdentityProviderReused)
	require.Len(t, f.remote.created, 1)
	require.Equal(t, inboundEMATestIDP+"/keys", *f.remote.created[0].JwksURI)
	require.Equal(t, inboundEMAProviderSlug(inboundEMATestIDP), f.remote.created[0].Slug)
	require.Len(t, f.users.updates, 1)
	require.Equal(t, output.IdentityProvider.RemoteSessionIssuerID, *f.users.updates[0].TrustedRemoteSessionIssuerID)
	require.NotNil(t, output.Issuer.TrustedIdentityProvider, "the result is read back after the change")
	require.Equal(t, output.IdentityProvider.RemoteSessionIssuerID, output.Issuer.TrustedIdentityProvider.RemoteSessionIssuerID)
	require.Contains(t, output.NextStep, output.Issuer.Audience)

	// Repeating it reuses the created record and changes nothing.
	again, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, ExpectedTrustedRemoteSessionIssuerID: output.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, again.IdentityProviderReused)
	require.False(t, again.Changed)
	require.Len(t, f.remote.created, 1)
	require.Len(t, f.users.updates, 1)
}

func TestInboundEMATrustReusesOrganizationRecordBeforePlatformRecord(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	platformID, organizationID := uuid.NewString(), uuid.NewString()
	f.remote.records[platformID] = &types.RemoteSessionIssuer{ID: platformID, Issuer: inboundEMATestIDP, JwksURI: new(inboundEMATestIDP + "/keys")}
	f.remote.records[organizationID] = &types.RemoteSessionIssuer{ID: organizationID, OrganizationID: f.principal.OrganizationID, Issuer: inboundEMATestIDP, JwksURI: new(inboundEMATestIDP + "/keys")}
	f.remote.matches = []*types.RemoteSessionIssuerDuplicateMatch{
		{ID: uuid.NewString(), Issuer: inboundEMATestIDP, Tier: "project-specific"},
		{ID: organizationID, Issuer: inboundEMATestIDP, Tier: remoteIssuerTierOrganization},
		{ID: platformID, Issuer: inboundEMATestIDP, Tier: remoteIssuerTierPlatform},
	}
	output, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	require.NoError(t, err)
	require.True(t, output.IdentityProviderReused)
	require.Equal(t, organizationID, output.IdentityProvider.RemoteSessionIssuerID)
	require.Empty(t, f.remote.created)
}

func TestInboundEMATrustRefusesProviderWithoutKeySet(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	f.remote.draft = &types.RemoteSessionIssuerDraft{Issuer: inboundEMATestIDP}
	_, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalNoKeySet, refusal.Code)
	require.Empty(t, f.users.updates)
}

func TestInboundEMATrustRefusesReplacingFederatedSignInProvider(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	trusted := uuid.NewString()
	f.users.issuers[f.issuerID].TrustedRemoteSessionIssuerID = new(trusted)
	f.users.issuers[f.issuerID].TrustedRemoteSessionClientID = new(uuid.NewString())
	_, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, ExpectedTrustedRemoteSessionIssuerID: trusted, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalBrowserSignIn, refusal.Code)
	require.Empty(t, f.remote.created)
	require.Empty(t, f.users.updates)
}

func TestInboundEMATrustRelaysManagementValidation(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	f.users.updateErr = oops.E(oops.CodeBadRequest, nil, "inbound-only trust requires a shared authorization server")
	_, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalRejected, refusal.Code)
	require.Contains(t, refusal.Message, "shared authorization server")
}

func TestInboundEMATrustRefusesProjectIssuer(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	projectIssuer := uuid.NewString()
	f.users.issuers[projectIssuer] = &types.UserSessionIssuer{ID: projectIssuer, ProjectID: uuid.NewString(), AuthorizationServerMode: "shared"}
	_, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: projectIssuer, IssuerURL: inboundEMATestIDP, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalNotOrganizationLevel, refusal.Code)
}

func TestInboundEMAUntrustClearsLinkAfterConfirmation(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	providerID := uuid.NewString()
	f.remote.records[providerID] = &types.RemoteSessionIssuer{ID: providerID, Issuer: inboundEMATestIDP, JwksURI: new(inboundEMATestIDP + "/keys")}
	f.users.issuers[f.issuerID].TrustedRemoteSessionIssuerID = new(providerID)

	preview, err := f.service.Untrust(t.Context(), f.principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID})
	require.NoError(t, err)
	require.False(t, preview.Applied)
	require.True(t, preview.Changed)
	require.Equal(t, providerID, preview.RemovedIdentityProvider.RemoteSessionIssuerID)
	require.Empty(t, f.users.updates)

	output, err := f.service.Untrust(t.Context(), f.principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, ExpectedTrustedRemoteSessionIssuerID: preview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	require.NoError(t, err)
	require.True(t, output.Applied)
	require.Len(t, f.users.updates, 1)
	require.Empty(t, *f.users.updates[0].TrustedRemoteSessionIssuerID)
	require.Nil(t, output.Issuer.TrustedIdentityProvider)
}

func TestInboundEMAUntrustRefusesWhenSignInIsFederated(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	f.users.issuers[f.issuerID].TrustedRemoteSessionIssuerID = new(uuid.NewString())
	f.users.issuers[f.issuerID].TrustedRemoteSessionClientID = new(uuid.NewString())
	_, err := f.service.Untrust(t.Context(), f.principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalBrowserSignIn, refusal.Code)
	require.Empty(t, f.users.updates)
}

func TestInboundEMATrustRefusesConfirmationAfterTrustChanged(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	preview, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP})
	require.NoError(t, err)
	require.Empty(t, preview.ExpectedTrustedRemoteSessionIssuerID)

	// Another administrator trusts a different provider in between.
	f.users.issuers[f.issuerID].TrustedRemoteSessionIssuerID = new(uuid.NewString())
	_, err = f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, ExpectedTrustedRemoteSessionIssuerID: preview.ExpectedTrustedRemoteSessionIssuerID, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalConflict, refusal.Code)
	require.Empty(t, f.remote.created)
	require.Empty(t, f.users.updates)

	_, err = f.service.Untrust(t.Context(), f.principal, UntrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, ExpectedTrustedRemoteSessionIssuerID: uuid.NewString(), Confirmed: true})
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, inboundEMARefusalConflict, refusal.Code)
	require.Empty(t, f.users.updates)
}

func TestInboundEMATrustReusesOnlyAnExactIssuerMatch(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	slashed := uuid.NewString()
	f.remote.records[slashed] = &types.RemoteSessionIssuer{ID: slashed, OrganizationID: f.principal.OrganizationID, Issuer: inboundEMATestIDP + "/", JwksURI: new(inboundEMATestIDP + "/keys")}
	f.remote.matches = []*types.RemoteSessionIssuerDuplicateMatch{{ID: slashed, Issuer: inboundEMATestIDP, Tier: remoteIssuerTierOrganization}}
	output, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	require.NoError(t, err)
	require.False(t, output.IdentityProviderReused, "a record spelled differently would never match the ID-JAG's iss")
	require.NotEqual(t, slashed, output.IdentityProvider.RemoteSessionIssuerID)
	require.Len(t, f.remote.created, 1)
	require.Equal(t, inboundEMATestIDP, f.remote.created[0].Issuer)
}

func TestInboundEMATrustConcurrentConfirmationsCreateOneProvider(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
		})
	}
	wg.Wait()
	for _, err := range errs {
		var refusal *InboundEMATrustRefusalError
		if errors.As(err, &refusal) {
			require.Equal(t, inboundEMARefusalConflict, refusal.Code, "a later caller may only lose the expected-trust check")
			continue
		}
		require.NoError(t, err)
	}
	f.remote.mu.Lock()
	defer f.remote.mu.Unlock()
	require.Len(t, f.remote.created, 1)
}

func TestInboundEMATrustPreviewWarnsWhenIssuerIsNotShared(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	f.users.issuers[f.issuerID].AuthorizationServerMode = "endpoint"
	output, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP})
	require.NoError(t, err)
	require.False(t, output.Applied)
	require.Len(t, output.Warnings, 1)
	require.Contains(t, output.Warnings[0], "shared authorization server")
}

func TestInboundEMATrustSaysCreatedProviderWasKeptWhenTrustIsRefused(t *testing.T) {
	t.Parallel()
	f := newInboundEMAFixture(t, 1)
	f.users.updateErr = oops.E(oops.CodeBadRequest, nil, "trust refused")
	_, err := f.service.Trust(t.Context(), f.principal, TrustInboundEMAIdentityProviderInput{UserSessionIssuerID: f.issuerID, IssuerURL: inboundEMATestIDP, Confirmed: true})
	var refusal *InboundEMATrustRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Len(t, f.remote.created, 1)
	require.Contains(t, refusal.Message, "was created and kept")
}
