package identitychaining

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// chainStoreFixture is one tenant with a trusted IdP, a resource client bound
// to an upstream by a ready binding, and a human with a retained delegation.
type chainStoreFixture struct {
	db        *pgxpool.Pool
	chainer   *Chainer
	req       Request
	sel       selection
	sessionID uuid.UUID
}

func newChainStoreFixture(t *testing.T) chainStoreFixture {
	t.Helper()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "identity_chaining_store")
	require.NoError(t, err)
	fixtures := testrepo.New(db)

	org := "org-" + uuid.NewString()
	human := "user-" + uuid.NewString()
	require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: "Chaining organization", Slug: "chaining-" + uuid.NewString()[:8]}))
	project, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "Chaining project", Slug: "chaining-project", OrganizationID: org})
	require.NoError(t, err)
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: human, Email: human + "@example.test", DisplayName: "Chaining user"}))
	require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: pgtype.Text{String: human, Valid: true}}))

	idpIssuer, idpClient := uuid.New(), uuid.New()
	require.NoError(t, fixtures.SeedDelegationLoaderIssuerFixture(ctx, testrepo.SeedDelegationLoaderIssuerFixtureParams{ID: idpIssuer, OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "idp", Issuer: "https://idp.example.test", AuthorizationEndpoint: pgtype.Text{}, TokenEndpoint: pgtype.Text{}, JwksUri: pgtype.Text{}}))
	require.NoError(t, fixtures.SeedDelegationLoaderClientFixture(ctx, testrepo.SeedDelegationLoaderClientFixtureParams{ID: idpClient, OrganizationID: org, RemoteSessionIssuerID: idpIssuer, ClientID: "idp-client", Scope: []string{"openid", "email"}}))
	usi, err := usersessionsrepo.New(db).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "chaining-usi", AuthnChallengeMode: "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: idpIssuer, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: idpClient, Valid: true},
	})
	require.NoError(t, err)

	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)
	resourceIssuer, resourceClient := prepareResourceClient(t, db, enc, org, testResourceIssuer, testResourceIssuer+"/token", testResourceClient, "resource-secret")
	req := Request{OrganizationID: org, ProjectID: project, UserSessionIssuerID: usi.ID, UserID: human, UpstreamResource: "https://api.resource.example.test"}
	binding, generation := bindResource(t, db, req, resourceIssuer, resourceClient, testResource, remotesessions.PreparationStateReady)
	session, err := repo.New(db).UpsertTrustedDelegationCredential(ctx, repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: org, ClientID: idpClient, IssuerID: idpIssuer, SubjectUrn: urn.NewUserSubject(human).String(), ExpectedGeneration: 0,
		ObservationStatus: pgtype.Text{String: "assertion_only", Valid: true},
	})
	require.NoError(t, err)

	chainer := &Chainer{Governor: Governor{logger: testenv.NewLogger(t), db: db, enc: enc, now: time.Now}, challenges: nil, delegation: nil, keys: nil, locks: nil}
	return chainStoreFixture{
		db:      db,
		chainer: chainer,
		req:     req,
		sel: selection{
			bindingID: binding, generation: generation, remoteIssuerID: resourceIssuer, clientID: resourceClient,
			externalClientID: testResourceClient, issuer: testResourceIssuer, resource: testResource,
			scopes: []string{"read"}, trustedIssuerID: idpIssuer, trustedClientID: idpClient, audience: testResourceIssuer,
		},
		sessionID: session.ID,
	}
}

// prepareResourceClient registers an organization-owned resource authorization
// server and a client_secret_post client that passes identity chaining
// readiness: the issuer advertises the ID-JAG profile and the JWT bearer grant,
// and the client holds the grant and its secret.
func prepareResourceClient(t *testing.T, db *pgxpool.Pool, enc *encryption.Client, org, issuerURL, tokenEndpoint, clientID, secret string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	q := repo.New(db)
	issuer, err := q.CreateRemoteSessionIssuer(t.Context(), repo.CreateRemoteSessionIssuerParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "resource-as-" + uuid.NewString()[:8], Issuer: issuerURL,
		TokenEndpoint:                       pgtype.Text{String: tokenEndpoint, Valid: true},
		GrantTypesSupported:                 []string{oauthwire.GrantTypeJWTBearer},
		AuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
		TokenEndpointAuthMethodsSupported:   []string{oauthwire.AuthMethodClientSecretPost},
		MetadataFetchedAt:                   pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
		ScopesSupported:                     []string{}, ResponseTypesSupported: []string{}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	})
	require.NoError(t, err)
	encrypted, err := enc.Encrypt([]byte(secret))
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, RemoteSessionIssuerID: issuer.ID, ClientID: clientID,
		ClientSecretEncrypted:   pgtype.Text{String: encrypted, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretPost, Valid: true},
		Scope:                   []string{},
	})
	require.NoError(t, err)
	f := chainStoreFixture{db: db}
	f.setClientGrants(t, org, client.ID, []string{oauthwire.GrantTypeJWTBearer})
	return issuer.ID, client.ID
}

// setClientGrants records the grants a resource client is registered for.
func (f chainStoreFixture) setClientGrants(t *testing.T, org string, client uuid.UUID, grants []string) {
	t.Helper()
	_, err := repo.New(f.db).SetEMAClientGrants(t.Context(), repo.SetEMAClientGrantsParams{GrantTypes: grants, ID: client, ProjectID: uuid.NullUUID{}, OrganizationID: pgtype.Text{String: org, Valid: true}})
	require.NoError(t, err)
}

// bindResource prepares a binding for resource the way the preparation API
// does, then moves it to state with the resource client, advancing its
// generation. It returns the binding and its new generation.
func bindResource(t *testing.T, db *pgxpool.Pool, req Request, remoteIssuer, client uuid.UUID, resource, state string) (uuid.UUID, int64) {
	t.Helper()
	q := repo.New(db)
	key := repo.GetEMABindingParams{ProjectID: req.ProjectID, OrganizationID: req.OrganizationID, UserSessionIssuerID: req.UserSessionIssuerID, RemoteSessionIssuerID: remoteIssuer, Resource: resource}
	require.NoError(t, q.EnsureEMABinding(t.Context(), repo.EnsureEMABindingParams(key)))
	current, err := q.GetEMABinding(t.Context(), key)
	require.NoError(t, err)
	updated, err := q.SetEMABinding(t.Context(), repo.SetEMABindingParams{
		State: pgtype.Text{String: state, Valid: true}, RemoteSessionClientID: uuid.NullUUID{UUID: client, Valid: true},
		Generation: current.Generation + 1, GrantSource: pgtype.Text{String: remotesessions.PreparationGrantSourceAdministratorDeclared, Valid: true},
		RequestedScopes: []string{"read"}, ClaimID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ClaimedAt: pgtype.Timestamptz{},
		ID: current.ID, ProjectID: req.ProjectID, OrganizationID: req.OrganizationID, ExpectedGeneration: current.Generation,
	})
	require.NoError(t, err)
	return updated.ID, updated.Generation
}

// addSlashVariantBinding adds or updates a second binding for the same upstream
// under its no-trailing-slash resource spelling.
func (f chainStoreFixture) addSlashVariantBinding(t *testing.T, state string) {
	t.Helper()
	bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, "https://api.resource.example.test", state)
}

func (f chainStoreFixture) credential(token string) credential {
	return credential{accessToken: token, expiresAt: time.Now().Add(time.Hour), grantedScopes: []string{"read"}, refreshObserved: false, trustedSessionID: f.sessionID, trustedObtainedAt: time.Time{}}
}

func (f chainStoreFixture) credentials(t *testing.T) []repo.ListEMACredentialsFixtureRow {
	t.Helper()
	rows, err := repo.New(f.db).ListEMACredentialsFixture(t.Context(), uuid.NullUUID{UUID: f.req.ProjectID, Valid: true})
	require.NoError(t, err)
	return rows
}

// setBinding moves the fixture's binding to state as a new incarnation.
func (f chainStoreFixture) setBinding(t *testing.T, state string) {
	t.Helper()
	bindResource(t, f.db, f.req, f.sel.remoteIssuerID, f.sel.clientID, f.sel.resource, state)
}

// deleteDelegation soft deletes the human's retained delegation.
func (f chainStoreFixture) deleteDelegation(t *testing.T) {
	t.Helper()
	deleted, err := repo.New(f.db).SoftDeleteTrustedIssuerSessionFixture(t.Context(), repo.SoftDeleteTrustedIssuerSessionFixtureParams{ID: f.sessionID, OrganizationID: f.req.OrganizationID})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
}

// signIn records a fresh sign-in on the human's retained delegation.
func (f chainStoreFixture) signIn(t *testing.T, at time.Time, status string) {
	t.Helper()
	q := repo.New(f.db)
	subject := urn.NewUserSubject(f.req.UserID).String()
	current, err := q.GetTrustedDelegationCredential(t.Context(), repo.GetTrustedDelegationCredentialParams{OrganizationID: f.req.OrganizationID, ClientID: f.sel.trustedClientID, IssuerID: f.sel.trustedIssuerID, SubjectUrn: subject})
	require.NoError(t, err)
	obtained := pgtype.Timestamptz{}
	if !at.IsZero() {
		obtained = pgtype.Timestamptz{Time: at, Valid: true, InfinityModifier: pgtype.Finite}
	}
	_, err = q.UpsertTrustedDelegationCredential(t.Context(), repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: f.req.OrganizationID, ClientID: f.sel.trustedClientID, IssuerID: f.sel.trustedIssuerID, SubjectUrn: subject, ExpectedGeneration: current.CredentialGeneration.Int64,
		ObservationStatus: pgtype.Text{String: status, Valid: true}, CredentialObtainedAt: obtained,
	})
	require.NoError(t, err)
}

func TestStore_PublishesEncryptedAndReuses(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("downstream-token")))

	rows := f.credentials(t)
	require.Len(t, rows, 1)
	require.NotContains(t, rows[0].AccessTokenEncrypted.String, "downstream-token", "the access token is stored encrypted")

	token, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.True(t, ok)
	require.Equal(t, "downstream-token", token.Value())
	require.Equal(t, "[redacted chained token]", token.String())

	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("renewed-token")))
	require.Len(t, f.credentials(t), 1, "renewal replaces the live slot instead of adding one")
}

func TestStore_RebindRetiresAndErasesCredential(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("downstream-token")))
	f.setBinding(t, remotesessions.PreparationStateReady)

	_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.False(t, ok, "a credential from an older binding generation is never reused")
	rows := f.credentials(t)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted)
	require.False(t, rows[0].AccessTokenEncrypted.Valid, "retirement erases the ciphertext")
}

func TestStore_ExpiredCredentialIsNotReused(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	cred := f.credential("expiring-token")
	cred.expiresAt = time.Now().Add(30 * time.Second)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, cred))
	_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.False(t, ok, "a credential inside the expiry skew is renewed, never served")
}

func TestStore_RejectsStalePublish(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, f chainStoreFixture)
	}{
		{"rebound", func(t *testing.T, f chainStoreFixture) {
			t.Helper()
			f.setBinding(t, remotesessions.PreparationStateReady)
		}},
		{"unlinked", func(t *testing.T, f chainStoreFixture) {
			t.Helper()
			f.setBinding(t, remotesessions.PreparationStateUnlinked)
		}},
		{"delegation revoked", func(t *testing.T, f chainStoreFixture) {
			t.Helper()
			f.deleteDelegation(t)
		}},
		{"issuer trust removed", func(t *testing.T, f chainStoreFixture) {
			t.Helper()
			require.NoError(t, testrepo.New(f.db).RevokeDelegationUserIssuersFixture(t.Context(), f.req.OrganizationID))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			tc.mutate(t, f)
			err := f.chainer.publish(t.Context(), f.req, f.sel, f.credential("stale-token"))
			require.ErrorIs(t, err, errStale)
			require.Empty(t, f.credentials(t), "a stale acquisition installs nothing")
		})
	}
}

func TestStore_RejectsSubstitutedCredentialUse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(req *Request, sel *selection)
		retire bool
	}{
		{"another organization", func(req *Request, _ *selection) { req.OrganizationID = "org-other" }, false},
		{"another human", func(req *Request, _ *selection) { req.UserID = "user-other" }, false},
		{"another project", func(req *Request, _ *selection) { req.ProjectID = uuid.New() }, false},
		{"another resource", func(_ *Request, sel *selection) { sel.resource = "https://other.resource.example.test/" }, false},
		{"another delegation client", func(_ *Request, sel *selection) { sel.trustedClientID = uuid.New() }, true},
		{"another binding", func(_ *Request, sel *selection) { sel.bindingID = uuid.New() }, true},
		{"changed scopes", func(_ *Request, sel *selection) { sel.scopes = []string{"read", "write"} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("victim-token")))
			req, sel := f.req, f.sel
			tc.mutate(&req, &sel)
			_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), req, sel)
			require.False(t, ok)
			rows := f.credentials(t)
			require.Len(t, rows, 1)
			require.Equal(t, tc.retire, rows[0].Deleted, "only provenance drift within the same slot retires it")
		})
	}
}

func TestStore_RevokedDelegationMakesCredentialUnusable(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("downstream-token")))
	f.deleteDelegation(t)
	_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.False(t, ok)
	rows := f.credentials(t)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted)
	require.False(t, rows[0].AccessTokenEncrypted.Valid)
}

func TestStore_Authorize(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	sel := f.sel
	sel.trustedIssuerID, sel.trustedClientID, sel.audience = uuid.Nil, uuid.Nil, ""
	require.True(t, f.chainer.authorize(t.Context(), testenv.NewLogger(t), f.req, &sel).Succeeded())
	require.Equal(t, f.sel.trustedIssuerID, sel.trustedIssuerID)
	require.Equal(t, f.sel.trustedClientID, sel.trustedClientID)
	require.Equal(t, testResourceIssuer, sel.audience, "without a confirmed Okta audience the resource authorization server's issuer is the audience")

	nonMember := f.req
	nonMember.UserID = "user-not-a-member"
	outcome := f.chainer.authorize(t.Context(), testenv.NewLogger(t), nonMember, &sel)
	require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
	require.Equal(t, StageAuthorization, outcome.Stage)

	otherProject := f.req
	otherProject.ProjectID = uuid.New()
	require.Equal(t, ReasonConfigurationRequired, f.chainer.authorize(t.Context(), testenv.NewLogger(t), otherProject, &sel).Reason)

	require.NoError(t, testrepo.New(f.db).DisableDelegationOrganizationFixture(t.Context(), f.req.OrganizationID))
	require.Equal(t, ReasonConfigurationRequired, f.chainer.authorize(t.Context(), testenv.NewLogger(t), f.req, &sel).Reason)
}

func TestStore_AuthorityRejectsSubstitutedBinding(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	authority := authority{chainer: f.chainer, req: f.req}
	valid := remotesessions.DelegationBinding{OrganizationID: f.req.OrganizationID, IssuerID: f.sel.trustedIssuerID, ClientID: f.sel.trustedClientID, HumanID: f.req.UserID}
	require.NoError(t, authority.AuthorizeDelegation(t.Context(), valid))
	for name, b := range map[string]remotesessions.DelegationBinding{
		"another human":        {OrganizationID: valid.OrganizationID, IssuerID: valid.IssuerID, ClientID: valid.ClientID, HumanID: "user-other"},
		"another organization": {OrganizationID: "org-other", IssuerID: valid.IssuerID, ClientID: valid.ClientID, HumanID: valid.HumanID},
		"another client":       {OrganizationID: valid.OrganizationID, IssuerID: valid.IssuerID, ClientID: uuid.New(), HumanID: valid.HumanID},
		"another issuer":       {OrganizationID: valid.OrganizationID, IssuerID: uuid.New(), ClientID: valid.ClientID, HumanID: valid.HumanID},
	} {
		require.ErrorIs(t, authority.AuthorizeDelegation(t.Context(), b), remotesessions.ErrDelegationConfiguration, name)
	}
}

func TestStore_SelectBinding(t *testing.T) {
	t.Parallel()
	t.Run("no binding keeps interactive", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		req := f.req
		req.UpstreamResource = "https://unbound.example.test"
		_, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), req)
		require.Equal(t, notApplicable, outcome)
		require.False(t, outcome.Applicable())
	})
	t.Run("unlinked binding opts out", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.setBinding(t, remotesessions.PreparationStateUnlinked)
		_, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), f.req)
		require.Equal(t, notApplicable, outcome)
		require.False(t, outcome.Applicable())
	})
	t.Run("ambiguous bindings require configuration", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.addSlashVariantBinding(t, remotesessions.PreparationStateReady)
		_, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), f.req)
		require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
		require.True(t, outcome.Applicable())
	})
	t.Run("leftover bindings do not conflict", func(t *testing.T) {
		t.Parallel()
		for _, state := range []string{remotesessions.PreparationStateUnlinked, remotesessions.PreparationStateConfigurationRequired} {
			f := newChainStoreFixture(t)
			f.addSlashVariantBinding(t, state)
			sel, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), f.req)
			require.Equal(t, success, outcome, "the sole ready binding is selected (%s leftover)", state)
			require.Equal(t, f.sel.bindingID, sel.bindingID)
		}
	})
}

func TestStore_GovernsOnlyBoundUpstreams(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.True(t, f.chainer.Governs(t.Context(), f.req), "a single ready binding governs the upstream")

	unbound := f.req
	unbound.UpstreamResource = "https://unbound.example.test"
	require.False(t, f.chainer.Governs(t.Context(), unbound), "an upstream without a binding keeps the strict gate")

	f.addSlashVariantBinding(t, remotesessions.PreparationStateUnlinked)
	require.True(t, f.chainer.Governs(t.Context(), f.req), "an unlinked leftover binding does not displace the ready one")

	f.addSlashVariantBinding(t, remotesessions.PreparationStateReady)
	require.True(t, f.chainer.Governs(t.Context(), f.req), "ambiguous ready bindings still claim the upstream")
}

func TestStore_BindingFailingReadinessDoesNotGovern(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.setClientGrants(t, f.req.OrganizationID, f.sel.clientID, []string{oauthwire.GrantTypeAuthorizationCode})
	_, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), f.req)
	require.Equal(t, bindingNotReady, outcome)
	require.False(t, f.chainer.Governs(t.Context(), f.req), "a binding whose client lost the JWT bearer grant keeps the strict gate")
}

func TestStore_SignInReplacesEarlierCredential(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("revoked-upstream")))
	_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.True(t, ok)

	f.signIn(t, time.Now().Add(time.Minute), "assertion_only")
	_, ok = f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.False(t, ok, "a credential from before the latest sign-in is never reused")
	rows := f.credentials(t)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted)
	require.False(t, rows[0].AccessTokenEncrypted.Valid)
}

func TestStore_RetireSparesConcurrentPublish(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("first-token")))
	judged := f.credentials(t)[0]
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("fresh-token")))

	require.NoError(t, repo.New(f.db).RetireEMACredential(t.Context(), repo.RetireEMACredentialParams{
		ID: judged.ID, OrganizationID: pgtype.Text{String: f.req.OrganizationID, Valid: true}, ProjectID: uuid.NullUUID{UUID: f.req.ProjectID, Valid: true}, ExpectedUpdatedAt: judged.UpdatedAt,
	}))
	token, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.True(t, ok, "retiring a stale read never erases a newer publish into the same slot")
	require.Equal(t, "fresh-token", token.Value())
}

func TestStore_WaitersAdoptHolderOutcome(t *testing.T) {
	t.Parallel()
	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	t.Run("holder failure", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.chainer.locks = cache.NewRedisCacheAdapter(client)
		failure := newOutcome(StageDelegation, ReasonReauthenticationRequired, ConfidenceVerified, false)
		key := cacheKey("identityChainingAttempt", f.req, f.sel)
		require.NoError(t, f.chainer.locks.Set(t.Context(), key, attemptResult{Outcome: failure, FinishedAt: time.Now().Add(time.Second)}, time.Minute))
		_, outcome := f.chainer.awaitConcurrentAcquisition(t.Context(), testenv.NewLogger(t), f.req, f.sel)
		require.Equal(t, failure, outcome, "a waiter reports the holder's failure instead of timing out")
	})
	t.Run("holder credential", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		f.chainer.locks = cache.NewRedisCacheAdapter(client)
		stale := newOutcome(StageExchange, ReasonInvalidTarget, ConfidenceInferred, false)
		key := cacheKey("identityChainingAttempt", f.req, f.sel)
		require.NoError(t, f.chainer.locks.Set(t.Context(), key, attemptResult{Outcome: stale, FinishedAt: time.Now().Add(-time.Hour)}, time.Minute))
		require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("holder-token")))
		token, outcome := f.chainer.awaitConcurrentAcquisition(t.Context(), testenv.NewLogger(t), f.req, f.sel)
		require.True(t, outcome.Succeeded(), "an earlier attempt's failure is never adopted")
		require.Equal(t, "holder-token", token.Value())
	})
}

func TestCacheKey_RebindChangesIdentity(t *testing.T) {
	t.Parallel()
	req := Request{OrganizationID: "org", ProjectID: uuid.New(), UserSessionIssuerID: uuid.New(), UserID: "user", UpstreamResource: testResource}
	sel := testSelection()
	rebound := sel
	rebound.generation++
	require.NotEqual(t, cacheKey("identityChainingFailure", req, sel), cacheKey("identityChainingFailure", req, rebound))
}

func TestStore_RevocationInvalidatesCachedAndPendingTokens(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	cred := f.credential("downstream-token")
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, cred))
	affected, err := repo.New(f.db).RevokeTrustedDelegationCredential(t.Context(), repo.RevokeTrustedDelegationCredentialParams{
		OrganizationID: f.req.OrganizationID, ClientID: f.sel.trustedClientID, IssuerID: f.sel.trustedIssuerID, SubjectUrn: urn.NewUserSubject(f.req.UserID).String(),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
	_, ok := f.chainer.cachedToken(t.Context(), testenv.NewLogger(t), f.req, f.sel)
	require.False(t, ok, "revocation leaves the session row live but must prevent credential reuse")
	require.ErrorIs(t, f.chainer.publish(t.Context(), f.req, f.sel, cred), errStale)
	rows := f.credentials(t)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Deleted)
	require.False(t, rows[0].AccessTokenEncrypted.Valid)
}

func TestStore_FreshSignInRejectsPendingToken(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	cred := f.credential("old-sign-in-token")
	signedIn := time.Now().UTC().Truncate(time.Microsecond)
	f.signIn(t, signedIn, "assertion_only")
	require.ErrorIs(t, f.chainer.publish(t.Context(), f.req, f.sel, cred), errStale)
	require.Empty(t, f.credentials(t))
	cred.trustedObtainedAt = signedIn
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, cred), "a token acquired under the latest sign-in may publish")
}

func TestStore_RoutineDelegationWriteKeepsPendingToken(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.signIn(t, time.Time{}, "durable_credential_present")
	require.NoError(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("pending-token")), "a delegation write without a new sign-in does not discard the exchange")
}

func TestStore_DisabledOrganizationRejectsPendingToken(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, testrepo.New(f.db).DisableDelegationOrganizationFixture(t.Context(), f.req.OrganizationID))
	require.ErrorIs(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("stale-token")), errStale)
	require.Empty(t, f.credentials(t))
}

func TestStore_RemovedMemberRejectsPendingToken(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	require.NoError(t, testrepo.New(f.db).ForceSoftDeleteOrganizationUserRelationshipsFixture(t.Context(), f.req.OrganizationID))
	require.ErrorIs(t, f.chainer.publish(t.Context(), f.req, f.sel, f.credential("stale-token")), errStale)
	require.Empty(t, f.credentials(t))
}

func TestStore_SelectBindingRestrictedToTunnelIssuer(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	other := f.req
	other.RemoteSessionIssuerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	_, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), other)
	require.Equal(t, notApplicable, outcome, "a tunnel claiming the resource of another issuer's binding selects nothing")

	own := f.req
	own.RemoteSessionIssuerID = uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}
	sel, outcome := f.chainer.selectBinding(t.Context(), testenv.NewLogger(t), own)
	require.Equal(t, success, outcome)
	require.Equal(t, f.sel.bindingID, sel.bindingID)
}
