package oautherr

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifyRefreshFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		code   string
		want   RefreshFailure
	}{
		{name: "invalid grant", status: http.StatusBadRequest, code: CodeInvalidGrant, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeInvalidGrant}},
		{name: "vendor dead grant", status: http.StatusBadRequest, code: "bad_refresh_token", want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeInvalidGrant}},
		{name: "dead grant on a 200", status: http.StatusOK, code: CodeInvalidGrant, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeInvalidGrant}},
		{name: "login required", status: http.StatusBadRequest, code: OIDCCodeLoginRequired, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeSignInRequired}},
		{name: "interaction required", status: http.StatusBadRequest, code: OIDCCodeInteractionRequired, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeSignInRequired}},
		{name: "consent required", status: http.StatusBadRequest, code: OIDCCodeConsentRequired, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeSignInRequired}},
		{name: "account selection required", status: http.StatusBadRequest, code: OIDCCodeAccountSelectionRequired, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeSignInRequired}},
		{name: "invalid client", status: http.StatusUnauthorized, code: CodeInvalidClient, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeInvalidClient}},
		{name: "unauthorized client", status: http.StatusBadRequest, code: CodeUnauthorizedClient, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeUnauthorizedClient}},
		{name: "invalid target", status: http.StatusBadRequest, code: CodeInvalidTarget, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeInvalidTarget}},
		{name: "temporarily unavailable", status: http.StatusBadRequest, code: CodeTemporarilyUnavailable, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeTransient}},
		{name: "server error code", status: http.StatusBadRequest, code: CodeServerError, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeTransient}},
		{name: "other code", status: http.StatusBadRequest, code: CodeInvalidScope, want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeOther}},
		{name: "no code", status: http.StatusForbidden, code: "", want: RefreshFailure{Status: RefreshStatusRejected, Code: RefreshCodeMissing}},
		{name: "rate limited", status: http.StatusTooManyRequests, code: "", want: RefreshFailure{Status: RefreshStatusRateLimited, Code: RefreshCodeMissing}},
		{name: "rate limited carrying invalid grant", status: http.StatusTooManyRequests, code: CodeInvalidGrant, want: RefreshFailure{Status: RefreshStatusRateLimited, Code: RefreshCodeInvalidGrant}},
		{name: "request timeout", status: http.StatusRequestTimeout, code: "", want: RefreshFailure{Status: RefreshStatusTimeout, Code: RefreshCodeMissing}},
		{name: "bad gateway", status: http.StatusBadGateway, code: "", want: RefreshFailure{Status: RefreshStatusServerError, Code: RefreshCodeMissing}},
		{name: "server error carrying invalid client", status: http.StatusInternalServerError, code: CodeInvalidClient, want: RefreshFailure{Status: RefreshStatusServerError, Code: RefreshCodeInvalidClient}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ClassifyRefreshFailure(tc.status, tc.code))
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "absent", value: "", want: 0},
		{name: "seconds", value: "120", want: 2 * time.Minute},
		{name: "padded seconds", value: " 30 ", want: 30 * time.Second},
		{name: "zero", value: "0", want: 0},
		{name: "negative", value: "-5", want: 0},
		{name: "signed", value: "+30", want: 0},
		{name: "past int64", value: "99999999999999999999", want: math.MaxInt64},
		{name: "huge", value: "99999999999999999", want: math.MaxInt64},
		{name: "http date", value: now.Add(90 * time.Second).Format(http.TimeFormat), want: 90 * time.Second},
		{name: "past date", value: now.Add(-time.Hour).Format(http.TimeFormat), want: 0},
		{name: "malformed", value: "soon", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, ParseRetryAfter(tc.value, now))
		})
	}
}
