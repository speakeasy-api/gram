// Package stripe provides the narrow Stripe boundary used by PAYG billing.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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

	event, err := stripewebhook.ConstructEvent(payload, signature, c.webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("verify Stripe webhook: %w", err)
	}
	if event.APIVersion != stripesdk.APIVersion {
		return nil, fmt.Errorf("verify Stripe webhook: expected API version %s, got %s", stripesdk.APIVersion, event.APIVersion)
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

func (s *stubClient) CreateCheckoutSession(ctx context.Context, input CreateCheckoutSessionInput) (*CheckoutSession, error) {
	s.logger.DebugContext(ctx, "stub Stripe Checkout session creation skipped")
	return &CheckoutSession{ID: "cs_local_stub", URL: fmt.Sprintf("http://localhost:3000/%s/billing", url.PathEscape(input.OrganizationSlug))}, nil
}

func (s *stubClient) GetCheckoutSession(context.Context, string) (*CheckoutSessionState, error) {
	return nil, errors.New("retrieve Stripe Checkout session is unavailable locally")
}

func (s *stubClient) CreateMeterEvent(ctx context.Context, _ CreateMeterEventInput) error {
	s.logger.DebugContext(ctx, "stub Stripe meter event skipped")
	return nil
}

func (s *stubClient) GetMeterEventSummary(ctx context.Context, _ GetMeterEventSummaryInput) (float64, error) {
	s.logger.DebugContext(ctx, "stub Stripe meter event summary skipped")
	return 0, nil
}

func (s *stubClient) GetInvoice(context.Context, string) (*InvoiceState, error) {
	return nil, errors.New("retrieve Stripe invoice is unavailable locally")
}

func (s *stubClient) CreateInvoiceItem(ctx context.Context, input CreateInvoiceItemInput) (*InvoiceItem, error) {
	s.logger.DebugContext(ctx, "stub Stripe invoice item skipped")
	return &InvoiceItem{
		ID:          "ii_local_stub",
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (s *stubClient) CreateCreditNote(ctx context.Context, input CreateCreditNoteInput) (*CreditNote, error) {
	s.logger.DebugContext(ctx, "stub Stripe credit note skipped")
	return &CreditNote{
		ID:          "cn_local_stub",
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}, nil
}

func (s *stubClient) FindInvoiceItem(context.Context, FindInvoiceAllocationInput) (*InvoiceItem, error) {
	return nil, nil
}

func (s *stubClient) FindCreditNote(context.Context, FindInvoiceAllocationInput) (*CreditNote, error) {
	return nil, nil
}

func (s *stubClient) VerifyWebhook(_ []byte, _ string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("verify Stripe webhook: %w", ErrWebhookNotConfigured)
}

func (s *stubClient) Catalog() Catalog {
	return Catalog{
		PriceIDTUM:     "",
		MeterIDTUM:     "",
		MeterEventName: "",
	}
}
