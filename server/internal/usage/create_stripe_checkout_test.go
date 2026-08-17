package usage

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type checkoutStripeClient struct {
	mu sync.Mutex

	customers       map[string]*stripeclient.Customer
	checkoutResults map[string]checkoutStripeResult
	customerInputs  []stripeclient.CreateCustomerInput
	checkoutInputs  []stripeclient.CreateCheckoutSessionInput
	customerError   error
	checkoutError   error
}

type checkoutStripeResult struct {
	input stripeclient.CreateCheckoutSessionInput
	url   string
}

func newCheckoutStripeClient() *checkoutStripeClient {
	return &checkoutStripeClient{
		customers:       make(map[string]*stripeclient.Customer),
		checkoutResults: make(map[string]checkoutStripeResult),
		customerInputs:  make([]stripeclient.CreateCustomerInput, 0),
		checkoutInputs:  make([]stripeclient.CreateCheckoutSessionInput, 0),
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
		return &stripeclient.CheckoutSession{URL: result.url}, nil
	}
	checkoutURL := fmt.Sprintf("https://checkout.stripe.test/%d", len(c.checkoutResults)+1)
	c.checkoutResults[input.IdempotencyKey] = checkoutStripeResult{input: input, url: checkoutURL}
	return &stripeclient.CheckoutSession{URL: checkoutURL}, nil
}

func (c *checkoutStripeClient) CreateMeterEvent(context.Context, stripeclient.CreateMeterEventInput) error {
	return errors.New("not implemented")
}

func (c *checkoutStripeClient) VerifyWebhook([]byte, string) (*stripeclient.WebhookEvent, error) {
	return nil, errors.New("not implemented")
}

func (c *checkoutStripeClient) Catalog() stripeclient.Catalog {
	return stripeclient.Catalog{PriceIDTUM: "price_tum", MeterEventName: "tum"}
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
		CustomerID:       "cus_first",
		OrganizationID:   "<ORG_ID>",
		OrganizationSlug: "billing-test",
		SuccessURL:       "https://app.example.test/billing-test/billing",
		CancelURL:        "https://app.example.test/billing-test/billing",
		TrialEnd:         nil,
		IdempotencyKey:   "checkout-session:<ORG_ID>",
	}

	_, err := client.CreateCheckoutSession(t.Context(), input)
	require.NoError(t, err)
	input.CustomerID = "cus_second"
	_, err = client.CreateCheckoutSession(t.Context(), input)
	require.ErrorContains(t, err, "reused with different checkout input")
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
		openRouter:    nil,
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
	require.Equal(t, "checkout-session:"+ti.orgID, checkouts[0].IdempotencyKey)
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

func TestCreateStripeCheckoutPreservesActiveTrialEnd(t *testing.T) {
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
	require.Equal(t, trialEnd, *checkouts[0].TrialEnd)
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
	require.Equal(t, "checkout-session:"+ti.orgID, checkouts[0].IdempotencyKey)
	require.Equal(t, checkouts[0].IdempotencyKey, checkouts[1].IdempotencyKey)

	stored, err := repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.True(t, stored.StripeCustomerID.Valid)
	require.Equal(t, "cus_1", stored.StripeCustomerID.String)
}

func TestCreateStripeCheckoutDoesNotPersistCustomerWhenCheckoutFails(t *testing.T) {
	t.Parallel()

	ti := newStripeCheckoutTestInstance(t)
	ti.stripe.checkoutError = errors.New("Stripe unavailable")

	_, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)
	_, err = repo.New(ti.db).GetBillingMetadata(t.Context(), ti.orgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	count, err := audittest.AuditLogCountByAction(t.Context(), ti.db, audit.ActionBillingMetadataCreateStripeCheckout)
	require.NoError(t, err)
	require.Zero(t, count)
}
