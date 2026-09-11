package mcp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// The verdict never quotes the member: a rejection on either leg wins, then the deadline, then the last status seen.
func TestClassifyProbe(t *testing.T) {
	t.Parallel()

	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	transport := &metaMemberError{message: `server "linear" did not answer: <body>`}

	cases := []struct {
		name       string
		expired    bool
		rejected   int
		lastStatus int
		err        error
		outcome    remotesessions.ValidationOutcome
		reason     string
	}{
		{name: "401 on connect", expired: false, rejected: http.StatusUnauthorized, lastStatus: http.StatusUnauthorized, err: transport, outcome: remotesessions.ValidationOutcomeRejectedByMember, reason: "Rejected by linear"},
		{name: "403 on list after a good handshake", expired: false, rejected: http.StatusForbidden, lastStatus: http.StatusForbidden, err: transport, outcome: remotesessions.ValidationOutcomeRejectedByMember, reason: "Rejected by linear"},
		{name: "rejection outranks the deadline", expired: true, rejected: http.StatusUnauthorized, lastStatus: http.StatusUnauthorized, err: context.DeadlineExceeded, outcome: remotesessions.ValidationOutcomeRejectedByMember, reason: "Rejected by linear"},
		{name: "deadline", expired: true, rejected: 0, lastStatus: http.StatusOK, err: context.DeadlineExceeded, outcome: remotesessions.ValidationOutcomeUnknown, reason: "linear did not answer in time"},
		{name: "nothing answered", expired: false, rejected: 0, lastStatus: 0, err: transport, outcome: remotesessions.ValidationOutcomeUnknown, reason: "Could not reach linear"},
		{name: "503", expired: false, rejected: 0, lastStatus: http.StatusServiceUnavailable, err: transport, outcome: remotesessions.ValidationOutcomeUnknown, reason: "linear answered with status 503"},
		{name: "404", expired: false, rejected: 0, lastStatus: http.StatusNotFound, err: transport, outcome: remotesessions.ValidationOutcomeUnknown, reason: "linear answered with status 404"},
		{name: "200 that is not a result", expired: false, rejected: 0, lastStatus: http.StatusOK, err: errors.New("decode: <body>"), outcome: remotesessions.ValidationOutcomeUnknown, reason: "Unexpected answer from linear"},
		{name: "too large", expired: false, rejected: 0, lastStatus: 0, err: errProbeResponseTooLarge, outcome: remotesessions.ValidationOutcomeUnknown, reason: "Unexpected answer from linear"},
	}
	for _, tc := range cases {
		ctx := context.Background()
		if tc.expired {
			ctx = expired
		}
		rt := &memberRoundTripper{build: nil, logger: nil, deadline: time.Time{}, closeFloor: 0, mu: sync.Mutex{}, rejected: tc.rejected, lastStatus: tc.lastStatus}
		outcome, reason := classifyProbe(ctx, rt, tc.err, "linear")
		require.Equal(t, tc.outcome, outcome, tc.name)
		require.Equal(t, tc.reason, reason, tc.name)
		require.NotContains(t, reason, "<body>", tc.name)
	}
}

func TestFormatTimeAgo(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "just now", formatTimeAgo(now, now))
	require.Equal(t, "just now", formatTimeAgo(now, now.Add(-30*time.Second)))
	require.Equal(t, "1 minute ago", formatTimeAgo(now, now.Add(-time.Minute)))
	require.Equal(t, "3 minutes ago", formatTimeAgo(now, now.Add(-3*time.Minute)))
	require.Equal(t, "2 hours 5 minutes ago", formatTimeAgo(now, now.Add(-2*time.Hour-5*time.Minute)))
	require.Equal(t, "1 day ago", formatTimeAgo(now, now.Add(-24*time.Hour)))
	// Clock skew: a timestamp from the future never renders as expired.
	require.Equal(t, "just now", formatTimeAgo(now, now.Add(5*time.Minute)))
	require.Equal(t, "just now", formatTimeAgo(now, now.Add(48*time.Hour)))
}

// An ambiguous member credential is a configuration state, not a probe result.
func TestAmbiguousMemberCredentialStaysMemberScoped(t *testing.T) {
	t.Parallel()

	_, err := routeMetaMemberToken(map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "a", Resource: "https://one.example.com/mcp", RemoteSessionClientID: uuid.Nil},
		uuid.New(): {Token: "b", Resource: "https://one.example.com/mcp", RemoteSessionClientID: uuid.Nil},
	}, metaMember{slug: "linear"}, "https://one.example.com/mcp")
	require.ErrorIs(t, err, errAmbiguousMemberCredential)
	memberErr, ok := errors.AsType[*metaMemberError](err)
	require.True(t, ok, "dispatch still renders it member-scoped")
	require.Contains(t, memberErr.Error(), `server "linear" has 2 upstream credentials`)
}

func TestConsentTemplateValidationStates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		card                  remoteSessionCard
		contains, notContains []string
	}{
		{name: "notice", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationNotice: validationLimitedNotice, CanValidate: true}, contains: []string{`data-validation="valid"`, `data-validation-notice >Try again in a moment`, `data-validate-link > Verify`}},
		{name: "verified", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "3 minutes ago", CanValidate: true}, contains: []string{`data-validation="valid"`, `Verified <time datetime="2026-09-08T12:00:00Z">3 minutes ago</time>`, `value="validate"`, `data-validate-link > Verify`}, notContains: []string{"Reconnect", "Not yet verified"}},
		{name: "rejected", card: remoteSessionCard{Connected: true, Rejected: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationReason: "Rejected by linear", CanValidate: true}, contains: []string{`data-validation="rejected"`, "Rejected by linear, reconnect", "Connected", `data-connect-link > Reconnect`, `data-validate-link > Verify`}, notContains: []string{"Verified"}},
		{name: "identity and validation reconnect", card: remoteSessionCard{Connected: true, Rejected: true, IdentityReconnect: true}, contains: []string{`data-connect-link > Reconnect`}},
		{name: "unknown", card: remoteSessionCard{Connected: true, Unverified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "5 minutes ago", ValidationReason: "linear did not answer in time", CanValidate: true}, contains: []string{`data-validation="unknown"`, `linear did not answer in time · checked <time datetime="2026-09-08T12:00:00Z">5 minutes ago</time>`}, notContains: []string{"Reconnect"}},
		{name: "never validated", card: remoteSessionCard{Connected: true, CanValidate: true}, contains: []string{`data-validation="none"`, "Not yet verified", `data-validate-link > Verify`}},
		{name: "unprobeable", card: remoteSessionCard{Connected: true}, notContains: []string{"data-validation", "Verify"}},
		{name: "disconnected", card: remoteSessionCard{CanValidate: true}, notContains: []string{"Verify"}},
		{name: "escaped reason", card: remoteSessionCard{Connected: true, Rejected: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationReason: `Rejected by <script>alert("x")</script>`, CanValidate: true}, contains: []string{"Rejected by &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;, reconnect"}, notContains: []string{"<script>alert"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html := renderValidationCard(t, tc.card)
			require.LessOrEqual(t, strings.Count(html, "data-connect-link"), 1, "reconnect reasons share one control")
			for _, text := range tc.contains {
				require.Contains(t, html, text)
			}
			for _, text := range tc.notContains {
				require.NotContains(t, html, text)
			}
		})
	}
}

func renderValidationCard(t *testing.T, card remoteSessionCard) string {
	t.Helper()
	card.ClientID = "client-id"
	card.IssuerSlug = "example-issuer"
	card.IssuerDisplay = "example-issuer"
	var page bytes.Buffer
	err := consentTemplate.Execute(&page, consentTemplateData{
		ClientName:         "Gram",
		MCPSlug:            "example",
		MCPRouteBase:       "mcp",
		State:              "state",
		CSRFToken:          "csrf",
		SubjectDisplay:     "user@example.com",
		ScriptURL:          "/mcp/consent-page-test.js",
		RemoteSessionCards: []remoteSessionCard{card},
		ConsentEnabled:     true,
		FirstParty:         true,
	})
	require.NoError(t, err)
	return normalizeWhitespace(page.String())
}
