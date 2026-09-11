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

// A member that accepted the token wins; otherwise an inactive introspection answer is the verdict.
// The token line follows the introspection deadline, then the stored access expiry, and goes away once either has passed.
func TestTokenLine(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	later, earlier := now.Add(time.Hour), now.Add(-time.Minute)

	active, at, in := tokenLine(now, &remotesessions.IntrospectedToken{Active: true, ExpiresAt: later}, nil)
	require.True(t, active)
	require.Equal(t, "2026-09-11T13:00:00Z", at)
	require.Equal(t, "1 hour", in)

	active, _, in = tokenLine(now, &remotesessions.IntrospectedToken{Active: true, ExpiresAt: time.Time{}}, &later)
	require.True(t, active, "the stored access expiry stands in for a missing exp")
	require.Equal(t, "1 hour", in)

	active, at, in = tokenLine(now, &remotesessions.IntrospectedToken{Active: true, ExpiresAt: time.Time{}}, nil)
	require.True(t, active)
	require.Empty(t, at)
	require.Empty(t, in)

	active, _, _ = tokenLine(now, &remotesessions.IntrospectedToken{Active: true, ExpiresAt: earlier}, nil)
	require.False(t, active, "a stale answer about an expired token is not rendered")

	active, _, _ = tokenLine(now, &remotesessions.IntrospectedToken{Active: false, ExpiresAt: later}, nil)
	require.False(t, active)
	active, _, _ = tokenLine(now, nil, &later)
	require.False(t, active)
}

func TestCombineUpstreamVerdict(t *testing.T) {
	t.Parallel()

	valid, rejected, unknown := remotesessions.ValidationOutcomeValid, remotesessions.ValidationOutcomeRejectedByMember, remotesessions.ValidationOutcomeUnknown
	cases := []struct {
		name       string
		verdict    remotesessions.ValidationOutcome
		reason     string
		inactive   bool
		want       remotesessions.ValidationOutcome
		wantReason string
	}{
		{name: "valid over inactive", verdict: valid, reason: "", inactive: true, want: valid, wantReason: ""},
		{name: "valid over active", verdict: valid, reason: "", inactive: false, want: valid, wantReason: ""},
		{name: "rejected and inactive", verdict: rejected, reason: "Rejected by member", inactive: true, want: remotesessions.ValidationOutcomeInactive, wantReason: "Inactive at linear"},
		{name: "rejected and active", verdict: rejected, reason: "Rejected by member", inactive: false, want: rejected, wantReason: "Rejected by member"},
		{name: "unknown and inactive", verdict: unknown, reason: "member did not answer in time", inactive: true, want: remotesessions.ValidationOutcomeInactive, wantReason: "Inactive at linear"},
		{name: "unknown and active or absent", verdict: unknown, reason: "member did not answer in time", inactive: false, want: unknown, wantReason: "member did not answer in time"},
	}
	for _, tc := range cases {
		got, gotReason := combineUpstreamVerdict(tc.verdict, tc.reason, remotesessions.UpstreamVerification{Inactive: tc.inactive}, "linear")
		require.Equal(t, tc.want, got, tc.name)
		require.Equal(t, tc.wantReason, gotReason, tc.name)
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
		{name: "verified", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "3 minutes ago", CanValidate: true}, contains: []string{`data-card-status >Connected<span class="text-muted-foreground" data-validation="valid" > · Verified <time datetime="2026-09-08T12:00:00Z">3 minutes ago</time></span >`, `value="validate"`, `data-validate-link > Verify`}, notContains: []string{"Reconnect", "Not yet verified", "data-card-details"}},
		{name: "rejected", card: remoteSessionCard{Connected: true, Rejected: true, CanRefresh: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationReason: "Rejected by linear", ConnectedAs: "grant-owner@example.com · Acme", TokenActive: true, TokenExpiresIn: "1 hour", RefreshExpiresIn: "29 days", AuthorizationExpiresIn: "365 days", CanValidate: true}, contains: []string{`data-validation="rejected" >Rejected by linear — reconnect to continue</span >`, `data-connect-link > Reconnect`, `aria-label="Disconnect example-issuer"`, `Authenticated as grant-owner@example.com · Acme`, `data-refresh-link`}, notContains: []string{"Verified", "data-card-status", "data-validation-reason", "data-validate-link", "data-token-line", "Lapses in", "Access ends in", "Auto refresh on."}},
		{name: "inactive", card: remoteSessionCard{Connected: true, Inactive: true, CanRefresh: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationReason: "Inactive at linear-idp", ConnectedAs: "grant-owner@example.com · Acme", TokenActive: true, TokenExpiresIn: "1 hour", RefreshExpiresIn: "29 days", AuthorizationExpiresIn: "365 days", CanValidate: true}, contains: []string{`data-validation="inactive" >Inactive at linear-idp — reconnect to continue</span >`, `data-connect-link > Reconnect`, `aria-label="Disconnect example-issuer"`, `Authenticated as grant-owner@example.com · Acme`, `data-refresh-link`}, notContains: []string{"Verified", `data-validation="rejected"`, "data-card-status", "data-validate-link", "data-validation-reason", "data-token-line", "Lapses in", "Access ends in", "Auto refresh on."}},
		{name: "rejected without identity", card: remoteSessionCard{Connected: true, Rejected: true, ValidationReason: "Rejected by linear", TokenActive: true, AuthorizationExpiresIn: "365 days", CanValidate: true}, notContains: []string{"data-card-details"}},
		{name: "expired keeps the account", card: remoteSessionCard{Expired: true, ConnectedAs: "grant-owner@example.com", TokenActive: true, AuthorizationExpiresIn: "365 days"}, contains: []string{"Expired", `Authenticated as grant-owner@example.com`, `data-connect-link > Reconnect`}, notContains: []string{"data-token-line", "Access ends in"}},
		{name: "identity and validation reconnect", card: remoteSessionCard{Connected: true, Rejected: true, IdentityReconnect: true}, contains: []string{`data-connect-link > Reconnect`, "Reconnect to add account details"}},
		{name: "unknown", card: remoteSessionCard{Connected: true, Unverified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "5 minutes ago", ValidationReason: "linear did not answer in time", CanValidate: true}, contains: []string{`data-validation="unknown" > · Checked <time datetime="2026-09-08T12:00:00Z">5 minutes ago</time>, unconfirmed</span >`, `data-card-details`, `data-validation-reason >linear did not answer in time</span >`}, notContains: []string{"Reconnect"}},
		{name: "never validated", card: remoteSessionCard{Connected: true, CanValidate: true}, contains: []string{`data-validation="none" > · Not yet verified`, `data-validate-link > Verify`}},
		{name: "unprobeable", card: remoteSessionCard{Connected: true}, notContains: []string{"data-validation", "Verify"}},
		{name: "token line from introspection", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", TokenActive: true, TokenExpiresAt: "2026-09-15T12:00:00Z", TokenExpiresIn: "6 days 23 hours", CanValidate: true}, contains: []string{`data-card-details`, `data-token-line >Token active · expires in <time datetime="2026-09-15T12:00:00Z">6 days 23 hours</time></span >`}, notContains: []string{"Authenticated as", "scope"}},
		{name: "token line without a deadline", card: remoteSessionCard{Connected: true, TokenActive: true, CanValidate: true}, contains: []string{`data-token-line >Token active</span >`}, notContains: []string{"expires in"}},
		{name: "auto refresh on line", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", AutoRefreshChecked: true, RefreshExpiresAt: "2026-10-08T12:00:00Z", RefreshExpiresIn: "29 days 23 hours", CanValidate: true}, contains: []string{`data-card-status >Connected<span`, `data-auto-refresh-line >Auto refresh on.</span >`}, notContains: []string{"Lapses in", "auto refresh on<"}},
		{name: "account context chips", card: remoteSessionCard{Connected: true, Verified: true, ConnectedAs: "Grant Owner · grant-owner@example.com", AccountChips: []string{"Acme Docs", "Acme Team", "octocat"}, CanValidate: true}, contains: []string{`Authenticated as Grant Owner · grant-owner@example.com</span >`, `data-account-context ><span class="break-words">Acme Docs</span><span class="break-words">&nbsp;·&nbsp;Acme Team</span><span class="break-words">&nbsp;·&nbsp;octocat</span></span >`}, notContains: []string{"whitespace-nowrap", `<span class="break-words"> ·`, `<span class="break-words">·`}},
		{name: "chips without identity", card: remoteSessionCard{Connected: true, Verified: true, AccountChips: []string{"Acme Docs"}, CanValidate: true}, contains: []string{`data-account-context ><span class="break-words">Acme Docs</span></span >`}, notContains: []string{"Authenticated as"}},
		{name: "identity without chips has no context line", card: remoteSessionCard{Connected: true, Verified: true, ConnectedAs: "grant-owner@example.com", CanValidate: true}, contains: []string{`Authenticated as grant-owner@example.com`}, notContains: []string{"data-account-context"}},
		{name: "details collapsed by default", card: remoteSessionCard{Connected: true, Verified: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ConnectedAs: "grant-owner@example.com · Acme Docs", RefreshExpiresAt: "2026-10-08T12:00:00Z", RefreshExpiresIn: "29 days 23 hours", AuthorizationExpiresAt: "2027-09-08T12:00:00Z", AuthorizationExpiresIn: "365 days", CanValidate: true}, contains: []string{`data-card-status >Connected<span class="text-muted-foreground" data-validation="valid" > · Verified`, `<details class="group mt-1" data-card-details> <summary`, `class="bg-accent mt-2 flex w-fit max-w-[70%] flex-col gap-1 border px-3 py-3 max-sm:w-full max-sm:max-w-full" data-card-details-panel`, `> Details <svg`, `group-open:rotate-180" data-card-details-chevron`, `data-card-details-panel> <span`, `Authenticated as grant-owner@example.com · Acme Docs`, `Lapses in <time datetime="2026-10-08T12:00:00Z" >29 days 23 hours</time > if unused.`, `Access ends in <time datetime="2027-09-08T12:00:00Z" >365 days</time >.`}, notContains: []string{`data-card-details open`}},
		{name: "disconnected", card: remoteSessionCard{CanValidate: true}, notContains: []string{"Verify"}},
		{name: "escaped reason", card: remoteSessionCard{Connected: true, Rejected: true, ValidatedAt: "2026-09-08T12:00:00Z", ValidatedAgo: "just now", ValidationReason: `Rejected by <script>alert("x")</script>`, CanValidate: true}, contains: []string{"Rejected by &lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; — reconnect to continue"}, notContains: []string{"<script>alert"}},
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

// A negative verdict does not hide Refresh: the refresh token may still be good.
func TestConsentTemplateKeepsRefreshOnNegativeVerdicts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		card remoteSessionCard
	}{
		{name: "rejected", card: remoteSessionCard{Connected: true, Rejected: true, CanRefresh: true, CanValidate: true}},
		{name: "inactive", card: remoteSessionCard{Connected: true, Inactive: true, CanRefresh: true, CanValidate: true}},
		{name: "rejected and inactive", card: remoteSessionCard{Connected: true, Rejected: true, Inactive: true, CanRefresh: true, CanValidate: true}},
		{name: "unroutable", card: remoteSessionCard{Unroutable: true, Rejected: true, CanRefresh: true, CanValidate: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			html := renderValidationCard(t, tc.card)
			require.Contains(t, html, "data-refresh-link")
			require.Contains(t, html, `data-connect-link > Reconnect`)
			require.NotContains(t, html, "data-validate-link", "Verify stays hidden until a reconnect or refresh")
			without := tc.card
			without.CanRefresh = false
			require.NotContains(t, renderValidationCard(t, without), "data-refresh-link")
		})
	}
}
