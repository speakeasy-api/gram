package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	audittestrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type checkoutStripeClient struct {
	mu sync.Mutex

	customers                map[string]*stripeclient.Customer
	checkoutResults          map[string]checkoutStripeResult
	customerInputs           []stripeclient.CreateCustomerInput
	checkoutInputs           []stripeclient.CreateCheckoutSessionInput
	customerError            error
	checkoutError            error
	afterCheckoutCreate      func()
	expireCheckoutError      error
	beforeCheckoutExpire     func(string)
	expiredCheckoutIDs       []string
	checkoutState            *stripeclient.CheckoutSessionState
	checkoutGetErr           error
	subscriptionState        *stripeclient.SubscriptionState
	subscriptionReadError    error
	subscriptionUpdateError  error
	subscriptionUpdates      []stripeclient.SetSubscriptionCancelAtPeriodEndInput
	afterSubscriptionUpdate  func()
	portalInputs             []stripeclient.CreatePortalSessionInput
	portalError              error
	portalCustomerIDOverride string
}

type checkoutStripeResult struct {
	input stripeclient.CreateCheckoutSessionInput
	id    string
	url   string
}

func newCheckoutStripeClient() *checkoutStripeClient {
	return &checkoutStripeClient{
		customers:           make(map[string]*stripeclient.Customer),
		checkoutResults:     make(map[string]checkoutStripeResult),
		customerInputs:      make([]stripeclient.CreateCustomerInput, 0),
		checkoutInputs:      make([]stripeclient.CreateCheckoutSessionInput, 0),
		subscriptionUpdates: make([]stripeclient.SetSubscriptionCancelAtPeriodEndInput, 0),
		portalInputs:        make([]stripeclient.CreatePortalSessionInput, 0),
	}
}

func (c *checkoutStripeClient) CreateCustomer(_ context.Context, input stripeclient.CreateCustomerInput) (*stripeclient.Customer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.customerInputs = append(c.customerInputs, input)
	if c.customerError != nil {
		return nil, c.customerError
	}
	if customer, ok := c.customers[input.IdempotencyKey]; ok {
		return customer, nil
	}
	customer := &stripeclient.Customer{ID: fmt.Sprintf("cus_%d", len(c.customers)+1)}
	c.customers[input.IdempotencyKey] = customer
	return customer, nil
}

func (c *checkoutStripeClient) CreateCheckoutSession(_ context.Context, input stripeclient.CreateCheckoutSessionInput) (*stripeclient.CheckoutSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.checkoutInputs = append(c.checkoutInputs, input)
	if c.checkoutError != nil {
		return nil, c.checkoutError
	}
	if result, ok := c.checkoutResults[input.IdempotencyKey]; ok {
		if !reflect.DeepEqual(result.input, input) {
			return nil, fmt.Errorf("idempotency key %q reused with different checkout input", input.IdempotencyKey)
		}
		return &stripeclient.CheckoutSession{ID: result.id, URL: result.url}, nil
	}
	checkoutID := fmt.Sprintf("cs_%d", len(c.checkoutResults)+1)
	checkoutURL := fmt.Sprintf("https://checkout.stripe.test/%d", len(c.checkoutResults)+1)
	c.checkoutResults[input.IdempotencyKey] = checkoutStripeResult{input: input, id: checkoutID, url: checkoutURL}
	if c.afterCheckoutCreate != nil {
		c.afterCheckoutCreate()
	}
	return &stripeclient.CheckoutSession{ID: checkoutID, URL: checkoutURL}, nil
}

func (c *checkoutStripeClient) ExpireCheckoutSession(_ context.Context, id string) error {
	c.mu.Lock()
	err := c.expireCheckoutError
	hook := c.beforeCheckoutExpire
	if err == nil {
		c.expiredCheckoutIDs = append(c.expiredCheckoutIDs, id)
	}
	c.mu.Unlock()

	if err != nil {
		return err
	}
	if hook != nil {
		hook(id)
	}
	return nil
}

func (c *checkoutStripeClient) GetCheckoutSession(_ context.Context, id string) (*stripeclient.CheckoutSessionState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.checkoutGetErr != nil {
		return nil, c.checkoutGetErr
	}
	if c.checkoutState == nil || c.checkoutState.ID != id {
		return nil, errors.New("checkout session not found")
	}
	state := *c.checkoutState
	return &state, nil
}

func (c *checkoutStripeClient) GetSubscription(context.Context, string) (*stripeclient.SubscriptionState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subscriptionReadError != nil {
		return nil, c.subscriptionReadError
	}
	if c.subscriptionState == nil {
		return nil, errors.New("subscription not found")
	}
	state := *c.subscriptionState
	return &state, nil
}

func (c *checkoutStripeClient) SetSubscriptionCancelAtPeriodEnd(_ context.Context, input stripeclient.SetSubscriptionCancelAtPeriodEndInput) (*stripeclient.SubscriptionState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscriptionUpdates = append(c.subscriptionUpdates, input)
	// Separate from the read error so a test can fail the write alone: one
	// field for both would short-circuit at the read every lifecycle call makes
	// first, leaving the write's own failure path never exercised.
	if c.subscriptionUpdateError != nil {
		return nil, c.subscriptionUpdateError
	}
	if c.subscriptionState == nil {
		return nil, errors.New("subscription not found")
	}
	c.subscriptionState.CancelAtPeriodEnd = input.CancelAtPeriodEnd
	state := *c.subscriptionState
	if c.afterSubscriptionUpdate != nil {
		c.afterSubscriptionUpdate()
	}
	return &state, nil
}

func (c *checkoutStripeClient) CreatePortalSession(_ context.Context, input stripeclient.CreatePortalSessionInput) (*stripeclient.PortalSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.portalInputs = append(c.portalInputs, input)
	if c.portalError != nil {
		return nil, c.portalError
	}
	customerID := input.CustomerID
	if c.portalCustomerIDOverride != "" {
		customerID = c.portalCustomerIDOverride
	}
	return &stripeclient.PortalSession{ID: "bps_test", CustomerID: customerID, URL: "https://billing.stripe.test/session"}, nil
}

func (c *checkoutStripeClient) CreateMeterEvent(context.Context, stripeclient.CreateMeterEventInput) error {
	return errors.New("not implemented")
}

func (c *checkoutStripeClient) GetMeterEventSummary(context.Context, stripeclient.GetMeterEventSummaryInput) (float64, error) {
	return 0, errors.New("not implemented")
}

func (c *checkoutStripeClient) GetInvoice(context.Context, string) (*stripeclient.InvoiceState, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) CreateInvoiceItem(context.Context, stripeclient.CreateInvoiceItemInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) CreateCreditNote(context.Context, stripeclient.CreateCreditNoteInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) FindInvoiceItem(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) FindCreditNote(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) VerifyWebhook([]byte, string) (*stripeclient.WebhookEvent, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) Catalog() stripeclient.Catalog {
	return stripeclient.Catalog{PriceIDTUM: "price_tum", MeterIDTUM: "mtr_tum", MeterEventName: "tum", PortalConfigurationID: "bpc_test"}
}

func (c *checkoutStripeClient) snapshot() (int, []stripeclient.CreateCustomerInput, []stripeclient.CreateCheckoutSessionInput) {
	c.mu.Lock()
	defer c.mu.Unlock()

	customers := make([]stripeclient.CreateCustomerInput, len(c.customerInputs))
	copy(customers, c.customerInputs)
	checkouts := make([]stripeclient.CreateCheckoutSessionInput, len(c.checkoutInputs))
	copy(checkouts, c.checkoutInputs)
	return len(c.customers), customers, checkouts
}

func TestCheckoutStripeClientRejectsIdempotencyKeyReuseWithDifferentInput(t *testing.T) {
	t.Parallel()

	client := newCheckoutStripeClient()
	input := stripeclient.CreateCheckoutSessionInput{
		CustomerID:         "cus_first",
		OrganizationID:     "<ORG_ID>",
		OrganizationSlug:   "billing-test",
		SuccessURL:         "https://app.example.test/billing-test/billing",
		CancelURL:          "https://app.example.test/billing-test/billing",
		TrialEnd:           nil,
		BillingCycleAnchor: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		IdempotencyKey:     "checkout-session:<ORG_ID>",
	}

	_, err := client.CreateCheckoutSession(t.Context(), input)
	require.NoError(t, err)
	input.CustomerID = "cus_second"
	_, err = client.CreateCheckoutSession(t.Context(), input)
	require.ErrorContains(t, err, "reused with different checkout input")
}

func TestNextStripeBillingCycleAnchor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 13, 45, 0, 0, time.FixedZone("test", 2*60*60))
	trialAtMidnight := time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC)
	trialDuringDay := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		now      time.Time
		trialEnd *time.Time
		want     time.Time
	}{
		{
			name:     "immediate checkout uses next UTC midnight",
			now:      now,
			trialEnd: nil,
			want:     time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "immediate checkout at midnight still gets a free day stub",
			now:      time.Date(2026, time.August, 14, 0, 0, 0, 0, time.UTC),
			trialEnd: nil,
			want:     time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "midnight trial end is already aligned",
			now:      now,
			trialEnd: &trialAtMidnight,
			want:     trialAtMidnight,
		},
		{
			name:     "daytime trial end gets a free stub to midnight",
			now:      now,
			trialEnd: &trialDuringDay,
			want:     time.Date(2026, time.August, 22, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, nextStripeBillingCycleAnchor(tt.now, tt.trialEnd))
		})
	}
}

func TestNewStripeCheckoutIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 14, 13, 45, 0, 0, time.UTC)
	productTrialEnd := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)

	t.Run("active product trial uses aligned Stripe trial end", func(t *testing.T) {
		t.Parallel()
		intent := newStripeCheckoutIntent("<ORG_ID>", now, &productTrialEnd)
		wantAnchor := time.Date(2026, time.August, 22, 0, 0, 0, 0, time.UTC)
		require.Equal(t, wantAnchor, intent.billingCycleAnchor)
		require.NotNil(t, intent.trialEnd)
		require.Equal(t, wantAnchor, *intent.trialEnd)
		require.Equal(t, now.Add(23*time.Hour+59*time.Minute), intent.expiresAt)
	})

	t.Run("immediate checkout expires at the next midnight anchor", func(t *testing.T) {
		t.Parallel()
		intent := newStripeCheckoutIntent("<ORG_ID>", now, nil)
		wantAnchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
		require.Equal(t, wantAnchor, intent.billingCycleAnchor)
		require.Nil(t, intent.trialEnd)
		require.Equal(t, wantAnchor.Add(-stripeCheckoutExpirySafetyMargin), intent.expiresAt)
	})

	t.Run("checkout near midnight moves to the following anchor", func(t *testing.T) {
		t.Parallel()
		nearMidnight := time.Date(2026, time.August, 14, 23, 45, 0, 0, time.UTC)
		intent := newStripeCheckoutIntent("<ORG_ID>", nearMidnight, nil)
		require.Equal(t, time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC), intent.billingCycleAnchor)
		require.Equal(t, nearMidnight.Add(23*time.Hour+59*time.Minute), intent.expiresAt)
		require.GreaterOrEqual(t, intent.expiresAt.Sub(nearMidnight), minimumStripeCheckoutSessionLifetime)
		require.Less(t, intent.expiresAt, intent.billingCycleAnchor)
	})
}

type checkoutLifecycleProvisioner struct {
	*openrouter.Development
	db                    repo.DBTX
	organizationID        string
	reconciledAfterCommit []bool
}

func (p *checkoutLifecycleProvisioner) ReconcileAPIKeyDisabled(ctx context.Context, _ string, _ openrouter.KeyType) error {
	trial, err := trialsrepo.New(p.db).GetTrial(ctx, p.organizationID)
	if err != nil {
		return fmt.Errorf("read trial during post-commit reconciliation: %w", err)
	}
	p.reconciledAfterCommit = append(p.reconciledAfterCommit, trial.ConvertedAt.Valid)
	return nil
}

type stripeCheckoutTestInstance struct {
	service *Service
	db      repo.DBTX
	flags   *feature.InMemory
	stripe  *checkoutStripeClient
	orgID   string
	orgSlug string
	userID  string
	email   string
}

func newStripeCheckoutTestInstance(t *testing.T) *stripeCheckoutTestInstance {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	db, err := infra.CloneTestDatabase(t, "stripe_checkout")
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	orgSlug := "billing-" + uuid.NewString()[:8]
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Billing Test Organization",
		Slug:        orgSlug,
		WorkosID:    conv.ToPGText("workos-" + orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPaygSelfServeBilling, orgID, true)
	stripe := newCheckoutStripeClient()
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())

	service := &Service{
		tracer:        testenv.NewTracerProvider(t).Tracer("test"),
		logger:        logger,
		auth:          nil,
		authz:         authzEngine,
		serverURL:     nil,
		siteURL:       siteURL,
		db:            db,
		repo:          repo.New(db),
		billingRepo:   nil,
		orgRepo:       orgrepo.New(db),
		telemetryRepo: nil,
		auditLogger:   audit.NewLogger(),
		posthogClient: nil,
		openRouter:    openrouter.NewDevelopment(""),
		stripeClient:  stripe,
		stripeHandler: nil,
		featureFlags:  flags,
	}

	return &stripeCheckoutTestInstance{
		service: service,
		db:      db,
		flags:   flags,
		stripe:  stripe,
		orgID:   orgID,
		orgSlug: orgSlug,
		userID:  "user-billing-admin",
		email:   "billing-admin@example.test",
	}
}

func (ti *stripeCheckoutTestInstance) context(t *testing.T, grants ...authz.Grant) context.Context {
	t.Helper()
	sessionID := "session-billing-test"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  ti.orgID,
		UserID:                ti.userID,
		SessionID:             &sessionID,
		OrganizationSlug:      ti.orgSlug,
		Email:                 &ti.email,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
	})
	return authztest.WithExactGrants(t, ctx, grants...)
}

func (ti *stripeCheckoutTestInstance) adminContext(t *testing.T) context.Context {
	t.Helper()
	return ti.context(t, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
}

func seedExpiredCheckoutSession(t *testing.T, ti *stripeCheckoutTestInstance, sessionID string) stripeCheckoutIntent {
	t.Helper()
	preparedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	proposal := newStripeCheckoutIntent(ti.orgID, preparedAt, nil)
	prepared, err := ti.service.prepareStripeCheckoutIntent(
		t.Context(),
		ti.orgID,
		"cus_test",
		preparedAt,
		proposal,
		pgtype.Text{String: "", Valid: false},
		pgtype.Text{String: "", Valid: false},
	)
	require.NoError(t, err)
	_, err = repo.New(ti.db).FinalizeStripeCheckoutIntent(t.Context(), repo.FinalizeStripeCheckoutIntentParams{
		StripeCheckoutSessionID:          sessionID,
		OrganizationID:                   ti.orgID,
		StripeCustomerID:                 "cus_test",
		StripeCheckoutIdempotencyKey:     prepared.idempotencyKey,
		StripeCheckoutBillingCycleAnchor: finiteTimestamptz(prepared.billingCycleAnchor),
		StripeCheckoutTrialEnd:           optionalTimestamptz(prepared.trialEnd),
		StripeCheckoutExpiresAt:          finiteTimestamptz(prepared.expiresAt),
	})
	require.NoError(t, err)
	require.LessOrEqual(t, prepared.expiresAt, time.Now().UTC())
	return prepared.stripeCheckoutIntent
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func TestCreateStripeCheckoutRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := ti.context(t, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))

	_, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	uniqueCustomers, _, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, checkouts)
}

func TestCreateStripeCheckoutFailsClosedWhenRolloutDisabled(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ti.flags.SetFlag(feature.FlagPaygSelfServeBilling, ti.orgID, false)

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
	uniqueCustomers, _, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, checkouts)
}

func TestCreateStripeCheckoutAllowsGatedOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := ti.adminContext(t)
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{
		ReqID:       "request-checkout-test",
		ReqURL:      "",
		Host:        "",
		Method:      "",
		Referer:     "",
		RefererHost: "",
		UserAgent:   "",
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.False(t, authCtx.Whitelisted)

	checkoutURL, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.stripe.test/1", checkoutURL)

	uniqueCustomers, customers, checkouts := ti.stripe.snapshot()
	require.Equal(t, 1, uniqueCustomers)
	require.Len(t, customers, 1)
	require.Equal(t, "customer:"+ti.orgID, customers[0].IdempotencyKey)
	require.Len(t, checkouts, 1)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing", checkouts[0].SuccessURL)
	require.Equal(t, checkouts[0].SuccessURL, checkouts[0].CancelURL)
	require.Nil(t, checkouts[0].TrialEnd)
	require.GreaterOrEqual(t, checkouts[0].BillingCycleAnchor.Sub(checkouts[0].ExpiresAt), stripeCheckoutExpirySafetyMargin)
	require.Greater(t, checkouts[0].ExpiresAt, time.Now().UTC())
	require.LessOrEqual(t, checkouts[0].ExpiresAt.Sub(time.Now().UTC()), maximumStripeCheckoutSessionLifetime)
	require.Contains(t, checkouts[0].IdempotencyKey, "checkout-session:"+ti.orgID+":")
}

func TestCreateStripeCheckoutFailsClosedWithoutRolloutProvider(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ti.service.featureFlags = nil

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
	uniqueCustomers, _, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, checkouts)
}

func TestCreateStripeCheckoutAlignsStripeTrialEndAndConvertsProductTrial(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.NotNil(t, checkouts[0].TrialEnd)
	wantAnchor := nextStripeBillingCycleAnchor(time.Now(), &trialEnd)
	require.Equal(t, wantAnchor, *checkouts[0].TrialEnd)
	require.Equal(t, wantAnchor, checkouts[0].BillingCycleAnchor)
	require.Less(t, checkouts[0].ExpiresAt, wantAnchor)

	storedTrial, err := trialsrepo.New(ti.db).GetTrial(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, trialEnd.Equal(storedTrial.EndsAt.Time))
	require.True(t, storedTrial.ConvertedAt.Valid)
}

func TestCreateStripeCheckoutStartsImmediatelyWhenTrialExpired(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Nil(t, checkouts[0].TrialEnd)
}

func TestCreateStripeCheckoutRejectsConcurrentTrialLifecycleChanges(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *stripeCheckoutTestInstance)
	}{
		{name: "extend", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
			require.NoError(t, err)
		}},
		{name: "rearm", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			_, err := ti.db.Exec(t.Context(), `UPDATE trials SET ends_at = clock_timestamp() - interval '1 hour', demoted_at = clock_timestamp(), updated_at = clock_timestamp() WHERE organization_id = $1`, ti.orgID)
			require.NoError(t, err)
			_, err = trialsrepo.New(ti.db).RearmTrial(t.Context(), trialsrepo.RearmTrialParams{OrganizationID: ti.orgID, RearmForDays: 7})
			require.NoError(t, err)
		}},
		{name: "demote", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			_, err := ti.db.Exec(t.Context(), `UPDATE trials SET demoted_at = clock_timestamp(), updated_at = clock_timestamp() WHERE organization_id = $1`, ti.orgID)
			require.NoError(t, err)
		}},
		{name: "convert", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			rows, err := trialsrepo.New(ti.db).MarkTrialConverted(t.Context(), ti.orgID)
			require.NoError(t, err)
			require.EqualValues(t, 1, rows)
		}},
		{name: "delete", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			_, err := ti.db.Exec(t.Context(), `DELETE FROM trials WHERE organization_id = $1`, ti.orgID)
			require.NoError(t, err)
		}},
		{name: "replace row", mutate: func(t *testing.T, ti *stripeCheckoutTestInstance) {
			t.Helper()
			_, err := ti.db.Exec(t.Context(), `DELETE FROM trials WHERE organization_id = $1`, ti.orgID)
			require.NoError(t, err)
			require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
				OrganizationID: ti.orgID, Tier: "enterprise",
				EndsAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(8 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
			}))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ti := newStripeCheckoutTestInstance(t)
			trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
			require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
				OrganizationID: ti.orgID, Tier: "enterprise",
				EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
			}))
			beforeOrganization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
			require.NoError(t, err)
			ti.stripe.afterCheckoutCreate = func() { test.mutate(t, ti) }

			_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
			require.Error(t, err)
			requireOopsCode(t, err, oops.CodeConflict)
			_, _, checkouts := ti.stripe.snapshot()
			require.Len(t, checkouts, 1)

			billingMetadata, readErr := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
			require.NoError(t, readErr)
			require.False(t, billingMetadata.StripeCheckoutSessionID.Valid)
			afterOrganization, readErr := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
			require.NoError(t, readErr)
			require.Equal(t, beforeOrganization.GramAccountType, afterOrganization.GramAccountType)
			require.Equal(t, beforeOrganization.Whitelisted, afterOrganization.Whitelisted)
			for _, action := range []audit.Action{audit.ActionOrganizationEnterpriseTrialConverted, audit.ActionBillingMetadataCreateStripeCheckout} {
				count, countErr := audittest.AuditLogCountByAction(t.Context(), ti.db, action)
				require.NoError(t, countErr)
				require.Zero(t, count)
			}
			_, outboxErr := audittestrepo.New(ti.db).GetLatestOutboxPayloadByOrg(t.Context(), audittestrepo.GetLatestOutboxPayloadByOrgParams{
				OrganizationID: ti.orgID, EventType: string(events.OrganizationEnterpriseTrialV1.EventType()),
			})
			require.ErrorIs(t, outboxErr, pgx.ErrNoRows)
		})
	}
}

func TestCreateStripeCheckoutRotatesLifecycleStaleSessionAfterConflict(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.stripe.test/2", checkoutURL)
	require.Equal(t, []string{"cs_1"}, ti.stripe.expiredCheckoutIDs)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 3)
	require.Equal(t, checkouts[0].IdempotencyKey, checkouts[1].IdempotencyKey)
	require.NotEqual(t, checkouts[0].IdempotencyKey, checkouts[2].IdempotencyKey)
}

func TestCreateStripeCheckoutFailsClosedWhenLifecycleStaleSessionExpirationFails(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	before, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	ti.stripe.expireCheckoutError = errors.New("expiration unavailable")

	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	after, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, before.StripeCheckoutIdempotencyKey, after.StripeCheckoutIdempotencyKey)
	require.Equal(t, before.StripeCheckoutExpiresAt, after.StripeCheckoutExpiresAt)
	require.False(t, after.StripeCheckoutSessionID.Valid)
}

func TestCreateStripeCheckoutFailsClosedWhenLifecycleStaleSessionRecoveryFails(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	before, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	beforeOrganization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	ti.stripe.checkoutError = errors.New("session recovery unavailable")

	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnavailable)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 2)
	require.Equal(t, checkouts[0].IdempotencyKey, checkouts[1].IdempotencyKey)
	require.Empty(t, ti.stripe.expiredCheckoutIDs)
	ti.stripe.mu.Lock()
	remoteSessionCount := len(ti.stripe.checkoutResults)
	ti.stripe.mu.Unlock()
	require.Equal(t, 1, remoteSessionCount)
	after, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, before.StripeCheckoutIdempotencyKey, after.StripeCheckoutIdempotencyKey)
	require.Equal(t, before.StripeCheckoutExpiresAt, after.StripeCheckoutExpiresAt)
	require.False(t, after.StripeCheckoutSessionID.Valid)
	afterOrganization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, beforeOrganization.GramAccountType, afterOrganization.GramAccountType)
	require.Equal(t, beforeOrganization.Whitelisted, afterOrganization.Whitelisted)
	for _, action := range []audit.Action{audit.ActionOrganizationEnterpriseTrialConverted, audit.ActionBillingMetadataCreateStripeCheckout} {
		count, countErr := audittest.AuditLogCountByAction(t.Context(), ti.db, action)
		require.NoError(t, countErr)
		require.Zero(t, count)
	}
}

func TestCreateStripeCheckoutConcurrentLifecycleRotationCreatesOneReplacement(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))
	ti.stripe.afterCheckoutCreate = func() {
		ti.stripe.afterCheckoutCreate = nil
		_, err := trialsrepo.New(ti.db).ExtendTrial(t.Context(), trialsrepo.ExtendTrialParams{OrganizationID: ti.orgID, ExtendByDays: 1})
		require.NoError(t, err)
	}
	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)

	expirationArrivals := make(chan string, 2)
	releaseExpiration := make(chan struct{})
	ti.stripe.beforeCheckoutExpire = func(id string) {
		expirationArrivals <- id
		<-releaseExpiration
	}
	type checkoutResult struct {
		url string
		err error
	}
	results := make(chan checkoutResult, 2)
	ctx := ti.adminContext(t)
	for range 2 {
		go func() {
			url, callErr := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
			results <- checkoutResult{url: url, err: callErr}
		}()
	}
	require.Equal(t, "cs_1", <-expirationArrivals)
	require.Equal(t, "cs_1", <-expirationArrivals)
	close(releaseExpiration)

	firstResults := []checkoutResult{<-results, <-results}
	finalURLs := make([]string, 0, 2)
	for _, result := range firstResults {
		if result.err != nil {
			requireOopsCode(t, result.err, oops.CodeConflict)
			result.url, result.err = ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
		}
		require.NoError(t, result.err)
		finalURLs = append(finalURLs, result.url)
	}
	require.Equal(t, finalURLs[0], finalURLs[1])
	require.Equal(t, "https://checkout.stripe.test/2", finalURLs[0])
	require.NotEmpty(t, ti.stripe.expiredCheckoutIDs)
	for _, id := range ti.stripe.expiredCheckoutIDs {
		require.Equal(t, "cs_1", id)
	}
	ti.stripe.mu.Lock()
	remoteSessions := make(map[string]checkoutStripeResult, len(ti.stripe.checkoutResults))
	maps.Copy(remoteSessions, ti.stripe.checkoutResults)
	ti.stripe.mu.Unlock()
	require.Len(t, remoteSessions, 2)
	replacementCount := 0
	for _, result := range remoteSessions {
		if result.id == "cs_2" {
			replacementCount++
		}
	}
	require.Equal(t, 1, replacementCount)
	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "cs_2", stored.StripeCheckoutSessionID.String)
}

func TestCreateStripeCheckoutUsesCurrentClockForLockedTrialActiveCheck(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	requestStartedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	trialEnd := requestStartedAt.Add(72 * time.Hour)
	clockReads := 0
	ti.service.now = func() time.Time {
		clockReads++
		if clockReads == 1 {
			return requestStartedAt
		}
		return trialEnd.Add(time.Second)
	}
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID, Tier: "enterprise",
		EndsAt: pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	trial, err := trialsrepo.New(ti.db).GetTrial(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.False(t, trial.ConvertedAt.Valid)
	require.GreaterOrEqual(t, clockReads, 2)
}

func TestCreateStripeCheckoutFirstConversionIsAtomicAndReceiptReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	provisioner := &checkoutLifecycleProvisioner{Development: openrouter.NewDevelopment(""), db: ti.db, organizationID: ti.orgID}
	ti.service.openRouter = provisioner
	trialEnd := time.Now().UTC().Add(7 * 24 * time.Hour)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: trialEnd, InfinityModifier: pgtype.Finite, Valid: true},
	}))

	firstURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	converted, err := trialsrepo.New(ti.db).GetTrial(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, converted.ConvertedAt.Valid)
	organization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", organization.GramAccountType)
	require.True(t, organization.Whitelisted)

	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.db, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.Equal(t, "system", record.ActorID)
	require.Equal(t, "System", record.ActorDisplay)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, "stripe_checkout", metadata["conversion_source"])
	require.NotEmpty(t, record.BeforeSnapshot)
	require.NotEmpty(t, record.AfterSnapshot)
	require.Equal(t, []bool{true, true}, provisioner.reconciledAfterCommit)

	envelope, err := audittestrepo.New(ti.db).GetLatestOutboxPayloadByOrg(t.Context(), audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: ti.orgID, EventType: string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.NoError(t, err)
	serialized, err := json.Marshal(map[string]any{
		"actor_id": record.ActorID, "actor_display": record.ActorDisplay, "metadata": json.RawMessage(record.Metadata),
		"before_snapshot": json.RawMessage(record.BeforeSnapshot), "after_snapshot": json.RawMessage(record.AfterSnapshot),
	})
	require.NoError(t, err)
	for _, forbidden := range []string{ti.email, "session-billing-test", "workos-" + ti.orgID, "sk-test", "hash-", "prompt", "spend"} {
		require.NotContains(t, string(serialized), forbidden)
		require.NotContains(t, string(envelope), forbidden)
	}

	replayedURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, firstURL, replayedURL)
	conversionCount, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionOrganizationEnterpriseTrialConverted)
	require.NoError(t, err)
	require.EqualValues(t, 1, conversionCount)
}

func TestCreateStripeCheckoutConversionAuditFailureRollsBackBusinessState(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ti.service.openRouter = openrouter.NewDevelopment("")
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(7 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	beforeOrganization, err := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	_, err = openrouterrepo.New(ti.db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: ti.orgID, KeyType: string(openrouter.KeyTypeChat),
		KeyEncrypted: pgtype.Text{String: "encrypted-placeholder", Valid: true}, KeyHash: "hash-placeholder", MonthlyCredits: 7,
	})
	require.NoError(t, err)
	_, err = ti.db.Exec(t.Context(), `UPDATE openrouter_api_keys SET disabled = TRUE, disable_causes = ARRAY['trial_demotion', 'billing_inactive', 'admin_lock']::text[] WHERE organization_id = $1 AND key_type = 'chat'`, ti.orgID)
	require.NoError(t, err)
	beforeKey, err := openrouterrepo.New(ti.db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: ti.orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)
	for _, featureName := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := featurerepo.New(ti.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: ti.orgID, FeatureName: string(featureName)})
		require.NoError(t, featureErr)
		require.False(t, enabled)
	}
	require.NoError(t, audittest.RejectAction(t.Context(), ti.db, audit.ActionOrganizationEnterpriseTrialConverted))

	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	afterTrial, readErr := trialsrepo.New(ti.db).GetTrial(t.Context(), ti.orgID)
	require.NoError(t, readErr)
	require.False(t, afterTrial.ConvertedAt.Valid)
	afterOrganization, readErr := orgrepo.New(ti.db).GetOrganizationMetadata(t.Context(), ti.orgID)
	require.NoError(t, readErr)
	require.Equal(t, beforeOrganization.GramAccountType, afterOrganization.GramAccountType)
	require.Equal(t, beforeOrganization.Whitelisted, afterOrganization.Whitelisted)
	afterKey, readErr := openrouterrepo.New(ti.db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: ti.orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, readErr)
	require.Equal(t, beforeKey.MonthlyCredits, afterKey.MonthlyCredits)
	require.Equal(t, beforeKey.DisableCauses, afterKey.DisableCauses)
	require.Equal(t, beforeKey.Disabled, afterKey.Disabled)
	require.Equal(t, openrouter.EffectiveDisabled(beforeKey.Disabled, beforeKey.DisableCauses), openrouter.EffectiveDisabled(afterKey.Disabled, afterKey.DisableCauses))
	for _, featureName := range productfeatures.TrialRuntimeFeatures {
		enabled, featureErr := featurerepo.New(ti.db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{OrganizationID: ti.orgID, FeatureName: string(featureName)})
		require.NoError(t, featureErr)
		require.False(t, enabled)
	}
	checkoutAuditCount, readErr := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, readErr)
	require.Zero(t, checkoutAuditCount)
	_, outboxErr := audittestrepo.New(ti.db).GetLatestOutboxPayloadByOrg(t.Context(), audittestrepo.GetLatestOutboxPayloadByOrgParams{
		OrganizationID: ti.orgID, EventType: string(events.OrganizationEnterpriseTrialV1.EventType()),
	})
	require.ErrorIs(t, outboxErr, pgx.ErrNoRows)
	billingMetadata, readErr := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, readErr)
	require.False(t, billingMetadata.StripeCheckoutSessionID.Valid)
}

func TestCreateStripeCheckoutStartsImmediatelyWhenTrialConverted(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	now := time.Now().UTC()
	require.NoError(t, trialsrepo.New(ti.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: ti.orgID,
		CreatedAt:      pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: now.Add(7 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ConvertedAt:    pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		DemotedAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Nil(t, checkouts[0].TrialEnd)
}

func TestCreateStripeCheckoutStartsImmediatelyWhenTrialDemoted(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	now := time.Now().UTC()
	require.NoError(t, trialsrepo.New(ti.db).InsertTrialFixture(t.Context(), trialsrepo.InsertTrialFixtureParams{
		OrganizationID: ti.orgID,
		CreatedAt:      pgtype.Timestamptz{Time: now.Add(-24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		EndsAt:         pgtype.Timestamptz{Time: now.Add(7 * 24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		ConvertedAt:    pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		DemotedAt:      pgtype.Timestamptz{Time: now.Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.Nil(t, checkouts[0].TrialEnd)
}

func TestCreateStripeCheckoutRejectsTrialUnderStripeMinimum(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	require.NoError(t, trialsrepo.New(ti.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: ti.orgID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	uniqueCustomers, _, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, checkouts)
}

func TestCreateStripeCheckoutReusesStoredCustomer(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	require.NoError(t, repo.New(ti.db).CreateStripeBillingMetadataFixture(t.Context(), repo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   ti.orgID,
		StripeCustomerID: pgtype.Text{String: "cus_stored", Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	uniqueCustomers, customers, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, customers)
	require.Len(t, checkouts, 1)
	require.Equal(t, "cus_stored", checkouts[0].CustomerID)
}

func TestCreateStripeCheckoutRejectsExistingSubscription(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	require.NoError(t, repo.New(ti.db).CreateStripeSubscriptionBillingMetadataFixture(t.Context(), repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       ti.orgID,
		StripeCustomerID:     pgtype.Text{String: "cus_subscribed", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "sub_existing", Valid: true},
	}))

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	uniqueCustomers, customers, checkouts := ti.stripe.snapshot()
	require.Zero(t, uniqueCustomers)
	require.Empty(t, customers)
	require.Empty(t, checkouts)
}

func TestCreateStripeCheckoutSequentialRequestsCreateCustomerOnce(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := ti.adminContext(t)

	firstURL, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	secondURL, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, firstURL, secondURL)

	uniqueCustomers, customers, checkouts := ti.stripe.snapshot()
	require.Equal(t, 1, uniqueCustomers)
	require.Len(t, customers, 1)
	require.Len(t, checkouts, 2)
	require.Equal(t, checkouts[0].CustomerID, checkouts[1].CustomerID)
	require.Equal(t, checkouts[0].IdempotencyKey, checkouts[1].IdempotencyKey)
	require.Equal(t, checkouts[0], checkouts[1])
	require.Empty(t, ti.stripe.expiredCheckoutIDs)
	auditCount, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount)
}

func TestPrepareStripeCheckoutIntentReusesAcrossMidnightAndRotatesAfterExpiry(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	beforeMidnight := time.Date(2026, time.August, 14, 23, 50, 0, 0, time.UTC)
	firstProposal := newStripeCheckoutIntent(ti.orgID, beforeMidnight, nil)
	first, err := ti.service.prepareStripeCheckoutIntent(t.Context(), ti.orgID, "cus_test", beforeMidnight, firstProposal, pgtype.Text{String: "", Valid: false}, pgtype.Text{String: "", Valid: false})
	require.NoError(t, err)

	afterMidnight := time.Date(2026, time.August, 15, 0, 10, 0, 0, time.UTC)
	secondProposal := newStripeCheckoutIntent(ti.orgID, afterMidnight, nil)
	require.NotEqual(t, firstProposal.idempotencyKey, secondProposal.idempotencyKey)
	second, err := ti.service.prepareStripeCheckoutIntent(t.Context(), ti.orgID, "cus_test", afterMidnight, secondProposal, pgtype.Text{String: "", Valid: false}, pgtype.Text{String: "", Valid: false})
	require.NoError(t, err)
	require.Equal(t, first.stripeCheckoutIntent, second.stripeCheckoutIntent)

	afterExpiry := first.expiresAt.Add(time.Second)
	replacementProposal := newStripeCheckoutIntent(ti.orgID, afterExpiry, nil)
	replacement, err := ti.service.prepareStripeCheckoutIntent(t.Context(), ti.orgID, "cus_test", afterExpiry, replacementProposal, pgtype.Text{String: "", Valid: false}, pgtype.Text{String: "", Valid: false})
	require.NoError(t, err)
	require.Equal(t, replacementProposal, replacement.stripeCheckoutIntent)
	require.NotEqual(t, first.idempotencyKey, replacement.idempotencyKey)
}

func TestCreateStripeCheckoutDoesNotRotateCompletedExpiredSession(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	oldIntent := seedExpiredCheckoutSession(t, ti, "cs_completed")
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID:             "cs_completed",
		Status:         "complete",
		CustomerID:     "cus_test",
		SubscriptionID: "sub_delayed_webhook",
	}

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	_, _, checkouts := ti.stripe.snapshot()
	require.Empty(t, checkouts)

	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, oldIntent.idempotencyKey, stored.StripeCheckoutIdempotencyKey.String)
	require.Equal(t, "cs_completed", stored.StripeCheckoutSessionID.String)
}

func TestCreateStripeCheckoutRotatesStripeConfirmedExpiredSession(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	oldIntent := seedExpiredCheckoutSession(t, ti, "cs_expired")
	ti.stripe.checkoutState = &stripeclient.CheckoutSessionState{
		ID:         "cs_expired",
		Status:     "expired",
		CustomerID: "cus_test",
	}

	checkoutURL, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, "https://checkout.stripe.test/1", checkoutURL)
	_, _, checkouts := ti.stripe.snapshot()
	require.Len(t, checkouts, 1)
	require.NotEqual(t, oldIntent.idempotencyKey, checkouts[0].IdempotencyKey)

	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, checkouts[0].IdempotencyKey, stored.StripeCheckoutIdempotencyKey.String)
	require.Equal(t, "cs_1", stored.StripeCheckoutSessionID.String)
}

func TestCreateStripeCheckoutAuditsActingUserWithoutStripeMetadata(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	baseline, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)

	_, err = ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
	record, err := audittest.LatestAuditLogByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)
	require.Equal(t, ti.userID, record.ActorID)
	require.Equal(t, string(urn.PrincipalTypeUser), record.ActorType)
	require.Equal(t, &ti.email, record.ActorDisplayName)
	require.Equal(t, "billing_metadata", record.SubjectType)
	require.Empty(t, record.Metadata)
	require.Empty(t, record.BeforeSnapshot)
	require.Empty(t, record.AfterSnapshot)
}

func TestCreateStripeCheckoutConcurrentDoubleClickCreatesOneCustomer(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ctx := ti.adminContext(t)

	type result struct {
		url string
		err error
	}
	results := make(chan result, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Go(func() {
			checkoutURL, err := ti.service.CreateStripeCheckout(ctx, &gen.CreateStripeCheckoutPayload{})
			results <- result{url: checkoutURL, err: err}
		})
	}
	wait.Wait()
	close(results)

	for result := range results {
		require.NoError(t, result.err)
		require.NotEmpty(t, result.url)
	}
	uniqueCustomers, customers, checkouts := ti.stripe.snapshot()
	require.Equal(t, 1, uniqueCustomers)
	require.NotEmpty(t, customers)
	require.LessOrEqual(t, len(customers), 2)
	for _, customer := range customers {
		require.Equal(t, "customer:"+ti.orgID, customer.IdempotencyKey)
	}
	require.Len(t, checkouts, 2)
	require.Equal(t, checkouts[0].IdempotencyKey, checkouts[1].IdempotencyKey)
	require.Equal(t, checkouts[0], checkouts[1])

	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, stored.StripeCustomerID.Valid)
	require.Equal(t, "cus_1", stored.StripeCustomerID.String)
}

func TestCreateStripeCheckoutPersistsIntentWhenCheckoutFails(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ti.stripe.checkoutError = errors.New("Stripe unavailable")

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)
	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, stored.StripeCustomerID.Valid)
	require.True(t, stored.StripeCheckoutIdempotencyKey.Valid)
	require.True(t, stored.StripeCheckoutBillingCycleAnchor.Valid)
	require.True(t, stored.StripeCheckoutExpiresAt.Valid)
	require.False(t, stored.StripeCheckoutSessionID.Valid)

	count, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)
	require.Zero(t, count)
}
