package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestAttachClientKeySet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-attach-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-attach-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-attach-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "keyset-attach-set")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachKeySet)
	require.NoError(t, err)

	attached, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, attached.JSONWebKeySetID)
	require.Equal(t, setID.String(), *attached.JSONWebKeySetID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachKeySet)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// The link is readable back through the ordinary get, which is what the
	// dashboard renders the picker's current value from.
	fetched, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.JSONWebKeySetID)
	require.Equal(t, setID.String(), *fetched.JSONWebKeySetID)
}

// TestAttachClientKeySet_RequiresEntitlement pins the one gate that separates
// this link from the rest of client management.
//
// Runs against an organization of its own. The entitlement lookup is cached in
// Redis under a key derived from the organization id, and this package's other
// tests share one seeded organization, so asserting the refusal against that
// organization would read whichever value a parallel sibling wrote last.
//
// The refusal has to be attributable to the entitlement and not to RBAC, so the
// context carries org:admin in the fresh organization. Enabling the feature and
// repeating the call is the control: the same request then gets past the gate
// and fails on the client id instead, which is only reachable once the
// entitlement check has passed.
func TestAttachClientKeySet_RequiresEntitlement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	orgID := createOrganization(t, ctx, ti.conn, "keyset-ent-org")
	ctx = withOrganization(t, ctx, ti.conn, orgID)

	payload := &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              uuid.NewString(),
		JSONWebKeySetID: uuid.NewString(),
	}

	_, err := ti.service.AttachClientKeySet(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           payload.ID,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	ti.enableCustomerManagedKeys(t, ctx, orgID)

	_, err = ti.service.AttachClientKeySet(ctx, payload)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestAttachClientKeySet_CrossOrganizationSetNotFound is the tenancy assertion.
// The composite foreign key already pins the set to the client's organization,
// but a constraint violation surfaces as a 500; the handler resolves the set in
// the caller's organization first so a set belonging to someone else reads as
// absent rather than as a server error, and without confirming it exists.
func TestAttachClientKeySet_CrossOrganizationSetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	otherOrgID := createOrganization(t, ctx, ti.conn, "keyset-crossorg-other")

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-crossorg-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-crossorg-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-crossorg-client")
	foreignSetID := createJsonWebKeySet(t, ctx, ti.conn, otherOrgID, "keyset-crossorg-set")

	_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: foreignSetID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAttachClientKeySet_UnknownSetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-missing-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-missing-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-missing-client")

	_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: uuid.NewString(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAttachClientKeySet_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-rbac-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-rbac-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-rbac-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "keyset-rbac-set")

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.AttachClientKeySet(readOnlyCtx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDetachClientKeySet(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-detach-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-detach-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-detach-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "keyset-detach-set")

	_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachKeySet)
	require.NoError(t, err)

	detached, err := ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	})
	require.NoError(t, err)
	require.Nil(t, detached.JSONWebKeySetID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachKeySet)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// TestDetachClientKeySet_AbsentSetIsNoop keeps detach idempotent for a dashboard
// that cannot know whether its cached row is current. A no-op must not audit:
// an entry claiming a detach that removed nothing is a false record.
func TestDetachClientKeySet_AbsentSetIsNoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-noop-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-noop-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-noop-client")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachKeySet)
	require.NoError(t, err)

	detached, err := ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	})
	require.NoError(t, err)
	require.Nil(t, detached.JSONWebKeySetID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientDetachKeySet)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// TestDetachClientKeySet_RefusedForPrivateKeyJWT exercises the coupling rule
// AIM-156 makes reachable. private_key_jwt is not in the Goa enum yet, so the
// value is planted directly; the point of landing the rule now is that it exists
// before the value becomes selectable.
func TestDetachClientKeySet_RefusedForPrivateKeyJWT(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-pkjwt-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-pkjwt-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-pkjwt-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "keyset-pkjwt-set")

	_, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)

	forceTokenEndpointAuthMethod(t, ctx, ti.conn, uuid.MustParse(clientID), "private_key_jwt")

	_, err = ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	// The set is still attached: a refused detach must not have written.
	fetched, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.JSONWebKeySetID)
	require.Equal(t, setID.String(), *fetched.JSONWebKeySetID)
}

// TestAttachKeySet_ProjectSurface covers the project-scoped twin of
// TestAttachClientKeySet.
func TestAttachKeySet_ProjectSurface(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ti.enableCustomerManagedKeys(t, ctx, activeOrganizationID(t, ctx))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-proj-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-proj-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-proj-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "keyset-proj-set")

	attached, err := ti.service.AttachKeySet(ctx, &clientsgen.AttachKeySetPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               clientID,
		JSONWebKeySetID:  setID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, attached.JSONWebKeySetID)
	require.Equal(t, setID.String(), *attached.JSONWebKeySetID)

	detached, err := ti.service.DetachKeySet(ctx, &clientsgen.DetachKeySetPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               clientID,
	})
	require.NoError(t, err)
	require.Nil(t, detached.JSONWebKeySetID)
}

// TestPrivateKeyJWTRequiresKeySet_UpdatePaths covers the converse rule on both
// tenant update surfaces: a client with no set cannot be moved onto
// private_key_jwt, and the same call succeeds once a set is attached.
//
// private_key_jwt is not in the Goa enum until AIM-156, so a request over HTTP
// is rejected at the transport. These call the handlers directly, which is the
// only way to reach the rule — and the reason to land it now is precisely that
// it must already be correct when the enum opens up.
func TestPrivateKeyJWTRequiresKeySet_UpdatePaths(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, orgID)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-pkjwt-upd-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-pkjwt-upd-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-pkjwt-upd-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, orgID, "keyset-pkjwt-upd-set")

	privateKeyJWT := "private_key_jwt"

	orgUpdate := &orgclientsgen.UpdateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &privateKeyJWT,
		Scope:                   nil,
		Audience:                nil,
	}
	projectUpdate := &clientsgen.UpdateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		ID:                      clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &privateKeyJWT,
		Scope:                   nil,
		Audience:                nil,
	}

	_, err := ti.service.UpdateClient(ctx, orgUpdate)
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.UpdateRemoteSessionClient(ctx, projectUpdate)
	requireOopsCode(t, err, oops.CodeConflict)

	// With a set attached the same updates are allowed, which is what proves the
	// refusals above came from the coupling rule and not from the method value.
	_, err = ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateClient(ctx, orgUpdate)
	require.NoError(t, err)
	require.NotNil(t, updated.TokenEndpointAuthMethod)
	require.Equal(t, privateKeyJWT, *updated.TokenEndpointAuthMethod)

	_, err = ti.service.UpdateRemoteSessionClient(ctx, projectUpdate)
	require.NoError(t, err)
}

// TestPrivateKeyJWTRequiresKeySet_CreatePaths covers the rule on every create
// surface. A client is born without a set — the link is attached afterwards —
// so declaring private_key_jwt at creation is always a refusal, and on the
// global surface permanently so: those rows carry a NULL organization_id by
// construction and the CHECK constraint forbids a set without one.
func TestPrivateKeyJWTRequiresKeySet_CreatePaths(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-pkjwt-create-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-pkjwt-create-usi")
	privateKeyJWT := "private_key_jwt"

	// Built inline rather than through newCreateClientPayload: that helper's
	// third parameter is the client secret and it pins TokenEndpointAuthMethod
	// to nil, so it cannot express the case under test.
	_, err := ti.service.CreateClient(ctx, &orgclientsgen.CreateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		RemoteSessionIssuerID:   issuerID,
		ProjectID:               nil,
		ClientID:                "keyset-pkjwt-create-org-client",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &privateKeyJWT,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ProjectSlugInput:        nil,
		RemoteSessionIssuerID:   issuerID,
		UserSessionIssuerIds:    []string{userIssuerID.String()},
		ClientID:                "keyset-pkjwt-create-client",
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &privateKeyJWT,
		Scope:                   nil,
		Audience:                nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

// TestAttachKeySet_ProjectSurfaceRefusesOrganizationLevelClient is the
// privilege-escalation defense. Project reads see organization-level clients, and
// AttachUserSessionIssuer deliberately lets a project bind one to its own
// user_session_issuer. Re-keying is not that kind of decision: it would let one
// project change the key every other project sharing the client signs with, so
// the key set surface follows UpdateRemoteSessionClient and stays blind to them.
func TestAttachKeySet_ProjectSurfaceRefusesOrganizationLevelClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, orgID)

	orgIssuer, err := ti.service.CreateIssuer(ctx, newCreateIssuerPayload("keyset-orglevel-issuer", nil))
	require.NoError(t, err)

	orgLevelClient, err := ti.service.CreateClient(ctx, newCreateClientPayload(orgIssuer.ID, nil, nil))
	require.NoError(t, err)
	require.Empty(t, orgLevelClient.ProjectID, "fixture must be organization-level")

	setID := createJsonWebKeySet(t, ctx, ti.conn, orgID, "keyset-orglevel-set")

	_, err = ti.service.AttachKeySet(ctx, &clientsgen.AttachKeySetPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               orgLevelClient.ID,
		JSONWebKeySetID:  setID.String(),
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	// The organization surface reaches the same client, so the refusal above is
	// about the surface rather than about the client being unreachable at all.
	attached, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              orgLevelClient.ID,
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, attached.JSONWebKeySetID)
}

// TestAttachClientKeySet_AdoptsClientWithoutOrganization covers the legacy rows
// left by the organization_id migration, which ran without a backfill. The
// CHECK constraint forbids a set on any row whose organization_id is still
// NULL, so attaching resolves the organization through the client's project and
// adopts the row, which is what AIM-77 means by matching ownership through
// project_id. Refusing instead would strand every pre-migration client on
// shared-secret authentication.
func TestAttachClientKeySet_AdoptsClientWithoutOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, orgID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-adopt-issuer", "")
	legacyClientID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(issuerID), "keyset-adopt-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, orgID, "keyset-adopt-set")

	attached, err := ti.service.AttachClientKeySet(ctx, &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              legacyClientID.String(),
		JSONWebKeySetID: setID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, attached.JSONWebKeySetID)
	require.Equal(t, setID.String(), *attached.JSONWebKeySetID)

	// The adoption is durable, not just reflected in the response: the row now
	// carries the organization the composite foreign key pins the set to.
	require.Equal(t, orgID, attached.OrganizationID)

	stored, err := repo.New(ti.conn).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             legacyClientID,
		OrganizationID: conv.ToPGText(orgID),
	})
	require.NoError(t, err)
	require.True(t, stored.RemoteSessionClient.OrganizationID.Valid)
	require.Equal(t, orgID, stored.RemoteSessionClient.OrganizationID.String)
}

// TestDetachClientKeySet_ClientWithoutOrganizationIsNoop proves detach never
// needs the adoption. The CHECK constraint guarantees a NULL-organization client
// holds no set, so there is nothing to clear and nothing to resolve, and the
// call must not rewrite the row's ownership as a side effect.
func TestDetachClientKeySet_ClientWithoutOrganizationIsNoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, orgID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-noorg-detach-issuer", "")
	legacyClientID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *authCtx.ProjectID, uuid.MustParse(issuerID), "keyset-noorg-detach-client")

	detached, err := ti.service.DetachClientKeySet(ctx, &orgclientsgen.DetachClientKeySetPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           legacyClientID.String(),
	})
	require.NoError(t, err)
	require.Nil(t, detached.JSONWebKeySetID)
	require.Empty(t, detached.OrganizationID, "a no-op detach must not adopt the client")
}

// TestClientManagementUngatedByEntitlement is the other half of the entitlement
// requirement: the gate covers the key set link and nothing else, so an
// organization that never bought customer-managed keys manages clients as
// before.
//
// Unlike the refusal test, this one is safe in the shared seeded organization
// because it never touches the entitlement in either direction: it calls only
// ungated methods, so whatever a parallel sibling wrote to the Redis-cached
// feature value cannot change its outcome.
func TestClientManagementUngatedByEntitlement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-ungated-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-ungated-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-ungated-client")

	authMethod := "client_secret_post"
	_, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		SessionToken:            nil,
		ApikeyToken:             nil,
		ID:                      clientID,
		ClientSecret:            nil,
		TokenEndpointAuthMethod: &authMethod,
		Scope:                   nil,
		Audience:                nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteClient(ctx, &orgclientsgen.DeleteClientPayload{
		SessionToken: nil,
		ApikeyToken:  nil,
		ID:           clientID,
	}))
}

// TestAttachClientKeySet_ReattachIsNoop keeps attach as idempotent as detach: a
// dashboard replaying its own state must not fill the audit log with entries for
// changes that did not happen.
func TestAttachClientKeySet_ReattachIsNoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := activeOrganizationID(t, ctx)
	ti.enableCustomerManagedKeys(t, ctx, orgID)

	issuerID := createRemoteIssuer(t, ctx, ti, "keyset-reattach-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "keyset-reattach-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "keyset-reattach-client")
	setID := createJsonWebKeySet(t, ctx, ti.conn, orgID, "keyset-reattach-set")

	payload := &orgclientsgen.AttachClientKeySetPayload{
		SessionToken:    nil,
		ApikeyToken:     nil,
		ID:              clientID,
		JSONWebKeySetID: setID.String(),
	}

	_, err := ti.service.AttachClientKeySet(ctx, payload)
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachKeySet)
	require.NoError(t, err)

	reattached, err := ti.service.AttachClientKeySet(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, reattached.JSONWebKeySetID)
	require.Equal(t, setID.String(), *reattached.JSONWebKeySetID)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientAttachKeySet)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
