package usage

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	"github.com/speakeasy-api/gram/server/internal/trials"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type fakeTrialSetupClient struct {
	*fakeStripeWebhookClient

	mu       sync.Mutex
	state    *stripeclient.TrialSetupCheckoutSessionState
	defaults []string
	detached []string
}

func (f *fakeTrialSetupClient) CreateTrialSetupCheckoutSession(context.Context, stripeclient.CreateTrialSetupCheckoutSessionInput) (*stripeclient.CheckoutSession, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTrialSetupClient) GetTrialSetupCheckoutSession(_ context.Context, id string) (*stripeclient.TrialSetupCheckoutSessionState, error) {
	if f.state == nil || f.state.ID != id {
		return nil, stripeclient.ErrCheckoutSessionNotFound
	}
	state := *f.state
	return &state, nil
}

func (f *fakeTrialSetupClient) DetachPaymentMethod(_ context.Context, paymentMethodID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detached = append(f.detached, paymentMethodID)
	return nil
}

func (f *fakeTrialSetupClient) SetDefaultPaymentMethod(_ context.Context, customerID, paymentMethodID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaults = append(f.defaults, customerID+"/"+paymentMethodID)
	return nil
}

type recordingTrialNotifier struct {
	trialemails.NoopNotifier

	mu      sync.Mutex
	started []string
}

func (r *recordingTrialNotifier) TrialStarted(_ context.Context, organizationID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, organizationID)
	return nil
}

const (
	trialSetupCustomerID = "cus_trial_placeholder"
	trialSetupSessionID  = "cs_trial_placeholder"
)

func newTrialSetupWebhookService(t *testing.T) (*Service, *pgxpool.Pool, *fakeTrialSetupClient, *recordingTrialNotifier) {
	t.Helper()
	service, db := newStripeWebhookService(t, trialSetupCustomerID, nil)
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          stripeWebhookOrganizationID,
		Name:        "Placeholder Organization",
		Slug:        "placeholder-organization",
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)
	base, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client := &fakeTrialSetupClient{fakeStripeWebhookClient: base, state: savedTrialCard(stripeWebhookOrganizationID, "fingerprint_placeholder")}
	client.verify = func([]byte, string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             "evt_trial_setup_placeholder",
			Type:           "checkout.session.completed",
			Created:        time.Time{},
			ObjectID:       trialSetupSessionID,
			CustomerID:     trialSetupCustomerID,
			SubscriptionID: "",
		}, nil
	}
	notifier := &recordingTrialNotifier{}
	service.stripeClient = client
	service.auditLogger = audit.NewLogger()
	service.trial = notifier
	service.productFeatures = &captureFeatureCache{}
	return service, db, client, notifier
}

func savedTrialCard(organizationID, fingerprint string) *stripeclient.TrialSetupCheckoutSessionState {
	return &stripeclient.TrialSetupCheckoutSessionState{
		ID:                trialSetupSessionID,
		Status:            "complete",
		TrialSetup:        true,
		OrganizationID:    organizationID,
		UserID:            "user_trial_placeholder",
		CustomerID:        trialSetupCustomerID,
		SetupIntentStatus: "succeeded",
		PaymentMethodID:   "pm_trial_placeholder",
		CardFingerprint:   fingerprint,
		CardFunding:       "credit",
	}
}

func requireNoTrial(t *testing.T, db *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := trialsrepo.New(db).GetTrial(t.Context(), organizationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestStripeWebhook_TrialSetupCheckoutStartsTrial(t *testing.T) {
	t.Parallel()

	service, db, client, notifier := newTrialSetupWebhookService(t)

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	trial, err := trialsrepo.New(db).GetTrial(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", trial.Tier)
	require.WithinDuration(t, time.Now().Add(14*24*time.Hour), trial.EndsAt.Time, time.Minute)

	org, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, org.Whitelisted)
	require.Equal(t, "enterprise", org.GramAccountType)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOrganizationEnterpriseTrialArmed)
	require.NoError(t, err)
	require.Contains(t, entry.ActorID, "user_trial_placeholder")

	require.Equal(t, []string{trialSetupCustomerID + "/pm_trial_placeholder"}, client.defaults)
	require.Equal(t, []string{stripeWebhookOrganizationID}, notifier.started)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))

	// A replayed event is answered from the receipt and starts nothing new.
	recorder = serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, []string{stripeWebhookOrganizationID}, notifier.started)
}

func TestStripeWebhook_TrialSetupCheckoutRejectsCardThatStartedAnotherTrial(t *testing.T) {
	t.Parallel()

	service, db, client, notifier := newTrialSetupWebhookService(t)

	const otherOrganizationID = "org_other_placeholder"
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{
		ID:   otherOrganizationID,
		Name: "Other Organization",
		Slug: "other-organization",
	}))
	_, err := trialsrepo.New(db).RecordTrialPaymentMethod(t.Context(), trialsrepo.RecordTrialPaymentMethodParams{
		OrganizationID:          otherOrganizationID,
		StripeCustomerID:        "cus_other_placeholder",
		StripePaymentMethodID:   "pm_other_placeholder",
		CardFingerprint:         client.state.CardFingerprint,
		CardFunding:             "credit",
		StripeCheckoutSessionID: "cs_other_placeholder",
		RejectionReason:         pgtype.Text{},
	})
	require.NoError(t, err)

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	requireNoTrial(t, db, stripeWebhookOrganizationID)
	card, err := trialsrepo.New(db).GetTrialPaymentMethod(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, trials.CardRejectionUsed, card.RejectionReason.String)
	require.Equal(t, []string{"pm_trial_placeholder"}, client.detached, "a rejected card is not left on the customer")
	require.Empty(t, client.defaults)
	require.Empty(t, notifier.started)

	sharing, err := trialsrepo.New(db).ListOrganizationsSharingTrialCard(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Len(t, sharing, 1)
	require.Equal(t, otherOrganizationID, sharing[0].OrganizationID)
}

func TestStripeWebhook_TrialSetupCheckoutRejectsPrepaidCardThenAcceptsAnother(t *testing.T) {
	t.Parallel()

	service, db, client, notifier := newTrialSetupWebhookService(t)
	client.state.CardFunding = "prepaid"

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	requireNoTrial(t, db, stripeWebhookOrganizationID)
	card, err := trialsrepo.New(db).GetTrialPaymentMethod(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, trials.CardRejectionPrepaid, card.RejectionReason.String)

	// The admin adds a credit card in a new Checkout.
	client.state = savedTrialCard(stripeWebhookOrganizationID, "fingerprint_second_placeholder")
	client.state.ID = "cs_second_placeholder"
	client.state.PaymentMethodID = "pm_second_placeholder"
	client.verify = func([]byte, string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID: "evt_second_placeholder", Type: "checkout.session.completed", Created: time.Time{},
			ObjectID: "cs_second_placeholder", CustomerID: trialSetupCustomerID, SubscriptionID: "",
		}, nil
	}
	recorder = serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	_, err = trialsrepo.New(db).GetTrial(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	card, err = trialsrepo.New(db).GetTrialPaymentMethod(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, card.RejectionReason.Valid, "the accepted card replaces the rejected one")
	require.Equal(t, []string{stripeWebhookOrganizationID}, notifier.started)
}

func TestStripeWebhook_TrialSetupCheckoutWithoutSavedCardStartsNothing(t *testing.T) {
	t.Parallel()

	service, db, client, notifier := newTrialSetupWebhookService(t)
	client.state.SetupIntentStatus = "requires_action"

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	requireNoTrial(t, db, stripeWebhookOrganizationID)
	require.Empty(t, notifier.started)
	require.Zero(t, stripeWebhookReceiptCount(t, db), "Stripe can redeliver once the card is saved")
}

func TestStripeWebhook_TrialSetupCheckoutForIneligibleOrganizationStartsNothing(t *testing.T) {
	t.Parallel()

	service, db, _, notifier := newTrialSetupWebhookService(t)
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          stripeWebhookOrganizationID,
		Name:        "Placeholder Organization",
		Slug:        "placeholder-organization",
		Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	requireNoTrial(t, db, stripeWebhookOrganizationID)
	require.Empty(t, notifier.started)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeWebhook_CheckoutWithoutSubscriptionThatIsNotTrialSetupIsRejected(t *testing.T) {
	t.Parallel()

	service, db, client, _ := newTrialSetupWebhookService(t)
	client.state.TrialSetup = false

	recorder := serveStripeWebhook(service, "{}")
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	requireNoTrial(t, db, stripeWebhookOrganizationID)
}

type trialSetupCheckoutClient struct {
	*checkoutStripeClient

	mu     sync.Mutex
	inputs []stripeclient.CreateTrialSetupCheckoutSessionInput
}

func (c *trialSetupCheckoutClient) CreateTrialSetupCheckoutSession(_ context.Context, input stripeclient.CreateTrialSetupCheckoutSessionInput) (*stripeclient.CheckoutSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inputs = append(c.inputs, input)
	return &stripeclient.CheckoutSession{ID: trialSetupSessionID, URL: "", ClientSecret: "cs_secret_placeholder"}, nil
}

func (c *trialSetupCheckoutClient) GetTrialSetupCheckoutSession(context.Context, string) (*stripeclient.TrialSetupCheckoutSessionState, error) {
	return nil, errors.New("not implemented")
}

func (c *trialSetupCheckoutClient) SetDefaultPaymentMethod(context.Context, string, string) error {
	return errors.New("not implemented")
}

func (c *trialSetupCheckoutClient) DetachPaymentMethod(context.Context, string) error {
	return errors.New("not implemented")
}

func newTrialSetupCheckoutInstance(t *testing.T) (*stripeCheckoutTestInstance, *trialSetupCheckoutClient) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	client := &trialSetupCheckoutClient{checkoutStripeClient: ti.stripe}
	ti.service.stripeClient = client
	return ti, client
}

func TestCreateTrialPaymentMethodCheckout_CreatesSetupCheckout(t *testing.T) {
	t.Parallel()

	ti, client := newTrialSetupCheckoutInstance(t)

	checkout, err := ti.service.CreateTrialPaymentMethodCheckout(ti.adminContext(t), &gen.CreateTrialPaymentMethodCheckoutPayload{Theme: "dark"})
	require.NoError(t, err)
	require.Equal(t, "cs_secret_placeholder", checkout.ClientSecret)
	require.NotEmpty(t, checkout.PublishableKey)

	require.Len(t, client.inputs, 1)
	input := client.inputs[0]
	require.Equal(t, ti.orgID, input.OrganizationID)
	require.Equal(t, ti.userID, input.UserID)
	require.True(t, input.Dark)
	require.Equal(t, "https://app.example.test/start-trial?session_id={CHECKOUT_SESSION_ID}", input.ReturnURL)
	require.True(t, strings.HasPrefix(input.IdempotencyKey, "trial-setup:"+ti.orgID+":"))

	metadata, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, input.CustomerID, metadata.StripeCustomerID.String, "the webhook resolves the organization from the stored customer")
}

func TestCreateTrialPaymentMethodCheckout_RequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti, client := newTrialSetupCheckoutInstance(t)

	_, err := ti.service.CreateTrialPaymentMethodCheckout(ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID)), &gen.CreateTrialPaymentMethodCheckoutPayload{Theme: "light"})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Empty(t, client.inputs)
}

func TestCreateTrialPaymentMethodCheckout_RejectsOrganizationThatCannotStartTrial(t *testing.T) {
	t.Parallel()

	ti, client := newTrialSetupCheckoutInstance(t)
	require.NoError(t, trialsrepo.New(ti.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		CreatedAt:      pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ConvertedAt:    pgtype.Timestamptz{},
		DemotedAt:      pgtype.Timestamptz{},
	}))

	_, err := ti.service.CreateTrialPaymentMethodCheckout(ti.adminContext(t), &gen.CreateTrialPaymentMethodCheckoutPayload{Theme: "light"})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Empty(t, client.inputs)
}
