package mcpunannotatedgrants_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/mcpunannotatedgrants"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	serverOne = "server-one"
	serverTwo = "server-two"
	projectID = "project-one"
)

func TestRunAddsUnannotatedTwinsOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())

	// server-one: a read-only rule used to reach tools without annotations.
	readOnly := limited(authz.ScopeMCPConnect, serverOne, authz.DispositionReadOnly)
	seedGrant(t, ctx, pool, orgID, principal, readOnly)
	// server-two: an unrestricted rule with two disposition blocks that used to
	// block tools without annotations. Both need the same twin.
	seedGrant(t, ctx, pool, orgID, principal, authz.NewGrant(authz.ScopeMCPConnect, serverTwo))
	destructive := limited(authz.ScopeMCPBlockedConnect, serverTwo, authz.DispositionDestructive)
	openWorld := limited(authz.ScopeMCPBlockedConnect, serverTwo, authz.DispositionOpenWorld)
	seedGrant(t, ctx, pool, orgID, principal, destructive)
	seedGrant(t, ctx, pool, orgID, principal, openWorld)

	before := storedGrants(t, ctx, pool, orgID, principal)
	require.False(t, callsUnannotated(t, ctx, pool, orgID, principal, serverOne), "the read-only rule no longer reaches tools without annotations")
	require.True(t, callsUnannotated(t, ctx, pool, orgID, principal, serverTwo), "the disposition blocks no longer reach tools without annotations")

	want := mcpunannotatedgrants.Report{Principals: 1, Added: 2, Extended: 0}
	report, err := mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: "", Apply: false})
	require.NoError(t, err)
	require.Equal(t, want, report)
	require.Equal(t, before, storedGrants(t, ctx, pool, orgID, principal), "a preview writes nothing")

	report, err = mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, want, report)
	require.ElementsMatch(t, append(before,
		grantKey(limited(authz.ScopeMCPConnect, serverOne, authz.DispositionUnannotated)),
		grantKey(limited(authz.ScopeMCPBlockedConnect, serverTwo, authz.DispositionUnannotated)),
	), storedGrants(t, ctx, pool, orgID, principal))
	require.True(t, callsUnannotated(t, ctx, pool, orgID, principal, serverOne), "the twin restores the read-only rule's reach")
	require.False(t, callsUnannotated(t, ctx, pool, orgID, principal, serverTwo), "the twin restores the blocks' reach")

	report, err = mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, mcpunannotatedgrants.Report{Principals: 0, Added: 0, Extended: 0}, report)
}

func TestRunKeepsTheLongerExpiry(t *testing.T) {
	t.Parallel()

	soon := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	later := soon.Add(24 * time.Hour)
	dated := func(at time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true}
	}
	permanent := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	for _, tc := range []struct {
		name             string
		twin, limitedOne pgtype.Timestamptz
		want             pgtype.Timestamptz
		extended         int
	}{
		{name: "limited rule lasts longer", twin: dated(soon), limitedOne: dated(later), want: dated(later), extended: 1},
		{name: "twin lasts longer", twin: dated(later), limitedOne: dated(soon), want: dated(later), extended: 0},
		{name: "limited rule is permanent", twin: dated(soon), limitedOne: permanent, want: permanent, extended: 1},
		{name: "twin is permanent", twin: permanent, limitedOne: dated(later), want: permanent, extended: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			pool := newTestDB(t)
			orgID := seedOrganization(t, ctx, pool)
			principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
			twin := limited(authz.ScopeMCPConnect, serverOne, authz.DispositionUnannotated)

			setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, twin), tc.twin)
			setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPConnect, serverOne, authz.DispositionReadOnly)), tc.limitedOne)

			report, err := mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: orgID, Apply: true})
			require.NoError(t, err)
			require.Equal(t, tc.extended, report.Extended)
			require.Zero(t, report.Added)

			selectors, err := twin.Selector.MarshalJSON()
			require.NoError(t, err)
			rows, err := mcpunannotatedgrants.New(pool).LockGrantBySelectors(ctx, mcpunannotatedgrants.LockGrantBySelectorsParams{
				OrganizationID: orgID,
				PrincipalUrn:   principal,
				Scope:          string(authz.ScopeMCPConnect),
				Selectors:      selectors,
			})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, tc.want.Valid, rows[0].ExpiresAt.Valid)
			if tc.want.Valid {
				require.True(t, tc.want.Time.Equal(rows[0].ExpiresAt.Time), "want %s, got %s", tc.want.Time, rows[0].ExpiresAt.Time)
			}
			require.Len(t, storedGrants(t, ctx, pool, orgID, principal), 2)
		})
	}
}

func TestRunCopiesExpiryAndSkipsExpiredRows(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)

	setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPConnect, serverOne, authz.DispositionReadOnly)),
		pgtype.Timestamptz{Time: expiry, InfinityModifier: pgtype.Finite, Valid: true})
	setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPConnect, serverTwo, authz.DispositionReadOnly)),
		pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true})

	report, err := mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	require.Equal(t, mcpunannotatedgrants.Report{Principals: 1, Added: 1, Extended: 0}, report)

	twin := limited(authz.ScopeMCPConnect, serverOne, authz.DispositionUnannotated)
	selectors, err := twin.Selector.MarshalJSON()
	require.NoError(t, err)
	rows, err := mcpunannotatedgrants.New(pool).LockGrantBySelectors(ctx, mcpunannotatedgrants.LockGrantBySelectorsParams{
		OrganizationID: orgID,
		PrincipalUrn:   principal,
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].ExpiresAt.Valid)
	require.True(t, expiry.Equal(rows[0].ExpiresAt.Time), "the twin expires with the row it stands beside")
}

func TestRunScopesToOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	target := seedOrganization(t, ctx, pool)
	other := seedOrganization(t, ctx, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	rule := limited(authz.ScopeMCPConnect, serverOne, authz.DispositionReadOnly)
	seedGrant(t, ctx, pool, target, principal, rule)
	seedGrant(t, ctx, pool, other, principal, rule)

	report, err := mcpunannotatedgrants.Run(ctx, pool, mcpunannotatedgrants.Options{OrganizationID: target, Apply: true})
	require.NoError(t, err)
	require.Equal(t, mcpunannotatedgrants.Report{Principals: 1, Added: 1, Extended: 0}, report)
	require.Equal(t, []string{grantKey(rule)}, storedGrants(t, ctx, pool, other, principal))
}

func limited(scope authz.Scope, resourceID, disposition string) authz.Grant {
	selector := authz.NewSelector(scope, resourceID)
	selector[authz.SelectorKeyProjectID] = projectID
	selector[authz.SelectorKeyDisposition] = disposition
	return authz.NewGrantWithSelector(scope, selector)
}

func grantKey(grant authz.Grant) string {
	return string(grant.Scope) + " " + grant.Selector.Key()
}

func seedOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(pool).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     "Test Org",
		Slug:     orgID,
		WorkosID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return orgID
}

func seedGrant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal, grant authz.Grant) uuid.UUID {
	t.Helper()

	selectors, err := grant.Selector.MarshalJSON()
	require.NoError(t, err)
	row, err := accessrepo.New(pool).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: orgID,
		PrincipalUrn:   principal,
		Scope:          string(grant.Scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	return row.ID
}

func setExpiry(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, id uuid.UUID, expiresAt pgtype.Timestamptz) {
	t.Helper()

	affected, err := mcpunannotatedgrants.New(pool).SetGrantExpiry(ctx, mcpunannotatedgrants.SetGrantExpiryParams{ExpiresAt: expiresAt, ID: id, OrganizationID: orgID})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
}

func storedGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal) []string {
	t.Helper()

	rows, err := accessrepo.New(pool).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: orgID,
		PrincipalUrns:  []string{principal.String()},
	})
	require.NoError(t, err)
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		keys = append(keys, row.Scope+" "+selector.Key())
	}
	return keys
}

// callsUnannotated reports whether the principal may call a tool without
// annotations on server.
func callsUnannotated(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal, server string) bool {
	t.Helper()

	grants, err := authz.LoadGrants(ctx, pool, orgID, []urn.Principal{principal})
	require.NoError(t, err)
	check := authz.MCPServer(authz.MCPServerIDFrom(server), authz.ProjectIDFrom(projectID)).ToolCall("lookup", authz.DispositionUnannotated)
	allowed, err := authz.GrantsAuthorize(grants, check)
	require.NoError(t, err)
	return allowed
}
