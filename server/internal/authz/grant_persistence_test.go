package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGrantWritesNormalizeConflictingLegacyEffect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		upsert bool
	}{
		{
			name:   "upsert",
			upsert: true,
		},
		{
			name:   "insert if absent",
			upsert: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			conn := newTestDB(t)
			organizationID := "org_legacy_grant_effect"
			principal := urn.NewPrincipal(urn.PrincipalTypeUser, "legacy_user")
			selector, err := NewSelector(ScopeProjectRead, "project_123").MarshalJSON()
			require.NoError(t, err)
			seedOrganization(t, ctx, conn, organizationID)

			fixtures := testrepo.New(conn)
			require.NoError(t, fixtures.InsertLegacyDenyPrincipalGrantFixture(ctx, testrepo.InsertLegacyDenyPrincipalGrantFixtureParams{
				OrganizationID: organizationID,
				PrincipalUrn:   principal,
				Scope:          string(ScopeProjectRead),
				Selectors:      selector,
			}))

			q := accessrepo.New(conn)
			if testCase.upsert {
				_, err = q.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
					OrganizationID: organizationID,
					PrincipalUrn:   principal,
					Scope:          string(ScopeProjectRead),
					Selectors:      selector,
				})
			} else {
				_, err = q.InsertPrincipalGrantIfAbsent(ctx, accessrepo.InsertPrincipalGrantIfAbsentParams{
					OrganizationID: organizationID,
					PrincipalUrn:   principal,
					Scope:          string(ScopeProjectRead),
					Selectors:      selector,
				})
			}
			require.NoError(t, err)

			effect, err := fixtures.GetPrincipalGrantEffectFixture(ctx, testrepo.GetPrincipalGrantEffectFixtureParams{
				OrganizationID: organizationID,
				PrincipalUrn:   principal,
				Scope:          string(ScopeProjectRead),
				Selectors:      selector,
			})
			require.NoError(t, err)
			require.False(t, effect.Valid)
		})
	}
}
