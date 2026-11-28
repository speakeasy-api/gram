package admin

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestTrialCardSharingOrganizations_ProjectsIdentityAndOutcomeOnly(t *testing.T) {
	t.Parallel()

	addedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	created := pgtype.Timestamptz{Time: addedAt, Valid: true, InfinityModifier: pgtype.Finite}
	got := trialCardSharingOrganizations([]trialsRepo.ListOrganizationsSharingTrialCardRow{
		{OrganizationID: "org_started", OrganizationName: "Started Co", OrganizationSlug: "started-co", OfferedAt: created},
		{OrganizationID: "org_rejected", OrganizationName: "Rejected Co", OrganizationSlug: "rejected-co", RejectionReason: conv.ToPGText(trials.CardRejectionUsed), OfferedAt: created},
	})

	require.Equal(t, []*gen.AdminTrialCardSharingOrganization{
		{ID: "org_started", Name: "Started Co", Slug: "started-co", CardAddedAt: "2026-03-04T05:06:07Z", RejectionReason: nil},
		{ID: "org_rejected", Name: "Rejected Co", Slug: "rejected-co", CardAddedAt: "2026-03-04T05:06:07Z", RejectionReason: new(trials.CardRejectionUsed)},
	}, got)
}

func TestTrialCardSharingOrganizations_EmptyWhenNoneShare(t *testing.T) {
	t.Parallel()

	got := trialCardSharingOrganizations(nil)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestGetOrganization_ListsOrganizationsSharingTrialCard(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_card_first", name: "First Co", slug: "first-co"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_card_second", name: "Second Co", slug: "second-co"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_card_prepaid", name: "Prepaid Co", slug: "prepaid-co"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_card_other", name: "Other Co", slug: "other-co"})

	q := trialsRepo.New(conn)
	for _, card := range []trialsRepo.RecordTrialPaymentMethodParams{
		{OrganizationID: "org_card_first", StripeCustomerID: "cus_first", StripePaymentMethodID: "pm_first", CardFingerprint: "fp_shared", CardFunding: "credit", StripeCheckoutSessionID: "cs_first"},
		{OrganizationID: "org_card_second", StripeCustomerID: "cus_second", StripePaymentMethodID: "pm_second", CardFingerprint: "fp_shared", CardFunding: "credit", StripeCheckoutSessionID: "cs_second", RejectionReason: conv.ToPGText(trials.CardRejectionUsed)},
		{OrganizationID: "org_card_prepaid", StripeCustomerID: "cus_prepaid", StripePaymentMethodID: "pm_prepaid", CardFingerprint: "fp_prepaid", CardFunding: "prepaid", StripeCheckoutSessionID: "cs_prepaid", RejectionReason: conv.ToPGText(trials.CardRejectionPrepaid)},
		{OrganizationID: "org_card_other", StripeCustomerID: "cus_other", StripePaymentMethodID: "pm_other", CardFingerprint: "fp_unique", CardFunding: "debit", StripeCheckoutSessionID: "cs_other"},
	} {
		rows, err := q.RecordTrialPaymentMethod(ctx, card)
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}

	second, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "second-co"})
	require.NoError(t, err)
	require.NotNil(t, second.TrialCardAddedAt)
	require.Equal(t, new(trials.CardRejectionUsed), second.TrialCardRejectionReason)
	require.Len(t, second.OrganizationsSharingTrialCard, 1)
	shared := second.OrganizationsSharingTrialCard[0]
	require.Equal(t, "org_card_first", shared.ID)
	require.Equal(t, "First Co", shared.Name)
	require.Equal(t, "first-co", shared.Slug)
	require.NotEmpty(t, shared.CardAddedAt)
	require.Nil(t, shared.RejectionReason, "the card started the first organization's trial")

	first, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_card_first"})
	require.NoError(t, err)
	require.NotNil(t, first.TrialCardAddedAt)
	require.Nil(t, first.TrialCardRejectionReason)
	require.Len(t, first.OrganizationsSharingTrialCard, 1)
	require.Equal(t, "org_card_second", first.OrganizationsSharingTrialCard[0].ID)
	require.Equal(t, new(trials.CardRejectionUsed), first.OrganizationsSharingTrialCard[0].RejectionReason)

	prepaid, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_card_prepaid"})
	require.NoError(t, err)
	require.NotNil(t, prepaid.TrialCardAddedAt)
	require.Equal(t, new(trials.CardRejectionPrepaid), prepaid.TrialCardRejectionReason)
	require.Empty(t, prepaid.OrganizationsSharingTrialCard)

	unique, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_card_other"})
	require.NoError(t, err)
	require.NotNil(t, unique.TrialCardAddedAt)
	require.Nil(t, unique.TrialCardRejectionReason)
	require.Empty(t, unique.OrganizationsSharingTrialCard)
}

func TestGetOrganization_OmitsTrialCardWithoutOne(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_no_card", name: "No Card Co", slug: "no-card-co"})

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_no_card"})
	require.NoError(t, err)
	require.Nil(t, got.TrialCardAddedAt)
	require.Nil(t, got.TrialCardRejectionReason)
	require.Nil(t, got.OrganizationsSharingTrialCard)
}

func TestListOrganizations_OmitsTrialCard(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_list_card", name: "List Card Co", slug: "list-card-co"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_list_card_peer", name: "Peer Co", slug: "peer-co"})

	q := trialsRepo.New(conn)
	for _, card := range []trialsRepo.RecordTrialPaymentMethodParams{
		{OrganizationID: "org_list_card", StripeCustomerID: "cus_list", StripePaymentMethodID: "pm_list", CardFingerprint: "fp_list", CardFunding: "credit", StripeCheckoutSessionID: "cs_list"},
		{OrganizationID: "org_list_card_peer", StripeCustomerID: "cus_peer", StripePaymentMethodID: "pm_peer", CardFingerprint: "fp_list", CardFunding: "credit", StripeCheckoutSessionID: "cs_peer", RejectionReason: conv.ToPGText(trials.CardRejectionUsed)},
	} {
		_, err := q.RecordTrialPaymentMethod(ctx, card)
		require.NoError(t, err)
	}

	result, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Organizations)
	for _, org := range result.Organizations {
		require.Nil(t, org.TrialCardAddedAt)
		require.Nil(t, org.TrialCardRejectionReason)
		require.Nil(t, org.OrganizationsSharingTrialCard)
	}
}
