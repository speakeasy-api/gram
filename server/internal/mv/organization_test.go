package mv_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/mv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type organizationBillingRepo struct {
	billing.Repository
	getCustomerTier func(context.Context, string) (*billing.Tier, bool, error)
}

func (r *organizationBillingRepo) GetCustomerTier(ctx context.Context, orgID string) (*billing.Tier, bool, error) {
	return r.getCustomerTier(ctx, orgID)
}

func TestDescribeOrganizationPaygIsAuthoritative(t *testing.T) {
	t.Parallel()

	queries, orgID := seedOrganization(t, billing.TierPayg)
	repo := &organizationBillingRepo{
		getCustomerTier: func(context.Context, string) (*billing.Tier, bool, error) {
			t.Fatal("billing provider must not be called for PAYG organizations")
			return nil, false, nil
		},
	}

	got, err := mv.DescribeOrganization(t.Context(), testenv.NewLogger(t), queries, repo, orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierPayg), got.GramAccountType)
	require.True(t, got.HasActiveSubscription)

	persisted, err := queries.GetOrganizationMetadata(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierPayg), persisted.GramAccountType)
}

func TestDescribeOrganizationDoesNotOverwriteConcurrentPaygUpdate(t *testing.T) {
	t.Parallel()

	queries, orgID := seedOrganization(t, billing.TierBase)
	repo := &organizationBillingRepo{
		getCustomerTier: func(ctx context.Context, gotOrgID string) (*billing.Tier, bool, error) {
			require.Equal(t, orgID, gotOrgID)
			require.NoError(t, queries.SetAccountType(ctx, orgrepo.SetAccountTypeParams{
				GramAccountType: string(billing.TierPayg),
				ID:              orgID,
			}))
			tier := billing.TierPro
			return &tier, false, nil
		},
	}

	got, err := mv.DescribeOrganization(t.Context(), testenv.NewLogger(t), queries, repo, orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierPayg), got.GramAccountType)
	require.True(t, got.HasActiveSubscription)

	persisted, err := queries.GetOrganizationMetadata(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierPayg), persisted.GramAccountType)
}

func TestDescribeOrganizationPreservesResolvedTierWhenPersistenceFails(t *testing.T) {
	t.Parallel()

	queries, orgID := seedOrganization(t, billing.TierBase)
	ctx, cancel := context.WithCancel(t.Context())
	repo := &organizationBillingRepo{
		getCustomerTier: func(context.Context, string) (*billing.Tier, bool, error) {
			cancel()
			tier := billing.TierPro
			return &tier, true, nil
		},
	}

	got, err := mv.DescribeOrganization(ctx, testenv.NewLogger(t), queries, repo, orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierPro), got.GramAccountType)
	require.True(t, got.HasActiveSubscription)

	persisted, err := queries.GetOrganizationMetadata(t.Context(), orgID)
	require.NoError(t, err)
	require.Equal(t, string(billing.TierBase), persisted.GramAccountType)
}

func seedOrganization(t *testing.T, tier billing.Tier) (*orgrepo.Queries, string) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	queries := orgrepo.New(conn)
	orgID := uuid.NewString()
	_, err = queries.UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Organization",
		Slug:        "test-org-" + orgID[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	require.NoError(t, queries.SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		GramAccountType: string(tier),
		ID:              orgID,
	}))

	return queries, orgID
}
