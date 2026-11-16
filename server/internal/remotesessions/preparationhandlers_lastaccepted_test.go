package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func plantEMACredential(t *testing.T, ctx context.Context, ti *testInstance, in remotesessions.PreparationInput, provider remotesessions.EMAProviderKind, updatedAt time.Time, retired bool, internalIssuer ...uuid.UUID) {
	t.Helper()
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	signer := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if len(internalIssuer) > 0 {
		signer = conv.ToNullUUID(internalIssuer[0])
	}
	_, err := repo.New(ti.conn).PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
		OrganizationID: conv.ToPGText(auth.ActiveOrganizationID), ProjectID: conv.ToNullUUID(*auth.ProjectID),
		UserSessionIssuerID: conv.ToNullUUID(in.UserSessionIssuerID), RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID),
		RemoteSessionClientID: conv.ToNullUUID(in.ClientID), Resource: in.Resource, SubjectUrn: urn.NewUserSubject("user-" + uuid.NewString()[:8]).String(),
		EmaProvider: string(provider), Retired: retired, UpdatedAt: conv.ToPGTimestamptz(updatedAt),
		InternalSessionIssuerID: signer, TrustedIssuerSessionID: uuid.NullUUID{}, BindingGeneration: pgtype.Int8{},
	})
	require.NoError(t, err)
}

func TestReadEMA_ReportsVendorAndBindingAcceptance(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.SetSpeakeasySigningAvailable(true)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, auth.ActiveOrganizationID)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)
	q := repo.New(ti.conn)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: uuid.NullUUID{}, InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), Position: 0}))
	prepare := func(resource string) {
		_, err := ti.service.PrepareEMA(ctx, &clientsgen.PrepareEMAPayload{
			UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(),
			ClientID: conv.PtrEmpty(in.ClientID.String()), Resource: resource, Scopes: in.Scopes, Mechanism: "manual",
			ConfirmGrants: []string{oauthwire.GrantTypeJWTBearer},
		})
		require.NoError(t, err)
	}
	prepare(in.Resource)
	other := in
	other.Resource = "https://other.example.com/"
	prepare(other.Resource)
	read := func(resource string) *clientsgen.IdentityChainingPreparation {
		out, err := ti.service.ReadEMA(ctx, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: resource})
		require.NoError(t, err)
		require.NotNil(t, out.EmaProviders)
		return out
	}

	first := read(in.Resource)
	require.Nil(t, first.LastAcceptedAt)
	require.Nil(t, first.EmaProviders.Providers[0].LastAcceptedAt)

	older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	newest := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, older, false, internalIssuer)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, newest, true, internalIssuer)
	plantEMACredential(t, ctx, ti, other, remotesessions.EMAProviderSpeakeasy, time.Now().UTC(), false)

	first = read(in.Resource)
	require.Nil(t, first.LastAcceptedAt, "this binding has never been accepted")
	require.NotNil(t, first.EmaProviders.Providers[0].LastAcceptedAt)
	require.Equal(t, older.Format(time.RFC3339), *first.EmaProviders.Providers[0].LastAcceptedAt,
		"the vendor trusts this Speakeasy issuer because another server's assertion was accepted; only a live, attributed credential counts")

	second := read(other.Resource)
	require.NotNil(t, second.LastAcceptedAt)
	require.Equal(t, older.Format(time.RFC3339), *second.LastAcceptedAt)

	projectOnly := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, auth.ProjectID.String()))
	scoped, err := ti.service.ReadEMA(projectOnly, &clientsgen.ReadEMAPayload{UserSessionIssuerID: in.UserSessionIssuerID.String(), RemoteSessionIssuerID: in.RemoteSessionIssuerID.String(), Resource: other.Resource})
	require.NoError(t, err)
	require.NotNil(t, scoped.EmaProviders)
	require.Nil(t, scoped.EmaProviders.Providers[0].LastAcceptedAt, "organization-wide acceptance needs org:read")
	require.Equal(t, older.Format(time.RFC3339), conv.PtrValOr(scoped.LastAcceptedAt, ""), "the binding's own acceptance needs only project:read")

	named := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(auth.ActiveOrganizationID), "idp-org-"+uuid.NewString()[:8], "https://idp.example.com/")
	issuer, err := ti.service.GetIssuer(ctx, &orgissuersgen.GetIssuerPayload{SessionToken: nil, ApikeyToken: nil, ID: named.String()})
	require.NoError(t, err)
	require.NotNil(t, issuer.EmaProviders)
	require.Equal(t, older.Format(time.RFC3339), conv.PtrValOr(issuer.EmaProviders.Providers[0].LastAcceptedAt, ""), "the issuer view stamps the same organization-wide acceptance")

	// Moving the vendor to a provider that has never been accepted, and that
	// this sign-in can't use, leaves the binding unproven.
	external := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, auth.ActiveOrganizationID, "idp-"+uuid.NewString()[:8])
	_, err = q.DeleteEMAProviders(ctx, repo.DeleteEMAProvidersParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor})
	require.NoError(t, err)
	require.NoError(t, q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: auth.ActiveOrganizationID, VendorIssuer: vendor, RemoteSessionIssuerID: conv.ToNullUUID(external), InternalSessionIssuerID: uuid.NullUUID{}, Position: 0}))
	require.Nil(t, read(other.Resource).LastAcceptedAt, "acceptance from a provider the order no longer tries proves nothing")
}

func TestEMAAcceptance_IgnoresCredentialsVouchedThroughDeletedDelegations(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := auth.ActiveOrganizationID
	q := repo.New(ti.conn)
	prepareEMABinding(t, ctx, ti, in)

	plantVouched := func() uuid.UUID {
		t.Helper()
		subject := urn.NewUserSubject("user-" + uuid.NewString()[:8]).String()
		session, err := q.PlantTrustedIssuerSessionFixture(ctx, repo.PlantTrustedIssuerSessionFixtureParams{OrganizationID: org, RemoteSessionClientID: in.ClientID, SubjectUrn: subject})
		require.NoError(t, err)
		_, err = q.PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
			OrganizationID: conv.ToPGText(org), ProjectID: conv.ToNullUUID(*auth.ProjectID),
			UserSessionIssuerID: conv.ToNullUUID(in.UserSessionIssuerID), RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID),
			RemoteSessionClientID: conv.ToNullUUID(in.ClientID), Resource: in.Resource, SubjectUrn: subject,
			EmaProvider: string(remotesessions.EMAProviderExternal), Retired: false, UpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
			InternalSessionIssuerID: uuid.NullUUID{}, TrustedIssuerSessionID: conv.ToNullUUID(session), BindingGeneration: pgtype.Int8{},
		})
		require.NoError(t, err)
		return session
	}
	// attributed reports, per acceptance query, whether any external
	// acceptance is still attributed to a vouching issuer.
	attributed := func() []bool {
		t.Helper()
		binding, err := q.ListEMABindingAcceptance(ctx, repo.ListEMABindingAcceptanceParams{
			OrganizationID: org, ProjectID: *auth.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID,
			RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
		})
		require.NoError(t, err)
		organization, err := q.ListEMABindingAcceptanceForOrganization(ctx, org)
		require.NoError(t, err)
		vendor, err := q.ListEMAVendorAcceptance(ctx, org)
		require.NoError(t, err)
		out := []bool{false, false, false}
		for _, row := range binding {
			out[0] = out[0] || row.ExternalIssuerID.Valid
		}
		for _, row := range organization {
			out[1] = out[1] || row.ExternalIssuerID.Valid
		}
		for _, row := range vendor {
			out[2] = out[2] || row.ExternalIssuerID.Valid
		}
		return out
	}

	session := plantVouched()
	require.Equal(t, []bool{true, true, true}, attributed())

	deleted, err := q.SoftDeleteTrustedIssuerSessionFixture(ctx, repo.SoftDeleteTrustedIssuerSessionFixtureParams{ID: session, OrganizationID: org})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.Equal(t, []bool{false, false, false}, attributed(), "a revoked delegation vouches for nothing")

	plantVouched()
	require.Equal(t, []bool{true, true, true}, attributed())
	deleted, err = q.SoftDeleteRemoteSessionClientFixture(ctx, in.ClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.Equal(t, []bool{false, false, false}, attributed(), "a deleted client vouches for nothing")
}

func TestEMAAcceptance_CountsOnlyTheBindingsCurrentGenerationClientAndLiveProject(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := auth.ActiveOrganizationID
	q := repo.New(ti.conn)
	internalIssuer := seedInternalSessionIssuer(t, ctx, ti, org)
	prepareEMABinding(t, ctx, ti, in)
	binding, err := q.GetEMABinding(ctx, repo.GetEMABindingParams{ProjectID: *auth.ProjectID, OrganizationID: org, UserSessionIssuerID: in.UserSessionIssuerID, RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource})
	require.NoError(t, err)
	vendor, err := remotesessions.NormalizeVendorIssuer("https://idp.example.com")
	require.NoError(t, err)

	plant := func(client uuid.UUID, generation pgtype.Int8) {
		t.Helper()
		_, err := q.PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
			OrganizationID: conv.ToPGText(org), ProjectID: conv.ToNullUUID(*auth.ProjectID),
			UserSessionIssuerID: conv.ToNullUUID(in.UserSessionIssuerID), RemoteSessionIssuerID: conv.ToNullUUID(in.RemoteSessionIssuerID),
			RemoteSessionClientID: conv.ToNullUUID(client), Resource: in.Resource, SubjectUrn: urn.NewUserSubject("user-" + uuid.NewString()[:8]).String(),
			BindingGeneration: generation, EmaProvider: string(remotesessions.EMAProviderSpeakeasy),
			InternalSessionIssuerID: conv.ToNullUUID(internalIssuer), TrustedIssuerSessionID: uuid.NullUUID{},
			Retired: false, UpdatedAt: conv.ToPGTimestamptz(time.Now().UTC()),
		})
		require.NoError(t, err)
	}
	// counted reports how many acceptance rows each query reads.
	counted := func() []int {
		t.Helper()
		bindingRows, err := q.ListEMABindingAcceptance(ctx, repo.ListEMABindingAcceptanceParams{
			OrganizationID: org, ProjectID: *auth.ProjectID, UserSessionIssuerID: in.UserSessionIssuerID,
			RemoteSessionIssuerID: in.RemoteSessionIssuerID, Resource: in.Resource,
		})
		require.NoError(t, err)
		organizationRows, err := q.ListEMABindingAcceptanceForOrganization(ctx, org)
		require.NoError(t, err)
		vendorRows, err := q.ListEMAVendorAcceptance(ctx, org)
		require.NoError(t, err)
		accepted, err := remotesessions.VendorEMAAcceptance(ctx, q, org, vendor)
		require.NoError(t, err)
		return []int{len(bindingRows), len(organizationRows), len(vendorRows), len(accepted)}
	}

	plant(in.ClientID, pgtype.Int8{Int64: binding.Generation - 1, Valid: true})
	require.Equal(t, []int{0, 0, 0, 0}, counted(), "a credential from an earlier binding generation proves nothing about this one")

	other := preparationManualClient(t, ctx, ti, *auth.ProjectID, in.RemoteSessionIssuerID, "other-client")
	plant(other, pgtype.Int8{})
	require.Equal(t, []int{0, 0, 0, 0}, counted(), "a credential obtained through another client proves nothing about the bound one")

	plant(in.ClientID, pgtype.Int8{})
	require.Equal(t, []int{1, 1, 1, 1}, counted(), "the current generation's credential counts")

	respelled, err := q.SetRemoteSessionIssuerURLFixture(ctx, repo.SetRemoteSessionIssuerURLFixtureParams{Issuer: "https://IDP.Example.com", ID: in.RemoteSessionIssuerID, ProjectID: *auth.ProjectID})
	require.NoError(t, err)
	require.EqualValues(t, 1, respelled)
	require.Equal(t, []int{1, 1, 1, 1}, counted(), "a host-case spelling of the vendor issuer still counts")

	_, err = projectsrepo.New(ti.conn).DeleteProject(ctx, *auth.ProjectID)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0, 0, 0}, counted(), "a deleted project's credentials count nowhere")
}
