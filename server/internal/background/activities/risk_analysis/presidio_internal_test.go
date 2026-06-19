package risk_analysis

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v3"
)

// TestConvertPresidioFindings_FiltersIPv6Unspecified verifies that the
// IPv6 unspecified address (`::` and its all-zero variants) — which
// Presidio aggressively flags wherever it appears — is dropped before
// becoming a finding. Real IPv6 addresses still flow through.
func TestConvertPresidioFindings_FiltersIPv6Unspecified(t *testing.T) {
	t.Parallel()

	text := ":: and ::0 and dead::beef"
	results := []presidioResult{
		{EntityType: "IP_ADDRESS", Start: 0, End: 2, Score: 0.9},   // "::"
		{EntityType: "IP_ADDRESS", Start: 7, End: 10, Score: 0.9},  // "::0"
		{EntityType: "IP_ADDRESS", Start: 15, End: 25, Score: 0.9}, // "dead::beef"
	}

	findings := convertPresidioFindings(text, results)
	require.Len(t, findings, 1, "only the real IPv6 address should survive the filter")
	assert.Equal(t, "dead::beef", findings[0].Match)
}

// TestIsPresidioFalsePositive_CorpusAllFiltered is the canonical
// positive-coverage gate: every IP in testdata/fp-ip.txt is an address
// the catalog must drop. Each line is run through
// isPresidioFalsePositive; any miss is a regression. The corpus is
// hand-curated — extend it by adding IPs (one per line, sorted) that
// surface as false positives during catalog tuning. Real residential
// IPs (PII) are never added to the corpus or otherwise committed.
func TestIsPresidioFalsePositive_CorpusAllFiltered(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/fp-ip.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var checked int
	for sc.Scan() {
		ip := strings.TrimSpace(sc.Text())
		if ip == "" || strings.HasPrefix(ip, "#") {
			continue
		}
		assert.True(t, isPresidioFalsePositive("IP_ADDRESS", ip),
			"corpus IP %q must be filtered", ip)
		checked++
	}
	require.NoError(t, sc.Err())
	assert.Positive(t, checked, "fp-ip.txt corpus must not be empty")
}

// TestIsPresidioFalsePositive_EmailCorpusAllFiltered is the email
// twin of TestIsPresidioFalsePositive_CorpusAllFiltered. Every line in
// testdata/fp-email.txt is a candidate the catalog must drop. The
// corpus is hand-curated from production EMAIL_ADDRESS findings —
// extend it by adding emails (one per line, sorted) that surface as
// false positives during catalog tuning. Real candidate-PII emails
// are never added to the corpus or otherwise committed.
func TestIsPresidioFalsePositive_EmailCorpusAllFiltered(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/fp-email.txt")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var checked int
	for sc.Scan() {
		email := strings.TrimSpace(sc.Text())
		if email == "" || strings.HasPrefix(email, "#") {
			continue
		}
		assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", email),
			"corpus email %q must be filtered", email)
		checked++
	}
	require.NoError(t, sc.Err())
	assert.Positive(t, checked, "fp-email.txt corpus must not be empty")
}

// TestIsPresidioFalsePositive_NegativesAndEntityScope locks the two
// invariants the corpus cannot express: real consumer-ISP IPs still
// surface as PII, and the filter only fires for IP_ADDRESS findings.
func TestIsPresidioFalsePositive_NegativesAndEntityScope(t *testing.T) {
	t.Parallel()

	// Real addresses still flow through. Consumer ISP IPs identify an end
	// user and are deliberately not in the infra ASN regex, so they are
	// still treated as PII.
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "71.126.87.167"), "residential Verizon")
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "82.15.226.61"), "residential Virgin Media")
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "dead::beef"), "two-group IPv6 still real")

	// Whitespace-trimming applies to the IP_ADDRESS path.
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "  ::  "), "trimmed unspecified")

	// IP-shaped inputs that happen to land in the EMAIL_ADDRESS lane
	// fall through cleanly (neither shape matches an email rule).
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "::"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "8.8.8.8"))

	// Unknown entity types are never filtered.
	assert.False(t, isPresidioFalsePositive("PERSON", "ada@speakeasy.com"))
	assert.False(t, isPresidioFalsePositive("", "8.8.8.8"))
}

func TestIsPresidioFalsePositive_Email(t *testing.T) {
	t.Parallel()

	// Real-shape emails flow through, including the lower-confidence
	// buckets we deliberately do NOT filter so we err on the side of
	// over-reporting rather than missing PII.
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "adam@speakeasy.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "alice.brown@techstartup.io"), "generic Faker localpart on a real domain is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "chadrick_quigley52@yahoo.com"), "generic Faker localpart on a real domain is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "support@speakeasy.com"), "role alias is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "u003ealice@speakeasy.com"), "JSON-escape prefix is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "170madam@speakeasy.com"), "ANSI prefix is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "47043212+thierry-dang@users.noreply.github.com"), "github noreply is not filtered")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "git@github.com"), "ssh git pseudo-user is a known FP")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "GIT@github.com"), "case-insensitive")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "no-reply-0EWsEuUO0Gky10deUMh0Kg@mail.anthropic.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "private@privaterelay.appleid.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "BOT_TOKEN}@github.com"), "template placeholder without slash is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "npresidio|EMAIL_ADDRESS|1068|107331|walker@speakeasy.com"), "presidio log-row wrapper is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@acme.co.uk"), "placeholder SLD under an out-of-list TLD is not filtered")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@invalid.com"), "invalid.com is a real registered domain; only the .invalid TLD is RFC 6761 reserved")
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@localhost.com"), "localhost.com is a real registered domain; only the .localhost TLD is RFC 6761 reserved")

	// Image file extensions mis-shaped as TLDs — Presidio sometimes
	// extracts a bare asset filename when the leading URL prefix is
	// stripped before the slash layer fires.
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "1f615@2x.png"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "icon@2x.SVG"), "case-insensitive")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "logo@retina.jpg"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "hero@2x.jpeg"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "spinner@2x.gif"))

	// RFC 6761 reserved special-use TLDs (.example, .invalid,
	// .localhost, .test) are guaranteed not to resolve to a public
	// mailbox, regardless of SLD or subdomain depth.
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@host.test"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@host.invalid"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@host.example"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@host.localhost"))

	// Fixture / placeholder domains — the primary motivation for the
	// filter. example.com / .org, asdf.com, fake.com, nowhere.com,
	// yourorg.com, acme.com, acmecorp.com, etc., regardless of the
	// local-part. Subdomain depth is irrelevant.
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "test@example.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "TEST@EXAMPLE.COM"), "case-insensitive")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@dev.example.com"), "subdomain depth doesn't matter")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "sibling-a135@test.example.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "SuperSecret123!@db.example.com"), "any local-part still filtered")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "asdf@asdf.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "fakey@fake.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "zzzunknown@nowhere.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "you@yourorg.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "alice@acme.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "alice@acme.io"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "john.smith@acmecorp.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "sarah.chen@acmestore.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user@test.com"), "test.com is technically real but every match in the production corpus is fixture noise")

	// KV / env / config wrappers are NOT filtered: they usually wrap
	// real production emails, so dropping them would mask PII.
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "DB_USERNAME=adam@speakeasy.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "identity=adam@speakeasy.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "user=david@speakeasyapi.dev"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "smtp.mailfrom=mail@hgstrust.org"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "nCLAUDE_CODE_USER_EMAIL=ecorella@moonpay.com"))

	// GCP service accounts are NOT filtered: the `@…gserviceaccount.com`
	// shape can carry IAM context worth flagging on first review, so we
	// over-report rather than drop the bucket wholesale.
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "argocd-image-updater@moonpay-sre.iam.gserviceaccount.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "{project_number}@cloudbuild.gserviceaccount.com"))

	// Any '/' makes the string a URL or path, not an addr-spec.
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "medium.com/@abdelghani.alhijawi"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "mail.google.com/mail/u/adamjamesbull@googlemail.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "iam.googleapis.com/projects/-/serviceAccounts/privacy@moonpay-prod.iam.gserviceaccount.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "a.slack-edge.com/production-standard-emoji-assets/15.0/apple-medium/1f4a1@2x.png"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "github.com/GoogleCloudPlatform/cloudsql-proxy/cmd/cloud_sql_proxy@v1.37.6"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "honnef.co/go/tools/cmd/staticcheck@v0.7.0"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "cloud.google.com/go/storage@v1.62.1"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "deno.land/x/zod@v3.21.4"))

	// Domain ends in a digit → version suffix on a slashless path
	// (TLDs are always letters per IANA).
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "go.opentelemetry.io/otel/sdk@v1.43.0"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "pkg@v1.2.3"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "react@18.3.1"))

	// Template-style local-parts and universally automated aliases that
	// can never identify a real person, even on real-shape domains.
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "first.last@company.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "First.Last@company.com"), "case-insensitive")
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "firstname.lastname@company.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "noreply@speakeasy.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "no-reply@speakeasy.com"))
	assert.True(t, isPresidioFalsePositive("EMAIL_ADDRESS", "NoReply@somewhere.io"), "case-insensitive")

	// Canonical placeholder person names are NOT filtered: real people
	// share these names so we accept the corpus noise to avoid the
	// over-filter risk.
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "john.doe@gmail.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "jane.doe@gmail.com"))
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "joe.bloggs@somewhere.co.uk"))
}

// TestIsPresidioFalsePositive_EquivalentIPFormsMatch verifies that
// catalogued exact IPs are caught regardless of how Presidio happens to
// spell them. The exact lookup canonicalises via netip before matching,
// so expanded zero-groups and uppercase hex resolve to the same entry as
// the curated key.
func TestIsPresidioFalsePositive_EquivalentIPFormsMatch(t *testing.T) {
	t.Parallel()

	// Cloudflare 2606:4700:4700::1111 spelled with explicit zero groups.
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "2606:4700:4700:0:0:0:0:1111"), "compressed-zero variant")
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "2606:4700:4700:0000:0000:0000:0000:1111"), "fully-expanded variant")
	// Google 2001:4860:4860::8888 with explicit zero groups.
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "2001:4860:4860:0:0:0:0:8888"), "Google DNS expanded")
	// AdGuard 2a10:50c0::bad1:ff in uppercase hex.
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "2A10:50C0::BAD1:FF"), "uppercase hex variant")
}

func TestStubPIIScannerReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	results, err := (&StubPIIScanner{}).AnalyzeBatch(t.Context(), []string{"one", "two"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, findings := range results {
		assert.Empty(t, findings)
	}
}

// TestPresidioClientShortCircuitsOnAllEmptyTexts asserts the client skips the
// HTTP round-trip (and the byte semaphore) when every input is the empty
// string — Presidio would either 500 or return no findings, so the work is
// wasted.
func TestPresidioClientShortCircuitsOnAllEmptyTexts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClient(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))

	results, err := client.AnalyzeBatch(t.Context(), []string{"", "", ""}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, r := range results {
		assert.Empty(t, r)
	}
	assert.Equal(t, int64(0), calls.Load(), "presidio /analyze must not be called when every input is empty")
}

// TestAnalyzeOnceSingleAttemptNoRetry asserts the inner single-attempt
// method does NOT retry on failure — retry lives one level up in
// analyzeOne. This is the test that justifies keeping analyzeOnce as a
// separate seam under analyzeOne.
func TestAnalyzeOnceSingleAttemptNoRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "presidio down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	_, err := client.analyzeOnce(t.Context(), "one", nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "presidio returned status 503")
	assert.Equal(t, int64(1), calls.Load(), "analyzeOnce must not retry internally")
}

// TestAnalyzeOnceRequestPayload confirms the inner single-attempt method
// emits one /analyze POST carrying the text in a one-element array and
// passes the requested entities + language through verbatim.
func TestAnalyzeOnceRequestPayload(t *testing.T) {
	t.Parallel()

	var got presidioRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	_, err := client.analyzeOnce(t.Context(), "alpha", []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, got.Text)
	assert.Equal(t, []string{"EMAIL_ADDRESS"}, got.Entities)
	assert.Equal(t, "en", got.Language)
}

// TestPresidioClientThrottleFiresHeartbeatWhileBlocked drains the byte budget
// before issuing the request so AnalyzeBatch must spin in the throttle wait
// loop. The test asserts that onProgress fires before the request unblocks.
func TestPresidioClientThrottleFiresHeartbeatWhileBlocked(t *testing.T) {
	t.Parallel()

	var serverHit atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHit.Add(1)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	// Shrink the throttle so the test deterministically blocks on a tiny
	// payload, and tighten the heartbeat interval so onProgress fires fast.
	client.throttle = semaphore.NewWeighted(4)
	client.throttleBudget = 4
	client.throttleHeartbeat = 5 * time.Millisecond

	// Hold the entire budget so the AnalyzeBatch call cannot acquire.
	require.True(t, client.throttle.TryAcquire(4))

	var progress atomic.Int64
	callResult := make(chan callOutcome, 1)

	go func() {
		results, err := client.AnalyzeBatch(t.Context(), []string{"hello"}, nil, func() {
			progress.Add(1)
		})
		callResult <- callOutcome{results: results, err: err}
	}()

	require.Eventually(t, func() bool { return progress.Load() >= 2 }, time.Second, 5*time.Millisecond,
		"onProgress did not fire while waiting on the byte semaphore")

	client.throttle.Release(4)

	outcome := <-callResult
	require.NoError(t, outcome.err)
	require.Len(t, outcome.results, 1)
	assert.Equal(t, int64(1), serverHit.Load(), "expected exactly one HTTP request after throttle release")
}

type callOutcome struct {
	results [][]Finding
	err     error
}

// TestPresidioClientRetriesThenSucceeds verifies the per-text retry budget
// is honored and the scanner returns real findings once Presidio recovers.
func TestPresidioClientRetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 2 {
			http.Error(w, "presidio down", http.StatusServiceUnavailable)
			return
		}
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		results := make([][]presidioResult, len(req.Text))
		for i, text := range req.Text {
			if idx := strings.Index(text, "alice@globex.com"); idx >= 0 {
				results[i] = []presidioResult{{
					EntityType: "EMAIL_ADDRESS",
					Start:      idx,
					End:        idx + len("alice@globex.com"),
					Score:      1,
				}}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(results))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"contact alice@globex.com"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)
	assert.Equal(t, "alice@globex.com", results[0][0].Match)
	assert.Empty(t, results[0][0].DeadLetterReason)
	assert.GreaterOrEqual(t, hits.Load(), int64(2), "expected at least one retry before success")
}

// TestPresidioClientDeadLettersAfterExhausting validates the retry budget
// emits a DL sentinel after maxAttempts failures rather than surfacing the
// error to the caller. Logs the per-text size so post-incident triage can
// recover what failed.
func TestPresidioClientDeadLettersAfterExhausting(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "still down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"will be dead-lettered"}, nil, nil)
	require.NoError(t, err, "per-text failures must not bubble up as activity-layer errors")
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)

	dl := results[0][0]
	assert.Equal(t, SourcePresidio, dl.Source)
	assert.Equal(t, DeadLetterRuleID, dl.RuleID)
	assert.NotEmpty(t, dl.DeadLetterReason)
	assert.Equal(t, int64(retryMaxAttempts), hits.Load())
}

// TestPresidioClientIsolatesPoisonedMessages confirms that a single
// poisoned message dead-letters without affecting its batch siblings — the
// failure mode the old bisecting client could not cleanly handle.
func TestPresidioClientIsolatesPoisonedMessages(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		// Every call carries exactly one text under the per-message fan-out.
		assert.Len(t, req.Text, 1)
		if len(req.Text) > 0 && req.Text[0] == "poison" {
			http.Error(w, "poison rejected", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(make([][]presidioResult, len(req.Text))))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"clean a", "poison", "clean b"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.Empty(t, results[0])
	assert.Empty(t, results[2])

	require.Len(t, results[1], 1)
	assert.Equal(t, SourcePresidio, results[1][0].Source)
	assert.Equal(t, DeadLetterRuleID, results[1][0].RuleID)
	assert.NotEmpty(t, results[1][0].DeadLetterReason)
}

// TestPresidioClientSurfacesOuterContextCancellation asserts that an
// outer-ctx cancellation aborts cleanly and returns an error so the Temporal
// activity layer can retry the whole batch rather than treating partial
// results as final.
func TestPresidioClientSurfacesOuterContextCancellation(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	client := newTestPresidioClient(t, srv.URL)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before the call so the first ctx.Err() check trips

	_, err := client.AnalyzeBatch(ctx, []string{"hang"}, nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

// TestPresidioClientDeadLettersOnPerRequestTimeout confirms that transient
// inner-timeouts consume the retry budget rather than bailing early — once
// exhausted the message dead-letters with the underlying deadline-exceeded
// error captured.
func TestPresidioClientDeadLettersOnPerRequestTimeout(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	client := newTestPresidioClient(t, srv.URL)
	// Shrink per-request timeout so the test exercises the retry path
	// without waiting out the 30s production default.
	client.requestTimeout = 30 * time.Millisecond

	results, err := client.AnalyzeBatch(t.Context(), []string{"hang"}, nil, nil)
	require.NoError(t, err, "inner per-request timeouts must not bubble up as activity-layer errors")
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)

	dl := results[0][0]
	assert.Equal(t, SourcePresidio, dl.Source)
	assert.Equal(t, DeadLetterRuleID, dl.RuleID)
	assert.NotEmpty(t, dl.DeadLetterReason)
}

// TestComputeRetryBackoffStaysWithinCap asserts the jittered exponential
// backoff is bounded so a stuck Presidio can't blow the activity heartbeat.
func TestComputeRetryBackoffStaysWithinCap(t *testing.T) {
	t.Parallel()

	for attempt := range 10 {
		got := computeRetryBackoff(50*time.Millisecond, attempt)
		assert.GreaterOrEqual(t, got, time.Duration(0))
		assert.LessOrEqual(t, got, retryMaxBackoff)
	}
	assert.Zero(t, computeRetryBackoff(0, 5))
}

// TestRequestByteCostCapsToBudget guards the deadlock-avoidance branch: a
// fresh client whose semaphore has the full budget free must still be able
// to issue a single-message request larger than the budget.
func TestRequestByteCostCapsToBudget(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", presidioMaxMessageBytes*2)
	assert.Equal(t, int64(presidioMaxMessageBytes), requestByteCost(big, presidioMaxMessageBytes))
	assert.Equal(t, int64(1), requestByteCost("", presidioMaxMessageBytes))
	assert.Equal(t, int64(4), requestByteCost("defg", presidioMaxMessageBytes))
}

// TestPresidioClientTruncatesOversizedMessages confirms the client clips a
// message larger than presidioMaxMessageBytes to a UTF-8 boundary before
// sending so a single fat blob can't crash Presidio (1 MB payloads have
// been observed to kill the analyzer). The truncated payload is what
// reaches the server; the original size is captured in logs/metrics.
func TestPresidioClientTruncatesOversizedMessages(t *testing.T) {
	t.Parallel()

	var receivedSize int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if assert.Len(t, req.Text, 1) {
			receivedSize = len(req.Text[0])
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	// Build an input that is double the limit and contains a multibyte rune
	// straddling the truncation point so we exercise the UTF-8 walk-back.
	body := strings.Repeat("a", presidioMaxMessageBytes-1) + "€" + strings.Repeat("b", presidioMaxMessageBytes)
	_, err := client.AnalyzeBatch(t.Context(), []string{body}, nil, nil)
	require.NoError(t, err)

	// Truncation walks back to a rune start, so we land strictly inside the
	// cap (the "€" occupies 3 bytes starting at presidioMaxMessageBytes-1).
	assert.LessOrEqual(t, receivedSize, presidioMaxMessageBytes)
	assert.Greater(t, receivedSize, presidioMaxMessageBytes-4, "expected truncation near the cap, not further back")
}

func TestTruncateAtRuneBoundaryHandlesMultibyte(t *testing.T) {
	t.Parallel()

	// "€" is 3 bytes in UTF-8. Cap at the middle byte: walk-back must land
	// before the rune starts so the suffix is well-formed UTF-8.
	in := "aa€bb"
	assert.Equal(t, "aa", truncateAtRuneBoundary(in, 3))
	assert.Equal(t, "aa", truncateAtRuneBoundary(in, 4))
	assert.Equal(t, "aa€", truncateAtRuneBoundary(in, 5))
	assert.Equal(t, in, truncateAtRuneBoundary(in, 100))
	assert.Empty(t, truncateAtRuneBoundary(in, 0))
}

// TestFilterEntitiesDropsBlacklistedTypes asserts that PERSON and
// US_DRIVER_LICENSE are stripped from any caller-supplied entity list
// before reaching Presidio. PERSON is dropped because NER trips on
// capitalized words inside tool calls; US_DRIVER_LICENSE because the
// upstream regex is unusably broad (microsoft/presidio#1063).
func TestFilterEntitiesDropsBlacklistedTypes(t *testing.T) {
	t.Parallel()

	// nil passes through untouched so Presidio's default entity set still applies.
	assert.Nil(t, filterEntities(nil))

	// Blacklisted entries are removed; the rest survive in order.
	got := filterEntities([]string{"EMAIL_ADDRESS", "PERSON", "US_DRIVER_LICENSE", "CREDIT_CARD"})
	assert.Equal(t, []string{"EMAIL_ADDRESS", "CREDIT_CARD"}, got)

	// All-blacklisted input returns an empty (non-nil) slice so AnalyzeBatch
	// can short-circuit instead of falling back to the unbounded default scan.
	got = filterEntities([]string{"PERSON", "US_DRIVER_LICENSE"})
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestIsCancelErrClassifiesContextErrors(t *testing.T) {
	t.Parallel()

	assert.True(t, isCancelErr(context.Canceled))
	assert.True(t, isCancelErr(context.DeadlineExceeded))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.Canceled)))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
	assert.False(t, isCancelErr(nil))
	assert.False(t, isCancelErr(errors.New("presidio returned status 500")))
}

// TestReformatJSONAsYAML_LeavesNonJSONUntouched documents the safety
// fallback: anything we cannot decode as a JSON value flows through to
// Presidio verbatim so plain prose and pre-formatted snippets are still
// scanned.
func TestReformatJSONAsYAML_LeavesNonJSONUntouched(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"hello world",
		"not { json",
		"  leading whitespace and trailing garbage }",
	}
	for _, in := range cases {
		assert.Equal(t, in, reformatJSONAsYAML(in))
	}
}

// TestReformatJSONAsYAML_EscapedNewlinesBecomeLiteralBlock is the
// regression test for POC-58: JSON-encoded multiline strings produce
// `\n` and similar escapes that Presidio's pattern-based recognizers
// (US_DRIVER_LICENSE in particular) misread as license numbers. The
// YAML literal-block form replaces those escapes with real newlines.
func TestReformatJSONAsYAML_EscapedNewlinesBecomeLiteralBlock(t *testing.T) {
	t.Parallel()

	in := `{"message":"first line\nsecond line\nthird line"}`
	out := reformatJSONAsYAML(in)

	assert.Contains(t, out, "|", "multiline strings should be emitted as a YAML literal block scalar")
	assert.NotContains(t, out, `\n`, "JSON newline escapes must not survive into the Presidio payload")
	for _, line := range []string{"first line", "second line", "third line"} {
		assert.Contains(t, out, line)
	}
	assert.GreaterOrEqual(t, strings.Count(out, "\n"), 3, "literal block should preserve real newlines between lines")
}

// TestReformatJSONAsYAML_PreservesStructureAndValues confirms that
// non-string JSON values (numbers, booleans, null, nested arrays and
// objects) round-trip through the converter into a YAML document that
// decodes back to the same data.
func TestReformatJSONAsYAML_PreservesStructureAndValues(t *testing.T) {
	t.Parallel()

	in := `{"a":1,"b":1.5,"c":true,"d":null,"e":["x","y"],"f":{"nested":"value"}}`
	out := reformatJSONAsYAML(in)

	var roundTrip map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(out), &roundTrip))
	assert.Equal(t, 1, roundTrip["a"])
	assert.InEpsilon(t, 1.5, roundTrip["b"], 1e-9)
	assert.Equal(t, true, roundTrip["c"])
	assert.Nil(t, roundTrip["d"])
	assert.Equal(t, []any{"x", "y"}, roundTrip["e"])
	assert.Equal(t, map[string]any{"nested": "value"}, roundTrip["f"])
}

// TestReformatJSONAsYAML_PreservesTrailingBytes guards against
// json.Decoder.Decode silently discarding bytes after the first JSON value:
// for mixed prefix-JSON-then-prose inputs we must still hand the trailing
// portion to Presidio so PII outside the JSON envelope is not dropped.
func TestReformatJSONAsYAML_PreservesTrailingBytes(t *testing.T) {
	t.Parallel()

	in := `{"a":1} contact sarah@example.com for details`
	out := reformatJSONAsYAML(in)

	assert.Contains(t, out, "a: 1", "JSON prefix should still be reformatted as YAML")
	assert.Contains(t, out, "sarah@example.com", "trailing bytes must survive into the Presidio payload")
}

// TestReformatJSONAsYAML_SortsMapKeys makes the output deterministic so
// repeated scans of the same payload produce stable Presidio offsets.
func TestReformatJSONAsYAML_SortsMapKeys(t *testing.T) {
	t.Parallel()

	in := `{"zebra":"z","apple":"a","mango":"m"}`
	out := reformatJSONAsYAML(in)

	appleIdx := strings.Index(out, "apple:")
	mangoIdx := strings.Index(out, "mango:")
	zebraIdx := strings.Index(out, "zebra:")
	require.GreaterOrEqual(t, appleIdx, 0)
	require.GreaterOrEqual(t, mangoIdx, 0)
	require.GreaterOrEqual(t, zebraIdx, 0)
	assert.Less(t, appleIdx, mangoIdx)
	assert.Less(t, mangoIdx, zebraIdx)
}

// TestAnalyzeOncePayloadIsYAMLForJSONInput is the end-to-end assertion
// that the client converts JSON bodies before handing them to Presidio.
// We send a JSON object with a multiline string and confirm the wire
// payload no longer contains the raw `\n` escape sequence.
func TestAnalyzeOncePayloadIsYAMLForJSONInput(t *testing.T) {
	t.Parallel()

	var got presidioRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	in := `{"body":"line one\nline two"}`
	_, err := client.AnalyzeBatch(t.Context(), []string{in}, nil, nil)
	require.NoError(t, err)

	require.Len(t, got.Text, 1)
	wire := got.Text[0]
	assert.NotContains(t, wire, `\n`, "JSON newline escapes must not reach Presidio")
	assert.Contains(t, wire, "body: |", "object key should be followed by a literal-block scalar marker")
	assert.Contains(t, wire, "line one")
	assert.Contains(t, wire, "line two")
}

// --- helpers ---

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), nil))
}

func newTestPresidioClient(t *testing.T, baseURL string) *PresidioClient {
	t.Helper()
	client := NewPresidioClient(baseURL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))
	// Zero backoff keeps tests deterministic; retry budget stays at the
	// production default so retry-related assertions stay representative.
	client.baseBackoff = 0
	return client
}
