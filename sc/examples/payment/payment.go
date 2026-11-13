// Package payment is a worked example of a chart designed from a TLA+
// model: an order that charges a card, retries declined charges after a
// delay, and can be cancelled. The model is Payment.tla in this directory.
// Payment.cfg checks it in about a second and PaymentLarge.cfg checks it
// with more declines and lost answers. Neither is run in CI, so a change to
// the chart must be made in the model as well and checked again.
//
// The model ruled out two simpler designs, and each is still in it behind a
// constant so its trace can be reproduced.
//
// Cancelling while a charge may still land cannot simply move the order to
// cancelled. The charge may already be on its way to the payment provider,
// and its answer would arrive at a chart that no longer listens, leaving
// the customer charged for a cancelled order. The chart records the request
// and keeps going until it knows: a charge that succeeds is refunded, and
// one that is declined needs nothing more.
//
// A charge that ends without a clear answer, such as a timeout, cannot be
// treated as a decline. The card may have been charged, so trying again as
// a new request can charge it twice. The chart asks again under the same
// idempotency key until the provider gives a clear answer.
package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/sc"
)

// State identifies a state of the chart.
type State string

const (
	// Idle is an order that has not been submitted.
	Idle State = "idle"

	// Charging is an order whose card is being charged.
	Charging State = "charging"

	// Retrying is an order waiting to charge again after a decline.
	Retrying State = "retrying"

	// Paid is an order whose charge succeeded.
	Paid State = "paid"

	// Cancelled is an order that will not be paid.
	Cancelled State = "cancelled"
)

// MaxAttempts is how many declined charges are retried before the order is
// cancelled. A card that is declined three times in a row is not going to
// work without the customer changing something.
const MaxAttempts = 2

// RetryDelay is how long the chart waits before charging again. Declines
// are often caused by a temporary hold, which a minute usually outlasts.
const RetryDelay = time.Minute

// Order is the chart's context.
type Order struct {
	// ID identifies the order to the payment provider.
	ID string `json:"id"`

	// Attempts counts the declined charges so far.
	Attempts int `json:"attempts"`

	// CancelRequested is set when the customer cancels while a charge
	// may still land.
	CancelRequested bool `json:"cancel_requested"`

	// Unresolved is set while the last charge has no clear answer, so the
	// card may or may not have been charged.
	Unresolved bool `json:"unresolved"`

	// Receipt is the provider's reference for the successful charge.
	Receipt string `json:"receipt"`
}

// IdempotencyKey identifies one charge attempt to the payment provider. An
// attempt is sent again when a worker repeats it and when its answer was
// lost, and the key is what lets the provider treat all of those as a
// single request. Only a clear decline starts a new attempt with a new key.
func (o Order) IdempotencyKey() string {
	return fmt.Sprintf("%s/%d", o.ID, o.Attempts)
}

// Submit asks for the order to be paid.
type Submit struct{}

// Cancel asks for the order to be cancelled.
type Cancel struct{}

// ErrDeclined is the error a [Provider] returns when it refused the charge
// and took no money.
var ErrDeclined = errors.New("card declined")

// Provider is the payment provider the chart charges and refunds through.
type Provider interface {
	// Charge charges the order's card and returns a receipt. It returns
	// an error wrapping ErrDeclined when the provider refused the charge.
	// Any other error means the outcome is unknown. The provider must
	// decide once per idempotency key and give every later request with
	// that key the same outcome.
	Charge(ctx context.Context, idempotencyKey string) (receipt string, err error)

	// Refund returns the money taken by the charge with the given receipt.
	Refund(ctx context.Context, receipt string)
}

// New builds the chart.
func New(provider Provider) (*sc.Machine[State, Order], error) {
	m, err := sc.New("payment", func(b *sc.Builder[State, Order]) {
		b.Initial(Idle)

		b.State(Idle).On[Submit]().To(Charging)

		charging := b.State(Charging)
		charging.On[Cancel]().Assign("requestCancel", requestCancel)

		charge := charging.Invoke("charge", func(ctx context.Context, o Order) (string, error) {
			return provider.Charge(ctx, o.IdempotencyKey())
		})
		charge.OnDone().
			When("cancelRequested", func(o Order, _ sc.Done[string]) bool { return o.CancelRequested }).
			Assign("resolveCharged", resolve[sc.Done[string]]).
			Do("refund", func(ctx context.Context, _ Order, d sc.Done[string]) { provider.Refund(ctx, d.Result) }).
			To(Cancelled)
		charge.OnDone().
			Assign("setReceipt", func(o Order, d sc.Done[string]) Order {
				o.Receipt = d.Result
				o.Unresolved = false

				return o
			}).
			To(Paid)
		charge.OnError().
			When("outcomeUnknown", func(_ Order, f sc.Failed) bool { return !errors.Is(f.Err, ErrDeclined) }).
			Assign("markUnresolved", func(o Order, _ sc.Failed) Order {
				o.Unresolved = true
				return o
			}).
			To(Retrying)
		charge.OnError().
			When("canRetry", func(o Order, _ sc.Failed) bool { return !o.CancelRequested && o.Attempts < MaxAttempts }).
			Assign("countAttempt", func(o Order, _ sc.Failed) Order {
				o.Attempts++
				o.Unresolved = false

				return o
			}).
			To(Retrying)
		charge.OnError().Assign("resolveDeclined", resolve[sc.Failed]).To(Cancelled)

		retrying := b.State(Retrying)
		retrying.After(RetryDelay).To(Charging)
		retrying.On[Cancel]().
			When("chargeMayHaveLanded", func(o Order, _ Cancel) bool { return o.Unresolved }).
			Assign("requestCancel", requestCancel)
		retrying.On[Cancel]().To(Cancelled)

		b.Final(Paid)
		b.Final(Cancelled)
	})
	if err != nil {
		return nil, fmt.Errorf("build payment chart: %w", err)
	}

	return m, nil
}

// resolve records that the last charge got a clear answer.
func resolve[Ev any](o Order, _ Ev) Order {
	o.Unresolved = false
	return o
}

func requestCancel(o Order, _ Cancel) Order {
	o.CancelRequested = true
	return o
}
