package usage

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type fakeStripeWebhookClient struct {
	verify        func([]byte, string) (*stripeclient.WebhookEvent, error)
	checkout      *stripeclient.CheckoutSessionState
	checkoutError error
	verifyCalls   atomic.Int32
}

const stripeWebhookOrganizationID = "org_placeholder"

func (f *fakeStripeWebhookClient) CreateCustomer(context.Context, stripeclient.CreateCustomerInput) (*stripeclient.Customer, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) CreateCheckoutSession(context.Context, stripeclient.CreateCheckoutSessionInput) (*stripeclient.CheckoutSession, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) GetCheckoutSession(context.Context, string) (*stripeclient.CheckoutSessionState, error) {
	return f.checkout, f.checkoutError
}

type captureFeatureCache struct {
	mu      sync.Mutex
	enabled []productfeatures.Feature
}

func (c *captureFeatureCache) UpdateFeatureCache(_ context.Context, _ string, feature productfeatures.Feature, enabled bool) {
	if !enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = append(c.enabled, feature)
}

func (c *captureFeatureCache) snapshot() []productfeatures.Feature {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]productfeatures.Feature(nil), c.enabled...)
}

func (f *fakeStripeWebhookClient) CreateMeterEvent(context.Context, stripeclient.CreateMeterEventInput) error {
	return errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) VerifyWebhook(payload []byte, signature string) (*stripeclient.WebhookEvent, error) {
	f.verifyCalls.Add(1)
	return f.verify(payload, signature)
}

func (f *fakeStripeWebhookClient) Catalog() stripeclient.Catalog {
	return stripeclient.Catalog{PriceIDTUM: "", MeterEventName: ""}
}

func testStripeWebhookHandler(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
	return stripeWebhookResult{newlyEnabledFeatures: nil}, nil
}

func newStripeWebhookService(t *testing.T, customerID string, handler stripeWebhookHandler) (*Service, *pgxpool.Pool) {
	t.Helper()

	db, err := infra.CloneTestDatabase(t, "stripe_webhook")
	require.NoError(t, err)

	err = orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{
		ID:   stripeWebhookOrganizationID,
		Name: "Placeholder Organization",
		Slug: "placeholder-organization",
	})
	require.NoError(t, err)
	require.NoError(t, authz.SeedSystemRoleGrants(t.Context(), db, stripeWebhookOrganizationID))
	err = repo.New(db).CreateStripeBillingMetadataFixture(t.Context(), repo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   stripeWebhookOrganizationID,
		StripeCustomerID: pgtype.Text{String: customerID, Valid: true},
	})
	require.NoError(t, err)

	client := &fakeStripeWebhookClient{
		verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return &stripeclient.WebhookEvent{
				ID:         "event_placeholder",
				Type:       "invoice.created",
				Created:    time.Time{},
				ObjectID:   "invoice_placeholder",
				CustomerID: customerID,
			}, nil
		},
	}
	if handler == nil {
		handler = testStripeWebhookHandler
	}

	return &Service{
		logger:        testenv.NewLogger(t),
		db:            db,
		stripeClient:  client,
		stripeHandler: handler,
	}, db
}

func serveStripeWebhook(service *Service, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", strings.NewReader(body))
	request.Header.Set("Stripe-Signature", "signature_placeholder")
	oops.ErrHandle(service.logger, service.handleStripeWebhook).ServeHTTP(recorder, request)
	return recorder
}

func stripeWebhookReceiptCount(t *testing.T, db *pgxpool.Pool) int {
	t.Helper()
	count, err := repo.New(db).CountStripeWebhookReceiptsFixture(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	return int(count)
}

func paygSchedulingIntentCount(t *testing.T, db *pgxpool.Pool) int {
	t.Helper()

	rows, err := testrepo.New(db).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)

	count := 0
	for _, row := range rows {
		var event webhooksv1.Event
		require.NoError(t, proto.Unmarshal(row.Message, &event))
		if event.GetEventType() != string(events.OrganizationBillingV1.EventType()) {
			continue
		}

		var payload events.AuditLogCreatedPayloadV1
		require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
		if payload.Action == string(audit.ActionOrganizationPaygActivated) {
			count++
		}
	}

	return count
}

func TestAttachStripeWebhookRoute(t *testing.T) {
	t.Parallel()
	client := &fakeStripeWebhookClient{
		verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return &stripeclient.WebhookEvent{
				ID:         "event_unsupported_placeholder",
				Type:       "customer.updated",
				Created:    time.Time{},
				ObjectID:   "customer_placeholder",
				CustomerID: "customer_placeholder",
			}, nil
		},
	}
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
	mux := goahttp.NewMuxer()
	Attach(mux, service)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", strings.NewReader("exact body"))
	request.Header.Set("Stripe-Signature", "signature_placeholder")
	mux.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.EqualValues(t, 1, client.verifyCalls.Load())
}

func TestAttachOmitsStripeWebhookRouteWithoutClient(t *testing.T) {
	t.Parallel()
	service := &Service{logger: testenv.NewLogger(t), stripeClient: nil, stripeHandler: testStripeWebhookHandler}
	mux := goahttp.NewMuxer()
	Attach(mux, service)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", nil))

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestStripeWebhookPreservesRawBody(t *testing.T) {
	t.Parallel()
	expectedBody := "{\n  \"data\": \"spacing matters\"\n}\n"
	client := &fakeStripeWebhookClient{
		verify: func(payload []byte, signature string) (*stripeclient.WebhookEvent, error) {
			require.Equal(t, expectedBody, string(payload))
			require.Equal(t, "signature_placeholder", signature)
			return &stripeclient.WebhookEvent{
				ID:         "event_unsupported_placeholder",
				Type:       "customer.updated",
				Created:    time.Time{},
				ObjectID:   "customer_placeholder",
				CustomerID: "customer_placeholder",
			}, nil
		},
	}
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}

	recorder := serveStripeWebhook(service, expectedBody)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestStripeWebhookRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	t.Run("unconfigured", func(t *testing.T) {
		t.Parallel()
		service := &Service{
			logger:        testenv.NewLogger(t),
			stripeClient:  stripeclient.NewStubClient(testenv.NewLogger(t)),
			stripeHandler: testStripeWebhookHandler,
		}
		require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return nil, errors.New("signature mismatch")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
		require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("unsigned", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, signature string) (*stripeclient.WebhookEvent, error) {
			require.Empty(t, signature)
			return nil, errors.New("missing signature")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
		mux := goahttp.NewMuxer()
		Attach(mux, service)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", strings.NewReader("{}")))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("malformed event", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return &stripeclient.WebhookEvent{ID: "", Type: "", Created: time.Time{}, ObjectID: "", CustomerID: ""}, nil
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
		require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("oversized body", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return nil, errors.New("must not be called")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
		require.Equal(t, http.StatusRequestEntityTooLarge, serveStripeWebhook(service, strings.Repeat("x", maxStripeWebhookBodyBytes+1)).Code)
		require.Zero(t, client.verifyCalls.Load())
	})
}

func TestStripeWebhookDatabaseUnavailable(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", testStripeWebhookHandler)
	db.Close()

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "{}").Code)

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:   "event_missing_customer",
			Type: "invoice.created",
		}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "missing-customer").Code)
}

func TestStripeWebhookAcknowledgesEventsWithoutDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		typeName   string
		customerID string
	}{
		{name: "unsupported", typeName: "customer.updated", customerID: "customer_placeholder"},
		{name: "missing customer", typeName: "invoice.created", customerID: ""},
		{name: "unknown customer", typeName: "invoice.created", customerID: "unknown_customer_placeholder"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
				t.Fatal("handler must not run")
				return stripeWebhookResult{}, nil
			})
			client, ok := service.stripeClient.(*fakeStripeWebhookClient)
			require.True(t, ok)
			client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
				return &stripeclient.WebhookEvent{
					ID:         "event_" + strings.ReplaceAll(test.name, " ", "_"),
					Type:       test.typeName,
					Created:    time.Time{},
					ObjectID:   "object_placeholder",
					CustomerID: test.customerID,
				}, nil
			}
			require.Equal(t, http.StatusOK, serveStripeWebhook(service, "{}").Code)
			require.Zero(t, stripeWebhookReceiptCount(t, db))
		})
	}
}

func TestStripeWebhookSequentialDuplicateDispatchesOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
		calls.Add(1)
		return stripeWebhookResult{}, nil
	})

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "second").Code)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeWebhookConcurrentDuplicateDispatchesOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
		calls.Add(1)
		enterOnce.Do(func() { close(entered) })
		<-release
		return stripeWebhookResult{}, nil
	})

	responses := make(chan int, 2)
	go func() { responses <- serveStripeWebhook(service, "first").Code }()
	<-entered
	go func() { responses <- serveStripeWebhook(service, "second").Code }()
	close(release)

	require.Equal(t, http.StatusOK, <-responses)
	require.Equal(t, http.StatusOK, <-responses)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeWebhookConcurrentDistinctEventsForSameCustomerDoNotSerialize(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
		calls.Add(1)
		<-release
		return stripeWebhookResult{}, nil
	})
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(payload []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         "event_" + string(payload),
			Type:       "invoice.created",
			ObjectID:   "invoice_" + string(payload),
			CustomerID: "customer_placeholder",
		}, nil
	}

	responses := make(chan int, 2)
	go func() { responses <- serveStripeWebhook(service, "first").Code }()
	go func() { responses <- serveStripeWebhook(service, "second").Code }()

	require.Eventually(t, func() bool { return calls.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
	close(release)
	released = true

	require.Equal(t, http.StatusOK, <-responses)
	require.Equal(t, http.StatusOK, <-responses)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 2, stripeWebhookReceiptCount(t, db))
}

func TestStripeWebhookHandlerFailureRollsBackForRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState) (stripeWebhookResult, error) {
		if calls.Add(1) == 1 {
			return stripeWebhookResult{}, errors.New("transient handler failure")
		}
		return stripeWebhookResult{}, nil
	})

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "first").Code)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "retry").Code)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func configurePaygCheckout(t *testing.T, service *Service, eventID, subscriptionID, subscriptionStatus string) *captureFeatureCache {
	t.Helper()

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             eventID,
			Type:           "checkout.session.completed",
			Created:        time.Now().UTC(),
			ObjectID:       "checkout_placeholder",
			CustomerID:     "customer_placeholder",
			SubscriptionID: subscriptionID,
		}, nil
	}
	client.checkout = &stripeclient.CheckoutSessionState{
		ID:                     "checkout_placeholder",
		Status:                 "complete",
		CustomerID:             "customer_placeholder",
		SubscriptionID:         subscriptionID,
		SubscriptionCustomerID: "customer_placeholder",
		SubscriptionStatus:     subscriptionStatus,
		BillingCycleAnchor:     time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC),
	}
	featureCache := &captureFeatureCache{enabled: nil}
	service.productFeatures = featureCache
	service.auditLogger = audit.NewLogger()
	service.stripeHandler = service.serviceStripeWebhookHandler
	return featureCache
}

func TestStripeCheckoutCompletionActivatesColdPaygOrganization(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	featureCache := configurePaygCheckout(t, service, "event_activation", "subscription_activation", "active")
	baseline, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "activation").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "customer_placeholder", metadata.StripeCustomerID.String)
	require.Equal(t, "subscription_activation", metadata.StripeSubscriptionID.String)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)
	require.True(t, service.stripeClient.(*fakeStripeWebhookClient).checkout.BillingCycleAnchor.Equal(metadata.StripeBillingCycleAnchor.Time))
	require.EqualValues(t, 23, metadata.BillingCycleAnchorDay)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)
	require.Equal(t, 1, paygSchedulingIntentCount(t, db))

	expectedFeatures := append([]productfeatures.Feature{productfeatures.FeaturePlatformMCP}, productfeatures.EnterpriseTrialBundle...)
	expectedFeatures = append(expectedFeatures, productfeatures.FeatureSkills)
	featureQueries := featurerepo.New(db)
	for _, feature := range expectedFeatures {
		enabled, err := featureQueries.IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{
			OrganizationID: stripeWebhookOrganizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		require.Truef(t, enabled, "feature %s should be enabled", feature)
	}
	require.ElementsMatch(t, expectedFeatures, featureCache.snapshot())

	after, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.Equal(t, baseline+1, after)
	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.Equal(t, "system", record.ActorID)
	require.NotContains(t, string(record.Metadata), "customer_placeholder")
	require.NotContains(t, string(record.BeforeSnapshot), "subscription_activation")
	require.NotContains(t, string(record.AfterSnapshot), "subscription_activation")
}

func TestStripeCheckoutCompletionPreservesTrialFeatureChoices(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	featureCache := configurePaygCheckout(t, service, "event_trial", "subscription_trial", "trialing")
	ctx := t.Context()
	tx := testenv.BeginTx(t, ctx, db)
	require.NoError(t, productfeatures.SeedEnterpriseTrialBundleTx(ctx, tx, stripeWebhookOrganizationID))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, orgrepo.New(db).SetAccountType(ctx, orgrepo.SetAccountTypeParams{
		ID:              stripeWebhookOrganizationID,
		GramAccountType: "enterprise",
	}))
	require.NoError(t, trialsrepo.New(db).CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: stripeWebhookOrganizationID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(7 * 24 * time.Hour), Valid: true},
	}))
	_, err := featurerepo.New(db).DeleteFeature(ctx, featurerepo.DeleteFeatureParams{
		OrganizationID: stripeWebhookOrganizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "trial").Code)

	trial, err := trialsrepo.New(db).GetTrial(ctx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, trial.ConvertedAt.Valid)
	ssoEnabled, err := featurerepo.New(db).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: stripeWebhookOrganizationID,
		FeatureName:    string(productfeatures.FeatureSSO),
	})
	require.NoError(t, err)
	require.False(t, ssoEnabled)
	require.NotContains(t, featureCache.snapshot(), productfeatures.FeatureSSO)
}

func TestStripeCheckoutDomainReplayIsNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_first", "subscription_replay", "past_due")
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             "event_second",
			Type:           "checkout.session.completed",
			Created:        time.Now().UTC(),
			ObjectID:       "checkout_placeholder",
			CustomerID:     "customer_placeholder",
			SubscriptionID: "subscription_replay",
		}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "second").Code)

	require.Equal(t, 1, paygSchedulingIntentCount(t, db))
	require.Equal(t, 2, stripeWebhookReceiptCount(t, db))
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestStripeCheckoutExactReplaySkipsCurrentStateRetrieval(t *testing.T) {
	t.Parallel()

	service, _ := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_exact_replay", "subscription_exact_replay", "active")
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.checkoutError = errors.New("Stripe retrieval unavailable")
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "replay").Code)
	require.Equal(t, 1, paygSchedulingIntentCount(t, service.db))
}

func TestStripeCheckoutAuditFailureRollsBackActivationAndSchedulingIntent(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	featureCache := configurePaygCheckout(t, service, "event_audit_failure", "subscription_audit_failure", "active")
	service.auditLogger = nil

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "failure").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.NotEqual(t, "payg", organization.GramAccountType)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Zero(t, paygSchedulingIntentCount(t, db))
	require.Empty(t, featureCache.snapshot())
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestStripeCheckoutSubscriptionConflictRollsBackTrialConversion(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_conflict", "subscription_new", "active")
	ctx := t.Context()
	require.NoError(t, repo.New(db).SetStripeSubscriptionFixture(ctx, repo.SetStripeSubscriptionFixtureParams{
		StripeSubscriptionID: pgtype.Text{String: "subscription_existing", Valid: true},
		OrganizationID:       stripeWebhookOrganizationID,
	}))
	require.NoError(t, trialsrepo.New(db).CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: stripeWebhookOrganizationID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(7 * 24 * time.Hour), Valid: true},
	}))

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "conflict").Code)

	trial, err := trialsrepo.New(db).GetTrial(ctx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, trial.ConvertedAt.Valid)
	metadata, err := repo.New(db).GetBillingMetadata(ctx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_existing", metadata.StripeSubscriptionID.String)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
}

func TestStripeCheckoutRejectsDuplicateSubscriptionOwners(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_duplicate_owners", "subscription_duplicate", "active")
	ctx := t.Context()
	require.NoError(t, repo.New(db).SetStripeSubscriptionFixture(ctx, repo.SetStripeSubscriptionFixtureParams{
		StripeSubscriptionID: pgtype.Text{String: "subscription_duplicate", Valid: true},
		OrganizationID:       stripeWebhookOrganizationID,
	}))

	const otherOrganizationID = "org_placeholder_other"
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   otherOrganizationID,
		Name: "Other Placeholder Organization",
		Slug: "other-placeholder-organization",
	}))
	require.NoError(t, repo.New(db).CreateStripeSubscriptionBillingMetadataFixture(ctx, repo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       otherOrganizationID,
		StripeCustomerID:     pgtype.Text{String: "customer_placeholder_other", Valid: true},
		StripeSubscriptionID: pgtype.Text{String: "subscription_duplicate", Valid: true},
	}))

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "duplicate").Code)

	organization, err := orgrepo.New(db).GetOrganizationMetadata(ctx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.NotEqual(t, "payg", organization.GramAccountType)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Zero(t, paygSchedulingIntentCount(t, db))
}

func TestStripeCheckoutTerminalStateIsDurableNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_terminal", "subscription_terminal", "canceled")

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "terminal").Code)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Zero(t, paygSchedulingIntentCount(t, db))
	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
}

func TestStripeCheckoutRejectsMalformedIdentifiers(t *testing.T) {
	t.Parallel()

	client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             "event_malformed",
			Type:           "checkout.session.completed",
			Created:        time.Now().UTC(),
			ObjectID:       "checkout_placeholder",
			CustomerID:     "customer_placeholder",
			SubscriptionID: "",
		}, nil
	}}
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
	require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "malformed").Code)
}
