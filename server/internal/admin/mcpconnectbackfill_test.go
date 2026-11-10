package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// connectBackfillFixture writes grants straight into principal_grants, so a
// fixture can hold rows no API writes today: temporary, expired, deny-effect
// or backdated grants.
type connectBackfillFixture struct {
	t     *testing.T
	db    *pgxpool.Pool
	orgID string
}

func newConnectBackfillFixture(t *testing.T) (*connectBackfillFixture, *Service, context.Context) {
	t.Helper()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "mcp-connect-backfill", slug: "mcp-connect-backfill-" + uuid.NewString()[:8], accountType: "enterprise"})
	return &connectBackfillFixture{t: t, db: db, orgID: orgID}, svc, ctx
}

type fixtureGrant struct {
	principal urn.Principal
	scope     authz.Scope
	selector  authz.Selector
	expiresAt *time.Time
	effect    string
	createdAt *time.Time
}

func optionalTime(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, InfinityModifier: pgtype.Finite, Valid: true}
}

func (f *connectBackfillFixture) grant(g fixtureGrant) {
	f.t.Helper()
	raw, err := json.Marshal(g.selector)
	require.NoError(f.t, err)
	err = repo.New(f.db).AdminInsertPrincipalGrantFixture(f.t.Context(), repo.AdminInsertPrincipalGrantFixtureParams{
		OrganizationID: f.orgID,
		PrincipalUrn:   g.principal,
		Scope:          string(g.scope),
		Effect:         pgtype.Text{String: g.effect, Valid: g.effect != ""},
		Selectors:      raw,
		ExpiresAt:      optionalTime(g.expiresAt),
		CreatedAt:      optionalTime(g.createdAt),
	})
	require.NoError(f.t, err)
}

// provision records grant as written by the default auth permission backfill,
// which loads it as inferred.
func (f *connectBackfillFixture) provision(principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	f.t.Helper()
	raw, err := json.Marshal(selector)
	require.NoError(f.t, err)
	err = repo.New(f.db).AdminInsertAuthPermissionProvisionFixture(f.t.Context(), repo.AdminInsertAuthPermissionProvisionFixtureParams{
		OrganizationID: f.orgID,
		PrincipalUrn:   principal.String(),
		Scope:          string(scope),
		Selectors:      raw,
	})
	require.NoError(f.t, err)
}

func (f *connectBackfillFixture) connectExpiry(principal urn.Principal, selector authz.Selector) (*time.Time, bool) {
	f.t.Helper()
	raw, err := json.Marshal(selector)
	require.NoError(f.t, err)
	expiresAt, err := repo.New(f.db).AdminGetConnectGrantExpiryFixture(f.t.Context(), repo.AdminGetConnectGrantExpiryFixtureParams{
		OrganizationID: f.orgID,
		PrincipalUrn:   principal,
		Selectors:      raw,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false
	}
	require.NoError(f.t, err)
	if !expiresAt.Valid {
		return nil, true
	}
	return &expiresAt.Time, true
}

func mcpSelector(resourceID string, dims ...string) authz.Selector {
	selector := authz.NewSelector(authz.ScopeMCPConnect, resourceID)
	for i := 0; i+1 < len(dims); i += 2 {
		selector[dims[i]] = dims[i+1]
	}
	return selector
}

// withoutReadWriteImplyingConnect emulates connect checks once mcp:read and
// mcp:write no longer satisfy mcp:connect: they only reach a connect check
// through that expansion, so dropping them leaves exactly what connect
// decisions will see.
func withoutReadWriteImplyingConnect(grants []authz.Grant) []authz.Grant {
	return slices.DeleteFunc(slices.Clone(grants), func(g authz.Grant) bool {
		return g.Scope == authz.ScopeMCPRead || g.Scope == authz.ScopeMCPWrite
	})
}

// TestBackfillMcpConnectGrantsPreservesConnectDecisions is the migration's
// safety proof: for every caller, server, project and tool in a fixture
// organization, connect decisions after the backfill without read and write
// implying connect equal the decisions before it with that implication.
func TestBackfillMcpConnectGrantsPreservesConnectDecisions(t *testing.T) {
	t.Parallel()
	f, svc, ctx := newConnectBackfillFixture(t)

	projectA, projectB := uuid.NewString(), uuid.NewString()
	serverA, serverB, serverC, serverD := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	inHour, inDay, past := time.Now().Add(time.Hour), time.Now().Add(24*time.Hour), time.Now().Add(-time.Hour)

	everyone := urn.NewPrincipal(urn.PrincipalTypeUser, urn.AllUsersPrincipalID)
	viewers := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	operators := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	user := func() urn.Principal { return urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString()) }
	namedOverride, wildcardViewer, temporaryViewer, inferredOverride, expiredViewer, toolViewer, denyRow, narrowedViewer :=
		user(), user(), user(), user(), user(), user(), user(), user()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	apiKey := urn.NewPrincipal(urn.PrincipalTypeAPIKey, uuid.NewString())
	workload := urn.NewPrincipal(urn.PrincipalTypeWorkload, uuid.NewString()+":repo:example/app")

	for _, g := range []fixtureGrant{
		// Roles: a project-wide viewer role that blocks serverA, and an operator
		// role that may manage one tool on serverB.
		{principal: viewers, scope: authz.ScopeMCPRead, selector: mcpSelector(authz.WildcardResource, authz.SelectorKeyProjectID, projectA)},
		{principal: viewers, scope: authz.ScopeMCPBlockedConnect, selector: mcpSelector(serverA)},
		{principal: operators, scope: authz.ScopeMCPWrite, selector: mcpSelector(serverB, authz.SelectorKeyTool, "deploy")},
		{principal: operators, scope: authz.ScopeMCPRead, selector: mcpSelector(serverB, authz.SelectorKeyDisposition, authz.DispositionReadOnly)},
		// Everyone can view serverC, and everyone is blocked from serverD.
		{principal: everyone, scope: authz.ScopeMCPRead, selector: mcpSelector(serverC)},
		{principal: everyone, scope: authz.ScopeMCPBlockedConnect, selector: mcpSelector(serverD)},
		// A direct grant naming serverA outranks the viewers role's block.
		{principal: namedOverride, scope: authz.ScopeMCPRead, selector: mcpSelector(serverA)},
		{principal: namedOverride, scope: authz.ScopeMCPWrite, selector: mcpSelector(serverD)},
		// A direct wildcard grant never outranks an inherited block.
		{principal: wildcardViewer, scope: authz.ScopeMCPRead, selector: mcpSelector(authz.WildcardResource)},
		// Temporary access: a read outlasting an existing temporary connect, and
		// a write outlasting the read on the same selector.
		{principal: temporaryViewer, scope: authz.ScopeMCPRead, selector: mcpSelector(serverB), expiresAt: &inHour},
		{principal: temporaryViewer, scope: authz.ScopeMCPWrite, selector: mcpSelector(serverB), expiresAt: &inDay},
		{principal: temporaryViewer, scope: authz.ScopeMCPConnect, selector: mcpSelector(serverB), expiresAt: &inHour},
		// A provisioned (inferred) connect grant next to an explicit read on the
		// same selector: the read is what outranks the block today.
		{principal: inferredOverride, scope: authz.ScopeMCPRead, selector: mcpSelector(serverA)},
		{principal: inferredOverride, scope: authz.ScopeMCPConnect, selector: mcpSelector(serverA)},
		// Expired access stays expired.
		{principal: expiredViewer, scope: authz.ScopeMCPRead, selector: mcpSelector(serverC), expiresAt: &past},
		// Tool- and disposition-narrowed grants keep every dimension.
		{principal: toolViewer, scope: authz.ScopeMCPRead, selector: mcpSelector(serverA, authz.SelectorKeyTool, "search", authz.SelectorKeyProjectID, projectA)},
		{principal: narrowedViewer, scope: authz.ScopeMCPWrite, selector: mcpSelector(serverD, authz.SelectorKeyDisposition, authz.DispositionDestructive)},
		// Grant loading ignores effect, so a deny-effect row allows today.
		{principal: denyRow, scope: authz.ScopeMCPRead, selector: mcpSelector(serverB), effect: "deny"},
		// Non-user principals.
		{principal: agent, scope: authz.ScopeMCPRead, selector: mcpSelector(serverA)},
		{principal: apiKey, scope: authz.ScopeMCPWrite, selector: mcpSelector(authz.WildcardResource, authz.SelectorKeyProjectID, projectB)},
		{principal: workload, scope: authz.ScopeMCPRead, selector: mcpSelector(serverC)},
	} {
		f.grant(g)
	}
	f.provision(inferredOverride, authz.ScopeMCPConnect, mcpSelector(serverA))

	callers := map[string][]urn.Principal{
		"namedOverride":    {namedOverride, viewers, everyone},
		"wildcardViewer":   {wildcardViewer, viewers, everyone},
		"temporaryViewer":  {temporaryViewer, operators, everyone},
		"inferredOverride": {inferredOverride, viewers, everyone},
		"expiredViewer":    {expiredViewer, everyone},
		"toolViewer":       {toolViewer, operators, everyone},
		"narrowedViewer":   {narrowedViewer, everyone},
		"denyRow":          {denyRow},
		"agent":            {agent, viewers},
		"apiKey":           {apiKey},
		"workload":         {workload},
		"everyone":         {everyone},
	}
	var checks []authz.Check
	for _, server := range []string{serverA, serverB, serverC, serverD, uuid.NewString()} {
		for _, project := range []string{projectA, projectB, ""} {
			checks = append(checks, authz.MCPCheck(authz.ScopeMCPConnect, server, project))
			for _, tool := range []string{"search", "deploy", "other"} {
				for _, disposition := range []string{"", authz.DispositionReadOnly, authz.DispositionDestructive} {
					checks = append(checks, authz.MCPToolCallCheck(server, authz.MCPToolCallDimensions{Tool: tool, Disposition: disposition, ProjectID: project}))
				}
			}
		}
	}

	decide := func(dropReadWrite bool) map[string]bool {
		t.Helper()
		decisions := map[string]bool{}
		for name, principals := range callers {
			grants, err := authz.LoadGrants(ctx, f.db, f.orgID, principals)
			require.NoError(t, err)
			if dropReadWrite {
				grants = withoutReadWriteImplyingConnect(grants)
			}
			for i, check := range checks {
				allowed, err := authz.GrantsAuthorize(grants, check)
				require.NoError(t, err)
				decisions[fmt.Sprintf("%s/%d/%s/%v", name, i, check.ResourceID, check.Dimensions)] = allowed
			}
		}
		return decisions
	}

	before := decide(false)
	require.NotEqual(t, before, decide(true), "the fixture must depend on read and write implying connect")
	allowed := 0
	for _, ok := range before {
		if ok {
			allowed++
		}
	}
	require.Positive(t, allowed)
	require.Less(t, allowed, len(before))

	result, err := svc.BackfillMcpConnectGrants(ctx, &gen.BackfillMcpConnectGrantsPayload{})
	require.NoError(t, err)
	require.Positive(t, result.Inserted)
	require.Equal(t, int64(1), result.Extended, "the temporary connect grant outlived by write")
	require.Equal(t, int64(1), result.TakenOver, "the provisioned connect grant next to an explicit read")

	require.Equal(t, before, decide(true), "connect decisions without the implication must match today's")
	require.Equal(t, before, decide(false), "the backfill must not change today's decisions")

	again, err := svc.BackfillMcpConnectGrants(ctx, &gen.BackfillMcpConnectGrantsPayload{})
	require.NoError(t, err)
	require.Equal(t, &gen.AdminBackfillMCPConnectGrantsResult{Inserted: 0, Extended: 0, TakenOver: 0}, again, "a rerun writes nothing")
	require.Equal(t, before, decide(true))
}

func TestBackfillMcpConnectGrantsCopiesExpiry(t *testing.T) {
	t.Parallel()
	f, svc, ctx := newConnectBackfillFixture(t)

	server := uuid.NewString()
	soon, later := time.Now().Add(time.Hour).Truncate(time.Microsecond), time.Now().Add(48*time.Hour).Truncate(time.Microsecond)
	temporary := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	mixed := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	permanentConnect := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	selector := mcpSelector(server)

	f.grant(fixtureGrant{principal: temporary, scope: authz.ScopeMCPRead, selector: selector, expiresAt: &soon})
	f.grant(fixtureGrant{principal: temporary, scope: authz.ScopeMCPWrite, selector: selector, expiresAt: &later})
	f.grant(fixtureGrant{principal: mixed, scope: authz.ScopeMCPRead, selector: selector})
	f.grant(fixtureGrant{principal: mixed, scope: authz.ScopeMCPConnect, selector: selector, expiresAt: &soon})
	f.grant(fixtureGrant{principal: permanentConnect, scope: authz.ScopeMCPRead, selector: selector, expiresAt: &soon})
	f.grant(fixtureGrant{principal: permanentConnect, scope: authz.ScopeMCPConnect, selector: selector})

	result, err := svc.BackfillMcpConnectGrants(ctx, &gen.BackfillMcpConnectGrantsPayload{})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Inserted)
	require.Equal(t, int64(1), result.Extended)

	expiresAt, ok := f.connectExpiry(temporary, selector)
	require.True(t, ok)
	require.NotNil(t, expiresAt, "temporary access must not become permanent")
	require.True(t, later.Equal(*expiresAt), "copied from the latest-expiring source")

	expiresAt, ok = f.connectExpiry(mixed, selector)
	require.True(t, ok)
	require.Nil(t, expiresAt, "a permanent read extends a temporary connect to permanent")

	expiresAt, ok = f.connectExpiry(permanentConnect, selector)
	require.True(t, ok)
	require.Nil(t, expiresAt, "a permanent connect is never shortened")
}

func TestBackfillMcpConnectGrantsCreatedBefore(t *testing.T) {
	t.Parallel()
	f, svc, ctx := newConnectBackfillFixture(t)

	cutoff := time.Now().Add(-time.Hour)
	beforeCutoff := cutoff.Add(-time.Hour)
	old := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	viewOnly := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	selector := mcpSelector(uuid.NewString())
	f.grant(fixtureGrant{principal: old, scope: authz.ScopeMCPRead, selector: selector, createdAt: &beforeCutoff})
	f.grant(fixtureGrant{principal: viewOnly, scope: authz.ScopeMCPRead, selector: selector})

	createdBefore := cutoff.Format(time.RFC3339)
	result, err := svc.BackfillMcpConnectGrants(ctx, &gen.BackfillMcpConnectGrantsPayload{CreatedBefore: &createdBefore})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Inserted)

	_, ok := f.connectExpiry(old, selector)
	require.True(t, ok, "grants written before the cutoff are copied")
	_, ok = f.connectExpiry(viewOnly, selector)
	require.False(t, ok, "grants written after the cutoff are left alone")

	invalid := "yesterday"
	_, err = svc.BackfillMcpConnectGrants(ctx, &gen.BackfillMcpConnectGrantsPayload{CreatedBefore: &invalid})
	require.Error(t, err)
}

func TestBackfillMcpConnectGrantsRequiresStaff(t *testing.T) {
	t.Parallel()
	_, svc, _ := newTestAdminService(t)
	_, err := svc.BackfillMcpConnectGrants(t.Context(), &gen.BackfillMcpConnectGrantsPayload{})
	require.Error(t, err)
}
