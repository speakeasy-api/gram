package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestRejectSurvivingClientCredentialRejection_NamesAdministrator(t *testing.T) {
	t.Parallel()

	priorCalls := 0
	p := &proxy.Proxy{UpstreamResponseInterceptor: func(context.Context, *http.Response) error {
		priorCalls++
		return nil
	}}
	rejectSurvivingClientCredentialRejection(httptest.NewRecorder(), p, testenv.NewLogger(t), remotesessions.UpstreamToken{Token: "self-token", CredentialOwner: remotesessions.CredentialOwnerSelf}, nil)

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		err := p.UpstreamResponseInterceptor(t.Context(), &http.Response{StatusCode: status})
		shareable, ok := errors.AsType[*oops.ShareableError](err)
		require.True(t, ok, "status %d error: %v", status, err)
		require.Equal(t, oops.CodeFailedPrecondition, shareable.Code)
		require.Contains(t, shareable.Error(), "Contact the MCP server administrator")
	}

	require.NoError(t, p.UpstreamResponseInterceptor(t.Context(), &http.Response{StatusCode: http.StatusOK}))
	require.Equal(t, 3, priorCalls, "the proxy's own interceptor still sees every response")
}

func TestRejectSurvivingClientCredentialRejection_FailedRenewalAsksForRetry(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	p := &proxy.Proxy{}
	renewal := &clientCredentialRenewal{err: remotesessions.ErrClientCredentialUnavailable}
	rejectSurvivingClientCredentialRejection(w, p, testenv.NewLogger(t), remotesessions.UpstreamToken{Token: "self-token", CredentialOwner: remotesessions.CredentialOwnerSelf}, renewal)

	err := p.UpstreamResponseInterceptor(t.Context(), &http.Response{StatusCode: http.StatusUnauthorized})
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "error: %v", err)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestRejectSurvivingClientCredentialRejection_LeavesSubjectCredential(t *testing.T) {
	t.Parallel()

	p := &proxy.Proxy{}
	rejectSurvivingClientCredentialRejection(httptest.NewRecorder(), p, testenv.NewLogger(t), remotesessions.UpstreamToken{Token: "subject-token", CredentialOwner: remotesessions.CredentialOwnerSubject}, nil)
	require.Nil(t, p.UpstreamResponseInterceptor, "a subject's 401 keeps relaying with the reconnect challenge")
}

func TestSubjectConnectedClients_DropsSelfClients(t *testing.T) {
	t.Parallel()

	subject := remotesessions.Client{ID: uuid.New(), CredentialOwner: remotesessions.CredentialOwnerSubject}
	clients := subjectConnectedClients([]remotesessions.Client{
		subject,
		{ID: uuid.New(), CredentialOwner: remotesessions.CredentialOwnerSelf},
	})
	require.Equal(t, []remotesessions.Client{subject}, clients)
}
