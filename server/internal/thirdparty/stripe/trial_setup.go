package stripe

import (
	"context"
	"errors"
	"fmt"
	"time"

	stripesdk "github.com/stripe/stripe-go/v85"
)

// checkoutPurposeTrialSetup marks the Checkout sessions that save the card a free
// trial starts with, so webhook routing never mistakes them for PAYG Checkout.
const (
	checkoutPurposeMetadataKey = "speakeasy_checkout_purpose"
	checkoutPurposeTrialSetup  = "trial_payment_method"
	checkoutUserIDMetadataKey  = "speakeasy_user_id"
)

// TrialPaymentMethodClient saves and reads the card a self-serve trial starts
// with. It is separate from Client so PAYG fakes do not have to implement it.
type TrialPaymentMethodClient interface {
	CreateTrialSetupCheckoutSession(context.Context, CreateTrialSetupCheckoutSessionInput) (*CheckoutSession, error)
	GetTrialSetupCheckoutSession(context.Context, string) (*TrialSetupCheckoutSessionState, error)
	SetDefaultPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error
	DetachPaymentMethod(ctx context.Context, paymentMethodID string) error
}

// CreateTrialSetupCheckoutSessionInput describes an embedded Checkout session
// in setup mode that saves a card without charging it.
type CreateTrialSetupCheckoutSessionInput struct {
	// CustomerID identifies the Stripe customer that keeps the card.
	CustomerID string

	// OrganizationID is Speakeasy's stable organization identifier.
	OrganizationID string

	// OrganizationSlug is the organization slug included in Stripe metadata.
	OrganizationSlug string

	// UserID is the Speakeasy user who started Checkout, recorded as the actor
	// of the trial the session starts.
	UserID string

	// ReturnURL is where embedded Checkout sends the browser after a redirect,
	// such as a 3-D Secure challenge. It must contain the literal
	// {CHECKOUT_SESSION_ID} template.
	ReturnURL string

	// Dark selects the dark appearance.
	Dark bool

	// ExpiresAt bounds how long the session can be completed.
	ExpiresAt time.Time

	// IdempotencyKey identifies one Checkout creation request.
	IdempotencyKey string
}

// TrialSetupCheckoutSessionState is the Stripe state needed to start a trial
// from a completed setup Checkout.
type TrialSetupCheckoutSessionState struct {
	// ID is Stripe's Checkout Session identifier.
	ID string

	// Status is the Checkout Session status: open, complete, or expired.
	Status string

	// TrialSetup reports whether Speakeasy created the session to start a trial.
	TrialSetup bool

	// OrganizationID is the organization named in the session metadata.
	OrganizationID string

	// UserID is the Speakeasy user named in the session metadata.
	UserID string

	// CustomerID identifies the Stripe customer the session belongs to.
	CustomerID string

	// SetupIntentStatus is the status of the session's SetupIntent.
	SetupIntentStatus string

	// PaymentMethodID identifies the saved card. Empty until the SetupIntent succeeds.
	PaymentMethodID string

	// CardFingerprint identifies the card number across customers. Empty for
	// payment methods that are not cards.
	CardFingerprint string

	// CardFunding is the card's funding type: credit, debit, prepaid, or unknown.
	CardFunding string
}

// CardSaved reports whether the session saved a card that can start a trial.
func (s *TrialSetupCheckoutSessionState) CardSaved() bool {
	return s != nil && s.Status == "complete" && s.SetupIntentStatus == string(stripesdk.SetupIntentStatusSucceeded) &&
		s.PaymentMethodID != "" && s.CardFingerprint != ""
}

func (c *client) CreateTrialSetupCheckoutSession(ctx context.Context, input CreateTrialSetupCheckoutSessionInput) (*CheckoutSession, error) {
	if input.IdempotencyKey == "" {
		return nil, errMissingIdempotencyKey
	}
	if input.ExpiresAt.IsZero() {
		return nil, errMissingCheckoutExpiration
	}
	if input.CustomerID == "" || input.ReturnURL == "" {
		return nil, errors.New("trial setup Checkout requires a customer and a return URL")
	}

	identity := organizationIdentity{id: input.OrganizationID, slug: input.OrganizationSlug}
	metadata := contractMetadata(identity)
	metadata[checkoutPurposeMetadataKey] = checkoutPurposeTrialSetup
	metadata[checkoutUserIDMetadataKey] = input.UserID

	backgroundColor, buttonColor := "#ffffff", "#303030"
	if input.Dark {
		backgroundColor, buttonColor = "#000000", "#ffffff"
	}

	params := new(stripesdk.CheckoutSessionCreateParams)
	params.Mode = stripesdk.String(string(stripesdk.CheckoutSessionModeSetup))
	params.UIMode = stripesdk.String(string(stripesdk.CheckoutSessionUIModeEmbeddedPage))
	params.ReturnURL = stripesdk.String(input.ReturnURL)
	params.RedirectOnCompletion = stripesdk.String(string(stripesdk.CheckoutSessionRedirectOnCompletionIfRequired))
	params.Customer = stripesdk.String(input.CustomerID)
	params.ClientReferenceID = stripesdk.String(input.OrganizationID)
	// Only cards carry a fingerprint that identifies one card across customers.
	params.PaymentMethodTypes = []*string{stripesdk.String("card")}
	params.Currency = stripesdk.String(string(stripesdk.CurrencyUSD))
	params.ExpiresAt = new(input.ExpiresAt.Unix())
	params.Metadata = metadata
	params.SetupIntentData = &stripesdk.CheckoutSessionCreateSetupIntentDataParams{
		Description: stripesdk.String("Card for the Speakeasy free trial"),
		Metadata:    metadata,
		OnBehalfOf:  nil,
	}
	params.BrandingSettings = &stripesdk.CheckoutSessionCreateBrandingSettingsParams{
		DisplayName:     nil,
		Icon:            nil,
		Logo:            nil,
		UnsetFields:     nil,
		BackgroundColor: stripesdk.String(backgroundColor),
		ButtonColor:     stripesdk.String(buttonColor),
		BorderStyle:     stripesdk.String("rectangular"),
		FontFamily:      stripesdk.String("inter"),
	}
	params.SetIdempotencyKey(input.IdempotencyKey)

	session, err := c.api.createCheckoutSession(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe trial setup Checkout session: %w", err)
	}
	return &CheckoutSession{ID: session.ID, URL: session.URL, ClientSecret: session.ClientSecret}, nil
}

func (c *client) GetTrialSetupCheckoutSession(ctx context.Context, id string) (*TrialSetupCheckoutSessionState, error) {
	if id == "" {
		return nil, errors.New("missing Stripe Checkout session id")
	}
	params := new(stripesdk.CheckoutSessionRetrieveParams)
	params.AddExpand("setup_intent.payment_method")

	session, err := c.api.retrieveCheckoutSession(ctx, id, params)
	if err != nil {
		if stripeErr, ok := errors.AsType[*stripesdk.Error](err); ok && stripeErr.Code == stripesdk.ErrorCodeResourceMissing {
			return nil, fmt.Errorf("%w: %w", ErrCheckoutSessionNotFound, err)
		}
		return nil, fmt.Errorf("retrieve Stripe trial setup Checkout session: %w", err)
	}
	if session == nil {
		return nil, errors.New("retrieve Stripe trial setup Checkout session: empty response")
	}

	state := &TrialSetupCheckoutSessionState{
		ID:                session.ID,
		Status:            string(session.Status),
		TrialSetup:        session.Mode == stripesdk.CheckoutSessionModeSetup && session.Metadata[checkoutPurposeMetadataKey] == checkoutPurposeTrialSetup,
		OrganizationID:    session.Metadata[organizationIDMetadataKey],
		UserID:            session.Metadata[checkoutUserIDMetadataKey],
		CustomerID:        "",
		SetupIntentStatus: "",
		PaymentMethodID:   "",
		CardFingerprint:   "",
		CardFunding:       "",
	}
	if session.Customer != nil {
		state.CustomerID = session.Customer.ID
	}
	if intent := session.SetupIntent; intent != nil {
		state.SetupIntentStatus = string(intent.Status)
		if method := intent.PaymentMethod; method != nil {
			state.PaymentMethodID = method.ID
			if method.Card != nil {
				state.CardFingerprint = method.Card.Fingerprint
				state.CardFunding = string(method.Card.Funding)
			}
		}
	}
	return state, nil
}

func (c *client) SetDefaultPaymentMethod(ctx context.Context, customerID, paymentMethodID string) error {
	if customerID == "" || paymentMethodID == "" {
		return errors.New("default payment method requires a customer and a payment method")
	}
	params := new(stripesdk.CustomerUpdateParams)
	params.InvoiceSettings = &stripesdk.CustomerUpdateInvoiceSettingsParams{
		CustomFields:         nil,
		DefaultPaymentMethod: stripesdk.String(paymentMethodID),
		Footer:               nil,
		RenderingOptions:     nil,
		UnsetFields:          nil,
	}
	if _, err := c.api.updateCustomer(ctx, customerID, params); err != nil {
		return fmt.Errorf("set Stripe default payment method: %w", err)
	}
	return nil
}

func (c *client) DetachPaymentMethod(ctx context.Context, paymentMethodID string) error {
	if paymentMethodID == "" {
		return errors.New("missing Stripe payment method id")
	}
	if _, err := c.api.detachPaymentMethod(ctx, paymentMethodID, new(stripesdk.PaymentMethodDetachParams)); err != nil {
		return fmt.Errorf("detach Stripe payment method: %w", err)
	}
	return nil
}
