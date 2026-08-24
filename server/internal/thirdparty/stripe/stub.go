package stripe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"sync"
	"time"
)

// LocalCheckoutPath is the in-process hosted Checkout stand-in used when Stripe
// is unconfigured in local development.
const LocalCheckoutPath = "/rpc/stripe.local-checkout"

const localSessionQueryKey = "session"

var (
	// ErrLocalCheckoutSessionRequired is returned when local Checkout is completed without a session id.
	ErrLocalCheckoutSessionRequired = errors.New("checkout session id is required")

	// ErrLocalCheckoutSessionUnknown is returned when local Checkout cannot find the session.
	ErrLocalCheckoutSessionUnknown = errors.New("checkout session not found")

	// ErrLocalCheckoutSessionExpired is returned when local Checkout is completed after expiry.
	ErrLocalCheckoutSessionExpired = errors.New("checkout session is expired")

	errLocalCheckoutNotOpen        = errors.New("checkout session is not open")
	errLocalSubscriptionRequired   = errors.New("subscription id is required")
	errLocalSubscriptionUnknown    = errors.New("subscription not found")
	errLocalInvoiceRequired        = errors.New("invoice id is required")
	errLocalInvoiceUnknown         = errors.New("invoice not found")
	errLocalPortalCustomerRequired = errors.New("customer id is required")
	errLocalPortalReturnRequired   = errors.New("portal return URL is required")
)

// LocalCheckout is implemented by the in-process Stripe stub so local
// development can complete hosted Checkout and start PAYG without Stripe.
type LocalCheckout interface {
	// CompleteCheckout finishes an open session and returns the webhook event
	// that starts PAYG, plus the dashboard URL to send the browser to after.
	CompleteCheckout(context.Context, string) (*LocalCheckoutCompletion, error)
}

// LocalCheckoutCompletion is the state produced when a stub Checkout session
// is completed locally.
type LocalCheckoutCompletion struct {
	// Event is the checkout.session.completed envelope the webhook handler
	// consumes after local completion.
	Event *WebhookEvent

	// SuccessURL is the dashboard destination after PAYG has started.
	SuccessURL string
}

type stubCheckout struct {
	id                 string
	customerID         string
	successURL         string
	status             string
	subscriptionID     string
	billingCycleAnchor time.Time
	trialEnd           *time.Time
	expiresAt          time.Time
	input              CreateCheckoutSessionInput
}

type stubInvoiceAllocation struct {
	item      *InvoiceItem
	note      *CreditNote
	invoiceID string
	key       string
	amount    int64
}

type stubClient struct {
	mu            sync.Mutex
	logger        *slog.Logger
	publicURL     *url.URL
	seq           int
	customers     map[string]*Customer
	sessions      map[string]*stubCheckout
	sessionByKey  map[string]string
	subscriptions map[string]*SubscriptionState
	invoices      map[string]*InvoiceState
	allocations   []stubInvoiceAllocation
	meterTotals   map[string]float64
}

var _ Client = (*stubClient)(nil)
var _ LocalCheckout = (*stubClient)(nil)

// NewStubClient returns the in-process Stripe stand-in used when Stripe is
// unconfigured. publicURL is the Gram server origin used to host local
// Checkout; when nil, Checkout returns SuccessURL so callers can complete the
// session through LocalCheckout directly.
func NewStubClient(logger *slog.Logger, publicURL *url.URL) Client {
	return &stubClient{
		mu:            sync.Mutex{},
		logger:        logger,
		publicURL:     cloneURL(publicURL),
		seq:           0,
		customers:     make(map[string]*Customer),
		sessions:      make(map[string]*stubCheckout),
		sessionByKey:  make(map[string]string),
		subscriptions: make(map[string]*SubscriptionState),
		invoices:      make(map[string]*InvoiceState),
		allocations:   make([]stubInvoiceAllocation, 0),
		meterTotals:   make(map[string]float64),
	}
}

func (s *stubClient) CreateCustomer(ctx context.Context, input CreateCustomerInput) (*Customer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.IdempotencyKey != "" {
		if customer, ok := s.customers[input.IdempotencyKey]; ok {
			copied := *customer
			return &copied, nil
		}
	}

	s.seq++
	customer := &Customer{ID: fmt.Sprintf("cus_local_%d", s.seq)}
	if input.IdempotencyKey != "" {
		s.customers[input.IdempotencyKey] = customer
	}
	s.logger.DebugContext(ctx, "created local Stripe customer")
	copied := *customer
	return &copied, nil
}

func (s *stubClient) CreateCheckoutSession(ctx context.Context, input CreateCheckoutSessionInput) (*CheckoutSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.IdempotencyKey != "" {
		if sessionID, ok := s.sessionByKey[input.IdempotencyKey]; ok {
			session, exists := s.sessions[sessionID]
			if !exists {
				return nil, ErrLocalCheckoutSessionUnknown
			}
			if !reflect.DeepEqual(session.input, input) {
				return nil, fmt.Errorf("idempotency key %q reused with different checkout input", input.IdempotencyKey)
			}
			return &CheckoutSession{ID: session.id, URL: s.checkoutURL(session.id, input)}, nil
		}
	}

	s.seq++
	session := &stubCheckout{
		id:                 fmt.Sprintf("cs_local_%d", s.seq),
		customerID:         input.CustomerID,
		successURL:         input.SuccessURL,
		status:             "open",
		subscriptionID:     "",
		billingCycleAnchor: localBillingCycleAnchor(input.BillingCycleAnchor),
		trialEnd:           cloneTime(input.TrialEnd),
		expiresAt:          input.ExpiresAt.UTC(),
		input:              input,
	}
	s.expireSession(session, time.Now().UTC())
	s.sessions[session.id] = session
	if input.IdempotencyKey != "" {
		s.sessionByKey[input.IdempotencyKey] = session.id
	}
	s.logger.DebugContext(ctx, "created local Stripe Checkout session")
	return &CheckoutSession{ID: session.id, URL: s.checkoutURL(session.id, input)}, nil
}

func (s *stubClient) GetCheckoutSession(_ context.Context, id string) (*CheckoutSessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrLocalCheckoutSessionUnknown
	}
	s.expireSession(session, time.Now().UTC())
	return checkoutState(session, s.subscriptions[session.subscriptionID]), nil
}

func (s *stubClient) GetSubscription(_ context.Context, id string) (*SubscriptionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return nil, errLocalSubscriptionRequired
	}
	state, ok := s.subscriptions[id]
	if !ok {
		return nil, errLocalSubscriptionUnknown
	}
	copied := *state
	return &copied, nil
}

func (s *stubClient) SetSubscriptionCancelAtPeriodEnd(_ context.Context, input SetSubscriptionCancelAtPeriodEndInput) (*SubscriptionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.SubscriptionID == "" {
		return nil, errLocalSubscriptionRequired
	}
	state, ok := s.subscriptions[input.SubscriptionID]
	if !ok {
		return nil, errLocalSubscriptionUnknown
	}

	now := time.Now().UTC()
	state.CancelAtPeriodEnd = input.CancelAtPeriodEnd
	if input.CancelAtPeriodEnd {
		state.CancelAt = state.CurrentPeriodEnd
		state.CanceledAt = now
	} else {
		state.CancelAt = time.Time{}
		state.CanceledAt = time.Time{}
	}
	copied := *state
	return &copied, nil
}

func (s *stubClient) CreatePortalSession(_ context.Context, input CreatePortalSessionInput) (*PortalSession, error) {
	if input.CustomerID == "" {
		return nil, errLocalPortalCustomerRequired
	}
	if input.ReturnURL == "" {
		return nil, errLocalPortalReturnRequired
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	return &PortalSession{
		ID:         fmt.Sprintf("bps_local_%d", s.seq),
		CustomerID: input.CustomerID,
		URL:        input.ReturnURL,
	}, nil
}

func (s *stubClient) CreateMeterEvent(ctx context.Context, input CreateMeterEventInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.meterTotals[input.CustomerID] += float64(input.Value)
	s.logger.DebugContext(ctx, "recorded local Stripe meter event")
	return nil
}

func (s *stubClient) GetMeterEventSummary(ctx context.Context, input GetMeterEventSummaryInput) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.DebugContext(ctx, "read local Stripe meter event summary")
	return s.meterTotals[input.CustomerID], nil
}

func (s *stubClient) GetInvoice(_ context.Context, id string) (*InvoiceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return nil, errLocalInvoiceRequired
	}
	state, ok := s.invoices[id]
	if !ok {
		return nil, errLocalInvoiceUnknown
	}
	copied := *state
	return &copied, nil
}

func (s *stubClient) CreateInvoiceItem(ctx context.Context, input CreateInvoiceItemInput) (*InvoiceItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	item := &InvoiceItem{
		ID:          fmt.Sprintf("ii_local_%d", s.seq),
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}
	s.allocations = append(s.allocations, stubInvoiceAllocation{
		item:      item,
		note:      nil,
		invoiceID: input.InvoiceID,
		key:       input.AllocationKey,
		amount:    input.AmountCents,
	})
	s.logger.DebugContext(ctx, "created local Stripe invoice item")
	copied := *item
	return &copied, nil
}

func (s *stubClient) CreateCreditNote(ctx context.Context, input CreateCreditNoteInput) (*CreditNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	note := &CreditNote{
		ID:          fmt.Sprintf("cn_local_%d", s.seq),
		InvoiceID:   input.InvoiceID,
		Currency:    "usd",
		AmountCents: input.AmountCents,
	}
	s.allocations = append(s.allocations, stubInvoiceAllocation{
		item:      nil,
		note:      note,
		invoiceID: input.InvoiceID,
		key:       input.AllocationKey,
		amount:    input.AmountCents,
	})
	s.logger.DebugContext(ctx, "created local Stripe credit note")
	copied := *note
	return &copied, nil
}

func (s *stubClient) FindInvoiceItem(_ context.Context, input FindInvoiceAllocationInput) (*InvoiceItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, allocation := range s.allocations {
		if allocation.item != nil &&
			allocation.invoiceID == input.InvoiceID &&
			allocation.key == input.AllocationKey &&
			allocation.amount == input.AmountCents {
			copied := *allocation.item
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *stubClient) FindCreditNote(_ context.Context, input FindInvoiceAllocationInput) (*CreditNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, allocation := range s.allocations {
		if allocation.note != nil &&
			allocation.invoiceID == input.InvoiceID &&
			allocation.key == input.AllocationKey &&
			allocation.amount == input.AmountCents {
			copied := *allocation.note
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *stubClient) VerifyWebhook(_ []byte, _ string) (*WebhookEvent, error) {
	return nil, fmt.Errorf("verify Stripe webhook: %w", ErrWebhookNotConfigured)
}

func (s *stubClient) Catalog() Catalog {
	return Catalog{
		PriceIDTUM:            "",
		MeterIDTUM:            "",
		MeterEventName:        "",
		PortalConfigurationID: "",
	}
}

func (s *stubClient) CompleteCheckout(ctx context.Context, sessionID string) (*LocalCheckoutCompletion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionID == "" {
		return nil, ErrLocalCheckoutSessionRequired
	}
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrLocalCheckoutSessionUnknown
	}

	now := time.Now().UTC()
	s.expireSession(session, now)
	if session.status == "expired" {
		return nil, ErrLocalCheckoutSessionExpired
	}

	if session.status != "complete" {
		if session.status != "open" {
			return nil, errLocalCheckoutNotOpen
		}
		s.seq++
		subscriptionID := fmt.Sprintf("sub_local_%d", s.seq)
		s.seq++
		invoiceID := fmt.Sprintf("in_local_%d", s.seq)
		subscription := newLocalSubscription(subscriptionID, session, invoiceID, now)
		s.subscriptions[subscriptionID] = subscription
		s.invoices[invoiceID] = &InvoiceState{
			ID:                 invoiceID,
			CustomerID:         session.customerID,
			SubscriptionID:     subscriptionID,
			Currency:           "usd",
			BillingReason:      "subscription_create",
			Status:             "paid",
			ServicePeriodStart: subscription.CurrentPeriodStart,
			ServicePeriodEnd:   subscription.CurrentPeriodEnd,
			FinalizedAt:        now,
			AmountRemaining:    0,
		}
		session.subscriptionID = subscriptionID
		session.status = "complete"
		s.logger.DebugContext(ctx, "completed local Stripe Checkout session")
	}

	return &LocalCheckoutCompletion{
		Event: &WebhookEvent{
			ID:             "evt_local_" + session.id,
			Type:           "checkout.session.completed",
			Created:        now,
			ObjectID:       session.id,
			CustomerID:     session.customerID,
			SubscriptionID: session.subscriptionID,
		},
		SuccessURL: session.successURL,
	}, nil
}

func (s *stubClient) checkoutURL(sessionID string, input CreateCheckoutSessionInput) string {
	if s.publicURL != nil {
		checkout := *s.publicURL
		checkout.Path = LocalCheckoutPath
		query := checkout.Query()
		query.Set(localSessionQueryKey, sessionID)
		checkout.RawQuery = query.Encode()
		return checkout.String()
	}
	if input.SuccessURL != "" {
		return input.SuccessURL
	}
	return fmt.Sprintf("http://localhost:3000/%s/billing", url.PathEscape(input.OrganizationSlug))
}

func (s *stubClient) expireSession(session *stubCheckout, now time.Time) {
	if session.status == "open" && !session.expiresAt.IsZero() && !session.expiresAt.After(now) {
		session.status = "expired"
	}
}

func checkoutState(session *stubCheckout, subscription *SubscriptionState) *CheckoutSessionState {
	state := &CheckoutSessionState{
		ID:                     session.id,
		Status:                 session.status,
		CustomerID:             session.customerID,
		SubscriptionID:         session.subscriptionID,
		SubscriptionCustomerID: "",
		SubscriptionStatus:     "",
		BillingCycleAnchor:     session.billingCycleAnchor,
	}
	if subscription != nil {
		state.SubscriptionCustomerID = subscription.CustomerID
		state.SubscriptionStatus = subscription.Status
		if !subscription.CurrentPeriodStart.IsZero() && session.billingCycleAnchor.IsZero() {
			state.BillingCycleAnchor = subscription.CurrentPeriodStart
		}
	}
	return state
}

func newLocalSubscription(id string, session *stubCheckout, invoiceID string, now time.Time) *SubscriptionState {
	periodStart := now.Truncate(time.Second)
	periodEnd := session.billingCycleAnchor
	if periodEnd.IsZero() || !periodEnd.After(periodStart) {
		periodEnd = periodStart.AddDate(0, 1, 0)
	}

	status := "active"
	trialStart := time.Time{}
	trialEnd := time.Time{}
	if session.trialEnd != nil && session.trialEnd.After(now) {
		status = "trialing"
		trialStart = periodStart
		trialEnd = session.trialEnd.UTC()
		periodEnd = trialEnd
	}

	return &SubscriptionState{
		ID:                           id,
		CustomerID:                   session.customerID,
		Status:                       status,
		CurrentPeriodStart:           periodStart,
		CurrentPeriodEnd:             periodEnd,
		TrialStart:                   trialStart,
		TrialEnd:                     trialEnd,
		CancelAtPeriodEnd:            false,
		CancelAt:                     time.Time{},
		CanceledAt:                   time.Time{},
		LatestInvoiceID:              invoiceID,
		LatestInvoiceStatus:          "paid",
		LatestInvoiceAmountRemaining: 0,
		PaymentFailed:                false,
	}
}

func localBillingCycleAnchor(value time.Time) time.Time {
	if !value.IsZero() {
		return value.UTC()
	}
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return midnight.AddDate(0, 0, 1)
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := value.UTC()
	return &copied
}
