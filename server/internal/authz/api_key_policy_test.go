package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type apiKeyFixture struct {
	db             *pgxpool.Pool
	engine         *Engine
	organizationID string
	creatorID      string
	keyID          uuid.UUID
	projectID      string
}

func newAPIKeyFixture(t *testing.T) apiKeyFixture {
	t.Helper()
	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-apikey-" + uuid.NewString()
	creatorID := "creator-" + uuid.NewString()
	seedOrganization(t, ctx, db, organizationID)
	_, err := usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: creatorID, Email: creatorID + "@example.com", DisplayName: creatorID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(creatorID),
	})
	require.NoError(t, err)
	key, err := keysrepo.New(db).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID: organizationID, ProjectID: uuid.NullUUID{}, CreatedByUserID: creatorID,
		Name: "legacy", KeyPrefix: "gram_test", KeyHash: "hash-" + uuid.NewString(), Scopes: []string{"consumer"},
	})
	require.NoError(t, err)
	engine := NewEngine(testenv.NewLogger(t), db, staticChallengeLogging(false), workos.NewStubClient())
	return apiKeyFixture{db: db, engine: engine, organizationID: organizationID, creatorID: creatorID, keyID: key.ID, projectID: "project-" + uuid.NewString()}
}

// withKey returns the fixture for a new key with the given binding and scopes.
func (f apiKeyFixture) withKey(t *testing.T, projectID uuid.NullUUID, scopes []string) apiKeyFixture {
	t.Helper()
	key, err := keysrepo.New(f.db).CreateAPIKey(t.Context(), keysrepo.CreateAPIKeyParams{
		OrganizationID: f.organizationID, ProjectID: projectID, CreatedByUserID: f.creatorID,
		Name: "legacy-" + uuid.NewString()[:8], KeyPrefix: "gram_test", KeyHash: "hash-" + uuid.NewString(), Scopes: scopes,
	})
	require.NoError(t, err)
	f.keyID = key.ID
	return f
}

func (f apiKeyFixture) keyContext(t *testing.T) context.Context {
	t.Helper()
	return contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: f.organizationID, UserID: f.creatorID, APIKeyID: f.keyID.String(), AccountType: "enterprise",
	})
}

func (f apiKeyFixture) grant(t *testing.T, scope Scope, selector Selector) {
	t.Helper()
	seedGrantWithSelector(t, t.Context(), f.db, f.organizationID, urn.NewAPIKeyPrincipal(f.keyID), scope, selector)
}

func projectConnectSelector(projectID string) Selector {
	return Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: WildcardResource, SelectorKeyProjectID: projectID}
}

func TestAPIKeyPrincipalParses(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	parsed, err := urn.ParsePrincipal("api_key:" + id.String())
	require.NoError(t, err)
	require.Equal(t, urn.NewAPIKeyPrincipal(id), parsed)
	_, err = urn.ParsePrincipal("api_key:not-a-uuid")
	require.Error(t, err)
	_, err = urn.ParsePrincipal("api_key:" + id.String()[:8])
	require.Error(t, err)
}

func TestValidatePrincipalRejectsAPIKeys(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	err := ValidatePrincipal(t.Context(), f.db, f.organizationID, urn.NewAPIKeyPrincipal(f.keyID))
	require.ErrorIs(t, err, ErrPrincipalInvalid, "a key's policy is written only through the keys service")
}

func TestAPIKeyGrantsMatchStrictly(t *testing.T) {
	t.Parallel()

	keyGrant := NewGrantWithSelector(ScopeMCPConnect, projectConnectSelector("p"))
	keyGrant.PrincipalUrn = urn.NewAPIKeyPrincipal(uuid.New()).String()
	userGrant := keyGrant
	userGrant.PrincipalUrn = "user:someone"

	withProject := MCPCheck(ScopeMCPConnect, "toolset_1", "p")
	withoutProject := MCPCheck(ScopeMCPConnect, "toolset_1", "")

	for name, tc := range map[string]struct {
		grant   Grant
		check   Check
		allowed bool
	}{
		"key grant, check names the project":   {keyGrant, withProject, true},
		"key grant, check omits the project":   {keyGrant, withoutProject, false},
		"user grant, check omits the project":  {userGrant, withoutProject, true},
		"key grant, check names other project": {keyGrant, MCPCheck(ScopeMCPConnect, "toolset_1", "q"), false},
	} {
		allowed, err := GrantsAuthorize([]Grant{tc.grant}, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)
	}
}

func TestAuthorizeMandatoryForLegacyAPIKeys(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	ctx := f.keyContext(t)
	connect := MCPCheck(ScopeMCPConnect, "toolset_1", f.projectID)
	clientA := MCPAuthClientCheck("client_a", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: f.projectID, ConnectionOwner: ""})
	clientB := MCPAuthClientCheck("client_b", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: f.projectID, ConnectionOwner: ""})

	enforce, err := f.engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce, "legacy keys still skip ordinary RBAC")

	// The creator's own grants never reach the key.
	seedGrant(t, t.Context(), f.db, f.organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, f.creatorID), ScopeMCPConnect, WildcardResource)
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNoPolicy}, connect)

	f.grant(t, ScopeMCPConnect, projectConnectSelector(f.projectID))
	f.grant(t, ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a"})
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNone}, connect, clientA)
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNotGranted}, clientB)
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonMissingProject}, MCPCheck(ScopeMCPConnect, "toolset_1", ""))
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNotGranted}, MCPCheck(ScopeMCPConnect, "toolset_1", "other-project"))

	f.grant(t, ScopeMCPBlockedAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a"})
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonBlocked}, clientA)

	expired := f.withKey(t, uuid.NullUUID{}, []string{"consumer"})
	expired.grant(t, ScopeMCPConnect, projectConnectSelector(f.projectID))
	requireMandatory(t, f.engine, expired.keyContext(t), MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNone}, connect)
	require.NoError(t, testrepo.New(f.db).ExpireAPIKeyFixture(t.Context(), testrepo.ExpireAPIKeyFixtureParams{ID: expired.keyID, OrganizationID: f.organizationID}))
	requireMandatory(t, f.engine, expired.keyContext(t), MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonAPIKeyInactive}, connect)

	_, err = keysrepo.New(f.db).DeleteAPIKey(t.Context(), keysrepo.DeleteAPIKeyParams{ID: f.keyID, OrganizationID: f.organizationID})
	require.NoError(t, err)
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonAPIKeyInactive}, connect)
}

func TestAuthorizeMandatoryRejectsForeignTenantKeys(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	f.grant(t, ScopeMCPConnect, projectConnectSelector(f.projectID))
	other := "org-other-" + uuid.NewString()
	seedOrganization(t, t.Context(), f.db, other)
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: other, UserID: f.creatorID, APIKeyID: f.keyID.String(), AccountType: "enterprise",
	})
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonAPIKeyInactive}, MCPCheck(ScopeMCPConnect, "toolset_1", f.projectID))
}

func TestAuthorizeMandatoryNeverReadsSkippedAsAllow(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	connect := MCPCheck(ScopeMCPConnect, "toolset_1", "p")

	sessionless := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"})
	require.NoError(t, engine.Require(sessionless, connect), "ordinary RBAC skips this caller")
	requireMandatory(t, engine, sessionless, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceNone, Reason: MandatoryDenyReasonNoPolicy}, connect)

	requireMandatory(t, engine, t.Context(), MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceNone, Reason: MandatoryDenyReasonUnauthenticated}, connect)

	session := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, WildcardResource)})
	requireMandatory(t, engine, session, MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNone}, connect)
	requireMandatory(t, engine, session, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNotGranted}, sharedClientCheck(authProjectP))
	requireMandatory(t, engine, session, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceNone, Reason: MandatoryDenyReasonUnsupportedScope}, checkForTest(ScopeProjectRead, "proj"))
}

func requireMandatory(t *testing.T, engine *Engine, ctx context.Context, want MandatoryDecision, checks ...Check) {
	t.Helper()
	got, err := engine.AuthorizeMandatory(ctx, checks...)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func checkForTest(scope Scope, resourceID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
}

func TestAuthorizeMandatoryKeepsAgentKeyAdmission(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	connect := MCPCheck(ScopeMCPConnect, "toolset_1", "p")
	allow := []Grant{NewGrant(ScopeMCPConnect, WildcardResource)}
	ctx := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "user_u", Policy: allow}, allow, allow, allow)
	requireMandatory(t, engine, ctx, MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNone}, connect)

	restricted := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "user_u", Policy: allow}, allow, nil, allow)
	requireMandatory(t, engine, restricted, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonNotGranted}, connect)
}

func TestAPIKeyBlockCannotBeSkippedByOmittingTheProject(t *testing.T) {
	t.Parallel()

	principal := urn.NewAPIKeyPrincipal(uuid.New()).String()
	allow := NewGrant(ScopeMCPConnect, WildcardResource)
	allow.PrincipalUrn = principal
	block := NewGrantWithSelector(ScopeMCPBlockedConnect, projectConnectSelector("p"))
	block.PrincipalUrn = principal
	grants := []Grant{allow, block}

	for name, tc := range map[string]struct {
		check   Check
		allowed bool
	}{
		"blocked project":  {MCPCheck(ScopeMCPConnect, "toolset_1", "p"), false},
		"other project":    {MCPCheck(ScopeMCPConnect, "toolset_1", "q"), true},
		"project omitted":  {MCPCheck(ScopeMCPConnect, "toolset_1", ""), false},
		"tool call no prj": {MCPToolCallCheck("toolset_1", MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""}), false},
	} {
		allowed, err := GrantsAuthorize(grants, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)
	}
}

func TestAuthorizeMandatoryHonorsKeyLimits(t *testing.T) {
	t.Parallel()

	base := newAPIKeyFixture(t)
	created, err := projectsrepo.New(base.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "bound", Slug: "bound-" + uuid.NewString()[:8], OrganizationID: base.organizationID})
	require.NoError(t, err)
	project := uuid.NullUUID{UUID: created.ID, Valid: true}
	inProject := MCPCheck(ScopeMCPConnect, "toolset_1", created.ID.String())

	bound := base.withKey(t, project, []string{"consumer"})
	bound.grant(t, ScopeMCPConnect, NewSelector(ScopeMCPConnect, WildcardResource))
	requireMandatory(t, bound.engine, bound.keyContext(t), MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNone}, inProject)
	requireMandatory(t, bound.engine, bound.keyContext(t), MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonOutsideKey}, MCPCheck(ScopeMCPConnect, "toolset_1", uuid.NewString()))

	hooks := base.withKey(t, uuid.NullUUID{}, []string{"hooks"})
	hooks.grant(t, ScopeMCPConnect, NewSelector(ScopeMCPConnect, WildcardResource))
	requireMandatory(t, hooks.engine, hooks.keyContext(t), MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonOutsideKey}, inProject)
}

func TestAuthorizeMandatoryDeniesInactiveAgentCredentials(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", APIKeyID: uuid.NewString()}, actor, contextvalues.PrincipalCredential{})
	requireMandatory(t, engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonInactiveCredential}, MCPCheck(ScopeMCPConnect, "toolset_1", "p"))
}

func TestAudienceEditsNeverSeeOrRemoveAPIKeyGrants(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	server := uuid.NewString()
	keyBlock := NewSelector(ScopeMCPBlockedConnect, server)
	f.grant(t, ScopeMCPBlockedConnect, keyBlock)
	f.grant(t, ScopeMCPConnect, NewSelector(ScopeMCPConnect, server))
	member := urn.NewPrincipal(urn.PrincipalTypeUser, f.creatorID)

	for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
		resource := Resource{OrganizationID: f.organizationID, Scope: scope, ResourceID: server}
		listed, err := ListGrantsForResource(t.Context(), f.db, resource)
		require.NoError(t, err)
		require.Empty(t, listed, "audience reads never list key grants")
		batched, err := ListGrantsForResourceIDs(t.Context(), f.db, f.organizationID, scope, []string{server})
		require.NoError(t, err)
		require.Empty(t, batched)

		require.NoError(t, ReplaceResourceAudience(t.Context(), f.db, resource, []PrincipalSelectors{{Principal: member, Selectors: nil}}))
		listed, err = ListGrantsForResource(t.Context(), f.db, resource)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.Equal(t, member.String(), listed[0].PrincipalUrn)
	}

	kept, err := LoadGrants(t.Context(), f.db, f.organizationID, []urn.Principal{urn.NewAPIKeyPrincipal(f.keyID)})
	require.NoError(t, err)
	require.Len(t, kept, 2, "replacing the audience keeps the key's allow and block")
}

func TestAuthorizeMandatoryHonorsANarrowerCredentialProject(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	other := uuid.New()
	f.grant(t, ScopeMCPConnect, projectConnectSelector(f.projectID))
	f.grant(t, ScopeMCPConnect, projectConnectSelector(other.String()))
	narrowed := uuid.New()
	ctx := contextvalues.WithLegacyAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: f.organizationID, UserID: f.creatorID, APIKeyID: f.keyID.String(), ProjectID: &narrowed, AccountType: "enterprise",
	})
	requireMandatory(t, f.engine, ctx, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonOutsideKey}, MCPCheck(ScopeMCPConnect, "toolset_1", other.String()))
}

func TestAuthorizeMandatoryRejectsModelessKeyContexts(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", APIKeyID: uuid.NewString(), AccountType: "enterprise"})
	_, err := engine.AuthorizeMandatory(ctx, MCPCheck(ScopeMCPConnect, "toolset_1", "p"))
	require.Error(t, err, "a wiring bug is an error, never a recorded denial")
}

func TestLegacyKeysNeverUseTheirCreatorsConnection(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	f.grant(t, ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: "client_a"})
	personal := MCPAuthClientCheck("client_a", MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: f.projectID, ConnectionOwner: f.creatorID})
	requireMandatory(t, f.engine, f.keyContext(t), MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNotGranted}, personal)
}

func TestPrepareMandatoryAdmitsOnceAndDecidesPerServer(t *testing.T) {
	t.Parallel()

	f := newAPIKeyFixture(t)
	f.grant(t, ScopeMCPConnect, projectConnectSelector(f.projectID))
	prepared, err := f.engine.PrepareMandatory(f.keyContext(t))
	require.NoError(t, err)
	require.Equal(t, MandatoryCallerAPIKey, prepared.Caller())
	require.Equal(t, f.keyID.String(), prepared.PrincipalID())
	require.Empty(t, prepared.RepresentedUserID(), "a key's creator is attribution")

	_, err = keysrepo.New(f.db).DeleteAPIKey(t.Context(), keysrepo.DeleteAPIKeyParams{ID: f.keyID, OrganizationID: f.organizationID})
	require.NoError(t, err)
	got, err := prepared.Decide(f.organizationID, MCPCheck(ScopeMCPConnect, "toolset_1", f.projectID), MCPCheck(ScopeMCPConnect, "toolset_2", f.projectID))
	require.NoError(t, err)
	require.Equal(t, MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonNone}, got, "deciding reuses the admission")

	got, err = prepared.Decide("org-elsewhere", MCPCheck(ScopeMCPConnect, "toolset_1", f.projectID))
	require.NoError(t, err)
	require.Equal(t, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceAPIKey, Reason: MandatoryDenyReasonOrganizationMismatch}, got)
}

func TestPrepareMandatoryBindsTheSignedInUser(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	session := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, WildcardResource)})
	prepared, err := engine.PrepareMandatory(session)
	require.NoError(t, err)
	require.Equal(t, MandatoryCallerUser, prepared.Caller())
	require.Equal(t, sessionUserID, prepared.RepresentedUserID())

	got, err := prepared.Decide("org-elsewhere", MCPCheck(ScopeMCPConnect, "toolset_1", "p"))
	require.NoError(t, err)
	require.Equal(t, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceRequest, Reason: MandatoryDenyReasonOrganizationMismatch}, got, "a wildcard grant in another organization is no authority here")

	anonymous, err := engine.PrepareMandatory(t.Context())
	require.NoError(t, err)
	require.Equal(t, MandatoryCallerAnonymous, anonymous.Caller())
}

func TestAuthorizeMandatoryValidatesChecksBeforeAdmission(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	modeless := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", APIKeyID: uuid.NewString(), AccountType: "enterprise"})
	requireMandatory(t, engine, modeless, MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceNone, Reason: MandatoryDenyReasonUnsupportedScope}, checkForTest(ScopeProjectRead, "proj"))
}
