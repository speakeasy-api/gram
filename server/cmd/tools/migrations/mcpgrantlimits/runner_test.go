package mcpgrantlimits_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/mcpgrantlimits"
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

func TestRunRewritesLimitedRulesOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())

	seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPRead, serverOne, authz.SelectorKeyTool, "search"))
	seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPRead, serverOne, authz.SelectorKeyTool, "lookup"))
	seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPWrite, serverTwo, authz.SelectorKeyDisposition, authz.DispositionDestructive))
	seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPBlockedRead, serverTwo, authz.SelectorKeyTool, "search"))
	// Limits on connect are what limits are for, and stay.
	connect := limited(authz.ScopeMCPConnect, serverOne, authz.SelectorKeyTool, "search")
	blockedConnect := limited(authz.ScopeMCPBlockedConnect, serverOne, authz.SelectorKeyDisposition, authz.DispositionDestructive)
	seedGrant(t, ctx, pool, orgID, principal, connect)
	seedGrant(t, ctx, pool, orgID, principal, blockedConnect)

	before := storedGrants(t, ctx, pool, orgID, principal)
	decisionsBefore := decisions(t, ctx, pool, orgID, principal)
	want := mcpgrantlimits.Report{Principals: 1, LimitsRemoved: 2, Merged: 1, BlocksRemoved: 1}

	report, err := mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: "", Apply: false})
	require.NoError(t, err)
	require.Equal(t, want, report)
	require.Equal(t, before, storedGrants(t, ctx, pool, orgID, principal), "a preview writes nothing")

	report, err = mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, want, report)
	require.ElementsMatch(t, []string{
		grantKey(authz.NewGrantWithSelector(authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, serverOne))),
		grantKey(authz.NewGrantWithSelector(authz.ScopeMCPWrite, authz.NewSelector(authz.ScopeMCPWrite, serverTwo))),
		grantKey(connect),
		grantKey(blockedConnect),
	}, storedGrants(t, ctx, pool, orgID, principal))
	require.Equal(t, decisionsBefore, decisions(t, ctx, pool, orgID, principal), "the rewrite changes no decision")

	report, err = mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, mcpgrantlimits.Report{Principals: 0, LimitsRemoved: 0, Merged: 0, BlocksRemoved: 0}, report)
}

func TestRunKeepsTheLongerExpiryWhenMerging(t *testing.T) {
	t.Parallel()

	soon := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	later := soon.Add(24 * time.Hour)
	dated := func(at time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: at, InfinityModifier: pgtype.Finite, Valid: true}
	}
	permanent := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	for _, tc := range []struct {
		name                  string
		unlimited, limitedOne pgtype.Timestamptz
		want                  pgtype.Timestamptz
	}{
		{name: "limited rule lasts longer", unlimited: dated(soon), limitedOne: dated(later), want: dated(later)},
		{name: "unlimited rule lasts longer", unlimited: dated(later), limitedOne: dated(soon), want: dated(later)},
		{name: "limited rule is permanent", unlimited: dated(soon), limitedOne: permanent, want: permanent},
		{name: "unlimited rule is permanent", unlimited: permanent, limitedOne: dated(later), want: permanent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			pool := newTestDB(t)
			orgID := seedOrganization(t, ctx, pool)
			principal := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
			unlimited := authz.NewGrantWithSelector(authz.ScopeMCPRead, authz.NewSelector(authz.ScopeMCPRead, serverOne))

			setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, unlimited), tc.unlimited)
			setExpiry(t, ctx, pool, orgID, seedGrant(t, ctx, pool, orgID, principal, limited(authz.ScopeMCPRead, serverOne, authz.SelectorKeyTool, "search")), tc.limitedOne)

			report, err := mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: orgID, Apply: true})
			require.NoError(t, err)
			require.Equal(t, mcpgrantlimits.Report{Principals: 1, LimitsRemoved: 0, Merged: 1, BlocksRemoved: 0}, report)

			selectors, err := unlimited.Selector.MarshalJSON()
			require.NoError(t, err)
			rows, err := mcpgrantlimits.New(pool).LockGrantBySelectors(ctx, mcpgrantlimits.LockGrantBySelectorsParams{
				OrganizationID: orgID,
				PrincipalUrn:   principal,
				Scope:          string(authz.ScopeMCPRead),
				Selectors:      selectors,
			})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, tc.want.Valid, rows[0].ExpiresAt.Valid)
			if tc.want.Valid {
				require.True(t, tc.want.Time.Equal(rows[0].ExpiresAt.Time), "want %s, got %s", tc.want.Time, rows[0].ExpiresAt.Time)
			}
			require.Len(t, storedGrants(t, ctx, pool, orgID, principal), 1)
		})
	}
}

func TestRunScopesToOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	target := seedOrganization(t, ctx, pool)
	other := seedOrganization(t, ctx, pool)
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	rule := limited(authz.ScopeMCPWrite, serverOne, authz.SelectorKeyTool, "search")
	seedGrant(t, ctx, pool, target, principal, rule)
	seedGrant(t, ctx, pool, other, principal, rule)

	report, err := mcpgrantlimits.Run(ctx, pool, mcpgrantlimits.Options{OrganizationID: target, Apply: true})
	require.NoError(t, err)
	require.Equal(t, mcpgrantlimits.Report{Principals: 1, LimitsRemoved: 1, Merged: 0, BlocksRemoved: 0}, report)
	require.Equal(t, []string{grantKey(rule)}, storedGrants(t, ctx, pool, other, principal))
}

func limited(scope authz.Scope, resourceID, key, value string) authz.Grant {
	selector := authz.NewSelector(scope, resourceID)
	selector[key] = value
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

	affected, err := mcpgrantlimits.New(pool).SetGrantExpiry(ctx, mcpgrantlimits.SetGrantExpiryParams{ExpiresAt: expiresAt, ID: id, OrganizationID: orgID})
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
}

// storedGrants reads the rows as they are stored, without the loaders that
// already present a limited rule as its effect.
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

func decisions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal) map[string]bool {
	t.Helper()

	grants, err := authz.LoadGrants(ctx, pool, orgID, []urn.Principal{principal})
	require.NoError(t, err)
	out := make(map[string]bool)
	for _, server := range []string{serverOne, serverTwo} {
		for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite, authz.ScopeMCPConnect} {
			allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(scope, server, projectID))
			require.NoError(t, err)
			out[string(scope)+" "+server] = allowed
		}
		for _, tool := range []string{"search", "delete"} {
			allowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(server, authz.MCPToolCallDimensions{Tool: tool, Disposition: authz.DispositionDestructive, ProjectID: projectID}))
			require.NoError(t, err)
			out["call "+tool+" "+server] = allowed
		}
	}
	return out
}
