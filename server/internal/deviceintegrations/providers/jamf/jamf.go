// Package jamf implements the Jamf Pro inventory-source provider: it pulls
// the managed-device fleet from a Jamf Pro (Cloud) tenant so Gram can compute
// agent coverage.
//
// Partner-program notes (Jamf Technology Partner Checklist):
//   - Every API request carries the unique User-Agent header below (REQUIRED).
//   - Endpoint → privilege mapping, for the checklist and customer docs:
//     POST /api/oauth/token              — authentication (no privilege)
//     GET  /api/v1/computers-inventory   — requires "Read Computers"
//     Customers should create an API Role with only "Read Computers" and an
//     API Client bound to it.
//   - Scalability: the OAuth token is cached until expiry (never minted per
//     request), pages are section-filtered and capped at 100 records, and
//     the sync scheduler backs off on failure and auto-pauses on credential
//     rejection.
//
// v1 supports Jamf Cloud tenants only: all HTTP runs through the guardian
// SSRF-hardened client, which blocks private address space, so on-prem
// instances behind a VPN are unsupported by design.
package jamf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ProviderID is the registry discriminator stored on config rows.
	ProviderID = "jamf"

	// ScheduleInventory is the provider's single sync pipeline.
	ScheduleInventory = "jamf_inventory"

	// userAgent uniquely identifies this integration on every Jamf API
	// request, as the Jamf Technology Partner Program requires.
	userAgent = "Speakeasy-Gram/1.0"

	// pageSize keeps inventory pages at Jamf's documented default: large
	// enough to cover big fleets in few requests, small enough that
	// section-filtered payloads stay well under maxResponseBytes even on
	// tenants with heavy extension attributes.
	pageSize = 100

	// maxResponseBytes bounds each response body read so a misbehaving
	// tenant cannot force unbounded allocations. Sized generously above a
	// full 100-device page with all requested sections.
	maxResponseBytes = 8 * 1024 * 1024

	// tokenExpirySlack renews the cached token this long before its actual
	// expiry so in-flight requests never race the cutoff.
	tokenExpirySlack = 30 * time.Second

	fieldInstanceURL  = "instance_url"
	fieldClientID     = "client_id"
	fieldClientSecret = "client_secret"
)

func init() {
	providers.Register(providers.Descriptor{
		ID:           ProviderID,
		DisplayName:  "Jamf Pro",
		Capabilities: []providers.Capability{providers.CapabilityInventorySource},
		Fields: []providers.CredentialField{
			{Key: fieldInstanceURL, Label: "Instance URL", Kind: providers.FieldKindURL, Secret: false, Required: true},
			{Key: fieldClientID, Label: "API Client ID", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: fieldClientSecret, Label: "API Client Secret", Kind: providers.FieldKindText, Secret: true, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: ScheduleInventory, Capability: providers.CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps providers.Deps) providers.InventorySource {
			return &source{client: deps.Client, mu: sync.Mutex{}, token: "", tokenExpiry: time.Time{}}
		},
		NewEvidenceSink: nil,
	})
}

// source is one Jamf Pro API session. The sync runner constructs a fresh
// source per run, so the token cache spans one full paginated pull.
type source struct {
	client *guardian.HTTPClient

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

var _ providers.InventorySource = (*source)(nil)

// instanceBaseURL validates and normalizes the customer-supplied instance
// URL: https only, no path/query — the API path is ours to append.
func instanceBaseURL(settings providers.Settings) (string, error) {
	raw := strings.TrimSpace(settings[fieldInstanceURL])
	if raw == "" {
		return "", fmt.Errorf("instance_url is not configured")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("instance_url is not a valid URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("instance_url must use https")
	}
	if parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("instance_url must be the tenant root, e.g. https://yourtenant.jamfcloud.com")
	}
	return "https://" + parsed.Host, nil
}

// bearerToken returns a cached OAuth token, minting one via the
// client-credentials grant when absent or near expiry. Never minted per
// request, per Jamf's scalability guidance.
func (s *source) bearerToken(ctx context.Context, creds providers.Credentials, base string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExpiry) {
		return s.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds[fieldClientID])
	form.Set("client_secret", creds[fieldClientSecret])

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request token: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Classify by status before touching the body: the error branches never
	// need it, and an oversized error response must not mask a credential
	// rejection as a generic read failure.
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", providers.NewAuthError(fmt.Errorf("token request rejected with status %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("token response carried no access token")
	}

	// Renew slack seconds before actual expiry so in-flight requests don't
	// race the cutoff — but never let slack consume the whole lifetime.
	// Jamf's admin-configured token lifetime defaults to 60s and can be set
	// lower; without the floor a lifetime <= slack would make the cache
	// permanently stale and degrade to per-request minting.
	ttl := time.Duration(payload.ExpiresIn) * time.Second
	slack := tokenExpirySlack
	if ttl <= 2*tokenExpirySlack {
		slack = ttl / 2
	}
	s.token = payload.AccessToken
	s.tokenExpiry = time.Now().Add(ttl - slack)
	return s.token, nil
}

// inventoryDevice mirrors the sections we request from
// GET /api/v1/computers-inventory.
type inventoryDevice struct {
	ID      string `json:"id"`
	General struct {
		Name            string `json:"name"`
		Platform        string `json:"platform"`
		LastContactTime string `json:"lastContactTime"`
	} `json:"general"`
	Hardware struct {
		SerialNumber string `json:"serialNumber"`
	} `json:"hardware"`
	OperatingSystem struct {
		Version string `json:"version"`
	} `json:"operatingSystem"`
	UserAndLocation struct {
		Email string `json:"email"`
	} `json:"userAndLocation"`
}

type inventoryPage struct {
	Results []json.RawMessage `json:"results"`
}

// fetchInventoryPage requests one section-filtered, id-sorted page of
// devices with ids strictly greater than afterID (-1 = from the start).
func (s *source) fetchInventoryPage(ctx context.Context, creds providers.Credentials, settings providers.Settings, afterID int, size int) (inventoryPage, error) {
	base, err := instanceBaseURL(settings)
	if err != nil {
		return inventoryPage{}, err
	}
	token, err := s.bearerToken(ctx, creds, base)
	if err != nil {
		return inventoryPage{}, err
	}

	query := url.Values{}
	query.Set("page", "0")
	query.Set("page-size", strconv.Itoa(size))
	// Keyset pagination: id-sorted pages windowed by an RSQL id filter
	// instead of page offsets. Offset pages over a live collection skip a
	// boundary record when a device earlier in the sort order is unenrolled
	// mid-pull (later rows shift into already-consumed windows); filtering
	// on id > last-seen is immune to both insertions and deletions.
	query.Set("sort", "id:asc")
	if afterID >= 0 {
		query.Set("filter", fmt.Sprintf("id>%d", afterID))
	}
	// Only the sections we map — full payloads bloat badly on large fleets.
	query["section"] = []string{"GENERAL", "HARDWARE", "OPERATING_SYSTEM", "USER_AND_LOCATION"}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/computers-inventory?"+query.Encode(), nil)
	if err != nil {
		return inventoryPage{}, fmt.Errorf("build inventory request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return inventoryPage{}, fmt.Errorf("request inventory: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Classify by status before touching the body: the error branches never
	// need it, and an oversized error response must not mask a credential
	// rejection as a generic read failure.
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Reachable when the token expires while a request is in flight (the
		// per-page expiry check re-mints between pages, so this is a narrow
		// race) or on genuine credential revocation. Drop the cache so any
		// later call on this source re-mints; the sync runner records this
		// run as an auth rejection either way, and only a streak of pure
		// rejections auto-pauses the schedule.
		s.mu.Lock()
		s.token = ""
		s.mu.Unlock()
		return inventoryPage{}, providers.NewAuthError(fmt.Errorf("inventory request rejected with status %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return inventoryPage{}, fmt.Errorf("inventory request failed with status %d", resp.StatusCode)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return inventoryPage{}, fmt.Errorf("read inventory response: %w", err)
	}

	var parsed inventoryPage
	if err := json.Unmarshal(body, &parsed); err != nil {
		return inventoryPage{}, fmt.Errorf("decode inventory response: %w", err)
	}
	return parsed, nil
}

// TestConnection proves the stored credentials against the cheapest
// authenticated read: one single-record inventory page.
func (s *source) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	if _, err := s.fetchInventoryPage(ctx, creds, settings, -1, 1); err != nil {
		return fmt.Errorf("jamf connection test: %w", err)
	}
	return nil
}

// ListDevices returns one page of the tenant's computer inventory. The
// cursor is the numeric id of the last device already returned; devices are
// pulled in id order with an id-greater-than filter (keyset pagination).
func (s *source) ListDevices(ctx context.Context, creds providers.Credentials, settings providers.Settings, cursor string) (providers.DevicePage, error) {
	afterID := -1
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("invalid jamf inventory cursor %q", cursor)
		}
		afterID = parsed
	}

	fetched, err := s.fetchInventoryPage(ctx, creds, settings, afterID, pageSize)
	if err != nil {
		return providers.DevicePage{Devices: nil, NextCursor: ""}, err
	}

	lastID := -1
	devices := make([]providers.Device, 0, len(fetched.Results))
	for _, raw := range fetched.Results {
		var d inventoryDevice
		if err := json.Unmarshal(raw, &d); err != nil {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("decode inventory device: %w", err)
		}
		// Jamf computer ids are numeric; the cursor depends on that. A
		// non-numeric or missing id cannot anchor the next window, so fail
		// loudly rather than risk an infinite pull.
		numericID, err := strconv.Atoi(d.ID)
		if err != nil || numericID < 0 {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("inventory device carried unusable id %q", d.ID)
		}
		lastID = numericID
		var lastContact time.Time
		if d.General.LastContactTime != "" {
			// Jamf emits RFC3339 with sub-second precision. An unparseable
			// value fails loudly: a vendor format drift would otherwise
			// silently NULL every stored check-in on the next pull,
			// fleet-wide, with nothing surfaced anywhere.
			parsed, err := time.Parse(time.RFC3339, d.General.LastContactTime)
			if err != nil {
				return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("device %s carried unparseable lastContactTime %q", d.ID, d.General.LastContactTime)
			}
			lastContact = parsed.UTC()
		}
		devices = append(devices, providers.Device{
			ExternalID:    d.ID,
			SerialNumber:  d.Hardware.SerialNumber,
			Hostname:      d.General.Name,
			OSName:        d.General.Platform,
			OSVersion:     d.OperatingSystem.Version,
			UserEmail:     strings.TrimSpace(d.UserAndLocation.Email),
			LastCheckInAt: lastContact,
			Raw:           raw,
		})
	}

	// A page is final only when it is EMPTY. Stopping on the first short
	// page would trust the vendor never to serve fewer records than
	// requested mid-listing (a silently clamped page-size): a non-final
	// short page would truncate the pull, and the completion-gated
	// mark-missing would then flag the entire unfetched remainder as
	// missing. One extra empty-page request per pull buys immunity;
	// totalCount is not consulted because it tracks the live filtered
	// collection, not our snapshot.
	next := ""
	if len(fetched.Results) > 0 && lastID >= 0 {
		next = strconv.Itoa(lastID)
	}
	return providers.DevicePage{Devices: devices, NextCursor: next}, nil
}
