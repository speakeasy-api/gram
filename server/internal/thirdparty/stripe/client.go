// Package stripe provides the narrow Stripe boundary used by PAYG billing.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	stripesdk "github.com/stripe/stripe-go/v85"
	stripewebhook "github.com/stripe/stripe-go/v85/webhook"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	organizationIDMetadataKey   = "organization_id"
	organizationSlugMetadataKey = "organization_slug"
	meterCustomerPayloadKey     = "stripe_customer_id"
	meterValuePayloadKey        = "value"
)

// ErrWebhookNotConfigured indicates that webhook verification cannot run because
// the Stripe webhook secret is unavailable.
var ErrWebhookNotConfigured = errors.New("stripe webhook is not configured")

var errMissingIdempotencyKey = errors.New("idempotency key is required")

// Catalog contains the Stripe object identifiers used by PAYG billing.
type Catalog struct {
	PriceIDTUM     string
	MeterEventName string
}

// Validate checks that every required Stripe catalog value is configured.
func (c Catalog) Validate() error {
	if !IsConfigured(c.PriceIDTUM) {
		return errors.New("missing TUM price id in catalog")
	}
	if !IsConfigured(c.MeterEventName) {
		return errors.New("missing meter event name in catalog")
	}
	return nil
}

// IsConfigured reports whether a Stripe config value is present.
func IsConfigured(value string) bool {
	return value != "" && value != "unset"
}

// Client is the Stripe surface used by PAYG billing.
type Client interface {
	CreateCustomer(context.Context, CreateCustomerInput) (*Customer, error)
	CreateCheckoutSession(context.Context, CreateCheckoutSessionInput) (*CheckoutSession, error)
	CreateMeterEvent(context.Context, CreateMeterEventInput) error
	VerifyWebhook(payload []byte, signature string) (*WebhookEvent, error)
	Catalog() Catalog
}

// CreateCustomerInput identifies the organization represented by a new Stripe customer.
type CreateCustomerInput struct {
	OrganizationID   string
	OrganizationSlug string
	IdempotencyKey   string
}

// Customer is the Stripe customer data needed by billing callers.
type Customer struct {
	ID string
}

// CreateCheckoutSessionInput describes the hosted Checkout session for a PAYG subscription.
type CreateCheckoutSessionInput struct {
	// CustomerID identifies the Stripe customer that owns the subscription.
	CustomerID string

	// OrganizationID is Gram's stable organization identifier.
	OrganizationID string

	// OrganizationSlug is the organization slug included in Stripe metadata.
	OrganizationSlug string

	// SuccessURL is the browser destination after successful Checkout.
	SuccessURL string

	// CancelURL is the browser destination when Checkout is canceled.
	CancelURL string

	// TrialEnd preserves an active in-product trial on the Stripe subscription.
	TrialEnd *time.Time

	// IdempotencyKey identifies one Checkout creation request.
	IdempotencyKey string
}

// CheckoutSession is the Stripe Checkout data needed by billing callers.
type CheckoutSession struct {
	// URL is Stripe's hosted Checkout page.
	URL string
}

// CreateMeterEventInput reports a TUM delta for one Stripe customer.
type CreateMeterEventInput struct {
	CustomerID     string
	Value          int64
	Timestamp      time.Time
	IdempotencyKey string
}

// WebhookEvent is the verified Stripe event envelope consumed by webhook handlers.
type WebhookEvent struct {
	// ID is Stripe's globally unique event identifier.
	ID string

	// Type identifies the event kind.
	Type string

	// Created is the time Stripe created the event.
	Created time.Time

	// ObjectID is the identifier of the event's data object, when present.
	ObjectID string

	// CustomerID is the normalized customer identifier carried by the data object, when present.
	CustomerID string
}

type stripeAPI interface {
	createCustomer(context.Context, *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error)
	createCheckoutSession(context.Context, *stripesdk.CheckoutSessionCreateParams) (*stripesdk.CheckoutSession, error)
	createMeterEvent(context.Context, *stripesdk.BillingMeterEventCreateParams) (*stripesdk.BillingMeterEvent, error)
}

type sdkAPI struct {
	client *stripesdk.Client
}

func (s *sdkAPI) createCustomer(ctx context.Context, params *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error) {
	customer, err := s.client.V1Customers.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create customer: %w", err)
	}
	return customer, nil
}

func (s *sdkAPI) createCheckoutSession(ctx context.Context, params *stripesdk.CheckoutSessionCreateParams) (*stripesdk.CheckoutSession, error) {
	session, err := s.client.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create Checkout session: %w", err)
	}
	return session, nil
}

func (s *sdkAPI) createMeterEvent(ctx context.Context, params *stripesdk.BillingMeterEventCreateParams) (*stripesdk.BillingMeterEvent, error) {
	event, err := s.client.V1BillingMeterEvents.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create meter event: %w", err)
	}
	return event, nil
}

type client struct {
	api           stripeAPI
	catalog       Catalog
	webhookSecret string
}

// NewClient creates a real Stripe client using the repository HTTP policy.
func NewClient(guardianPolicy *guardian.Policy, apiKey, webhookSecret string, catalog Catalog) Client {
	retries := guardian.DefaultRetryConfig()
	retries.WaitMax = 10 * time.Second
	retries.MaxAttempts = 1
	httpClient := guardianPolicy.PooledClient(guardian.WithRetryConfig(retries))
	httpClient.Timeout = 30 * time.Second

	backendConfig := new(stripesdk.BackendConfig)
	backendConfig.HTTPClient = httpClient
	// Guardian owns retries so the two retry layers cannot amplify each other.
	backendConfig.MaxNetworkRetries = stripesdk.Int64(0)
	backends := stripesdk.NewBackendsWithConfig(backendConfig)
	sdkClient := stripesdk.NewClient(apiKey, stripesdk.WithBackends(backends))

	return &client{
		api:           &sdkAPI{client: sdkClient},
		catalog:       catalog,
		webhookSecret: webhookSecret,
	}
}

func (c *client) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	if input.IdempotencyKey == "" {
		return nil, errMissingIdempotencyKey
	}

	params := new(stripesdk.CustomerCreateParams)
	params.Metadata = map[string]string{
		organizationIDMetadataKey:   input.OrganizationID,
		organizationSlugMetadataKey: input.OrganizationSlug,
	}
	params.SetIdempotencyKey(input.IdempotencyKey)

	customer, err := c.api.createCustomer(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe customer: %w", err)
	}
	return &Customer{ID: customer.ID}, nil
}

func (c *client) CreateCheckoutSession(ctx context.Context, input CreateCheckoutSessionInput) (*CheckoutSession, error) {
	if input.IdempotencyKey == "" {
		return nil, errMissingIdempotencyKey
	}

	params := new(stripesdk.CheckoutSessionCreateParams)
	params.CancelURL = stripesdk.String(input.CancelURL)
	params.ClientReferenceID = stripesdk.String(input.OrganizationID)
	params.Customer = stripesdk.String(input.CustomerID)
	params.LineItems = []*stripesdk.CheckoutSessionCreateLineItemParams{
		{
			Price:    stripesdk.String(c.catalog.PriceIDTUM),
			Quantity: nil,
		},
	}
	params.Metadata = map[string]string{
		organizationIDMetadataKey:   input.OrganizationID,
		organizationSlugMetadataKey: input.OrganizationSlug,
	}
	params.Mode = stripesdk.String(stripesdk.CheckoutSessionModeSubscription)
	params.PaymentMethodCollection = stripesdk.String(stripesdk.CheckoutSessionPaymentMethodCollectionAlways)
	params.SubscriptionData = new(stripesdk.CheckoutSessionCreateSubscriptionDataParams)
	params.SubscriptionData.Metadata = map[string]string{
		organizationIDMetadataKey:   input.OrganizationID,
		organizationSlugMetadataKey: input.OrganizationSlug,
	}
	params.SuccessURL = stripesdk.String(input.SuccessURL)
	if input.TrialEnd != nil {
		params.SubscriptionData.TrialEnd = new(input.TrialEnd.Unix())
	}
	params.SetIdempotencyKey(input.IdempotencyKey)

	session, err := c.api.createCheckoutSession(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe Checkout session: %w", err)
	}
	return &CheckoutSession{URL: session.URL}, nil
}

func (c *client) CreateMeterEvent(ctx context.Context, input CreateMeterEventInput) error {
	if input.IdempotencyKey == "" {
		return errMissingIdempotencyKey
	}

	params := new(stripesdk.BillingMeterEventCreateParams)
	params.EventName = stripesdk.String(c.catalog.MeterEventName)
	params.Identifier = stripesdk.String(input.IdempotencyKey)
	params.Payload = map[string]string{
		meterCustomerPayloadKey: input.CustomerID,
		meterValuePayloadKey:    strconv.FormatInt(input.Value, 10),
	}
	if !input.Timestamp.IsZero() {
		params.Timestamp = new(input.Timestamp.Unix())
	}
	params.SetIdempotencyKey(input.IdempotencyKey)

	if _, err := c.api.createMeterEvent(ctx, params); err != nil {
		return fmt.Errorf("create Stripe meter event: %w", err)
	}
	return nil
}

func (c *client) VerifyWebhook(payload []byte, signature string) (*WebhookEvent, error) {
	if !IsConfigured(c.webhookSecret) {
		return nil, fmt.Errorf("verify Stripe webhook: %w", ErrWebhookNotConfigured)
	}

	event, err := stripewebhook.ConstructEvent(payload, signature, c.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("verify Stripe webhook: %w", err)
	}
	if event.APIVersion != stripesdk.APIVersion {
		return nil, fmt.Errorf("verify Stripe webhook: expected API version %s, got %s", stripesdk.APIVersion, event.APIVersion)
	}

	var objectID string
	var customerID string
	if event.Data != nil {
		objectID, customerID, err = webhookObjectIdentifiers(event.Data.Raw)
		if err != nil {
			return nil, fmt.Errorf("parse Stripe webhook data object: %w", err)
		}
	}
	return &WebhookEvent{
		ID:         event.ID,
		Type:       string(event.Type),
		Created:    time.Unix(event.Created, 0),
		ObjectID:   objectID,
		CustomerID: customerID,
	}, nil
}

func webhookObjectIdentifiers(raw json.RawMessage) (string, string, error) {
	var object *struct {
		ID       string          `json:"id"`
		Customer json.RawMessage `json:"customer"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", "", fmt.Errorf("decode data object: %w", err)
	}
	if object == nil {
		return "", "", errors.New("data object is null")
	}

	var customerID string
	if err := json.Unmarshal(object.Customer, &customerID); err == nil {
		return object.ID, customerID, nil
	}

	var customer struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(object.Customer, &customer); err == nil {
		return object.ID, customer.ID, nil
	}

	return object.ID, "", nil
}

func (c *client) Catalog() Catalog {
	return c.catalog
}

type stubClient struct {
	logger *slog.Logger
}

// NewStubClient returns a local no-op Stripe client.
func NewStubClient(logger *slog.Logger) Client {
	return &stubClient{logger: logger}
}

func (s *stubClient) CreateCustomer(ctx context.Context, _ CreateCustomerInput) (*Customer, error) {
	s.logger.DebugContext(ctx, "stub Stripe customer creation skipped")
	return &Customer{ID: "cus_local_stub"}, nil
}

func (s *stubClient) CreateCheckoutSession(ctx context.Context, _ CreateCheckoutSessionInput) (*CheckoutSession, error) {
	s.logger.DebugContext(ctx, "stub Stripe Checkout session creation skipped")
	return &CheckoutSession{URL: "http://localhost:3000/billing"}, nil
}

func (s *stubClient) CreateMeterEvent(ctx context.Context, _ CreateMeterEventInput) error {
	s.logger.DebugContext(ctx, "stub Stripe meter event skipped")
	return nil
}

func (s *stubClient) VerifyWebhook(_ []byte, _ string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("verify Stripe webhook: %w", ErrWebhookNotConfigured)
}

func (s *stubClient) Catalog() Catalog {
	return Catalog{
		PriceIDTUM:     "",
		MeterEventName: "",
	}
}
