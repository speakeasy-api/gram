package stripe

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	stripesdk "github.com/stripe/stripe-go/v85"
)

func TestGetCustomerRejectsDeletedCustomer(t *testing.T) {
	t.Parallel()
	c := &client{api: &fakeStripeAPI{customer: &stripesdk.Customer{ID: "cus_example", Deleted: true}}}
	customer, err := c.GetCustomer(t.Context(), "cus_example")
	require.ErrorIs(t, err, ErrCustomerNotFound)
	require.Nil(t, customer)
}

func TestGetCustomerRecognizesWrappedMissingCustomer(t *testing.T) {
	t.Parallel()
	upstream := &stripesdk.Error{Code: stripesdk.ErrorCodeResourceMissing}
	c := &client{api: &fakeStripeAPI{err: fmt.Errorf("stripe SDK retrieve customer: %w", upstream)}}
	customer, err := c.GetCustomer(t.Context(), "cus_example")
	require.ErrorIs(t, err, ErrCustomerNotFound)
	require.ErrorIs(t, err, upstream)
	require.Nil(t, customer)
}

func TestGetCustomerDoesNotReportUpstreamFailureAsMissing(t *testing.T) {
	t.Parallel()
	upstream := errors.New("connection reset")
	c := &client{api: &fakeStripeAPI{err: upstream}}
	customer, err := c.GetCustomer(t.Context(), "cus_example")
	require.ErrorIs(t, err, upstream)
	require.NotErrorIs(t, err, ErrCustomerNotFound)
	require.Nil(t, customer)
}

func TestGetCustomerRejectsUnexpectedIdentity(t *testing.T) {
	t.Parallel()
	c := &client{api: &fakeStripeAPI{customer: &stripesdk.Customer{ID: "cus_other"}}}
	customer, err := c.GetCustomer(t.Context(), "cus_example")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrCustomerNotFound)
	require.Nil(t, customer)
}
