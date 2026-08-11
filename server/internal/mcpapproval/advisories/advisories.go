// Package advisories asks OSV.dev which published vulnerability advisories
// name an MCP server's package.
//
// OSV aggregates GHSA, PyPA, and the other ecosystem databases behind one
// unauthenticated query API, which makes it the single deterministic answer to
// "does anything published say this package is vulnerable". The answer is
// citable — every advisory has a stable id and URL — and, unlike web search,
// not seedable by the party under review.
//
// The distinction this package is careful about: a clean answer is a real
// finding ("OSV knows this package and lists nothing"), while a failed query
// is a gap. The two must never collapse, because an approver reading
// checked-and-clean where the truth is could-not-check is exactly the
// conflation the evidence document's gap contract exists to prevent.
package advisories

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// maxResponseBytes bounds an OSV response. Advisory lists for real packages
// run to a few hundred kilobytes at most.
const maxResponseBytes = 8 << 20

// maxStoredAdvisories caps how many advisories the report carries. The count
// always reflects everything OSV returned; the detail list is a sample of the
// most recently published, which is what an approver reads first.
const maxStoredAdvisories = 10

// Doer issues HTTP requests. `*guardian.HTTPClient` satisfies it, which is
// what the composition root supplies so lookups inherit egress protection.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Report is OSV's answer for one package.
type Report struct {
	// Ecosystem and Package name what was queried, in OSV's vocabulary.
	Ecosystem string
	Package   string

	// Version is the exact version queried, empty when the query covered the
	// whole package. A version-scoped answer says nothing about other
	// versions.
	Version string

	// KnownCount is how many advisories OSV returned in total. Zero with a
	// successful query is checked-and-clean.
	KnownCount int

	// Advisories is a most-recent sample of the advisories, at most
	// maxStoredAdvisories long.
	Advisories []Advisory
}

// Advisory is one published advisory naming the package.
type Advisory struct {
	// ID is the advisory's stable identifier (`GHSA-…`, `PYSEC-…`), which is
	// also its citation.
	ID string

	// Summary is the advisory's one-line description.
	Summary string

	// Severity is the advisory's qualitative severity when its database
	// published one (`CRITICAL`, `HIGH`, …), empty otherwise.
	Severity string

	// Published is when the advisory was published.
	Published time.Time
}

// osvVuln is one advisory as OSV's query endpoint returns it.
type osvVuln struct {
	ID               string    `json:"id"`
	Summary          string    `json:"summary"`
	Published        time.Time `json:"published"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

// Client queries the public OSV API.
type Client struct {
	http    Doer
	baseURL string
}

// Option overrides a client default.
type Option func(*Client)

// WithBaseURL points queries at a different host, for a test server.
func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(base, "/") }
}

// NewClient builds a client against the public OSV API. The Doer should be
// guardian-backed in production so queries are subject to egress policy.
func NewClient(doer Doer, options ...Option) *Client {
	client := &Client{
		http:    doer,
		baseURL: "https://api.osv.dev",
	}
	for _, option := range options {
		option(client)
	}

	return client
}

// osvEcosystems maps a package registry to OSV's ecosystem name.
var osvEcosystems = map[identity.Registry]string{
	identity.RegistryNPM:  "npm",
	identity.RegistryPyPI: "PyPI",
}

// maxAdvisoryPages bounds how many result pages one query follows. A package
// with more advisories than that is extraordinary; reporting a partial count
// as the whole truth would break the clean-versus-gap contract, so exceeding
// the bound is an error the assembler records as a gap.
const maxAdvisoryPages = 5

// Query asks OSV for advisories against a package, scoped to one version when
// the reference pinned one.
//
// A registry OSV does not cover returns (nil, nil): not consulted is not a
// failure. A successful query with no advisories returns a Report with
// KnownCount zero — checked and clean, which is a finding. Paginated results
// are followed to the end (within maxAdvisoryPages), so the count is never a
// silently truncated first page.
func (c *Client) Query(ctx context.Context, registry identity.Registry, name string, version string) (*Report, error) {
	ecosystem, ok := osvEcosystems[registry]
	trimmed := strings.TrimSpace(name)
	if !ok || trimmed == "" {
		return nil, nil
	}

	trimmedVersion := strings.TrimSpace(version)

	var vulns []osvVuln
	pageToken := ""
	for page := 0; ; page++ {
		if page == maxAdvisoryPages {
			return nil, fmt.Errorf("advisory results exceeded %d pages", maxAdvisoryPages)
		}

		doc, err := c.queryPage(ctx, ecosystem, trimmed, trimmedVersion, pageToken)
		if err != nil {
			return nil, err
		}

		vulns = append(vulns, doc.Vulns...)
		if doc.NextPageToken == "" {
			break
		}
		pageToken = doc.NextPageToken
	}

	report := &Report{
		Ecosystem:  ecosystem,
		Package:    trimmed,
		Version:    trimmedVersion,
		KnownCount: len(vulns),
		Advisories: nil,
	}

	// Newest first, so the stored sample is the advisories an approver would
	// read first anyway. Ties break on id so repeated gathers store the same
	// sample.
	slices.SortFunc(vulns, func(a, b osvVuln) int {
		if c := b.Published.Compare(a.Published); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	for _, vuln := range vulns {
		if len(report.Advisories) == maxStoredAdvisories {
			break
		}
		report.Advisories = append(report.Advisories, Advisory{
			ID:        vuln.ID,
			Summary:   vuln.Summary,
			Severity:  vuln.DatabaseSpecific.Severity,
			Published: vuln.Published,
		})
	}

	return report, nil
}

// osvQueryResponse is one page of OSV's query results. A response may carry
// only a page token and no vulns at all, which is a continuation, not a clean
// answer.
type osvQueryResponse struct {
	Vulns         []osvVuln `json:"vulns"`
	NextPageToken string    `json:"next_page_token"`
}

func (c *Client) queryPage(ctx context.Context, ecosystem, name, version, pageToken string) (*osvQueryResponse, error) {
	type osvPackage struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	}
	payload := struct {
		Package   osvPackage `json:"package"`
		Version   string     `json:"version,omitempty"`
		PageToken string     `json:"page_token,omitempty"`
	}{
		Package:   osvPackage{Name: name, Ecosystem: ecosystem},
		Version:   version,
		PageToken: pageToken,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode advisory query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/query", bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build advisory query: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query advisories: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query advisories: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read advisory response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("advisory response exceeded the %d-byte limit", maxResponseBytes)
	}

	var doc osvQueryResponse
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode advisory response: %w", err)
	}

	return &doc, nil
}
