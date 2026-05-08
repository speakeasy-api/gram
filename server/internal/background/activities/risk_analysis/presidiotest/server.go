// Package presidiotest provides an in-process mock of the Presidio Analyzer
// HTTP API. It speaks the same wire protocol as the real
// mcr.microsoft.com/presidio-analyzer image but performs deterministic regex
// based detection for the entity types Gram cares about. Tests get sub-second
// startup and stable output instead of waiting on the ML container to boot.
package presidiotest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Result mirrors a single entity returned by the Presidio analyzer over the
// wire. Exported so tests that override the detector can construct results
// directly.
type Result struct {
	EntityType string  `json:"entity_type"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
	Score      float64 `json:"score"`
}

// Detector returns the entities Presidio would report for a single text.
// When entities is empty, the detector should return its full default set.
// When non-empty, it should return only entities in the allow-list.
type Detector func(text string, entities []string) []Result

// MockServer is an in-process HTTP server that responds on the Presidio
// Analyzer endpoints (/health, /analyze). The default detector recognises
// EMAIL_ADDRESS, CREDIT_CARD, PHONE_NUMBER, URL, and PERSON via regex; tests
// can swap it out with SetDetector for failure-mode coverage.
type MockServer struct {
	logger      *slog.Logger
	srv         *httptest.Server
	mu          sync.RWMutex
	detector    Detector
	analyzeReqs atomic.Int64
}

// NewMockServer starts an httptest server bound to a random localhost port
// and registers t.Cleanup to shut it down at the end of the test. Pass
// nil for testing.TB outside of a test (e.g. testenv.Launch) and call Close
// manually.
func NewMockServer(t testing.TB, logger *slog.Logger) *MockServer {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	m := &MockServer{
		logger:   logger,
		detector: DefaultDetector,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", m.handleHealth)
	mux.HandleFunc("POST /analyze", m.handleAnalyze)
	m.srv = httptest.NewServer(mux)

	if t != nil {
		t.Cleanup(m.Close)
	}

	return m
}

// URL returns the base URL of the mock server, suitable for passing to
// risk_analysis.NewPresidioClient.
func (m *MockServer) URL() string {
	return m.srv.URL
}

// ParsedURL returns URL() pre-parsed for callers that need a *url.URL.
func (m *MockServer) ParsedURL() *url.URL {
	u, err := url.Parse(m.srv.URL)
	if err != nil {
		panic(fmt.Sprintf("presidiotest: parse server URL: %v", err))
	}
	return u
}

// Close shuts the underlying httptest server down. Safe to call multiple
// times; the second call is a no-op.
func (m *MockServer) Close() {
	m.srv.Close()
}

// SetDetector overrides the default detector. Useful for tests that want to
// simulate specific Presidio responses or failure modes.
func (m *MockServer) SetDetector(d Detector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d == nil {
		d = DefaultDetector
	}
	m.detector = d
}

// AnalyzeRequestCount returns the number of /analyze requests served. Useful
// for tests that want to assert the client batched as expected.
func (m *MockServer) AnalyzeRequestCount() int64 {
	return m.analyzeReqs.Load()
}

func (m *MockServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Presidio Analyzer service is up"))
}

type analyzeRequest struct {
	Text     []string `json:"text"`
	Language string   `json:"language"`
	ScoreMin float64  `json:"score_threshold"`
	Entities []string `json:"entities,omitempty"`
}

func (m *MockServer) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	m.analyzeReqs.Add(1)

	ctx := r.Context()

	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Real Presidio 500s on an empty text array; mirror that so client
	// short-circuit logic stays honest.
	if len(req.Text) == 0 {
		http.Error(w, "No text provided", http.StatusInternalServerError)
		return
	}

	m.mu.RLock()
	detector := m.detector
	m.mu.RUnlock()

	out := make([][]Result, len(req.Text))
	for i, text := range req.Text {
		findings := detector(text, req.Entities)
		// Apply the request's score threshold, matching real Presidio.
		filtered := findings[:0]
		for _, f := range findings {
			if f.Score >= req.ScoreMin {
				filtered = append(filtered, f)
			}
		}
		out[i] = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		m.logger.ErrorContext(ctx, "presidiotest: encode response", attr.SlogError(err))
	}
}

// DefaultDetector is the built-in deterministic detector used when callers
// don't override one. It recognises a curated set of entities chosen to
// satisfy the production code paths Gram exercises in tests:
//
//   - EMAIL_ADDRESS  matches RFC-ish local@domain.tld
//   - CREDIT_CARD    13-19 digit groups with optional separators, Luhn-valid
//   - PHONE_NUMBER   NANP triplet (3-3-4 with - or space) or +CC prefix
//   - URL            http(s) URLs
//   - PERSON         two adjacent capitalised words (mirrors NER false-positive
//     prone behaviour of real Presidio)
//
// Score values are picked to roughly match what real Presidio returns so that
// client-side confidence thresholds behave the same way.
func DefaultDetector(text string, entities []string) []Result {
	allowed := allowSet(entities)

	var out []Result
	if allowed("EMAIL_ADDRESS") {
		out = append(out, matchAll(text, emailRegex, "EMAIL_ADDRESS", 1.0)...)
	}
	if allowed("URL") {
		out = append(out, matchAll(text, urlRegex, "URL", 0.95)...)
	}
	if allowed("CREDIT_CARD") {
		out = append(out, detectCreditCards(text)...)
	}
	if allowed("PHONE_NUMBER") {
		out = append(out, detectPhoneNumbers(text)...)
	}
	if allowed("PERSON") {
		out = append(out, detectPersons(text)...)
	}
	return out
}

// allowSet returns a predicate. When entities is empty, every entity type is
// allowed; otherwise only those in the list.
func allowSet(entities []string) func(string) bool {
	if len(entities) == 0 {
		return func(string) bool { return true }
	}
	set := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		set[e] = struct{}{}
	}
	return func(e string) bool {
		_, ok := set[e]
		return ok
	}
}

var (
	emailRegex = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	urlRegex   = regexp.MustCompile(`https?://[^\s"')]+`)
	// NANP-style 3-3-4 with dash, dot, or space separators. Anchored on word
	// boundaries so version strings like "1.234.567.890" don't match.
	phoneNANPRegex = regexp.MustCompile(`\b\d{3}[-\s]\d{3}[-\s]\d{4}\b`)
	// International prefix + at least 8 digits (any spacing in between).
	phoneIntlRegex = regexp.MustCompile(`\+\d{1,3}(?:[\s\-]?\d){7,}`)
	// Two adjacent capitalised words. Matches Presidio's NER bias toward
	// over-flagging proper nouns.
	personRegex = regexp.MustCompile(`\b[A-Z][a-z]+ [A-Z][a-z]+\b`)
	// 13-19 digits with optional - or space separators. Luhn check applied.
	creditCardRegex = regexp.MustCompile(`\b(?:\d[ \-]?){12,18}\d\b`)
)

func matchAll(text string, re *regexp.Regexp, entityType string, score float64) []Result {
	idxs := re.FindAllStringIndex(text, -1)
	if len(idxs) == 0 {
		return nil
	}
	out := make([]Result, 0, len(idxs))
	for _, idx := range idxs {
		out = append(out, Result{
			EntityType: entityType,
			Start:      runeIndex(text, idx[0]),
			End:        runeIndex(text, idx[1]),
			Score:      score,
		})
	}
	return out
}

func detectCreditCards(text string) []Result {
	idxs := creditCardRegex.FindAllStringIndex(text, -1)
	if len(idxs) == 0 {
		return nil
	}
	var out []Result
	for _, idx := range idxs {
		raw := text[idx[0]:idx[1]]
		digits := stripNonDigits(raw)
		if len(digits) < 13 || len(digits) > 19 {
			continue
		}
		if !luhnValid(digits) {
			continue
		}
		out = append(out, Result{
			EntityType: "CREDIT_CARD",
			Start:      runeIndex(text, idx[0]),
			End:        runeIndex(text, idx[1]),
			Score:      1.0,
		})
	}
	return out
}

func detectPhoneNumbers(text string) []Result {
	var out []Result
	for _, idx := range phoneNANPRegex.FindAllStringIndex(text, -1) {
		out = append(out, Result{
			EntityType: "PHONE_NUMBER",
			Start:      runeIndex(text, idx[0]),
			End:        runeIndex(text, idx[1]),
			Score:      0.75,
		})
	}
	for _, idx := range phoneIntlRegex.FindAllStringIndex(text, -1) {
		out = append(out, Result{
			EntityType: "PHONE_NUMBER",
			Start:      runeIndex(text, idx[0]),
			End:        runeIndex(text, idx[1]),
			Score:      0.75,
		})
	}
	return out
}

func detectPersons(text string) []Result {
	idxs := personRegex.FindAllStringIndex(text, -1)
	if len(idxs) == 0 {
		return nil
	}
	out := make([]Result, 0, len(idxs))
	for _, idx := range idxs {
		out = append(out, Result{
			EntityType: "PERSON",
			Start:      runeIndex(text, idx[0]),
			End:        runeIndex(text, idx[1]),
			Score:      0.85,
		})
	}
	return out
}

func stripNonDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func luhnValid(digits string) bool {
	sum := 0
	parity := len(digits) % 2
	for i, r := range digits {
		d, err := strconv.Atoi(string(r))
		if err != nil {
			return false
		}
		if i%2 == parity {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// runeIndex converts a byte offset into a rune offset, matching Presidio's
// rune-based output positions. The PresidioClient converts back to bytes.
func runeIndex(s string, byteOffset int) int {
	if byteOffset >= len(s) {
		return len([]rune(s))
	}
	return len([]rune(s[:byteOffset]))
}
