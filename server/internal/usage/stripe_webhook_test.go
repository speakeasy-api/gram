package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type fakeStripeWebhookClient struct {
	verify        func([]byte, string) (*stripeclient.WebhookEvent, error)
	checkout      *stripeclient.CheckoutSessionState
	checkoutError error
	invoice       *stripeclient.InvoiceState
	invoiceError  error
	checkoutCalls atomic.Int32
	invoiceCalls  atomic.Int32
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
	f.checkoutCalls.Add(1)
	return f.checkout, f.checkoutError
}

func (f *fakeStripeWebhookClient) GetSubscription(context.Context, string) (*stripeclient.SubscriptionState, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) SetSubscriptionCancelAtPeriodEnd(context.Context, stripeclient.SetSubscriptionCancelAtPeriodEndInput) (*stripeclient.SubscriptionState, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) CreatePortalSession(context.Context, stripeclient.CreatePortalSessionInput) (*stripeclient.PortalSession, error) {
	return nil, errors.New("not implemented")
}

type captureFeatureCache struct {
	mu      sync.Mutex
	enabled []productfeatures.Feature
}

type captureStripeWebhookMetrics struct {
	invoicePaymentFailures atomic.Int32
	subscriptionLosses     atomic.Int32
}

func (c *captureStripeWebhookMetrics) RecordInvoicePaymentFailed(context.Context) {
	c.invoicePaymentFailures.Add(1)
}

func (c *captureStripeWebhookMetrics) RecordSubscriptionLost(context.Context) {
	c.subscriptionLosses.Add(1)
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

func (f *fakeStripeWebhookClient) GetMeterEventSummary(context.Context, stripeclient.GetMeterEventSummaryInput) (float64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) GetInvoice(context.Context, string) (*stripeclient.InvoiceState, error) {
	f.invoiceCalls.Add(1)
	return f.invoice, f.invoiceError
}

func (f *fakeStripeWebhookClient) CreateInvoiceItem(context.Context, stripeclient.CreateInvoiceItemInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) CreateCreditNote(context.Context, stripeclient.CreateCreditNoteInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) FindInvoiceItem(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) FindCreditNote(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStripeWebhookClient) VerifyWebhook(payload []byte, signature string) (*stripeclient.WebhookEvent, error) {
	f.verifyCalls.Add(1)
	return f.verify(payload, signature)
}

func (f *fakeStripeWebhookClient) Catalog() stripeclient.Catalog {
	return stripeclient.Catalog{PriceIDTUM: "", MeterIDTUM: "", MeterEventName: "", PortalConfigurationID: ""}
}

func testStripeWebhookHandler(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
	return stripeWebhookResult{newlyEnabledFeatures: nil, invoicePaymentFailed: false, subscriptionLost: false}, nil
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
		invoice: &stripeclient.InvoiceState{
			ID:                 "invoice_placeholder",
			CustomerID:         customerID,
			SubscriptionID:     "subscription_placeholder",
			Currency:           "usd",
			BillingReason:      "subscription_cycle",
			Status:             "draft",
			ServicePeriodStart: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
			ServicePeriodEnd:   time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		},
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
		openRouter:    openrouter.NewDevelopment("test-key"),
	}, db
}

func serveStripeWebhook(service *Service, body string) *httptest.ResponseRecorder {
	return serveStripeWebhookWithContext(context.Background(), service, body)
}

func serveStripeWebhookWithContext(ctx context.Context, service *Service, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", strings.NewReader(body)).WithContext(ctx)
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

func configurePaygInvoiceIdentity(t *testing.T, service *Service, subscriptionID string, billingCycleAnchor time.Time) {
	t.Helper()

	queries := repo.New(service.db)
	_, err := queries.ActivatePaygBillingMetadata(t.Context(), repo.ActivatePaygBillingMetadataParams{
		StripeSubscriptionID:     pgtype.Text{String: subscriptionID, Valid: true},
		StripeBillingCycleAnchor: pgtype.Timestamptz{Time: billingCycleAnchor.UTC(), Valid: true},
		BillingCycleAnchorDay:    int32(billingCycleAnchor.UTC().Day()),
		OrganizationID:           stripeWebhookOrganizationID,
		StripeCustomerID:         pgtype.Text{String: "customer_placeholder", Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, queries.ActivatePaygOrganization(t.Context(), stripeWebhookOrganizationID))
	service.stripeHandler = service.serviceStripeWebhookHandler
}

func configureInvoiceWebhook(t *testing.T, service *Service, eventID string, invoice *stripeclient.InvoiceState) *fakeStripeWebhookClient {
	t.Helper()

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.invoice = invoice
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         eventID,
			Type:       "invoice.created",
			Created:    time.Now().UTC(),
			ObjectID:   invoice.ID,
			CustomerID: "customer_placeholder",
		}, nil
	}
	return client
}

func stripeInvoices(t *testing.T, db *pgxpool.Pool) []repo.StripeInvoice {
	t.Helper()

	invoices, err := repo.New(db).ListStripeInvoicesFixture(t.Context(), pgtype.Text{String: stripeWebhookOrganizationID, Valid: true})
	require.NoError(t, err)
	return invoices
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

func organizationBillingActionIntentCount(t *testing.T, db *pgxpool.Pool, action audit.Action) int {
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
		if payload.Action == string(action) {
			count++
		}
	}

	return count
}

func configurePaygSubscriptionDeletion(t *testing.T, service *Service, eventID, currentSubscriptionID, deletedSubscriptionID string) *captureStripeWebhookMetrics {
	t.Helper()

	anchor := time.Date(2026, time.August, 23, 0, 0, 0, 0, time.UTC)
	configurePaygInvoiceIdentity(t, service, currentSubscriptionID, anchor)
	service.auditLogger = audit.NewLogger()
	metrics := &captureStripeWebhookMetrics{}
	service.stripeMetrics = metrics

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             eventID,
			Type:           "customer.subscription.deleted",
			Created:        time.Now().UTC(),
			ObjectID:       deletedSubscriptionID,
			CustomerID:     "customer_placeholder",
			SubscriptionID: "subscription_payload_must_not_be_used",
		}, nil
	}

	return metrics
}

func createOpenRouterKeyFixture(t *testing.T, db *pgxpool.Pool, keyType openrouter.KeyType, credits int64) {
	t.Helper()

	_, err := openrouterrepo.New(db).CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(keyType),
		KeyEncrypted:   pgtype.Text{String: "", Valid: false},
		KeyHash:        "hash_placeholder_" + string(keyType),
		MonthlyCredits: credits,
	})
	require.NoError(t, err)
}

func setOpenRouterKeyLifecycleFixture(t *testing.T, db *pgxpool.Pool, keyType openrouter.KeyType, disabled bool, causes []string, credits int64) {
	t.Helper()

	rows, err := testrepo.New(db).SetOpenRouterKeyLifecycleFixture(t.Context(), testrepo.SetOpenRouterKeyLifecycleFixtureParams{
		Disabled:       disabled,
		DisableCauses:  causes,
		MonthlyCredits: credits,
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(keyType),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

func stripeLifecycleOpenRouterProvisioner(t *testing.T, db *pgxpool.Pool, baseURL string) openrouter.Provisioner {
	t.Helper()

	option, err := openrouter.WithTestBaseURL(baseURL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)
}

func openRouterKeyLifecycleFixture(t *testing.T, db *pgxpool.Pool, keyType openrouter.KeyType) openrouterrepo.OpenrouterApiKey {
	t.Helper()

	key, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(keyType),
	})
	require.NoError(t, err)
	return key
}

func createDemotedEnterpriseTrialFixture(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	require.NoError(t, trialsrepo.New(db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{
		OrganizationID: stripeWebhookOrganizationID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true},
	}))
	_, err := trialsrepo.New(db).MarkTrialDemoted(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
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

			service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
				t.Fatal("handler must not run")
				return stripeWebhookResult{}, nil
			})
			client, ok := service.stripeClient.(*fakeStripeWebhookClient)
			require.True(t, ok)
			client.invoice = &stripeclient.InvoiceState{
				ID:                 "object_placeholder",
				CustomerID:         test.customerID,
				SubscriptionID:     "subscription_placeholder",
				Currency:           "usd",
				BillingReason:      "subscription_cycle",
				Status:             "draft",
				ServicePeriodStart: time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC),
				ServicePeriodEnd:   time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
			}
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
			require.Zero(t, client.invoiceCalls.Load())
		})
	}
}

func TestStripeWebhookSequentialDuplicateDispatchesOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
		calls.Add(1)
		<-release
		return stripeWebhookResult{}, nil
	})
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(payload []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         "event_" + string(payload),
			Type:       "invoice.payment_failed",
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, pgx.Tx, string, *stripeclient.WebhookEvent, *stripeclient.CheckoutSessionState, *stripeclient.InvoiceState) (stripeWebhookResult, error) {
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

func TestStripeInvoiceCreatedPersistsImmutableBillingIdentity(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		state       string
		finalizedAt time.Time
	}{
		{name: "draft", state: "draft"},
		{name: "open", state: "open", finalizedAt: periodStart.Add(2 * time.Hour)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, db := newStripeWebhookService(t, "customer_placeholder", nil)
			configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
			invoice := &stripeclient.InvoiceState{
				ID:                 "invoice_" + test.name,
				CustomerID:         "customer_placeholder",
				SubscriptionID:     "subscription_placeholder",
				Currency:           "usd",
				BillingReason:      "subscription_cycle",
				Status:             test.state,
				ServicePeriodStart: periodStart,
				ServicePeriodEnd:   periodEnd,
				FinalizedAt:        test.finalizedAt,
			}
			configureInvoiceWebhook(t, service, "event_"+test.name, invoice)

			recorder := serveStripeWebhook(service, test.name)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
			rows := stripeInvoices(t, db)
			require.Len(t, rows, 1)
			row := rows[0]
			require.Equal(t, invoice.ID, row.StripeInvoiceID)
			require.Equal(t, stripeWebhookOrganizationID, row.OrganizationID.String)
			require.True(t, row.OrganizationID.Valid)
			require.Equal(t, invoice.CustomerID, row.StripeCustomerID)
			require.Equal(t, invoice.SubscriptionID, row.StripeSubscriptionID)
			require.True(t, periodStart.Equal(row.ServicePeriodStart.Time))
			require.True(t, periodEnd.Equal(row.ServicePeriodEnd.Time))
			require.Equal(t, test.state, row.InvoiceState)
			if test.finalizedAt.IsZero() {
				require.False(t, row.FinalizedAt.Valid)
			} else {
				require.True(t, row.FinalizedAt.Valid)
				require.True(t, test.finalizedAt.Equal(row.FinalizedAt.Time))
			}
		})
	}
}

func TestStripeInvoiceCreatedStoresCurrentStateForDelayedEvent(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	finalizedAt := periodStart.Add(3 * time.Hour)
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
	client := configureInvoiceWebhook(t, service, "event_delayed_invoice", &stripeclient.InvoiceState{
		ID:                 "invoice_delayed",
		CustomerID:         "customer_placeholder",
		SubscriptionID:     "subscription_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "open",
		ServicePeriodStart: periodStart,
		ServicePeriodEnd:   periodEnd,
		FinalizedAt:        finalizedAt,
	})
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         "event_delayed_invoice",
			Type:       "invoice.created",
			Created:    periodStart.Add(-24 * time.Hour),
			ObjectID:   "invoice_delayed",
			CustomerID: "customer_placeholder",
		}, nil
	}

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "stale-event-payload").Code)
	rows := stripeInvoices(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, "open", rows[0].InvoiceState)
	require.True(t, finalizedAt.Equal(rows[0].FinalizedAt.Time))
}

func TestStripeInvoiceRetrievalFailureRetriesWithoutReceipt(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
	client := configureInvoiceWebhook(t, service, "event_invoice_retrieval_retry", &stripeclient.InvoiceState{
		ID:                 "invoice_retrieval_retry",
		CustomerID:         "customer_placeholder",
		SubscriptionID:     "subscription_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "draft",
		ServicePeriodStart: periodStart,
		ServicePeriodEnd:   periodStart.AddDate(0, 1, 0),
	})
	client.invoiceError = errors.New("Stripe retrieval unavailable")

	require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(service, "first").Code)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Empty(t, stripeInvoices(t, db))
	require.EqualValues(t, 1, client.invoiceCalls.Load())

	client.invoiceError = nil
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "retry").Code)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Len(t, stripeInvoices(t, db), 1)
	require.EqualValues(t, 2, client.invoiceCalls.Load())
}

func TestStripeInvoiceCreatedDurablyIgnoresNonBillableInvoices(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		billingReason string
		currency      string
		subscription  string
		periodStart   time.Time
		periodEnd     time.Time
	}{
		{
			name:          "free pre-anchor stub",
			billingReason: "subscription_create",
			currency:      "usd",
			subscription:  "subscription_placeholder",
			periodStart:   anchor.AddDate(0, -1, 0).Add(9*time.Hour + 17*time.Minute),
			periodEnd:     anchor,
		},
		{
			name:          "zero-length first subscription invoice",
			billingReason: "subscription_create",
			currency:      "usd",
			subscription:  "subscription_placeholder",
			periodStart:   anchor,
			periodEnd:     anchor,
		},
		{
			name:          "unsupported billing reason",
			billingReason: "manual",
			currency:      "eur",
			subscription:  "",
			periodStart:   time.Time{},
			periodEnd:     time.Time{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, db := newStripeWebhookService(t, "customer_placeholder", nil)
			configurePaygInvoiceIdentity(t, service, "subscription_placeholder", anchor)
			configureInvoiceWebhook(t, service, "event_"+strings.ReplaceAll(test.name, " ", "_"), &stripeclient.InvoiceState{
				ID:                 "invoice_" + strings.ReplaceAll(test.name, " ", "_"),
				CustomerID:         "customer_placeholder",
				SubscriptionID:     test.subscription,
				Currency:           test.currency,
				BillingReason:      test.billingReason,
				Status:             "draft",
				ServicePeriodStart: test.periodStart,
				ServicePeriodEnd:   test.periodEnd,
			})

			require.Equal(t, http.StatusOK, serveStripeWebhook(service, test.name).Code)
			require.Empty(t, stripeInvoices(t, db))
			require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
		})
	}
}

func TestStripeInvoiceCreatedRetriesUntilCheckoutActivationCommits(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	require.NoError(t, repo.New(db).SetStripeCheckoutSessionFixture(t.Context(), repo.SetStripeCheckoutSessionFixtureParams{
		StripeCheckoutSessionID: pgtype.Text{String: "checkout_placeholder", Valid: true},
		OrganizationID:          stripeWebhookOrganizationID,
	}))
	configureInvoiceWebhook(t, service, "event_before_activation", &stripeclient.InvoiceState{
		ID:                 "invoice_before_activation",
		CustomerID:         "customer_placeholder",
		SubscriptionID:     "subscription_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "draft",
		ServicePeriodStart: periodStart,
		ServicePeriodEnd:   periodStart.AddDate(0, 1, 0),
	})
	service.stripeHandler = service.serviceStripeWebhookHandler

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "before-activation").Code)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Empty(t, stripeInvoices(t, db))

	configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "after-activation").Code)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Len(t, stripeInvoices(t, db), 1)
}

func TestStripeInvoiceCreatedRejectsMismatchedFinancialIdentity(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		wantStatus int
		mutate     func(*stripeclient.InvoiceState)
	}{
		{
			name:       "wrong customer",
			wantStatus: http.StatusBadRequest,
			mutate:     func(invoice *stripeclient.InvoiceState) { invoice.CustomerID = "customer_other" },
		},
		{
			name:       "wrong subscription",
			wantStatus: http.StatusInternalServerError,
			mutate:     func(invoice *stripeclient.InvoiceState) { invoice.SubscriptionID = "subscription_other" },
		},
		{
			name:       "wrong currency",
			wantStatus: http.StatusInternalServerError,
			mutate:     func(invoice *stripeclient.InvoiceState) { invoice.Currency = "eur" },
		},
		{
			name:       "unaligned period",
			wantStatus: http.StatusInternalServerError,
			mutate: func(invoice *stripeclient.InvoiceState) {
				invoice.ServicePeriodStart = invoice.ServicePeriodStart.Add(time.Hour)
				invoice.ServicePeriodEnd = invoice.ServicePeriodEnd.Add(time.Hour)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service, db := newStripeWebhookService(t, "customer_placeholder", nil)
			configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
			invoice := &stripeclient.InvoiceState{
				ID:                 "invoice_" + strings.ReplaceAll(test.name, " ", "_"),
				CustomerID:         "customer_placeholder",
				SubscriptionID:     "subscription_placeholder",
				Currency:           "usd",
				BillingReason:      "subscription_cycle",
				Status:             "draft",
				ServicePeriodStart: periodStart,
				ServicePeriodEnd:   periodEnd,
			}
			test.mutate(invoice)
			configureInvoiceWebhook(t, service, "event_"+strings.ReplaceAll(test.name, " ", "_"), invoice)

			require.Equal(t, test.wantStatus, serveStripeWebhook(service, test.name).Code)
			require.Empty(t, stripeInvoices(t, db))
			require.Zero(t, stripeWebhookReceiptCount(t, db))
		})
	}
}

func TestStripeInvoiceIDCannotRebindImmutableIdentity(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
	invoice := &stripeclient.InvoiceState{
		ID:                 "invoice_immutable",
		CustomerID:         "customer_placeholder",
		SubscriptionID:     "subscription_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "draft",
		ServicePeriodStart: periodStart,
		ServicePeriodEnd:   periodEnd,
	}
	configureInvoiceWebhook(t, service, "event_immutable_first", invoice)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)

	rebound := *invoice
	rebound.ServicePeriodStart = periodEnd
	rebound.ServicePeriodEnd = periodEnd.AddDate(0, 1, 0)
	configureInvoiceWebhook(t, service, "event_immutable_second", &rebound)
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "second").Code)

	rows := stripeInvoices(t, db)
	require.Len(t, rows, 1)
	require.True(t, periodStart.Equal(rows[0].ServicePeriodStart.Time))
	require.True(t, periodEnd.Equal(rows[0].ServicePeriodEnd.Time))
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeInvoiceExactReplaySkipsCurrentStateRetrieval(t *testing.T) {
	t.Parallel()

	periodStart := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygInvoiceIdentity(t, service, "subscription_placeholder", periodStart)
	client := configureInvoiceWebhook(t, service, "event_invoice_exact_replay", &stripeclient.InvoiceState{
		ID:                 "invoice_exact_replay",
		CustomerID:         "customer_placeholder",
		SubscriptionID:     "subscription_placeholder",
		Currency:           "usd",
		BillingReason:      "subscription_cycle",
		Status:             "draft",
		ServicePeriodStart: periodStart,
		ServicePeriodEnd:   periodStart.AddDate(0, 1, 0),
	})
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)
	require.EqualValues(t, 1, client.invoiceCalls.Load())

	client.invoiceError = errors.New("Stripe retrieval unavailable")
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "replay").Code)
	require.EqualValues(t, 1, client.invoiceCalls.Load())
	require.Len(t, stripeInvoices(t, db), 1)
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
	preparedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	_, err = service.prepareStripeCheckoutIntent(
		t.Context(),
		stripeWebhookOrganizationID,
		"customer_placeholder",
		preparedAt,
		newStripeCheckoutIntent(stripeWebhookOrganizationID, preparedAt, nil),
		pgtype.Text{String: "", Valid: false},
	)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "activation").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "customer_placeholder", metadata.StripeCustomerID.String)
	require.Equal(t, "subscription_activation", metadata.StripeSubscriptionID.String)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)
	stripeClient, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	require.True(t, stripeClient.checkout.BillingCycleAnchor.Equal(metadata.StripeBillingCycleAnchor.Time))
	require.EqualValues(t, 23, metadata.BillingCycleAnchorDay)
	require.False(t, metadata.StripeCheckoutIdempotencyKey.Valid)
	require.False(t, metadata.StripeCheckoutBillingCycleAnchor.Valid)
	require.False(t, metadata.StripeCheckoutTrialEnd.Valid)
	require.False(t, metadata.StripeCheckoutExpiresAt.Valid)
	require.False(t, metadata.StripeCheckoutSessionID.Valid)
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

func postgresBackendPID(t *testing.T, tx pgx.Tx) int32 {
	t.Helper()

	var pid int32
	err := tx.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid) //nolint:glint // notestingrawsql: backend identity is a PostgreSQL test synchronization primitive unavailable through application SQLc queries
	require.NoError(t, err)
	return pid
}

func waitForStripeWebhookWaitersBlockedByPID(t *testing.T, db *pgxpool.Pool, holderPID int32, count int) []int32 {
	t.Helper()

	var waiterPIDs []int32
	require.Eventually(t, func() bool {
		rows, err := db.Query( //nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL test synchronization primitive unavailable to SQLc generation
			t.Context(), `
SELECT activity.pid
FROM pg_stat_activity AS activity
WHERE activity.datname = current_database()
  AND $1 = ANY(pg_blocking_pids(activity.pid))
ORDER BY activity.pid
`, holderPID)
		if err != nil {
			return false
		}
		waiterPIDs, err = pgx.CollectRows(rows, pgx.RowTo[int32])
		require.NoError(t, err)
		return len(waiterPIDs) == count
	}, 2*time.Second, 10*time.Millisecond)
	return waiterPIDs
}

func waitForStripeWebhookBlockedByPID(t *testing.T, db *pgxpool.Pool, holderPID int32) {
	t.Helper()
	waitForStripeWebhookWaitersBlockedByPID(t, db, holderPID, 1)
}

func waitForStripeReceiptInsertBlockedByPID(t *testing.T, db *pgxpool.Pool, holderPID int32) {
	t.Helper()

	require.Eventually(t, func() bool {
		var blocked bool
		err := db.QueryRow( //nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL test synchronization primitive unavailable to SQLc generation
			t.Context(), `
SELECT EXISTS (
  SELECT 1
  FROM pg_stat_activity AS activity
  WHERE activity.datname = current_database()
    AND $1 = ANY(pg_blocking_pids(activity.pid))
    AND activity.query LIKE '%INSERT INTO stripe_webhook_receipts%'
)
`, holderPID).Scan(&blocked)
		require.NoError(t, err)
		return blocked
	}, 2*time.Second, 10*time.Millisecond)
}

func receiveStripeWebhookStatus(t *testing.T, response <-chan int) int {
	t.Helper()

	select {
	case status := <-response:
		return status
	case <-time.After(15 * time.Second):
		require.FailNow(t, "timed out waiting for Stripe webhook response")
		return 0
	}
}

func TestStripeCheckoutLocksConvertedTrialBeforeOpenRouterKeys(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_trial_lock_order", "subscription_trial_lock_order", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	_, err := trialsrepo.New(db).MarkTrialConverted(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)

	trialTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the test must hold a trial-row transaction open while the webhook blocks
	require.NoError(t, err)
	t.Cleanup(func() { _ = trialTx.Rollback(context.Background()) })
	trialHolderPID := postgresBackendPID(t, trialTx)
	_, err = trialsrepo.New(trialTx).LockTrialLifecycleForRearm(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)

	response := make(chan int, 1)
	go func() { response <- serveStripeWebhook(service, "convert").Code }()
	waitForStripeWebhookBlockedByPID(t, db, trialHolderPID)

	probeTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the probe transaction proves the chat advisory lock remains available
	require.NoError(t, err)
	probeCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, repo.New(probeTx).AcquireOpenRouterBillingLock(probeCtx, repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeChat),
		OrganizationID: stripeWebhookOrganizationID,
	}))
	require.NoError(t, probeTx.Rollback(t.Context()))
	require.NoError(t, trialTx.Rollback(t.Context()))
	require.Equal(t, http.StatusOK, receiveStripeWebhookStatus(t, response))
}

func TestStripeCheckoutAcquiresOpenRouterLocksInAllKeyTypesOrder(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_key_lock_order", "subscription_key_lock_order", "active")
	require.Equal(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, openrouter.AllKeyTypes)

	chatTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the test must hold the first advisory lock while the webhook blocks
	require.NoError(t, err)
	t.Cleanup(func() { _ = chatTx.Rollback(context.Background()) })
	chatHolderPID := postgresBackendPID(t, chatTx)
	require.NoError(t, repo.New(chatTx).AcquireOpenRouterBillingLock(t.Context(), repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeChat),
		OrganizationID: stripeWebhookOrganizationID,
	}))

	response := make(chan int, 1)
	go func() { response <- serveStripeWebhook(service, "activate").Code }()
	waitForStripeWebhookBlockedByPID(t, db, chatHolderPID)

	probeTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the probe transaction proves the internal advisory lock remains available
	require.NoError(t, err)
	probeCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, repo.New(probeTx).AcquireOpenRouterBillingLock(probeCtx, repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: stripeWebhookOrganizationID,
	}))
	require.NoError(t, probeTx.Rollback(t.Context()))
	require.NoError(t, chatTx.Rollback(t.Context()))
	require.Equal(t, http.StatusOK, receiveStripeWebhookStatus(t, response))
}

func TestStripeSubscriptionDeletionAcquiresEveryOpenRouterLockInOrder(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_deletion_key_order", "subscription_key_order", "subscription_key_order")
	service.stripeHandler = service.serviceStripeWebhookHandler
	require.Equal(t, []openrouter.KeyType{openrouter.KeyTypeChat, openrouter.KeyTypeInternal}, openrouter.AllKeyTypes)

	internalTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the targeted backend holds the second canonical advisory lock
	require.NoError(t, err)
	t.Cleanup(func() { _ = internalTx.Rollback(context.Background()) })
	internalHolderPID := postgresBackendPID(t, internalTx)
	require.NoError(t, repo.New(internalTx).AcquireOpenRouterBillingLock(t.Context(), repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: stripeWebhookOrganizationID,
	}))

	requestCtx, cancelRequest := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelRequest()
	response := make(chan int, 1)
	go func() { response <- serveStripeWebhookWithContext(requestCtx, service, "delete").Code }()
	waitForStripeWebhookBlockedByPID(t, db, internalHolderPID)

	probeTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the probe proves deletion retained the first lock while waiting for the second
	require.NoError(t, err)
	probeCtx, cancelProbe := context.WithTimeout(t.Context(), 500*time.Millisecond)
	probeErr := repo.New(probeTx).AcquireOpenRouterBillingLock(probeCtx, repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeChat),
		OrganizationID: stripeWebhookOrganizationID,
	})
	cancelProbe()
	_ = probeTx.Rollback(t.Context())
	require.ErrorIs(t, probeErr, context.DeadlineExceeded)

	require.NoError(t, internalTx.Rollback(t.Context()))
	require.Equal(t, http.StatusOK, receiveStripeWebhookStatus(t, response))
}

func TestReplacementCheckoutAndPriorSubscriptionDeletionAcquireOpenRouterBeforeBilling(t *testing.T) {
	t.Parallel()

	deletionService, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, deletionService, "event_prior_deletion", "subscription_prior", "subscription_prior")
	deletionService.stripeHandler = deletionService.serviceStripeWebhookHandler
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 100)

	checkoutService := *deletionService
	checkoutService.stripeClient = &fakeStripeWebhookClient{}
	configurePaygCheckout(t, &checkoutService, "event_replacement_checkout", "subscription_replacement", "active")

	holderTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: the test queues both lifecycle transactions behind one targeted backend
	require.NoError(t, err)
	t.Cleanup(func() { _ = holderTx.Rollback(context.Background()) })
	holderPID := postgresBackendPID(t, holderTx)
	require.NoError(t, repo.New(holderTx).AcquireOpenRouterBillingLock(t.Context(), repo.AcquireOpenRouterBillingLockParams{
		KeyType:        string(openrouter.KeyTypeChat),
		OrganizationID: stripeWebhookOrganizationID,
	}))

	requestCtx, cancelRequests := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelRequests()
	deletionResponse := make(chan int, 1)
	go func() {
		deletionResponse <- serveStripeWebhookWithContext(requestCtx, deletionService, "delete prior").Code
	}()
	deletionWaiter := waitForStripeWebhookWaitersBlockedByPID(t, db, holderPID, 1)

	checkoutResponse := make(chan int, 1)
	go func() {
		checkoutResponse <- serveStripeWebhookWithContext(requestCtx, &checkoutService, "replace").Code
	}()
	waiters := waitForStripeWebhookWaitersBlockedByPID(t, db, holderPID, 2)
	require.Contains(t, waiters, deletionWaiter[0])

	probeTx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: availability of the canonical next lock is the ordering assertion
	require.NoError(t, err)
	probeCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	probeErr := repo.New(probeTx).LockBillingMetadataOrganization(probeCtx, stripeWebhookOrganizationID)
	cancel()
	_ = probeTx.Rollback(t.Context())

	require.NoError(t, holderTx.Rollback(t.Context()))
	deletionStatus := receiveStripeWebhookStatus(t, deletionResponse)
	checkoutStatus := receiveStripeWebhookStatus(t, checkoutResponse)
	require.NoError(t, probeErr, "billing lock must remain available while both lifecycle transactions wait for OpenRouter")
	require.Equal(t, http.StatusOK, deletionStatus)
	require.Equal(t, http.StatusOK, checkoutStatus)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_replacement", metadata.StripeSubscriptionID.String)
}

func TestStripeCheckoutConversionReplacesTrialAndBillingCausesIndependently(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_lifecycle_conversion", "subscription_lifecycle_conversion", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 41)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 59)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "trial_demotion", "billing_inactive", "policy_hold"}, 41)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"admin_lock", "trial_demotion"}, 59)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "convert").Code)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "duplicate_convert").Code)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "policy_hold"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Equal(t, []string{"admin_lock"}, internal.DisableCauses)
	require.True(t, internal.Disabled)
}

func TestStripeCheckoutConversionRecoversBillingWithoutAdminLock(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_conversion_billing_only", "subscription_conversion_billing_only", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 17)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 23)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"trial_demotion", "billing_inactive"}, 17)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"trial_demotion", "security_hold"}, 23)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "convert").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Empty(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Equal(t, []string{"security_hold"}, internal.DisableCauses)
	require.True(t, internal.Disabled)
	require.EqualValues(t, 23, internal.MonthlyCredits)
}

func TestStripeCheckoutConversionRefreshesCurrentPaygCapWhenBillingCauseIsAbsent(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_conversion_cap_refresh", "subscription_conversion_cap_refresh", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 29)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 31)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "trial_demotion"}, 29)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"trial_demotion"}, 31)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "convert").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Empty(t, internal.DisableCauses)
	require.False(t, internal.Disabled)
	require.EqualValues(t, 31, internal.MonthlyCredits)
}

func TestStripeCheckoutConvertedTrialAuditFailureRollsBackLifecycleAndReceipt(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_conversion_rollback", "subscription_conversion_rollback", "active")
	service.auditLogger = nil
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 37)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 43)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "trial_demotion", "billing_inactive"}, 37)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"trial_demotion"}, 43)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "convert").Code)

	trial, err := trialsrepo.New(db).GetTrial(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, trial.ConvertedAt.Valid)
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "trial_demotion", "billing_inactive"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	require.EqualValues(t, 37, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Equal(t, []string{"trial_demotion"}, internal.DisableCauses)
	require.True(t, internal.Disabled)
	require.EqualValues(t, 43, internal.MonthlyCredits)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Zero(t, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))
}

func TestStripeCheckoutRecoveryRemovesOnlyBillingCauseAndRefreshesCurrentPaygCap(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_billing_recovery", "subscription_billing_recovery", "active")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 7)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "billing_inactive"}, 7)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "recover").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
}

func TestStripeCheckoutRecoveryRefreshesCurrentPaygCapWhenBillingCauseIsAbsent(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_billing_refresh", "subscription_billing_refresh", "active")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 13)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, false, []string{}, 13)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "refresh").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Empty(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
}

func TestStripeCheckoutCompletionRestoresDemotedTrialRuntimeFeatures(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	featureCache := configurePaygCheckout(t, service, "event_demoted_trial", "subscription_demoted_trial", "trialing")
	ctx := t.Context()
	tx := testenv.BeginTx(t, ctx, db)
	q := featurerepo.New(tx)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		_, err := q.EnableFeature(ctx, featurerepo.EnableFeatureParams{
			OrganizationID: stripeWebhookOrganizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(ctx))

	err := trialsrepo.New(db).CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: stripeWebhookOrganizationID,
		Tier:           "enterprise",
		EndsAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true},
	})
	require.NoError(t, err)
	_, err = trialsrepo.New(db).MarkTrialDemoted(ctx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		_, err := featurerepo.New(db).DeleteFeature(ctx, featurerepo.DeleteFeatureParams{
			OrganizationID: stripeWebhookOrganizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
	}

	preparedAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	_, err = service.prepareStripeCheckoutIntent(
		ctx,
		stripeWebhookOrganizationID,
		"customer_placeholder",
		preparedAt,
		newStripeCheckoutIntent(stripeWebhookOrganizationID, preparedAt, nil),
		pgtype.Text{String: "", Valid: false},
	)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "demoted_trial").Code)
	for _, feature := range productfeatures.TrialRuntimeFeatures {
		enabled, err := featurerepo.New(db).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
			OrganizationID: stripeWebhookOrganizationID,
			FeatureName:    string(feature),
		})
		require.NoError(t, err)
		require.Truef(t, enabled, "PAYG activation should restore %s", feature)
	}
	require.ElementsMatch(t, productfeatures.TrialRuntimeFeatures, featureCache.snapshot())
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
	featureCache := configurePaygCheckout(t, service, "event_first", "subscription_replay", "past_due")
	metrics := &captureStripeWebhookMetrics{}
	service.stripeMetrics = metrics
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 17)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"billing_inactive"}, 17)

	var patches atomic.Int32
	var firstPatchMu sync.Mutex
	var firstPatch map[string]any
	var handlerErrorsMu sync.Mutex
	var handlerErrors []error
	recordHandlerError := func(err error) {
		handlerErrorsMu.Lock()
		defer handlerErrorsMu.Unlock()
		handlerErrors = append(handlerErrors, err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			recordHandlerError(fmt.Errorf("unexpected OpenRouter method %s", r.Method))
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if patches.Add(1) == 1 {
			firstPatchMu.Lock()
			err := json.NewDecoder(r.Body).Decode(&firstPatch)
			firstPatchMu.Unlock()
			if err != nil {
				recordHandlerError(fmt.Errorf("decode OpenRouter PATCH: %w", err))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 100.0, "hash": "hash_placeholder_chat"},
		}); err != nil {
			recordHandlerError(fmt.Errorf("encode OpenRouter response: %w", err))
		}
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)
	patchesAfterFirst := patches.Load()
	require.Positive(t, patchesAfterFirst)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	firstPatchMu.Lock()
	patchedLimit := firstPatch["limit"]
	firstPatchMu.Unlock()
	require.EqualValues(t, limit, patchedLimit, "true recovery must PATCH the current PAYG cap")
	cacheAfterFirst := featureCache.snapshot()
	keyAfterFirst := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)

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
	handlerErrorsMu.Lock()
	collectedHandlerErrors := slices.Clone(handlerErrors)
	handlerErrorsMu.Unlock()
	for _, handlerErr := range collectedHandlerErrors {
		require.NoError(t, handlerErr)
	}

	require.Equal(t, 1, paygSchedulingIntentCount(t, db))
	require.Equal(t, 2, stripeWebhookReceiptCount(t, db), "the distinct event receipt remains durable")
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))
	require.Equal(t, patchesAfterFirst, patches.Load(), "unchanged active checkout must not PATCH OpenRouter again")
	require.Equal(t, cacheAfterFirst, featureCache.snapshot())
	require.EqualValues(t, 2, client.checkoutCalls.Load(), "a distinct event must verify current Stripe state")
	require.Zero(t, metrics.invoicePaymentFailures.Load())
	require.Zero(t, metrics.subscriptionLosses.Load())
	require.Equal(t, keyAfterFirst, openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat))
}

func TestStripeCheckoutFinalBillingCauseRecoveryReconcilesStaleDisabledMirror(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_final_cause_recovery", "subscription_final_cause_recovery", "past_due")
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, int64(limit))
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, false, []string{"billing_inactive"}, int64(limit))

	patches := make(chan map[string]any, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		patches <- patch
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": float64(limit), "hash": "hash_placeholder_chat"},
		})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "final cause recovery").Code)
	require.Len(t, patches, 1, "removing the final cause must schedule postcommit reconciliation")
	require.Equal(t, false, (<-patches)["disabled"])
	key := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.False(t, key.Disabled)
	require.Empty(t, key.DisableCauses)
	require.Equal(t, int64(limit), key.MonthlyCredits)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeCheckoutLayeredBillingRecoveryDoesNotReconcileUnchangedAccess(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_layered_recovery", "subscription_layered_recovery", "past_due")
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, int64(limit))
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, false, []string{"admin_lock", "billing_inactive", "unknown_policy"}, int64(limit))

	var patches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		patches.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "layered recovery").Code)
	require.Zero(t, patches.Load(), "removing billing_inactive must not PATCH while another cause keeps access disabled")
	key := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.True(t, key.Disabled)
	require.Equal(t, []string{"admin_lock", "unknown_policy"}, key.DisableCauses)
	require.Equal(t, int64(limit), key.MonthlyCredits)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
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

func TestStripeCheckoutConvertedDemotedTrialExactReplayRepairsInternalPostCommitFailure(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_internal_repair_replay", "subscription_internal_repair_replay", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 17)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 23)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "trial_demotion", "billing_inactive"}, 17)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"trial_demotion"}, 23)

	var chatPatches atomic.Int32
	var internalPatches atomic.Int32
	var internalSecurityUpstream struct {
		sync.Mutex
		disabled     bool
		monthlyLimit float64
		limitReset   string
		patchPaths   []string
	}
	internalSecurityUpstream.disabled = true
	internalSecurityUpstream.monthlyLimit = 23
	internalSecurityUpstream.limitReset = "monthly"
	var failInternal atomic.Bool
	failInternal.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var patch struct {
			Limit      *float64 `json:"limit"`
			LimitReset string   `json:"limit_reset"`
			Disabled   *bool    `json:"disabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid patch", http.StatusBadRequest)
			return
		}

		keyHash := strings.TrimPrefix(r.URL.Path, "/v1/keys/")
		switch keyHash {
		case "hash_placeholder_chat":
			chatPatches.Add(1)
		case "hash_placeholder_internal":
			internalPatches.Add(1)
			internalSecurityUpstream.Lock()
			internalSecurityUpstream.patchPaths = append(internalSecurityUpstream.patchPaths, r.URL.Path)
			internalSecurityUpstream.Unlock()
			if failInternal.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			internalSecurityUpstream.Lock()
			if patch.Disabled != nil {
				internalSecurityUpstream.disabled = *patch.Disabled
			}
			if patch.Limit != nil {
				internalSecurityUpstream.monthlyLimit = *patch.Limit
				internalSecurityUpstream.limitReset = patch.LimitReset
			}
			internalSecurityUpstream.Unlock()
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 100.0, "hash": keyHash},
		})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "first").Code)
	failedInternalPatchAttempts := internalPatches.Load()
	require.Positive(t, failedInternalPatchAttempts)
	require.Positive(t, chatPatches.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))
	require.Equal(t, 1, paygSchedulingIntentCount(t, db))

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Empty(t, internal.DisableCauses)
	require.False(t, internal.Disabled)
	require.EqualValues(t, 23, internal.MonthlyCredits)

	failInternal.Store(false)
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	require.EqualValues(t, 1, client.checkoutCalls.Load())
	client.checkoutError = errors.New("exact replay must not fetch Stripe checkout state")

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "exact replay").Code)
	require.Greater(t, internalPatches.Load(), failedInternalPatchAttempts)
	require.EqualValues(t, 1, client.checkoutCalls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))
	require.Equal(t, 1, paygSchedulingIntentCount(t, db))

	chat = openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	internal = openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Empty(t, internal.DisableCauses)
	require.False(t, internal.Disabled)
	require.EqualValues(t, 23, internal.MonthlyCredits)

	internalSecurityUpstream.Lock()
	defer internalSecurityUpstream.Unlock()
	require.False(t, internalSecurityUpstream.disabled)
	require.InDelta(t, 23, internalSecurityUpstream.monthlyLimit, 0)
	require.Equal(t, "monthly", internalSecurityUpstream.limitReset)
	require.NotEqualValues(t, limit, internalSecurityUpstream.monthlyLimit)
	require.NotEmpty(t, internalSecurityUpstream.patchPaths)
	for _, path := range internalSecurityUpstream.patchPaths {
		require.Equal(t, "/v1/keys/hash_placeholder_internal", path)
	}
}

func TestStripeCheckoutLostReceiptInsertRepairsWinnerPostCommitFailure(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_lost_insert_repair", "subscription_lost_insert_repair", "active")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 17)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"billing_inactive"}, 17)

	const duplicateOrganizationID = "org_duplicate_placeholder"
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{
		ID: duplicateOrganizationID, Name: "Duplicate Placeholder Organization", Slug: "duplicate-placeholder-organization",
	}))
	require.NoError(t, repo.New(db).CreateStripeBillingMetadataFixture(t.Context(), repo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   duplicateOrganizationID,
		StripeCustomerID: pgtype.Text{String: "customer_duplicate_placeholder", Valid: true},
	}))

	failingUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failingUpstream.Close)
	service.openRouter = stripeLifecycleOpenRouterProvisioner(t, db, failingUpstream.URL)

	var repairedPatches atomic.Int32
	var repairedUpstream struct {
		sync.Mutex
		disabled *bool
		limit    *float64
	}
	repairingUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var patch struct {
			Limit    *float64 `json:"limit"`
			Disabled *bool    `json:"disabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid patch", http.StatusBadRequest)
			return
		}
		repairedUpstream.Lock()
		repairedUpstream.disabled = patch.Disabled
		repairedUpstream.limit = patch.Limit
		repairedUpstream.Unlock()
		repairedPatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 100.0, "hash": "hash_placeholder_chat"},
		})
	}))
	t.Cleanup(repairingUpstream.Close)

	winnerEntered := make(chan int32, 1)
	releaseWinner := make(chan struct{})
	originalHandler := service.stripeHandler
	service.stripeHandler = func(ctx context.Context, logger *slog.Logger, tx pgx.Tx, organizationID string, event *stripeclient.WebhookEvent, checkout *stripeclient.CheckoutSessionState, invoice *stripeclient.InvoiceState) (stripeWebhookResult, error) {
		result, err := originalHandler(ctx, logger, tx, organizationID, event, checkout, invoice)
		if err != nil {
			return stripeWebhookResult{}, err
		}
		var pid int32
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil { //nolint:glint // notestingrawsql: backend identity synchronizes the real PostgreSQL conflict
			return stripeWebhookResult{}, fmt.Errorf("read winner backend PID: %w", err)
		}
		select {
		case winnerEntered <- pid:
		default:
			return stripeWebhookResult{}, errors.New("winner backend was already reported")
		}
		select {
		case <-releaseWinner:
			return result, nil
		case <-ctx.Done():
			return stripeWebhookResult{}, ctx.Err()
		}
	}

	duplicateClient := &fakeStripeWebhookClient{
		verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return &stripeclient.WebhookEvent{
				ID: "event_lost_insert_repair", Type: "invoice.payment_failed", ObjectID: "invoice_duplicate_placeholder",
				CustomerID: "customer_duplicate_placeholder",
			}, nil
		},
		checkout: nil, checkoutError: nil, invoice: nil, invoiceError: nil,
		checkoutCalls: atomic.Int32{}, invoiceCalls: atomic.Int32{}, verifyCalls: atomic.Int32{},
	}
	duplicate := *service
	duplicate.stripeClient = duplicateClient
	duplicate.openRouter = stripeLifecycleOpenRouterProvisioner(t, db, repairingUpstream.URL)

	winnerResponse := make(chan int, 1)
	go func() { winnerResponse <- serveStripeWebhook(service, "winner").Code }()
	var winnerPID int32
	select {
	case winnerPID = <-winnerEntered:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for winner transaction")
	}

	duplicateResponse := make(chan int, 1)
	go func() { duplicateResponse <- serveStripeWebhook(&duplicate, "duplicate").Code }()
	waitForStripeReceiptInsertBlockedByPID(t, db, winnerPID)
	close(releaseWinner)

	require.Equal(t, http.StatusInternalServerError, receiveStripeWebhookStatus(t, winnerResponse))
	require.Equal(t, http.StatusOK, receiveStripeWebhookStatus(t, duplicateResponse))
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))
	require.Equal(t, 1, paygSchedulingIntentCount(t, db))
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	winnerClient, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	require.EqualValues(t, 1, winnerClient.checkoutCalls.Load())
	require.Zero(t, duplicateClient.checkoutCalls.Load())
	require.EqualValues(t, 1, repairedPatches.Load())

	repairedUpstream.Lock()
	defer repairedUpstream.Unlock()
	require.NotNil(t, repairedUpstream.disabled)
	require.False(t, *repairedUpstream.disabled)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.NotNil(t, repairedUpstream.limit)
	require.InDelta(t, limit, *repairedUpstream.limit, 0)
}

func TestStripeCheckoutExactReplayRepairsPostCommitOpenRouterFailure(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_repair_replay", "subscription_repair_replay", "active")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 17)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"billing_inactive"}, 17)

	var patches atomic.Int32
	var failPatches atomic.Bool
	failPatches.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		patches.Add(1)
		if failPatches.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 100.0, "hash": "hash_placeholder_chat"},
		})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "first").Code)
	failedPatchAttempts := patches.Load()
	require.Positive(t, failedPatchAttempts)
	failPatches.Store(false)
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	require.EqualValues(t, 1, client.checkoutCalls.Load())
	client.checkoutError = errors.New("exact replay must not fetch Stripe checkout state")

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "exact replay").Code)
	require.Greater(t, patches.Load(), failedPatchAttempts)
	require.EqualValues(t, 1, client.checkoutCalls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygActivated))

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	require.Empty(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
}

func TestStripeCheckoutAuditFailureRollsBackActivationAndSchedulingIntent(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	featureCache := configurePaygCheckout(t, service, "event_audit_failure", "subscription_audit_failure", "active")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 7)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "billing_inactive"}, 7)
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
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "billing_inactive"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	require.EqualValues(t, 7, chat.MonthlyCredits)
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

func TestStripeSubscriptionDeletionAddsOnlyBillingInactiveCause(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_billing_loss_cause", "subscription_current", "subscription_current")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 321)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock"}, 321)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "deactivate").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "billing_inactive"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	require.EqualValues(t, 321, chat.MonthlyCredits)
}

func TestStripeSubscriptionDeletionRejectsUnclassifiedKeyAndRollsBackAtomically(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_unclassified_key", "subscription_current", "subscription_current")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 321)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, false, nil, 321)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "deactivate").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_current", metadata.StripeSubscriptionID.String)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Nil(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
	require.EqualValues(t, 321, chat.MonthlyCredits)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Zero(t, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
}

func TestStripeSubscriptionDeletionPostCommitReconcileFailurePreservesDurableIntent(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_postcommit_failure", "subscription_current", "subscription_current")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 321)
	var sawCommittedIntent atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chat, keyErr := openrouterrepo.New(db).GetOpenRouterAPIKey(r.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: stripeWebhookOrganizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		})
		receipts, receiptErr := repo.New(db).CountStripeWebhookReceiptsFixture(r.Context(), stripeWebhookOrganizationID)
		if r.Method == http.MethodPatch && keyErr == nil && receiptErr == nil && chat.Disabled && slices.Equal(chat.DisableCauses, []string{"billing_inactive"}) && receipts == 1 {
			sawCommittedIntent.Store(true)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t),
		tracerProvider,
		guardianPolicy,
		db,
		"test",
		"provisioning_key_placeholder",
		nil,
		nil,
		nil,
		testenv.NewEncryptionClient(t),
		option,
	)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "deactivate").Code)

	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"billing_inactive"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	require.True(t, sawCommittedIntent.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
}

func TestStripeSubscriptionDeletionExactReplayRepairsPostCommitOpenRouterFailure(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_deletion_repair_replay", "subscription_current", "subscription_current")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 321)

	var patches atomic.Int32
	var failPatches atomic.Bool
	failPatches.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		patches.Add(1)
		if failPatches.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 321.0, "hash": "hash_placeholder_chat"},
		})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "first").Code)
	failedPatchAttempts := patches.Load()
	require.Positive(t, failedPatchAttempts)
	failPatches.Store(false)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "exact replay").Code)
	require.Greater(t, patches.Load(), failedPatchAttempts)
	require.EqualValues(t, 1, metrics.subscriptionLosses.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.True(t, chat.Disabled)
	require.Equal(t, []string{"billing_inactive"}, chat.DisableCauses)
}

func TestStripeSubscriptionDeletionReplayCannotOverrideLaterConversion(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_loss_before_conversion", "subscription_old", "subscription_old")
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	deletedEvent := client.verify
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 73)

	var patches atomic.Int32
	var failPatches atomic.Bool
	failPatches.Store(true)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		patches.Add(1)
		if failPatches.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"limit": 100.0, "hash": "hash_placeholder_chat"},
		})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	service.openRouter = openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "loss").Code)
	require.Positive(t, patches.Load())
	failPatches.Store(false)

	configurePaygCheckout(t, service, "event_conversion_after_loss", "subscription_replacement", "active")
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "conversion").Code)
	patchesAfterConversion := patches.Load()
	client.verify = deletedEvent
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "stale exact loss replay").Code)
	require.Equal(t, patchesAfterConversion, patches.Load())

	require.EqualValues(t, 1, metrics.subscriptionLosses.Load())
	require.Equal(t, 2, stripeWebhookReceiptCount(t, db))
	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_replacement", metadata.StripeSubscriptionID.String)
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	limit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, limit, chat.MonthlyCredits)
	require.Empty(t, chat.DisableCauses)
	require.False(t, chat.Disabled)
}

func TestConvertedDemotedTrialSubscriptionLossWinsBeforeDelayedActivationReconcile(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygCheckout(t, service, "event_converted_before_loss", "subscription_converted_before_loss", "active")
	createDemotedEnterpriseTrialFixture(t, db)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 41)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 59)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock", "trial_demotion"}, 41)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal, true, []string{"trial_demotion", "security_hold"}, 59)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "convert").Code)
	trial, err := trialsrepo.New(db).GetTrial(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.True(t, trial.ConvertedAt.Valid)
	require.True(t, trial.DemotedAt.Valid)

	configurePaygSubscriptionDeletion(t, service, "event_loss_before_activation_wakeup", "subscription_converted_before_loss", "subscription_converted_before_loss")
	service.stripeHandler = service.serviceStripeWebhookHandler
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "lose subscription").Code)

	var patches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patches.Add(1)
		}
		hash := strings.TrimPrefix(r.URL.Path, "/v1/keys/")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"hash": hash, "limit": 100.0}})
	}))
	t.Cleanup(upstream.Close)
	option, err := openrouter.WithTestBaseURL(upstream.URL)
	require.NoError(t, err)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	production := openrouter.New(
		testenv.NewLogger(t), tracerProvider, guardianPolicy, db, "test", "provisioning_key_placeholder",
		nil, nil, nil, testenv.NewEncryptionClient(t), option,
	)
	require.NoError(t, RepairPaygOpenRouterChatKey(
		t.Context(), testenv.NewLogger(t), db, production, stripeWebhookOrganizationID, openrouter.KeyDesiredStateEnabled,
	))

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "free", organization.GramAccountType)
	chat := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat)
	require.Equal(t, []string{"admin_lock", "billing_inactive"}, chat.DisableCauses)
	require.True(t, chat.Disabled)
	paygLimit, ok := openrouter.AccountTypeCreditLimit("payg")
	require.True(t, ok)
	require.EqualValues(t, paygLimit, chat.MonthlyCredits)
	internal := openRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeInternal)
	require.Equal(t, []string{"security_hold"}, internal.DisableCauses)
	require.True(t, internal.Disabled)
	require.EqualValues(t, 59, internal.MonthlyCredits)
	require.Zero(t, patches.Load(), "delayed activation must not patch over committed subscription loss")
}

func TestStripeSubscriptionDeletionDeactivatesCurrentPaygBillingAtomically(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_deactivation", "subscription_current", "subscription_current")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 321)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeInternal, 654)

	tx := testenv.BeginTx(t, t.Context(), db)
	_, err := productfeatures.SeedPaygEntitlementsTx(t.Context(), tx, stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))

	before, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "deactivate").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "customer_placeholder", metadata.StripeCustomerID.String)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	require.False(t, metadata.StripeBillingCycleAnchor.Valid)
	require.EqualValues(t, 23, metadata.BillingCycleAnchorDay)

	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "free", organization.GramAccountType)
	require.False(t, organization.Whitelisted)

	chatKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.True(t, chatKey.Disabled)
	require.EqualValues(t, 321, chatKey.MonthlyCredits)
	internalKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.False(t, internalKey.Disabled)
	require.EqualValues(t, 654, internalKey.MonthlyCredits)

	featureEnabled, err := featurerepo.New(db).IsFeatureEnabled(t.Context(), featurerepo.IsFeatureEnabledParams{
		OrganizationID: stripeWebhookOrganizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	require.True(t, featureEnabled)

	after, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	require.Equal(t, "system", record.ActorID)
	require.Equal(t, "organization", record.SubjectType)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "payg", beforeSnapshot["account_type"])
	require.Equal(t, true, beforeSnapshot["whitelisted"])
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "free", afterSnapshot["account_type"])
	require.Equal(t, false, afterSnapshot["whitelisted"])
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Zero(t, metrics.invoicePaymentFailures.Load())
	require.EqualValues(t, 1, metrics.subscriptionLosses.Load())
}

func TestStripeSubscriptionDeletionDeactivatesUnwhitelistedPaygBilling(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_deactivation_unwhitelisted", "subscription_unwhitelisted", "subscription_unwhitelisted")
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          stripeWebhookOrganizationID,
		Name:        "Placeholder Organization",
		Slug:        "placeholder-organization",
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: true},
	})
	require.NoError(t, err)
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "deactivate").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "customer_placeholder", metadata.StripeCustomerID.String)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	require.False(t, metadata.StripeBillingCycleAnchor.Valid)
	require.EqualValues(t, 23, metadata.BillingCycleAnchorDay)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "free", organization.GramAccountType)
	require.False(t, organization.Whitelisted)
	chatKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.True(t, chatKey.Disabled)

	record, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "payg", beforeSnapshot["account_type"])
	require.Equal(t, false, beforeSnapshot["whitelisted"])
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.EqualValues(t, 1, metrics.subscriptionLosses.Load())
}

func TestStripeSubscriptionDeletionDistinctReplayIsDomainNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_deactivation_first", "subscription_replay", "subscription_replay")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)

	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         "event_deactivation_second",
			Type:       "customer.subscription.deleted",
			Created:    time.Now().UTC(),
			ObjectID:   "subscription_replay",
			CustomerID: "customer_placeholder",
		}, nil
	}
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "second").Code)

	require.Equal(t, 2, stripeWebhookReceiptCount(t, db))
	count, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, 1, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
	require.EqualValues(t, 1, metrics.subscriptionLosses.Load())
}

func TestStripeSubscriptionDeletionForNonCurrentSubscriptionIsDurableNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_deactivation_stale", "subscription_current", "subscription_stale")
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "stale").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_current", metadata.StripeSubscriptionID.String)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)
	chatKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.False(t, chatKey.Disabled)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 0, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
}

func TestStripeSubscriptionDeletionForEnterpriseOrganizationIsDurableNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_deactivation_enterprise", "subscription_enterprise", "subscription_enterprise")
	require.NoError(t, orgrepo.New(db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		ID:              stripeWebhookOrganizationID,
		GramAccountType: "enterprise",
	}))
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "enterprise").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_enterprise", metadata.StripeSubscriptionID.String)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", organization.GramAccountType)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 0, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
}

func TestStripeSubscriptionDeletionForPolarOrganizationIsDurableNoop(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	configurePaygSubscriptionDeletion(t, service, "event_deactivation_polar", "subscription_polar", "subscription_polar")
	require.NoError(t, orgrepo.New(db).SetAccountType(t.Context(), orgrepo.SetAccountTypeParams{
		ID:              stripeWebhookOrganizationID,
		GramAccountType: "pro",
	}))
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "polar").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_polar", metadata.StripeSubscriptionID.String)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "pro", organization.GramAccountType)
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 0, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
}

func TestStripeSubscriptionDeletionAuditFailureRollsBackDomainAndReceipt(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := configurePaygSubscriptionDeletion(t, service, "event_deactivation_failure", "subscription_failure", "subscription_failure")
	service.auditLogger = nil
	createOpenRouterKeyFixture(t, db, openrouter.KeyTypeChat, 100)
	setOpenRouterKeyLifecycleFixture(t, db, openrouter.KeyTypeChat, true, []string{"admin_lock"}, 100)
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "failure").Code)

	metadata, err := repo.New(db).GetBillingMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_failure", metadata.StripeSubscriptionID.String)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), stripeWebhookOrganizationID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)
	chatKey, err := openrouterrepo.New(db).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: stripeWebhookOrganizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.True(t, chatKey.Disabled)
	require.Equal(t, []string{"admin_lock"}, chatKey.DisableCauses)
	require.EqualValues(t, 100, chatKey.MonthlyCredits)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Equal(t, 0, organizationBillingActionIntentCount(t, db, audit.ActionOrganizationPaygDeactivated))
	require.Zero(t, metrics.subscriptionLosses.Load())
}

func TestStripeInvoicePaymentFailureIsObservedOnceAfterDurableReceipt(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	metrics := &captureStripeWebhookMetrics{}
	service.stripeMetrics = metrics
	service.stripeHandler = service.serviceStripeWebhookHandler
	client, ok := service.stripeClient.(*fakeStripeWebhookClient)
	require.True(t, ok)
	client.verify = func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:         "event_payment_failure",
			Type:       "invoice.payment_failed",
			Created:    time.Now().UTC(),
			ObjectID:   "invoice_payment_failure",
			CustomerID: "customer_placeholder",
		}, nil
	}

	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)
	require.EqualValues(t, 1, metrics.invoicePaymentFailures.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "replay").Code)
	require.EqualValues(t, 1, metrics.invoicePaymentFailures.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}

func TestStripeSubscriptionDeletionRejectsMissingObjectSubscription(t *testing.T) {
	t.Parallel()

	client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
		return &stripeclient.WebhookEvent{
			ID:             "event_deactivation_malformed",
			Type:           "customer.subscription.deleted",
			Created:        time.Now().UTC(),
			CustomerID:     "customer_placeholder",
			SubscriptionID: "subscription_payload_must_not_be_used",
		}, nil
	}}
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: testStripeWebhookHandler}
	require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "malformed").Code)
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
