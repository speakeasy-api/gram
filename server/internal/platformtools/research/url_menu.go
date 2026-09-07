package research

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxMenuEntriesPerRun bounds one run's menu. A run's ceiling of organic
// growth is its budgets — every fetch can harvest links and every search adds
// results — so the cap is sized well above that and exists only to stop a
// pathological page (thousands of anchors) from growing the map without
// limit. Entries past the cap are dropped; the run keeps working with the
// menu it has.
const maxMenuEntriesPerRun = 2000

// URLMenu is the set of URLs a run may fetch: the model selects from it and
// can never add to it. Only trusted code writes entries — search results,
// links harvested from fetched pages, and the briefing's own URLs — so a
// fetched URL is always one the ecosystem presented, never one the model
// composed. That is the property that keeps fetch from being an exfiltration
// channel: an injected model cannot address a byte of its context to an
// attacker endpoint, only choose among endpoints trusted code already saw.
//
// Entries are keyed per run and expire with the same window as the call
// budgets, so the map stays bounded by the runs active within it.
type URLMenu struct {
	// mu guards startedAt and entries.
	mu sync.Mutex

	// startedAt records when each run's menu began, for window expiry.
	startedAt map[string]time.Time

	// entries holds each run's allowed canonical URLs.
	entries map[string]map[string]struct{}
}

// NewURLMenu builds an empty menu.
func NewURLMenu() *URLMenu {
	return &URLMenu{
		mu:        sync.Mutex{},
		startedAt: make(map[string]time.Time),
		entries:   make(map[string]map[string]struct{}),
	}
}

// CanonicalMenuURL is the single canonicalization point for menu entries and
// fetch targets. The string it returns is both what the menu stores and what
// the HTTP client is handed, byte for byte — the menu check and the request
// can never diverge on parsing, which is the classic bypass against
// allow-list designs. Fragments are stripped because they never reach the
// wire: two URLs differing only in fragment are the same request.
func CanonicalMenuURL(raw string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	// Hostname, not Host: a malformed authority like https://:443/path has a
	// non-empty Host by way of its port and no machine behind it.
	if !target.IsAbs() || target.Hostname() == "" {
		return "", fmt.Errorf("url must be absolute with a host")
	}
	if target.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q: only https pages are fetchable, because a page fetched over plaintext http is not evidence anyone can rely on", target.Scheme)
	}
	target.Fragment = ""

	return target.String(), nil
}

// Allow adds one URL to a run's menu. URLs that do not canonicalize are
// dropped silently: the sources feeding the menu (search results, harvested
// hrefs, briefing text) routinely contain relative links, mailto:, http-only
// pages and plain junk, and none of that is an error — it is simply not
// fetchable.
func (m *URLMenu) Allow(runID string, rawURL string) {
	canonical, err := CanonicalMenuURL(rawURL)
	if err != nil {
		return
	}

	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	m.expire(now)

	if _, seen := m.startedAt[runID]; !seen {
		m.startedAt[runID] = now
	}
	runEntries := m.entries[runID]
	if runEntries == nil {
		runEntries = make(map[string]struct{})
		m.entries[runID] = runEntries
	}
	if len(runEntries) >= maxMenuEntriesPerRun {
		return
	}
	runEntries[canonical] = struct{}{}
}

// Allowed reports whether a run may fetch rawURL, returning the canonical
// form to fetch when it may. The canonical return is what the caller must
// hand to the HTTP client: fetching anything else would reopen the parsing
// gap Canonical exists to close.
func (m *URLMenu) Allowed(runID string, rawURL string) (string, bool) {
	canonical, err := CanonicalMenuURL(rawURL)
	if err != nil {
		return "", false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.expire(time.Now())

	_, ok := m.entries[runID][canonical]
	return canonical, ok
}

// expire drops runs older than the budget window. Callers hold mu.
func (m *URLMenu) expire(now time.Time) {
	for runID, started := range m.startedAt {
		if now.Sub(started) >= callBudgetWindow {
			delete(m.startedAt, runID)
			delete(m.entries, runID)
		}
	}
}
