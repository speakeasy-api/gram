package usage

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type fakeStripeWebhookClient struct {
	verify      func([]byte, string) (*stripeclient.WebhookEvent, error)
	verifyCalls atomic.Int32
}

const stripeWebhookOrganizationID = "org_placeholder"

func (f *fakeStripeWebhookClient) CreateCustomer(context.Context, stripeclient.CreateCustomerInput) (*stripeclient.Customer, error) {
	return nil, errors.New("not implemented")
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
		handler = serviceStripeWebhookHandler
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
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}
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
	service := &Service{logger: testenv.NewLogger(t), stripeClient: nil, stripeHandler: serviceStripeWebhookHandler}
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
	service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}

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
			stripeHandler: serviceStripeWebhookHandler,
		}
		require.Equal(t, http.StatusServiceUnavailable, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return nil, errors.New("signature mismatch")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}
		require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("unsigned", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, signature string) (*stripeclient.WebhookEvent, error) {
			require.Empty(t, signature)
			return nil, errors.New("missing signature")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}
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
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}
		require.Equal(t, http.StatusBadRequest, serveStripeWebhook(service, "{}").Code)
	})

	t.Run("oversized body", func(t *testing.T) {
		t.Parallel()
		client := &fakeStripeWebhookClient{verify: func(_ []byte, _ string) (*stripeclient.WebhookEvent, error) {
			return nil, errors.New("must not be called")
		}}
		service := &Service{logger: testenv.NewLogger(t), stripeClient: client, stripeHandler: serviceStripeWebhookHandler}
		require.Equal(t, http.StatusRequestEntityTooLarge, serveStripeWebhook(service, strings.Repeat("x", maxStripeWebhookBodyBytes+1)).Code)
		require.Zero(t, client.verifyCalls.Load())
	})
}

func TestStripeWebhookDatabaseUnavailable(t *testing.T) {
	t.Parallel()

	service, db := newStripeWebhookService(t, "customer_placeholder", serviceStripeWebhookHandler)
	db.Close()

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "{}").Code)
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

			service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error {
				t.Fatal("handler must not run")
				return nil
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error {
		calls.Add(1)
		return nil
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error {
		calls.Add(1)
		enterOnce.Do(func() { close(entered) })
		<-release
		return nil
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error {
		calls.Add(1)
		<-release
		return nil
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
	service, db := newStripeWebhookService(t, "customer_placeholder", func(context.Context, *slog.Logger, *repo.Queries, string, *stripeclient.WebhookEvent) error {
		if calls.Add(1) == 1 {
			return errors.New("transient handler failure")
		}
		return nil
	})

	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "first").Code)
	require.Zero(t, stripeWebhookReceiptCount(t, db))
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "retry").Code)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 1, stripeWebhookReceiptCount(t, db))
}
