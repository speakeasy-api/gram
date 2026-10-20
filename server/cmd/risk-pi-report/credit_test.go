package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func creditServer(t *testing.T, credits, keyLimit string, status int) (*httptest.Server, *http.Client) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/credits", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(credits))
	})
	mux.HandleFunc("GET /v1/key", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(keyLimit))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return server, policy.PooledClient()
}

func TestCheckCreditPassesWhenTheBalanceCoversTheRun(t *testing.T) {
	t.Parallel()

	server, client := creditServer(t, `{"data":{"total_credits":60,"total_usage":50}}`, `{"data":{"is_management_key":true,"limit_remaining":null}}`, http.StatusOK)
	require.NoError(t, checkCredit(t.Context(), client, server.URL, "test-key", 2046))
}

func TestCheckCreditFailsWhenTheBalanceIsShort(t *testing.T) {
	t.Parallel()

	server, client := creditServer(t, `{"data":{"total_credits":60,"total_usage":59.5}}`, `{"data":{"is_management_key":true,"limit_remaining":null}}`, http.StatusOK)
	err := checkCredit(t.Context(), client, server.URL, "test-key", 2046)
	require.ErrorContains(t, err, "$0.50 of checked allowance left")
}

func TestCheckCreditUsesTheKeyLimitWhenLower(t *testing.T) {
	t.Parallel()

	server, client := creditServer(t, `{"data":{"total_credits":100,"total_usage":0}}`, `{"data":{"is_management_key":true,"limit_remaining":1}}`, http.StatusOK)
	err := checkCredit(t.Context(), client, server.URL, "test-key", 2046)
	require.ErrorContains(t, err, "$1.00 of checked allowance left")
}

func TestCheckCreditOnlyWarnsWhenTheCheckFails(t *testing.T) {
	t.Parallel()

	server, client := creditServer(t, `{}`, `{}`, http.StatusInternalServerError)
	require.NoError(t, checkCredit(t.Context(), client, server.URL, "test-key", 2046))
}

func TestCreditLookupRejectsMissingAccountTotals(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{}`, `{"data":{"total_credits":10}}`, `{"data":{"total_usage":2}}`, `{"data":{"total_credits":null,"total_usage":2}}`} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			server, client := creditServer(t, body, `{"data":{"is_management_key":true,"limit_remaining":null}}`, http.StatusOK)
			_, err := openRouterCredit(t.Context(), client, server.URL, "test-key")
			require.ErrorContains(t, err, "missing account totals")
			require.NoError(t, checkCredit(t.Context(), client, server.URL, "test-key", 2046))
		})
	}
}

func TestCreditLookupUsesInferenceKeyAllowanceWithoutAccountEndpoint(t *testing.T) {
	t.Parallel()
	server, client := creditServer(t, `{}`, `{"data":{"is_management_key":false,"limit_remaining":10}}`, http.StatusForbidden)
	balance, err := openRouterCredit(t.Context(), client, server.URL, "test-key")
	require.NoError(t, err)
	require.InDelta(t, 10.0, balance.Remaining, 1e-9)
	require.False(t, balance.AccountVerified)
	require.NoError(t, checkCredit(t.Context(), client, server.URL, "test-key", 2046))
}

func TestCreditLookupDoesNotTreatUnboundedInferenceKeyAsZero(t *testing.T) {
	t.Parallel()
	server, client := creditServer(t, `{}`, `{"data":{"is_management_key":false,"limit_remaining":null}}`, http.StatusForbidden)
	_, err := openRouterCredit(t.Context(), client, server.URL, "test-key")
	require.ErrorContains(t, err, "account balance cannot be verified")
	require.NoError(t, checkCredit(t.Context(), client, server.URL, "test-key", 2046))
}
