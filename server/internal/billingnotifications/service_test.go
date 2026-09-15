package billingnotifications

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type capturedEmail struct {
	recipient string
	key       string
	template  email.Template
}

type captureSender struct {
	mu    sync.Mutex
	sends []capturedEmail
}

func (s *captureSender) SendIdempotent(_ context.Context, recipient, key string, template email.Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends = append(s.sends, capturedEmail{recipient: recipient, key: key, template: template})
	return nil
}

func newNotificationTestService(t *testing.T, accountType string, whitelisted bool) (*Service, *captureSender, string) {
	t.Helper()
	db, err := testInfra.CloneTestDatabase(t, "billing_notifications")
	require.NoError(t, err)
	organizationID := "org-billing-notifications"
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Example Organization",
		Slug:        "example-org",
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{Bool: whitelisted, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, orgrepo.New(db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		ID:              organizationID,
		GramAccountType: accountType,
	}))
	sender := &captureSender{}
	flags := new(feature.InMemory)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	return NewService(testenv.NewLogger(t), db, sender, flags, siteURL), sender, organizationID
}

func TestSendTrialEndingSoonUsesConfiguredBillingEmailAndStableTrialIdentity(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "enterprise", true)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, trialsrepo.New(service.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID,
		Tier:           "enterprise",
		CreatedAt:      pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: now.Add(7 * 24 * time.Hour), Valid: true},
		ConvertedAt:    pgtype.Timestamptz{},
		DemotedAt:      pgtype.Timestamptz{},
	}))
	_, err := usagerepo.New(service.db).UpsertBillingEmail(t.Context(), usagerepo.UpsertBillingEmailParams{
		OrganizationID: organizationID,
		AlertEmail:     pgtype.Text{String: "billing@example.test", Valid: true},
	})
	require.NoError(t, err)

	state, err := service.ResolveTrialReminder(t.Context(), organizationID)
	require.NoError(t, err)
	require.True(t, state.Active)
	require.Equal(t, state.TrialEndsAt.Add(-72*time.Hour), state.SendAt)
	result, err := service.SendTrialEndingSoon(t.Context(), SendTrialEndingSoonInput{
		OrganizationID: organizationID,
		TrialCreatedAt: state.TrialCreatedAt,
		TrialEndsAt:    state.TrialEndsAt,
	})
	require.NoError(t, err)
	require.False(t, result.Reschedule)
	require.Len(t, sender.sends, 1)
	require.Equal(t, "billing@example.test", sender.sends[0].recipient)
	require.Len(t, sender.sends[0].key, 64)
	template, ok := sender.sends[0].template.(email.TrialEndingSoon)
	require.True(t, ok)
	require.Equal(t, "https://app.example.test/example-org/billing", template.ActionURL)
}

func TestSendAccessPausedTrialDemotionFailsClosedAndUsesOrganizationGate(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "free", false)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, trialsrepo.New(service.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID,
		Tier:           "enterprise",
		CreatedAt:      pgtype.Timestamptz{Time: now.Add(-15 * 24 * time.Hour), Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true},
		ConvertedAt:    pgtype.Timestamptz{},
		DemotedAt:      pgtype.Timestamptz{Time: now, Valid: true},
	}))
	_, err := usagerepo.New(service.db).UpsertBillingEmail(t.Context(), usagerepo.UpsertBillingEmailParams{
		OrganizationID: organizationID,
		AlertEmail:     pgtype.Text{String: "billing@example.test", Valid: true},
	})
	require.NoError(t, err)
	input := SendAccessPausedInput{EventID: "event-placeholder", OrganizationID: organizationID, Kind: AccessPausedTrialDemotion}

	require.NoError(t, service.SendAccessPaused(t.Context(), input))
	require.Empty(t, sender.sends)
	flags, ok := service.features.(*feature.InMemory)
	require.True(t, ok)
	flags.SetFlag(feature.FlagPaygSelfServeBilling, organizationID, true)
	require.NoError(t, service.SendAccessPaused(t.Context(), input))
	require.Len(t, sender.sends, 1)
	template, ok := sender.sends[0].template.(email.AccessPaused)
	require.True(t, ok)
	require.Equal(t, "https://app.example.test/example-org", template.ActionURL)
}

func TestSendAccessPausedSubscriptionLossDoesNotDependOnRolloutFlag(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "free", false)
	_, err := usagerepo.New(service.db).UpsertBillingEmail(t.Context(), usagerepo.UpsertBillingEmailParams{
		OrganizationID: organizationID,
		AlertEmail:     pgtype.Text{String: "billing@example.test", Valid: true},
	})
	require.NoError(t, err)

	require.NoError(t, service.SendAccessPaused(t.Context(), SendAccessPausedInput{
		EventID:        "event-placeholder",
		OrganizationID: organizationID,
		Kind:           AccessPausedSubscriptionLoss,
	}))
	require.Len(t, sender.sends, 1)
	require.Equal(t, "billing@example.test", sender.sends[0].recipient)
}

func TestSendTrialEndingSoonDeliversResolvedRecipientsBeforeReturningResolutionError(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "enterprise", true)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, trialsrepo.New(service.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: organizationID,
		Tier:           "enterprise",
		CreatedAt:      pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: now.Add(7 * 24 * time.Hour), Valid: true},
		ConvertedAt:    pgtype.Timestamptz{},
		DemotedAt:      pgtype.Timestamptz{},
	}))
	resolutionErr := errors.New("one administrator could not be resolved")
	service.resolveRecipients = func(context.Context, accessrepo.DBTX, string, string, *string) ([]string, error) {
		return []string{"resolved@example.test"}, resolutionErr
	}
	state, err := service.ResolveTrialReminder(t.Context(), organizationID)
	require.NoError(t, err)

	_, err = service.SendTrialEndingSoon(t.Context(), SendTrialEndingSoonInput{
		OrganizationID: organizationID,
		TrialCreatedAt: state.TrialCreatedAt,
		TrialEndsAt:    state.TrialEndsAt,
	})
	require.ErrorIs(t, err, resolutionErr)
	require.Len(t, sender.sends, 1)
	require.Equal(t, "resolved@example.test", sender.sends[0].recipient)
}

func TestSendAccessPausedDeliversResolvedRecipientsBeforeReturningResolutionError(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "free", false)
	resolutionErr := errors.New("one administrator could not be resolved")
	service.resolveRecipients = func(context.Context, accessrepo.DBTX, string, string, *string) ([]string, error) {
		return []string{"resolved@example.test"}, resolutionErr
	}

	err := service.SendAccessPaused(t.Context(), SendAccessPausedInput{
		EventID:        "event-placeholder",
		OrganizationID: organizationID,
		Kind:           AccessPausedSubscriptionLoss,
	})
	require.ErrorIs(t, err, resolutionErr)
	require.Len(t, sender.sends, 1)
	require.Equal(t, "resolved@example.test", sender.sends[0].recipient)
}

func seedPaygStripeSubscription(t *testing.T, service *Service, organizationID string) {
	t.Helper()
	queries := usagerepo.New(service.db)
	customerID := pgtype.Text{String: "<STRIPE_CUSTOMER_ID>", Valid: true}
	require.NoError(t, queries.CreateStripeBillingMetadataFixture(t.Context(), usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: customerID,
	}))
	activatePaygStripeSubscription(t, service, organizationID)
}

func activatePaygStripeSubscription(t *testing.T, service *Service, organizationID string) {
	t.Helper()
	anchor := time.Now().UTC().Truncate(24 * time.Hour)
	_, err := usagerepo.New(service.db).ActivatePaygBillingMetadata(t.Context(), usagerepo.ActivatePaygBillingMetadataParams{
		StripeSubscriptionID:     pgtype.Text{String: "<STRIPE_SUBSCRIPTION_ID>", Valid: true},
		StripeBillingCycleAnchor: pgtype.Timestamptz{Time: anchor, InfinityModifier: pgtype.Finite, Valid: true},
		BillingCycleAnchorDay:    int32(anchor.Day()),
		OrganizationID:           organizationID,
		StripeCustomerID:         pgtype.Text{String: "<STRIPE_CUSTOMER_ID>", Valid: true},
	})
	require.NoError(t, err)
}

func TestSendPaygActivatedRequiresActivePaygSubscription(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "payg", true)
	input := SendPaygActivatedInput{EventID: "event-placeholder", OrganizationID: organizationID}

	require.NoError(t, service.SendPaygActivated(t.Context(), input))
	require.Empty(t, sender.sends)

	require.NoError(t, usagerepo.New(service.db).CreateStripeBillingMetadataFixture(t.Context(), usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "<STRIPE_CUSTOMER_ID>", Valid: true},
	}))
	_, err := usagerepo.New(service.db).UpsertBillingEmail(t.Context(), usagerepo.UpsertBillingEmailParams{
		OrganizationID: organizationID,
		AlertEmail:     pgtype.Text{String: "billing@example.test", Valid: true},
	})
	require.NoError(t, err)

	require.NoError(t, service.SendPaygActivated(t.Context(), input))
	require.Empty(t, sender.sends)

	activatePaygStripeSubscription(t, service, organizationID)

	require.NoError(t, service.SendPaygActivated(t.Context(), input))
	require.Len(t, sender.sends, 1)
	require.Equal(t, "billing@example.test", sender.sends[0].recipient)
	template, ok := sender.sends[0].template.(email.PaygActivated)
	require.True(t, ok)
	require.Equal(t, "Example Organization", template.OrganizationName)
	require.Equal(t, billing.TUMPricePerMillionUSD, template.TumPricePerMillionUsd)
	require.Equal(t, "https://app.example.test/example-org/billing", template.ActionURL)

	// Each activation carries its own durable event, so a later return to PAYG
	// derives a distinct provider key and sends again.
	require.NoError(t, service.SendPaygActivated(t.Context(), SendPaygActivatedInput{EventID: "later-event-placeholder", OrganizationID: organizationID}))
	require.Len(t, sender.sends, 2)
	require.NotEqual(t, sender.sends[0].key, sender.sends[1].key)
}

func TestSendPaygActivatedSkipsOrganizationsOffPayg(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "free", false)
	seedPaygStripeSubscription(t, service, organizationID)

	require.NoError(t, service.SendPaygActivated(t.Context(), SendPaygActivatedInput{EventID: "event-placeholder", OrganizationID: organizationID}))
	require.Empty(t, sender.sends)
}

func TestSendPaygActivatedFallsBackToOrganizationAdministrators(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "payg", true)
	seedPaygStripeSubscription(t, service, organizationID)
	var requestedTier string
	var requestedEmail *string
	service.resolveRecipients = func(_ context.Context, _ accessrepo.DBTX, _ string, accountType string, configuredEmail *string) ([]string, error) {
		requestedTier = accountType
		requestedEmail = configuredEmail
		return []string{"admin@example.test"}, nil
	}

	require.NoError(t, service.SendPaygActivated(t.Context(), SendPaygActivatedInput{EventID: "event-placeholder", OrganizationID: organizationID}))
	require.Equal(t, string(billing.TierPayg), requestedTier)
	require.Nil(t, requestedEmail)
	require.Len(t, sender.sends, 1)
	require.Equal(t, "admin@example.test", sender.sends[0].recipient)
}

func TestSendPaygActivatedDeliversResolvedRecipientsBeforeReturningResolutionError(t *testing.T) {
	t.Parallel()
	service, sender, organizationID := newNotificationTestService(t, "payg", true)
	seedPaygStripeSubscription(t, service, organizationID)
	resolutionErr := errors.New("resolve recipients")
	service.resolveRecipients = func(context.Context, accessrepo.DBTX, string, string, *string) ([]string, error) {
		return []string{"admin@example.test"}, resolutionErr
	}

	err := service.SendPaygActivated(t.Context(), SendPaygActivatedInput{EventID: "event-placeholder", OrganizationID: organizationID})
	require.ErrorIs(t, err, resolutionErr)
	require.Len(t, sender.sends, 1)
}
