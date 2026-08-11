// Package domainmeta looks up when a remote MCP server's domain was
// registered.
//
// Registration age is one of the few facts about an unknown endpoint that its
// operator cannot cheaply fake backwards: a domain registered six weeks ago
// hosting a server that claims an established vendor's name is a strong
// prompt for a closer look. The age says nothing by itself — plenty of real
// products launch on fresh domains — so it is presented as a date, never a
// verdict.
//
// The source is RDAP, the registry-operated successor to WHOIS: structured
// JSON over public endpoints, no credential, and answers straight from the
// authoritative registry. Lookups go through rdap.org, which redirects each
// query to the registry responsible for the TLD.
package domainmeta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// maxResponseBytes bounds an RDAP response, which is a few kilobytes for any
// real domain.
const maxResponseBytes = 1 << 20

// Doer issues HTTP requests. `*guardian.HTTPClient` satisfies it, which is
// what the composition root supplies so lookups inherit egress protection.
// The Doer must follow redirects: rdap.org answers with a redirect to the
// registry responsible for the TLD.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Registration is what the registry publishes about a domain's lifecycle.
type Registration struct {
	// Domain is the registrable domain that was looked up.
	Domain string

	// RegisteredAt is the registry's registration event, zero when the
	// registry published no such event — unknown, not "recently".
	RegisteredAt time.Time

	// Registrar is the sponsoring registrar's name, empty when the registry
	// does not publish one.
	Registrar string
}

// Client looks domains up over RDAP.
type Client struct {
	http    Doer
	baseURL string
}

// Option overrides a client default.
type Option func(*Client)

// WithBaseURL points lookups at a different RDAP bootstrap host, for a test
// server.
func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(base, "/") }
}

// NewClient builds a client against the public RDAP bootstrap service. The
// Doer should be guardian-backed in production so lookups are subject to
// egress policy.
func NewClient(doer Doer, options ...Option) *Client {
	client := &Client{
		http:    doer,
		baseURL: "https://rdap.org",
	}
	for _, option := range options {
		option(client)
	}

	return client
}

// Lookup fetches the registration record for a registrable domain.
//
// A domain the registry does not know returns (nil, nil) — for a server that
// is currently answering on that domain this is odd enough to be its own
// signal, but it is an answer, not a failure. An empty domain (an IP literal
// or a host under no public suffix) also returns (nil, nil): there is nothing
// to look up.
func (c *Client) Lookup(ctx context.Context, domain string) (*Registration, error) {
	trimmed := strings.ToLower(strings.TrimSpace(domain))
	if trimmed == "" {
		return nil, nil
	}

	endpoint := c.baseURL + "/domain/" + url.PathEscape(trimmed)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build domain registration request: %w", err)
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch domain registration: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch domain registration: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read domain registration: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("domain registration response exceeded the %d-byte limit", maxResponseBytes)
	}

	var doc struct {
		LDHName string `json:"ldhName"`
		Events  []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
		Entities []struct {
			Roles      []string        `json:"roles"`
			VCardArray json.RawMessage `json:"vcardArray"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode domain registration: %w", err)
	}
	if doc.LDHName == "" {
		return nil, fmt.Errorf("unrecognized domain registration response for %q", trimmed)
	}

	registration := &Registration{
		Domain:       trimmed,
		RegisteredAt: time.Time{},
		Registrar:    "",
	}

	for _, event := range doc.Events {
		if event.Action != "registration" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, event.Date); err == nil {
			registration.RegisteredAt = parsed
		}
		break
	}

	for _, entity := range doc.Entities {
		for _, role := range entity.Roles {
			if role != "registrar" {
				continue
			}
			if name := vcardFullName(entity.VCardArray); name != "" {
				registration.Registrar = name
			}
			break
		}
		if registration.Registrar != "" {
			break
		}
	}

	return registration, nil
}

// vcardFullName reads the `fn` property out of an RDAP jCard, which is the
// only entity field this package needs. A jCard is ["vcard", [[name, params,
// type, value], …]]; anything that does not match that shape yields "".
func vcardFullName(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var card []json.RawMessage
	if err := json.Unmarshal(raw, &card); err != nil || len(card) < 2 {
		return ""
	}

	var properties [][]json.RawMessage
	if err := json.Unmarshal(card[1], &properties); err != nil {
		return ""
	}

	for _, property := range properties {
		if len(property) < 4 {
			continue
		}
		var name string
		if err := json.Unmarshal(property[0], &name); err != nil || name != "fn" {
			continue
		}
		var value string
		if err := json.Unmarshal(property[3], &value); err == nil && value != "" {
			return value
		}
	}

	return ""
}
