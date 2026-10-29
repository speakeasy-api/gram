package identitychaining

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGovernor_ServesOnlyAnUnambiguousBinding(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)

	issuer, ok := g.Serves(t.Context(), f.req)
	require.True(t, ok)
	require.Equal(t, f.sel.remoteIssuerID, issuer)

	unbound := f.req
	unbound.UpstreamResource = "https://unbound.example.test"
	_, ok = g.Serves(t.Context(), unbound)
	require.False(t, ok)

	f.addAmbiguousBinding(t)
	_, ok = g.Serves(t.Context(), f.req)
	require.False(t, ok, "ambiguous bindings require configuration and are never presented as served")
	require.True(t, g.Governs(t.Context(), f.req), "the runtime still claims an ambiguous upstream")
}

func TestGovernor_Configured(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)

	require.True(t, g.Configured(t.Context(), f.req.OrganizationID, f.req.ProjectID, f.req.UserSessionIssuerID))
	require.False(t, g.Configured(t.Context(), f.req.OrganizationID, f.req.ProjectID, uuid.New()))
	require.False(t, g.Configured(t.Context(), f.req.OrganizationID, uuid.New(), f.req.UserSessionIssuerID))
}

func TestGovernor_HasUsableCredential(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.False(t, g.HasUsableCredential(t.Context(), f.req), "no credential stored yet")

	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("downstream-token")))
	require.True(t, g.HasUsableCredential(t.Context(), f.req))

	rotated, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x24}, 32))
	require.NoError(t, err)
	require.False(t, NewGovernor(testenv.NewLogger(t), f.db, rotated, f.chainer.delegation).HasUsableCredential(t.Context(), f.req), "a credential the runtime cannot decrypt is unusable")

	other := f.req
	other.UserID = "user-other"
	require.False(t, g.HasUsableCredential(t.Context(), other))

	f.deleteDelegation(t)
	require.False(t, g.HasUsableCredential(t.Context(), f.req))
	require.Len(t, f.credentials(t), 1)
	require.False(t, f.credentials(t)[0].Deleted, "a status read never retires a credential")
}

// retainRefreshableDelegation records a retained delegation whose assertion has
// expired but whose refresh grant can renew it without a fresh sign-in.
func (f chainStoreFixture) retainRefreshableDelegation(t *testing.T, status string) {
	t.Helper()
	q := repo.New(f.db)
	subject := urn.NewUserSubject(f.req.UserID).String()
	current, err := q.GetTrustedDelegationCredential(t.Context(), repo.GetTrustedDelegationCredentialParams{OrganizationID: f.req.OrganizationID, ClientID: f.sel.trustedClientID, IssuerID: f.sel.trustedIssuerID, SubjectUrn: subject})
	require.NoError(t, err)
	sealed := func(plain string) pgtype.Text {
		ciphertext, err := f.chainer.enc.Encrypt([]byte(plain))
		require.NoError(t, err)
		return pgtype.Text{String: ciphertext, Valid: true}
	}
	refresh := pgtype.Text{String: "", Valid: false}
	if status == "durable_credential_present" {
		refresh = sealed("refresh")
	}
	_, err = q.UpsertTrustedDelegationCredential(t.Context(), repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: f.req.OrganizationID, ClientID: f.sel.trustedClientID, IssuerID: f.sel.trustedIssuerID, SubjectUrn: subject, ExpectedGeneration: current.CredentialGeneration.Int64,
		CredentialConfigHash:       pgtype.Text{String: f.configHash, Valid: true},
		ObservationStatus:          pgtype.Text{String: status, Valid: true},
		IdentityAssertionEncrypted: sealed("assertion"),
		IdentityAssertionExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true, InfinityModifier: pgtype.Finite},
		RefreshTokenEncrypted:      refresh,
		UpstreamSubjectEncrypted:   sealed("upstream-subject"),
	})
	require.NoError(t, err)
}

func TestGovernor_RefreshDecisionFollowsDelegation(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	external := remotesessions.EMAProviderExternal

	require.Equal(t, RefreshVerdict{Decision: RefreshReauthenticate, RemoteSessionIssuerID: f.sel.remoteIssuerID, Provider: external, Reason: ReasonReauthenticationRequired}, g.RefreshDecision(t.Context(), f.req),
		"a delegation with neither a live assertion nor a refresh grant needs a fresh sign-in")

	f.retainRefreshableDelegation(t, "durable_credential_present")
	require.Equal(t, RefreshVerdict{Decision: RefreshServed, RemoteSessionIssuerID: f.sel.remoteIssuerID, Provider: external, Reason: ReasonSuccess}, g.RefreshDecision(t.Context(), f.req),
		"a refresh grant renews the delegation without a fresh sign-in")

	unbound := f.req
	unbound.UpstreamResource = "https://unbound.example.test"
	require.Equal(t, RefreshNotChained, g.RefreshDecision(t.Context(), unbound).Decision)

	other := f.req
	other.UserID = "user-other"
	require.Equal(t, RefreshUnconfirmed, g.RefreshDecision(t.Context(), other).Decision, "a non-member is forbidden at runtime, which no reconnect repairs")

	f.deleteDelegation(t)
	require.Equal(t, RefreshReauthenticate, g.RefreshDecision(t.Context(), f.req).Decision, "a missing delegation needs a fresh sign-in")
}

func TestGovernor_RefreshDecisionNeedsReauthenticationRecordedOnTheDelegation(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	f.retainRefreshableDelegation(t, "reauthentication_required")
	require.True(t, f.chainer.checkDelegation(t.Context(), f.req, f.sel).Succeeded(), "the registration itself is still current")
	require.Equal(t, RefreshReauthenticate, g.RefreshDecision(t.Context(), f.req).Decision, "a delegation that recorded reauthentication cannot renew")
}

func TestGovernor_RefreshDecisionWithoutAServingProvider(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	f.retainRefreshableDelegation(t, "durable_credential_present")
	fixtures := testrepo.New(f.db)
	keys, err := fixtures.SeedJsonWebKeySetFixture(t.Context(), testrepo.SeedJsonWebKeySetFixtureParams{OrganizationID: f.req.OrganizationID, Name: "speakeasy-issuer-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	internal, err := fixtures.SeedInternalSessionIssuerFixture(t.Context(), testrepo.SeedInternalSessionIssuerFixtureParams{OrganizationID: f.req.OrganizationID, Issuer: "https://speakeasy.example.test/oauth/idp/org/" + f.req.OrganizationID, JsonWebKeySetID: keys})
	require.NoError(t, err)
	vendor, err := remotesessions.NormalizeVendorIssuer(testResourceIssuer)
	require.NoError(t, err)
	// Only the Speakeasy provider, which this governor cannot sign with.
	require.NoError(t, repo.New(f.db).InsertEMAProvider(t.Context(), repo.InsertEMAProviderParams{
		OrganizationID: f.req.OrganizationID, VendorIssuer: vendor,
		RemoteSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		InternalSessionIssuerID: uuid.NullUUID{UUID: internal, Valid: true},
		Position:                0,
	}))

	verdict := g.RefreshDecision(t.Context(), f.req)
	require.Equal(t, RefreshNotChained, verdict.Decision, "a ready binding whose order cannot serve leaves the upstream on the interactive path")
	require.Equal(t, ReasonProviderUnavailable, verdict.Reason)
	require.Equal(t, remotesessions.EMAProviderSpeakeasy, verdict.Provider)
}

func TestGovernor_RefreshDecisionTrustedRegistrationRotated(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	f.retainRefreshableDelegation(t, "durable_credential_present")
	require.Equal(t, RefreshServed, g.RefreshDecision(t.Context(), f.req).Decision)

	rotated, err := f.chainer.enc.Encrypt([]byte("rotated-secret"))
	require.NoError(t, err)
	_, err = repo.New(f.db).UpdateOrganizationRemoteSessionClient(t.Context(), repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted: pgtype.Text{String: rotated, Valid: true}, ID: f.sel.trustedClientID, OrganizationID: pgtype.Text{String: f.req.OrganizationID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, RefreshVerdict{Decision: RefreshReauthenticate, RemoteSessionIssuerID: f.sel.remoteIssuerID, Provider: remotesessions.EMAProviderExternal, Reason: ReasonReauthenticationRequired}, g.RefreshDecision(t.Context(), f.req),
		"a delegation retained under the previous registration is replaced by the next sign-in")
}

func TestGovernor_RefreshDecisionUncertainty(t *testing.T) {
	t.Parallel()
	t.Run("discovery transiently failed", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		g.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
			t.Error("refresh must never probe the upstream")
			return protectedresource.EndpointResource{}, errors.New("unexpected probe")
		})
		g.SetCachedResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
			return protectedresource.EndpointResource{}, true, errors.New("endpoint resource discovery temporarily unavailable")
		})
		verdict := g.RefreshDecision(t.Context(), f.req)
		require.Equal(t, RefreshUnconfirmed, verdict.Decision)
		require.Equal(t, ReasonTransientFailure, verdict.Reason)
	})
	t.Run("nothing cached", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		g.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
			t.Error("refresh must never probe the upstream")
			return protectedresource.EndpointResource{}, errors.New("unexpected probe")
		})
		alias := f.req
		alias.UpstreamResource = "https://api.resource.example.test/mcp"
		require.Equal(t, RefreshUnconfirmed, g.RefreshDecision(t.Context(), alias).Decision, "an alias on a bound origin is unknown until discovery is cached")
		require.Equal(t, RefreshUnconfirmed, g.RefreshDecision(t.Context(), f.req).Decision, "even a binding naming the transport URL waits for discovery")
	})
	t.Run("ambiguous bindings", func(t *testing.T) {
		t.Parallel()
		f := newChainStoreFixture(t)
		g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
		f.addAmbiguousBinding(t)
		verdict := g.RefreshDecision(t.Context(), f.req)
		require.Equal(t, RefreshUnconfirmed, verdict.Decision, "the runtime forbids an ambiguous upstream, which no reconnect repairs")
		require.Equal(t, ReasonConfigurationRequired, verdict.Reason)
	})
}

func TestGovernor_TrustedRegistrationRotationMakesCredentialUnusable(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("downstream-token")))
	require.True(t, g.HasUsableCredential(t.Context(), f.req))

	rotated, err := f.chainer.enc.Encrypt([]byte("rotated-secret"))
	require.NoError(t, err)
	_, err = repo.New(f.db).UpdateOrganizationRemoteSessionClient(t.Context(), repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted: pgtype.Text{String: rotated, Valid: true}, ID: f.sel.trustedClientID, OrganizationID: pgtype.Text{String: f.req.OrganizationID, Valid: true},
	})
	require.NoError(t, err)
	require.False(t, g.HasUsableCredential(t.Context(), f.req), "an unexpired token minted under the previous trusted registration is not usable")
	require.False(t, NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, nil).HasUsableCredential(t.Context(), f.req), "without a delegation check nothing is usable")
}

func TestNewRequest(t *testing.T) {
	t.Parallel()
	project, usi, tunnel := uuid.New(), uuid.New(), uuid.New()

	direct, ok := NewRequest("org", project, usi, "user", "https://up.example.test/mcp", false, uuid.NullUUID{UUID: tunnel, Valid: true})
	require.True(t, ok)
	require.False(t, direct.RemoteSessionIssuerID.Valid, "a direct upstream selects by resource alone")

	tunneled, ok := NewRequest("org", project, usi, "user", "urn:tunnel", true, uuid.NullUUID{UUID: tunnel, Valid: true})
	require.True(t, ok)
	require.Equal(t, uuid.NullUUID{UUID: tunnel, Valid: true}, tunneled.RemoteSessionIssuerID)

	_, ok = NewRequest("org", project, usi, "user", "urn:tunnel", true, uuid.NullUUID{})
	require.False(t, ok, "a tunnel without a derived issuer never chains")
	_, ok = NewRequest("org", project, usi, "", "https://up.example.test/mcp", false, uuid.NullUUID{})
	require.False(t, ok)
	_, ok = NewRequest("org", project, usi, "user", "/", false, uuid.NullUUID{})
	require.False(t, ok)
}

func TestGovernor_EmptyCredentialIsUnusable(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("")))
	require.False(t, g.HasUsableCredential(t.Context(), f.req), "an empty decrypted token is unusable and must not panic")
}

func TestInspectIdentityChainingForTenant(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)

	results, err := remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}, f.req.UpstreamResource, true)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, remotesessions.PreparationStateReady, results[0].State)
	require.Equal(t, f.sel.bindingID, results[0].BindingID)
	require.Equal(t, []string{"read"}, results[0].Scopes)

	f.addSlashVariantBinding(t, remotesessions.PreparationStateUnknownGrants)
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}, f.req.UpstreamResource, true)
	require.NoError(t, err)
	require.Len(t, results, 1, "inspection excludes a different exact resource even in a live state")

	for range 5 {
		issuerURL := "https://resource-" + uuid.NewString() + ".example.test"
		issuer, client := prepareResourceClient(t, f.db, f.chainer.enc, f.req.OrganizationID, issuerURL, issuerURL+"/token", "inspection-client", "inspection-secret")
		bindResource(t, f.db, f.req, issuer, client, f.sel.resource, remotesessions.PreparationStateUnknownGrants)
	}
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, f.req.UpstreamResource, true)
	require.NoError(t, err)
	require.Len(t, results, 6, "inspection never truncates exact-resource bindings in any live state")

	for _, foreign := range []struct {
		project uuid.UUID
		org     string
	}{{uuid.New(), f.req.OrganizationID}, {f.req.ProjectID, "org-" + uuid.NewString()}} {
		results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, foreign.project, foreign.org, f.req.UserSessionIssuerID, uuid.NullUUID{UUID: f.sel.remoteIssuerID, Valid: true}, f.req.UpstreamResource, true)
		require.NoError(t, err)
		require.Empty(t, results, "another tenant never reads these bindings")
	}
	results, err = remotesessions.InspectIdentityChainingForTenant(t.Context(), f.db, f.req.ProjectID, f.req.OrganizationID, f.req.UserSessionIssuerID, uuid.NullUUID{UUID: uuid.New(), Valid: true}, f.req.UpstreamResource, true)
	require.NoError(t, err)
	require.Empty(t, results)
}

// failingQueries fails every query whose sqlc name it lists, so a test can
// fault one configuration read and leave the others working.
type failingQueries []string

func (f failingQueries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, name := range f {
		if strings.HasPrefix(data.SQL, "-- name: "+name+" ") {
			failed, cancel := context.WithCancel(ctx)
			cancel()
			return failed
		}
	}
	return ctx
}

func (failingQueries) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// faultyPool connects to db's database with every query named in names failing.
func faultyPool(t *testing.T, db *pgxpool.Pool, names ...string) *pgxpool.Pool {
	t.Helper()
	cfg := db.Config()
	cfg.ConnConfig.Tracer = failingQueries(names)
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestGovernor_RefreshDecisionReadFaultsAreUnconfirmed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		query string
	}{
		{name: "binding count", query: "CountActiveEMABindingsForUserIssuer"},
		{name: "binding list", query: "ListEMAChainingBindings"},
		{name: "binding readiness", query: "ReadEMAProject"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newChainStoreFixture(t)
			healthy := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
			require.Equal(t, RefreshReauthenticate, healthy.RefreshDecision(t.Context(), f.req).Decision, "the healthy read confirms a fresh sign-in")

			g := NewGovernor(testenv.NewLogger(t), faultyPool(t, f.db, tc.query), f.chainer.enc, f.chainer.delegation)
			verdict := g.RefreshDecision(t.Context(), f.req)
			require.Equal(t, RefreshUnconfirmed, verdict.Decision, "a failed read is unknown configuration, never a reconnect or an unchained upstream")
			require.Equal(t, ReasonTransientFailure, verdict.Reason)
		})
	}
}

func TestGovernor_RefreshDecisionStoredCredentialReadFault(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	f.retainRefreshableDelegation(t, "assertion_only")
	require.NoError(t, f.chainer.publish(t.Context(), f.chainer.logger, f.req, f.sel, f.credential("downstream-token")))
	healthy := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	require.Equal(t, RefreshServed, healthy.RefreshDecision(t.Context(), f.req).Decision, "a usable stored credential serves after the assertion expires")

	g := NewGovernor(testenv.NewLogger(t), faultyPool(t, f.db, "GetEMACredentialForUse"), f.chainer.enc, f.chainer.delegation)
	verdict := g.RefreshDecision(t.Context(), f.req)
	require.Equal(t, RefreshUnconfirmed, verdict.Decision, "a failed credential read is not an unusable credential")
	require.Equal(t, ReasonTransientFailure, verdict.Reason)
}

func TestGovernor_RefreshDecisionNotConfigured(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	other := f.req
	other.UserSessionIssuerID = uuid.New()
	require.Equal(t, RefreshNotChained, g.RefreshDecision(t.Context(), other).Decision, "a user session issuer without bindings is not chained")
}

func TestGovernor_RefreshDecisionConfigurationFailureUnderTheCurrentRegistration(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	f.retainRefreshableDelegation(t, "configuration_failure")
	verdict := g.RefreshDecision(t.Context(), f.req)
	require.Equal(t, RefreshUnconfirmed, verdict.Decision, "a sign-in under the same registration cannot repair a configuration failure")
	require.Equal(t, ReasonConfigurationRequired, verdict.Reason)
}

func TestGovernor_RefreshDecisionColdCacheWithTransportAndCanonicalBindings(t *testing.T) {
	t.Parallel()
	const transport = "https://api.resource.example.test/mcp"
	f := newChainStoreFixture(t)
	f.retainRefreshableDelegation(t, "durable_credential_present")
	transportIssuer, transportClient := prepareResourceClient(t, f.db, f.chainer.enc, f.req.OrganizationID, "https://transport-as.example.test", "https://transport-as.example.test/token", "transport-client", "transport-secret")
	bindResource(t, f.db, f.req, transportIssuer, transportClient, transport, remotesessions.PreparationStateReady)
	req := f.req
	req.UpstreamResource = transport

	var cached *protectedresource.EndpointResource
	g := NewGovernor(testenv.NewLogger(t), f.db, f.chainer.enc, f.chainer.delegation)
	g.SetResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, error) {
		t.Error("refresh must never probe the upstream")
		return protectedresource.EndpointResource{}, errors.New("unexpected probe")
	})
	g.SetCachedResourceResolver(func(context.Context, string, uuid.UUID, string) (protectedresource.EndpointResource, bool, error) {
		if cached == nil {
			return protectedresource.EndpointResource{}, false, nil
		}
		return *cached, true, nil
	})

	verdict := g.RefreshDecision(t.Context(), req)
	require.Equal(t, RefreshUnconfirmed, verdict.Decision, "discovery may name the canonical binding, so the transport-named one cannot answer for the runtime")
	require.Equal(t, ReasonTransientFailure, verdict.Reason)

	cached = &protectedresource.EndpointResource{Resource: testResource, AuthorizationServers: []string{testResourceIssuer}, Discovered: true}
	verdict = g.RefreshDecision(t.Context(), req)
	require.Equal(t, RefreshServed, verdict.Decision)
	require.Equal(t, f.sel.remoteIssuerID, verdict.RemoteSessionIssuerID, "cached discovery selects the canonical binding")

	cached = &protectedresource.EndpointResource{Resource: transport, AuthorizationServers: nil, Discovered: false}
	verdict = g.RefreshDecision(t.Context(), req)
	require.Equal(t, transportIssuer, verdict.RemoteSessionIssuerID, "a cached absence of metadata selects the transport-named binding")
}
