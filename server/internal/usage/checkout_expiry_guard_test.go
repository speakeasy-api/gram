package usage

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
)

func TestExpiredCheckoutSessionForReplacementRemoteState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, status, customer, subscription     string
		localExpired, readError, wantReplacement bool
		wantCode                                 oops.Code
	}{
		{name: "early remote expiry", status: "expired", customer: "cus_test", wantReplacement: true},
		{name: "scheduled expiry", status: "expired", customer: "cus_test", localExpired: true, wantReplacement: true},
		{name: "open session stays reusable", status: "open", customer: "cus_test"},
		{name: "open after intended expiry fails closed", status: "open", customer: "cus_test", localExpired: true, wantCode: oops.CodeConflict},
		{name: "complete before intended expiry", status: "complete", customer: "cus_test", wantCode: oops.CodeConflict},
		{name: "expired with subscription", status: "expired", customer: "cus_test", subscription: "sub_test", wantCode: oops.CodeConflict},
		{name: "open with subscription", status: "open", customer: "cus_test", subscription: "sub_test", wantCode: oops.CodeConflict},
		{name: "wrong customer", status: "expired", customer: "cus_other", wantCode: oops.CodeUnavailable},
		{name: "unknown status", status: "unknown", customer: "cus_test", wantCode: oops.CodeConflict},
		{name: "read failure", readError: true, wantCode: oops.CodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api := newCheckoutStripeClient()
			api.checkoutState = &stripeclient.CheckoutSessionState{
				ID: "cs_test", CustomerID: tc.customer, Status: tc.status, SubscriptionID: tc.subscription,
			}
			if tc.readError {
				api.checkoutGetErr = errors.New("read failed")
			}
			service := &Service{stripeClient: api, logger: testenv.NewLogger(t)}
			expiresAt := now.Add(time.Hour)
			if tc.localExpired {
				expiresAt = now
			}
			replacement, err := service.expiredCheckoutSessionForReplacement(t.Context(), repo.BillingMetadatum{
				StripeCheckoutSessionID: pgtype.Text{String: "cs_test", Valid: true},
				StripeCheckoutExpiresAt: finiteTimestamptz(expiresAt),
			}, "cus_test", now)
			if tc.wantCode != "" {
				require.Error(t, err)
				requireOopsCode(t, err, tc.wantCode)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantReplacement, replacement.Valid)
			if replacement.Valid {
				require.Equal(t, "cs_test", replacement.String)
			}
		})
	}
}
