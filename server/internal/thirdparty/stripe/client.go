// Package stripe provides the narrow Stripe boundary used by PAYG billing.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	allocationMetadataKey       = "gram_billing_allocation"
)

// ErrWebhookNotConfigured indicates that webhook verification cannot run because
// the Stripe webhook secret is unavailable.
var ErrWebhookNotConfigured = errors.New("stripe webhook is not configured")

var errMissingIdempotencyKey = errors.New("idempotency key is required")

var errMissingMeterEventName = errors.New("meter event name is required")

var errMissingBillingCycleAnchor = errors.New("billing cycle anchor is required")

var errMissingCheckoutExpiration = errors.New("checkout expiration is required")

// Catalog contains the Stripe object identifiers used by PAYG billing.
type Catalog struct {
	// PriceIDTUM identifies the Stripe price included in PAYG subscriptions.
	PriceIDTUM string

	// MeterIDTUM identifies the Stripe meter queried during reconciliation.
	MeterIDTUM string

	// MeterEventName identifies the event stream used when reporting TUM deltas.
	MeterEventName string

	// PortalConfigurationID identifies the controlled Stripe customer portal configuration.
	PortalConfigurationID string
}

// Validate checks that every required Stripe catalog value is configured.
func (c Catalog) Validate() error {
	if !IsConfigured(c.PriceIDTUM) {
		return errors.New("missing TUM price id in catalog")
	}
	if !IsConfigured(c.MeterIDTUM) {
		return errors.New("missing TUM meter id in catalog")
	}
	if !IsConfigured(c.MeterEventName) {
		return errors.New("missing meter event name in catalog")
	}
	if !IsConfigured(c.PortalConfigurationID) {
		return errors.New("missing portal configuration id in catalog")
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
	GetCheckoutSession(context.Context, string) (*CheckoutSessionState, error)
	GetSubscription(context.Context, string) (*SubscriptionState, error)
	SetSubscriptionCancelAtPeriodEnd(context.Context, SetSubscriptionCancelAtPeriodEndInput) (*SubscriptionState, error)
	CreatePortalSession(context.Context, CreatePortalSessionInput) (*PortalSession, error)
	CreateMeterEvent(context.Context, CreateMeterEventInput) error
	GetMeterEventSummary(context.Context, GetMeterEventSummaryInput) (float64, error)
	GetInvoice(context.Context, string) (*InvoiceState, error)
	CreateInvoiceItem(context.Context, CreateInvoiceItemInput) (*InvoiceItem, error)
	CreateCreditNote(context.Context, CreateCreditNoteInput) (*CreditNote, error)
	FindInvoiceItem(context.Context, FindInvoiceAllocationInput) (*InvoiceItem, error)
	FindCreditNote(context.Context, FindInvoiceAllocationInput) (*CreditNote, error)
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

	// TrialEnd is the UTC-aligned financial trial end. Gram retains the exact
	// product trial end separately and the interval between them is a free stub.
	TrialEnd *time.Time

	// BillingCycleAnchor starts the first full paid period at an exact UTC boundary.
	BillingCycleAnchor time.Time

	// ExpiresAt prevents the session from completing after its intended paid anchor.
	ExpiresAt time.Time

	// IdempotencyKey identifies one Checkout creation request.
	IdempotencyKey string
}

// CheckoutSession is the Stripe Checkout data needed by billing callers.
type CheckoutSession struct {
	// ID is Stripe's stable Checkout Session identifier.
	ID string

	// URL is Stripe's hosted Checkout page.
	URL string
}

// CheckoutSessionState is the current Stripe state used to activate PAYG.
type CheckoutSessionState struct {
	ID                     string
	Status                 string
	CustomerID             string
	SubscriptionID         string
	SubscriptionCustomerID string
	SubscriptionStatus     string
	BillingCycleAnchor     time.Time
}

// SubscriptionState is the live Stripe subscription data needed by billing callers.
type SubscriptionState struct {
	// ID is Stripe's stable subscription identifier.
	ID string

	// CustomerID identifies the Stripe customer that owns the subscription.
	CustomerID string

	// Status is Stripe's current subscription lifecycle status.
	Status string

	// CurrentPeriodStart is the inclusive service-period boundary for the TUM item.
	CurrentPeriodStart time.Time

	// CurrentPeriodEnd is the exclusive service-period boundary for the TUM item.
	CurrentPeriodEnd time.Time

	// TrialStart is the start of the subscription trial, when present.
	TrialStart time.Time

	// TrialEnd is the end of the subscription trial, when present.
	TrialEnd time.Time

	// CancelAtPeriodEnd reports whether cancellation is scheduled at the period boundary.
	CancelAtPeriodEnd bool

	// CancelAt is Stripe's scheduled cancellation timestamp, when present.
	CancelAt time.Time

	// CanceledAt is when cancellation was requested or completed, when present.
	CanceledAt time.Time

	// LatestInvoiceID identifies the most recent invoice, when one exists.
	LatestInvoiceID string

	// LatestInvoiceStatus is the live status of the most recent invoice, when one exists.
	LatestInvoiceStatus string

	// LatestInvoiceAmountRemaining is the unpaid amount in Stripe minor units.
	LatestInvoiceAmountRemaining int64

	// PaymentFailed reports whether a past-due subscription has an unpaid open invoice.
	PaymentFailed bool
}

// SetSubscriptionCancelAtPeriodEndInput changes scheduled cancellation without
// immediately terminating service.
type SetSubscriptionCancelAtPeriodEndInput struct {
	// SubscriptionID identifies the subscription to update.
	SubscriptionID string

	// CancelAtPeriodEnd schedules cancellation when true and resumes renewal when false.
	CancelAtPeriodEnd bool
}

// CreatePortalSessionInput describes one short-lived Stripe customer portal session.
type CreatePortalSessionInput struct {
	// CustomerID identifies the Stripe customer entering the portal.
	CustomerID string

	// ReturnURL is the Gram billing page Stripe returns the customer to.
	ReturnURL string
}

// PortalSession is the Stripe customer portal data needed by callers.
type PortalSession struct {
	// ID is Stripe's stable portal session identifier.
	ID string

	// CustomerID identifies the Stripe customer the portal session belongs to.
	CustomerID string

	// URL is the short-lived hosted Stripe portal URL.
	URL string
}

// CreateMeterEventInput reports a TUM delta for one Stripe customer.
type CreateMeterEventInput struct {
	// CustomerID identifies the Stripe customer receiving the usage.
	CustomerID string

	// EventName is the immutable event stream captured with the delivery intent.
	EventName string

	// Value is the signed TUM delta.
	Value int64

	// Timestamp places the event inside its billing cycle.
	Timestamp time.Time

	// IdempotencyKey is also used as Stripe's meter-event identifier.
	IdempotencyKey string
}

// GetMeterEventSummaryInput identifies a customer's half-open metering interval.
type GetMeterEventSummaryInput struct {
	// CustomerID identifies the Stripe customer whose meter is queried.
	CustomerID string

	// Start is the inclusive, minute-aligned interval boundary.
	Start time.Time

	// End is the exclusive, minute-aligned interval boundary.
	End time.Time
}

// InvoiceState is the current Stripe invoice state required by pass-through
// billing. Amounts are Stripe minor units.
type InvoiceState struct {
	ID                 string
	CustomerID         string
	SubscriptionID     string
	Currency           string
	BillingReason      string
	Status             string
	ServicePeriodStart time.Time
	ServicePeriodEnd   time.Time
	FinalizedAt        time.Time
	AmountRemaining    int64
}

// CreateInvoiceItemInput adds one allocation to a draft Stripe invoice.
type CreateInvoiceItemInput struct {
	CustomerID     string
	SubscriptionID string
	InvoiceID      string
	Description    string
	AmountCents    int64
	PeriodStart    time.Time
	PeriodEnd      time.Time
	AllocationKey  string
	IdempotencyKey string
}

// InvoiceItem identifies a confirmed Stripe invoice item.
type InvoiceItem struct {
	ID          string
	InvoiceID   string
	Currency    string
	AmountCents int64
}

// CreateCreditNoteInput credits one allocation against a finalized invoice.
type CreateCreditNoteInput struct {
	InvoiceID         string
	Description       string
	AmountCents       int64
	CreditAmountCents int64
	AllocationKey     string
	IdempotencyKey    string
}

// CreditNote identifies a confirmed Stripe credit note.
type CreditNote struct {
	ID          string
	InvoiceID   string
	Currency    string
	AmountCents int64
}

// FindInvoiceAllocationInput finds a prior Stripe write by durable allocation
// metadata after its idempotency window has elapsed.
type FindInvoiceAllocationInput struct {
	InvoiceID     string
	AllocationKey string
	AmountCents   int64
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

	// SubscriptionID is the normalized subscription identifier carried by the data object, when present.
	SubscriptionID string
}

type stripeAPI interface {
	createCustomer(context.Context, *stripesdk.CustomerCreateParams) (*stripesdk.Customer, error)
	createCheckoutSession(context.Context, *stripesdk.CheckoutSessionCreateParams) (*stripesdk.CheckoutSession, error)
	retrieveCheckoutSession(context.Context, string, *stripesdk.CheckoutSessionRetrieveParams) (*stripesdk.CheckoutSession, error)
	retrieveSubscription(context.Context, string, *stripesdk.SubscriptionRetrieveParams) (*stripesdk.Subscription, error)
	updateSubscription(context.Context, string, *stripesdk.SubscriptionUpdateParams) (*stripesdk.Subscription, error)
	createPortalSession(context.Context, *stripesdk.BillingPortalSessionCreateParams) (*stripesdk.BillingPortalSession, error)
	createMeterEvent(context.Context, *stripesdk.BillingMeterEventCreateParams) (*stripesdk.BillingMeterEvent, error)
	listMeterEventSummaries(context.Context, *stripesdk.BillingMeterEventSummaryListParams) stripesdk.Seq2[*stripesdk.BillingMeterEventSummary, error]
	retrieveInvoice(context.Context, string, *stripesdk.InvoiceRetrieveParams) (*stripesdk.Invoice, error)
	createInvoiceItem(context.Context, *stripesdk.InvoiceItemCreateParams) (*stripesdk.InvoiceItem, error)
	listInvoiceItems(context.Context, *stripesdk.InvoiceItemListParams) stripesdk.Seq2[*stripesdk.InvoiceItem, error]
	createCreditNote(context.Context, *stripesdk.CreditNoteCreateParams) (*stripesdk.CreditNote, error)
	listCreditNotes(context.Context, *stripesdk.CreditNoteListParams) stripesdk.Seq2[*stripesdk.CreditNote, error]
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

func (s *sdkAPI) retrieveCheckoutSession(ctx context.Context, id string, params *stripesdk.CheckoutSessionRetrieveParams) (*stripesdk.CheckoutSession, error) {
	session, err := s.client.V1CheckoutSessions.Retrieve(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK retrieve Checkout session: %w", err)
	}
	return session, nil
}

func (s *sdkAPI) retrieveSubscription(ctx context.Context, id string, params *stripesdk.SubscriptionRetrieveParams) (*stripesdk.Subscription, error) {
	subscription, err := s.client.V1Subscriptions.Retrieve(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK retrieve subscription: %w", err)
	}
	return subscription, nil
}

func (s *sdkAPI) updateSubscription(ctx context.Context, id string, params *stripesdk.SubscriptionUpdateParams) (*stripesdk.Subscription, error) {
	subscription, err := s.client.V1Subscriptions.Update(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK update subscription: %w", err)
	}
	return subscription, nil
}

func (s *sdkAPI) createPortalSession(ctx context.Context, params *stripesdk.BillingPortalSessionCreateParams) (*stripesdk.BillingPortalSession, error) {
	session, err := s.client.V1BillingPortalSessions.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create billing portal session: %w", err)
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

func (s *sdkAPI) listMeterEventSummaries(ctx context.Context, params *stripesdk.BillingMeterEventSummaryListParams) stripesdk.Seq2[*stripesdk.BillingMeterEventSummary, error] {
	return s.client.V1BillingMeterEventSummaries.List(ctx, params).All(ctx)
}

func (s *sdkAPI) retrieveInvoice(ctx context.Context, id string, params *stripesdk.InvoiceRetrieveParams) (*stripesdk.Invoice, error) {
	invoice, err := s.client.V1Invoices.Retrieve(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK retrieve invoice: %w", err)
	}
	return invoice, nil
}

func (s *sdkAPI) createInvoiceItem(ctx context.Context, params *stripesdk.InvoiceItemCreateParams) (*stripesdk.InvoiceItem, error) {
	item, err := s.client.V1InvoiceItems.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create invoice item: %w", err)
	}
	return item, nil
}

func (s *sdkAPI) listInvoiceItems(ctx context.Context, params *stripesdk.InvoiceItemListParams) stripesdk.Seq2[*stripesdk.InvoiceItem, error] {
	return s.client.V1InvoiceItems.List(ctx, params).All(ctx)
}

func (s *sdkAPI) createCreditNote(ctx context.Context, params *stripesdk.CreditNoteCreateParams) (*stripesdk.CreditNote, error) {
	note, err := s.client.V1CreditNotes.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe SDK create credit note: %w", err)
	}
	return note, nil
}

func (s *sdkAPI) listCreditNotes(ctx context.Context, params *stripesdk.CreditNoteListParams) stripesdk.Seq2[*stripesdk.CreditNote, error] {
	return s.client.V1CreditNotes.List(ctx, params).All(ctx)
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
	if input.BillingCycleAnchor.IsZero() {
		return nil, errMissingBillingCycleAnchor
	}
	if input.ExpiresAt.IsZero() {
		return nil, errMissingCheckoutExpiration
	}

	params := new(stripesdk.CheckoutSessionCreateParams)
	params.CancelURL = stripesdk.String(input.CancelURL)
	params.ClientReferenceID = stripesdk.String(input.OrganizationID)
	params.Customer = stripesdk.String(input.CustomerID)
	params.ExpiresAt = new(input.ExpiresAt.Unix())
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
	} else {
		params.SubscriptionData.BillingCycleAnchor = new(input.BillingCycleAnchor.Unix())
		params.SubscriptionData.ProrationBehavior = stripesdk.String("none")
	}
	params.SetIdempotencyKey(input.IdempotencyKey)

	session, err := c.api.createCheckoutSession(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe Checkout session: %w", err)
	}
	return &CheckoutSession{ID: session.ID, URL: session.URL}, nil
}

func (c *client) GetCheckoutSession(ctx context.Context, id string) (*CheckoutSessionState, error) {
	params := new(stripesdk.CheckoutSessionRetrieveParams)
	params.AddExpand("subscription")

	session, err := c.api.retrieveCheckoutSession(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe Checkout session: %w", err)
	}
	if session == nil {
		return nil, errors.New("retrieve Stripe Checkout session: empty response")
	}

	state := &CheckoutSessionState{
		ID:                     session.ID,
		Status:                 string(session.Status),
		CustomerID:             "",
		SubscriptionID:         "",
		SubscriptionCustomerID: "",
		SubscriptionStatus:     "",
		BillingCycleAnchor:     time.Time{},
	}
	if session.Customer != nil {
		state.CustomerID = session.Customer.ID
	}
	if session.Subscription != nil {
		state.SubscriptionID = session.Subscription.ID
		state.SubscriptionStatus = string(session.Subscription.Status)
		if session.Subscription.Customer != nil {
			state.SubscriptionCustomerID = session.Subscription.Customer.ID
		}
		if session.Subscription.BillingCycleAnchor > 0 {
			state.BillingCycleAnchor = time.Unix(session.Subscription.BillingCycleAnchor, 0).UTC()
		}
	}

	return state, nil
}

func (c *client) GetSubscription(ctx context.Context, id string) (*SubscriptionState, error) {
	if id == "" {
		return nil, errors.New("subscription id is required")
	}

	params := new(stripesdk.SubscriptionRetrieveParams)
	params.AddExpand("latest_invoice")
	subscription, err := c.api.retrieveSubscription(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe subscription: %w", err)
	}
	return c.subscriptionState(subscription)
}

func (c *client) SetSubscriptionCancelAtPeriodEnd(ctx context.Context, input SetSubscriptionCancelAtPeriodEndInput) (*SubscriptionState, error) {
	if input.SubscriptionID == "" {
		return nil, errors.New("subscription id is required")
	}

	params := new(stripesdk.SubscriptionUpdateParams)
	params.CancelAtPeriodEnd = new(input.CancelAtPeriodEnd)
	params.AddExpand("latest_invoice")
	subscription, err := c.api.updateSubscription(ctx, input.SubscriptionID, params)
	if err != nil {
		return nil, fmt.Errorf("update Stripe subscription cancellation: %w", err)
	}
	return c.subscriptionState(subscription)
}

func (c *client) CreatePortalSession(ctx context.Context, input CreatePortalSessionInput) (*PortalSession, error) {
	if input.CustomerID == "" {
		return nil, errors.New("customer id is required")
	}
	if input.ReturnURL == "" {
		return nil, errors.New("portal return URL is required")
	}
	if !IsConfigured(c.catalog.PortalConfigurationID) {
		return nil, errors.New("portal configuration id is required")
	}

	params := new(stripesdk.BillingPortalSessionCreateParams)
	params.Configuration = stripesdk.String(c.catalog.PortalConfigurationID)
	params.Customer = stripesdk.String(input.CustomerID)
	params.ReturnURL = stripesdk.String(input.ReturnURL)
	session, err := c.api.createPortalSession(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe billing portal session: %w", err)
	}
	if session == nil || session.ID == "" || session.Customer == "" || session.URL == "" {
		return nil, errors.New("create Stripe billing portal session: incomplete response")
	}
	return &PortalSession{ID: session.ID, CustomerID: session.Customer, URL: session.URL}, nil
}

func (c *client) subscriptionState(subscription *stripesdk.Subscription) (*SubscriptionState, error) {
	if subscription == nil || subscription.ID == "" {
		return nil, errors.New("stripe subscription response is empty")
	}

	customerID := ""
	if subscription.Customer != nil {
		customerID = subscription.Customer.ID
	}

	var tumItem *stripesdk.SubscriptionItem
	if subscription.Items != nil {
		for _, item := range subscription.Items.Data {
			if item != nil && item.Price != nil && item.Price.ID == c.catalog.PriceIDTUM {
				tumItem = item
				break
			}
		}
	}
	if tumItem == nil || tumItem.CurrentPeriodStart <= 0 || tumItem.CurrentPeriodEnd <= tumItem.CurrentPeriodStart {
		return nil, errors.New("stripe subscription is missing the configured TUM service period")
	}

	latestInvoiceID := ""
	latestInvoiceStatus := ""
	var latestInvoiceAmountRemaining int64
	if subscription.LatestInvoice != nil {
		latestInvoiceID = subscription.LatestInvoice.ID
		latestInvoiceStatus = string(subscription.LatestInvoice.Status)
		latestInvoiceAmountRemaining = subscription.LatestInvoice.AmountRemaining
	}

	status := string(subscription.Status)
	paymentFailed := status == "past_due" && latestInvoiceStatus == "open" && latestInvoiceAmountRemaining > 0

	return &SubscriptionState{
		ID:                           subscription.ID,
		CustomerID:                   customerID,
		Status:                       status,
		CurrentPeriodStart:           time.Unix(tumItem.CurrentPeriodStart, 0).UTC(),
		CurrentPeriodEnd:             time.Unix(tumItem.CurrentPeriodEnd, 0).UTC(),
		TrialStart:                   unixTime(subscription.TrialStart),
		TrialEnd:                     unixTime(subscription.TrialEnd),
		CancelAtPeriodEnd:            subscription.CancelAtPeriodEnd,
		CancelAt:                     unixTime(subscription.CancelAt),
		CanceledAt:                   unixTime(subscription.CanceledAt),
		LatestInvoiceID:              latestInvoiceID,
		LatestInvoiceStatus:          latestInvoiceStatus,
		LatestInvoiceAmountRemaining: latestInvoiceAmountRemaining,
		PaymentFailed:                paymentFailed,
	}, nil
}

func unixTime(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func (c *client) CreateMeterEvent(ctx context.Context, input CreateMeterEventInput) error {
	if input.IdempotencyKey == "" {
		return errMissingIdempotencyKey
	}
	if !IsConfigured(input.EventName) {
		return errMissingMeterEventName
	}

	params := new(stripesdk.BillingMeterEventCreateParams)
	params.EventName = stripesdk.String(input.EventName)
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

// GetMeterEventSummary returns Stripe's eventually consistent observed total.
// Callers decide whether the value is sufficiently settled for reconciliation.
func (c *client) GetMeterEventSummary(ctx context.Context, input GetMeterEventSummaryInput) (float64, error) {
	if !input.Start.Equal(input.Start.Truncate(time.Minute)) || !input.End.Equal(input.End.Truncate(time.Minute)) {
		return 0, errors.New("meter event summary bounds must be minute-aligned")
	}
	if !input.End.After(input.Start) {
		return 0, errors.New("meter event summary end must be after start")
	}

	params := new(stripesdk.BillingMeterEventSummaryListParams)
	params.ID = stripesdk.String(c.catalog.MeterIDTUM)
	params.Customer = stripesdk.String(input.CustomerID)
	params.StartTime = new(input.Start.Unix())
	params.EndTime = new(input.End.Unix())
	params.Limit = stripesdk.Int64(100)

	var total float64
	for summary, err := range c.api.listMeterEventSummaries(ctx, params) {
		if err != nil {
			return 0, fmt.Errorf("list Stripe meter event summaries: %w", err)
		}
		total += summary.AggregatedValue
	}
	return total, nil
}

func (c *client) GetInvoice(ctx context.Context, id string) (*InvoiceState, error) {
	if id == "" {
		return nil, errors.New("invoice id is required")
	}

	params := new(stripesdk.InvoiceRetrieveParams)
	params.AddExpand("parent.subscription_details.subscription")
	invoice, err := c.api.retrieveInvoice(ctx, id, params)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe invoice: %w", err)
	}
	if invoice == nil {
		return nil, errors.New("retrieve Stripe invoice: empty response")
	}

	state := &InvoiceState{
		ID:                 invoice.ID,
		CustomerID:         "",
		SubscriptionID:     "",
		Currency:           string(invoice.Currency),
		BillingReason:      string(invoice.BillingReason),
		Status:             string(invoice.Status),
		ServicePeriodStart: time.Unix(invoice.PeriodStart, 0).UTC(),
		ServicePeriodEnd:   time.Unix(invoice.PeriodEnd, 0).UTC(),
		FinalizedAt:        time.Time{},
		AmountRemaining:    invoice.AmountRemaining,
	}
	if invoice.Customer != nil {
		state.CustomerID = invoice.Customer.ID
	}
	if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil && invoice.Parent.SubscriptionDetails.Subscription != nil {
		state.SubscriptionID = invoice.Parent.SubscriptionDetails.Subscription.ID
	}
	if invoice.StatusTransitions != nil && invoice.StatusTransitions.FinalizedAt > 0 {
		state.FinalizedAt = time.Unix(invoice.StatusTransitions.FinalizedAt, 0).UTC()
	}
	return state, nil
}

func (c *client) CreateInvoiceItem(ctx context.Context, input CreateInvoiceItemInput) (*InvoiceItem, error) {
	if input.IdempotencyKey == "" || input.AllocationKey == "" {
		return nil, errMissingIdempotencyKey
	}
	if input.CustomerID == "" || input.SubscriptionID == "" || input.InvoiceID == "" {
		return nil, errors.New("customer, subscription, and invoice ids are required")
	}
	if input.AmountCents <= 0 {
		return nil, errors.New("invoice item amount must be positive")
	}
	if input.PeriodStart.IsZero() || !input.PeriodEnd.After(input.PeriodStart) {
		return nil, errors.New("invoice item period is invalid")
	}

	params := new(stripesdk.InvoiceItemCreateParams)
	params.Amount = new(input.AmountCents)
	params.Currency = stripesdk.String(string(stripesdk.CurrencyUSD))
	params.Customer = stripesdk.String(input.CustomerID)
	params.Subscription = stripesdk.String(input.SubscriptionID)
	params.Invoice = stripesdk.String(input.InvoiceID)
	params.Description = stripesdk.String(input.Description)
	params.Discountable = new(false)
	params.Period = &stripesdk.InvoiceItemCreatePeriodParams{
		Start: new(input.PeriodStart.Unix()),
		End:   new(input.PeriodEnd.Add(-time.Second).Unix()),
	}
	params.Metadata = map[string]string{allocationMetadataKey: input.AllocationKey}
	params.SetIdempotencyKey(input.IdempotencyKey)

	item, err := c.api.createInvoiceItem(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe invoice item: %w", err)
	}
	if item == nil || item.ID == "" {
		return nil, errors.New("create Stripe invoice item: empty response")
	}
	return &InvoiceItem{
		ID:          item.ID,
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (c *client) CreateCreditNote(ctx context.Context, input CreateCreditNoteInput) (*CreditNote, error) {
	if input.IdempotencyKey == "" || input.AllocationKey == "" {
		return nil, errMissingIdempotencyKey
	}
	if input.InvoiceID == "" {
		return nil, errors.New("invoice id is required")
	}
	if input.AmountCents <= 0 || input.CreditAmountCents < 0 || input.CreditAmountCents > input.AmountCents {
		return nil, errors.New("credit note amount is invalid")
	}

	params := new(stripesdk.CreditNoteCreateParams)
	params.Invoice = stripesdk.String(input.InvoiceID)
	params.Amount = new(input.AmountCents)
	if input.CreditAmountCents > 0 {
		params.CreditAmount = new(input.CreditAmountCents)
	}
	params.EmailType = stripesdk.String("none")
	params.Memo = stripesdk.String(input.Description)
	params.Metadata = map[string]string{allocationMetadataKey: input.AllocationKey}
	params.Reason = stripesdk.String(string(stripesdk.CreditNoteReasonOrderChange))
	params.SetIdempotencyKey(input.IdempotencyKey)

	note, err := c.api.createCreditNote(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe credit note: %w", err)
	}
	if note == nil || note.ID == "" {
		return nil, errors.New("create Stripe credit note: empty response")
	}
	return &CreditNote{
		ID:          note.ID,
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (c *client) FindInvoiceItem(ctx context.Context, input FindInvoiceAllocationInput) (*InvoiceItem, error) {
	if input.InvoiceID == "" || input.AllocationKey == "" || input.AmountCents <= 0 {
		return nil, errors.New("invoice id, allocation key, and positive amount are required")
	}
	params := new(stripesdk.InvoiceItemListParams)
	params.Invoice = stripesdk.String(input.InvoiceID)
	params.Limit = stripesdk.Int64(100)
	for item, err := range c.api.listInvoiceItems(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list Stripe invoice items: %w", err)
		}
		if item == nil || item.Metadata[allocationMetadataKey] != input.AllocationKey {
			continue
		}
		invoiceID := ""
		if item.Invoice != nil {
			invoiceID = item.Invoice.ID
		}
		if invoiceID != input.InvoiceID || item.Currency != stripesdk.CurrencyUSD || item.Amount != input.AmountCents {
			return nil, errors.New("stripe invoice item allocation metadata matches different financial data")
		}
		return &InvoiceItem{ID: item.ID, InvoiceID: invoiceID, Currency: string(item.Currency), AmountCents: item.Amount}, nil
	}
	return nil, nil
}

func (c *client) FindCreditNote(ctx context.Context, input FindInvoiceAllocationInput) (*CreditNote, error) {
	if input.InvoiceID == "" || input.AllocationKey == "" || input.AmountCents <= 0 {
		return nil, errors.New("invoice id, allocation key, and positive amount are required")
	}
	params := new(stripesdk.CreditNoteListParams)
	params.Invoice = stripesdk.String(input.InvoiceID)
	params.Limit = stripesdk.Int64(100)
	for note, err := range c.api.listCreditNotes(ctx, params) {
		if err != nil {
			return nil, fmt.Errorf("list Stripe credit notes: %w", err)
		}
		if note == nil || note.Metadata[allocationMetadataKey] != input.AllocationKey {
			continue
		}
		invoiceID := ""
		if note.Invoice != nil {
			invoiceID = note.Invoice.ID
		}
		if invoiceID != input.InvoiceID || note.Currency != stripesdk.CurrencyUSD || note.Amount != input.AmountCents {
			return nil, errors.New("stripe credit note allocation metadata matches different financial data")
		}
		return &CreditNote{ID: note.ID, InvoiceID: invoiceID, Currency: string(note.Currency), AmountCents: note.Amount}, nil
	}
	return nil, nil
}

func (c *client) VerifyWebhook(payload []byte, signature string) (*WebhookEvent, error) {
	if !IsConfigured(c.webhookSecret) {
		return nil, fmt.Errorf("verify Stripe webhook: %w", ErrWebhookNotConfigured)
	}

	event, err := stripewebhook.ConstructEventWithOptions(payload, signature, c.webhookSecret, stripewebhook.ConstructEventOptions{
		Tolerance:                0,
		IgnoreTolerance:          false,
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		return nil, fmt.Errorf("verify Stripe webhook: %w", err)
	}

	var objectID string
	var customerID string
	var subscriptionID string
	if event.Data != nil {
		objectID, customerID, subscriptionID, err = webhookObjectIdentifiers(event.Data.Raw)
		if err != nil {
			return nil, fmt.Errorf("parse Stripe webhook data object: %w", err)
		}
	}
	return &WebhookEvent{
		ID:             event.ID,
		Type:           string(event.Type),
		Created:        time.Unix(event.Created, 0),
		ObjectID:       objectID,
		CustomerID:     customerID,
		SubscriptionID: subscriptionID,
	}, nil
}

func webhookObjectIdentifiers(raw json.RawMessage) (string, string, string, error) {
	var object *struct {
		ID           string          `json:"id"`
		Customer     json.RawMessage `json:"customer"`
		Subscription json.RawMessage `json:"subscription"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", "", "", fmt.Errorf("decode data object: %w", err)
	}
	if object == nil {
		return "", "", "", errors.New("data object is null")
	}

	return object.ID, expandedObjectID(object.Customer), expandedObjectID(object.Subscription), nil
}

func expandedObjectID(raw json.RawMessage) string {
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return id
	}

	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return object.ID
	}

	return ""
}

func (c *client) Catalog() Catalog {
	return c.catalog
}
