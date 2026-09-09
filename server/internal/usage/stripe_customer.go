package usage

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/oops"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

// GetStripeCustomer retrieves the live identity for an independently authorized admin operation.
func (s *Service) GetStripeCustomer(ctx context.Context, customerID string) (*stripeclient.CustomerDetails, error) {
	if s.stripeClient == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "Stripe customer lookup is not configured")
	}
	customer, err := s.stripeClient.GetCustomer(ctx, customerID)
	switch {
	case errors.Is(err, stripeclient.ErrCustomerNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "Stripe customer not found or deleted")
	case errors.Is(err, stripeclient.ErrCustomerLookupUnavailable):
		return nil, oops.E(oops.CodeUnavailable, err, "Stripe customer lookup is not configured")
	case err != nil:
		return nil, oops.E(oops.CodeGatewayError, err, "could not retrieve Stripe customer").LogWarn(ctx, s.logger)
	case customer == nil || customer.ID != customerID:
		return nil, oops.E(oops.CodeGatewayError, nil, "Stripe returned an unexpected customer identity").LogWarn(ctx, s.logger)
	}
	return customer, nil
}
