// Package iru implements the Iru (formerly Kandji) inventory-source
// provider: it pulls the managed-device fleet from an Iru tenant so Gram can
// compute agent coverage.
//
// Endpoint → permission mapping, for customer docs:
//
//	GET /api/v1/devices — requires an API token with the "Device list"
//	permission. Customers should create a token with only that permission
//	enabled; the token and the tenant's API URL both live in the Iru console
//	under Settings → Access.
//
// All HTTP runs through the guardian SSRF-hardened client; Iru is cloud-only
// so every tenant API URL is publicly routable.
package iru

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ProviderID is the registry discriminator stored on config rows. It
	// uses the vendor's current brand; tenants answer on both the rebranded
	// API domain (*.api.iru.com) and the legacy one (*.api.kandji.io), and
	// the instance_url setting accepts either.
	ProviderID = "iru"

	// ScheduleInventory is the provider's single sync pipeline.
	ScheduleInventory = "iru_inventory"

	// userAgent identifies this integration on every API request.
	userAgent = "Speakeasy-Gram/1.0"

	// pageSize is the Devices API's documented maximum. Offset pagination
	// (the only kind the API offers) is vulnerable to row shift at page
	// boundaries, so the fewer boundaries per pull the better.
	pageSize = 300

	// maxResponseBytes bounds each response body read so a misbehaving
	// tenant cannot force unbounded allocations. Sized generously above a
	// full 300-device page.
	maxResponseBytes = 8 * 1024 * 1024

	fieldInstanceURL = "instance_url"
	fieldAPIToken    = "api_token"
)

func init() {
	providers.Register(providers.Descriptor{
		ID:           ProviderID,
		DisplayName:  "Iru (formerly Kandji)",
		Capabilities: []providers.Capability{providers.CapabilityInventorySource},
		Fields: []providers.CredentialField{
			{Key: fieldInstanceURL, Label: "API URL", Kind: providers.FieldKindURL, Secret: false, Required: true},
			{Key: fieldAPIToken, Label: "API Token", Kind: providers.FieldKindText, Secret: true, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: ScheduleInventory, Capability: providers.CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps providers.Deps) providers.InventorySource {
			return &source{client: deps.Client}
		},
		NewEvidenceSink: nil,
	})
}

// source is one Iru API session. Auth is a static bearer token, so unlike
// Jamf there is no token cache and the source is stateless.
type source struct {
	client *guardian.HTTPClient
}

var _ providers.InventorySource = (*source)(nil)

// instanceBaseURL validates and normalizes the customer-supplied API URL —
// the value the Iru console shows under Settings → Access, e.g.
// https://yourtenant.api.iru.com (legacy *.api.kandji.io and EU-region
// domains work the same way). https only, no path/query — the API path is
// ours to append.
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
		return "", fmt.Errorf("instance_url must be the tenant API root, e.g. https://yourtenant.api.iru.com")
	}
	// The single most common paste mistake is the web-console URL
	// (yourtenant.iru.com) instead of the API root (yourtenant.api.iru.com);
	// dropping the tenant label (api.iru.com) is the runner-up. On the
	// vendor's own domains, require the <tenant>.api.<...> shape and say
	// exactly what to paste — the console host never serves the API, and a
	// generic request failure at test time diagnoses neither mistake.
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if vendorHost(host) && !vendorAPIHost(host) {
		return "", fmt.Errorf("instance_url must be the tenant API root shown in the console under Settings → Access, e.g. https://yourtenant.api.iru.com — not the console URL")
	}
	return "https://" + parsed.Host, nil
}

// vendorHost reports whether the host belongs to the vendor's domains
// (including their apexes), where the tenant-API-root shape is enforceable.
func vendorHost(host string) bool {
	for _, domain := range []string{"iru.com", "kandji.io"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// vendorAPIHost reports the <tenant>.api.<...> shape: at least one tenant
// label followed by the literal "api" label (covers regional forms like
// tenant.api.eu.kandji.io).
func vendorAPIHost(host string) bool {
	labels := strings.Split(host, ".")
	return len(labels) >= 4 && labels[1] == "api"
}

// deviceRecord mirrors the fields we map from GET /api/v1/devices.
type deviceRecord struct {
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	SerialNumber string `json:"serial_number"`
	Platform     string `json:"platform"`
	OSVersion    string `json:"os_version"`
	// User is an object when a user is assigned; the API emits an empty
	// string (not null) when unassigned, so it cannot decode into a struct
	// directly.
	User        json.RawMessage `json:"user"`
	LastCheckIn string          `json:"last_check_in"`
}

// userEmail extracts the assigned user's email. The API's unassigned
// representations — an empty-string user (confirmed against the real
// tenant), null, or an absent field — map to "". Schema drift fails
// loudly: an undecodable user, or a user object whose email KEY has
// vanished (renamed or dropped), must not silently read as unassigned and
// quietly zero the whole fleet's coverage attribution. A present-but-null
// email stays unassigned — one odd user record must not block the org's
// entire pull.
func (d deviceRecord) userEmail() (string, error) {
	trimmed := strings.TrimSpace(string(d.User))
	if trimmed == "" || trimmed == `""` || trimmed == "null" {
		return "", nil
	}
	var user map[string]json.RawMessage
	if err := json.Unmarshal(d.User, &user); err != nil {
		return "", fmt.Errorf("decode user: %w", err)
	}
	rawEmail, ok := user["email"]
	if !ok {
		return "", fmt.Errorf("user object carried no email field")
	}
	var email string
	if err := json.Unmarshal(rawEmail, &email); err != nil {
		return "", fmt.Errorf("decode user email: %w", err)
	}
	return strings.TrimSpace(email), nil
}

// fetchDevicesPage requests one page of the Devices API at the given offset.
func (s *source) fetchDevicesPage(ctx context.Context, creds providers.Credentials, settings providers.Settings, offset int, size int) ([]json.RawMessage, error) {
	base, err := instanceBaseURL(settings)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("limit", strconv.Itoa(size))
	query.Set("offset", strconv.Itoa(offset))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/devices?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build devices request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds[fieldAPIToken])
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request devices: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Classify by status before touching the body: the error branches never
	// need it, and an oversized error response must not mask a credential
	// rejection as a generic read failure.
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, providers.NewAuthError(fmt.Errorf("devices request rejected with status %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("devices request failed with status %d", resp.StatusCode)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read devices response: %w", err)
	}

	// The Devices API returns a bare JSON array, not an envelope.
	var results []json.RawMessage
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode devices response: %w", err)
	}
	return results, nil
}

// TestConnection proves the stored credentials against the cheapest
// authenticated read: one single-record devices page.
func (s *source) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	if _, err := s.fetchDevicesPage(ctx, creds, settings, 0, 1); err != nil {
		return fmt.Errorf("iru connection test: %w", err)
	}
	return nil
}

// ListDevices returns one page of the tenant's device inventory. The cursor
// is the next page offset.
//
// The Devices API only offers limit/offset pagination — no sort or id-window
// filter — so unlike Jamf's keyset cursor this pull is not immune to mid-pull
// churn: each device unenrolled between pages shifts later rows one slot
// down and can drop one boundary device from the pull (N unenrollments can
// drop up to N). The default listing order was verified stable across
// back-to-back requests against a real tenant. A skipped device stays
// marked missing until a later pull fetches it — one cycle in the common
// case, longer only while sustained per-pull churn keeps re-skipping it
// (see the snapshot-consistency note on providers.InventorySource) — and
// an evidence-sink push firing inside that window exports the mis-mark
// until its next push. pageSize is pinned at the API maximum to minimize
// boundaries per pull.
func (s *source) ListDevices(ctx context.Context, creds providers.Credentials, settings providers.Settings, cursor string) (providers.DevicePage, error) {
	offset := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("invalid iru devices cursor %q", cursor)
		}
		offset = parsed
	}

	results, err := s.fetchDevicesPage(ctx, creds, settings, offset, pageSize)
	if err != nil {
		return providers.DevicePage{Devices: nil, NextCursor: ""}, err
	}

	devices := make([]providers.Device, 0, len(results))
	for _, raw := range results {
		var d deviceRecord
		if err := json.Unmarshal(raw, &d); err != nil {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("decode device record: %w", err)
		}
		// device_id anchors upserts (external_id is unique per config); a
		// record without one cannot be tracked, so fail loudly rather than
		// silently churn a phantom device.
		if strings.TrimSpace(d.DeviceID) == "" {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("device record carried no device_id")
		}
		var lastCheckIn time.Time
		if d.LastCheckIn != "" {
			// Iru emits RFC3339 with sub-second precision and a numeric
			// offset (e.g. 2026-07-01T19:02:37.320664+00:00). An
			// unparseable value fails loudly: a vendor format drift would
			// otherwise silently NULL every stored check-in on the next
			// pull, fleet-wide, with nothing surfaced anywhere.
			parsed, err := time.Parse(time.RFC3339, d.LastCheckIn)
			if err != nil {
				return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("device %s carried unparseable last_check_in %q", d.DeviceID, d.LastCheckIn)
			}
			lastCheckIn = parsed.UTC()
		}
		email, err := d.userEmail()
		if err != nil {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("device %s: %w", d.DeviceID, err)
		}
		devices = append(devices, providers.Device{
			ExternalID:    d.DeviceID,
			SerialNumber:  d.SerialNumber,
			Hostname:      d.DeviceName,
			OSName:        d.Platform,
			OSVersion:     d.OSVersion,
			UserEmail:     email,
			LastCheckInAt: lastCheckIn,
			Raw:           raw,
		})
	}

	// A page is final only when it is EMPTY. Stopping on the first short
	// page would trust the vendor never to serve fewer records than
	// requested mid-listing (a silently clamped limit, a lowered API
	// maximum): a non-final short page would truncate the pull, and the
	// completion-gated mark-missing would then flag the entire unfetched
	// remainder of the fleet as missing. One extra empty-page request per
	// pull buys immunity to that failure mode.
	next := ""
	if len(results) > 0 {
		next = strconv.Itoa(offset + len(results))
	}
	return providers.DevicePage{Devices: devices, NextCursor: next}, nil
}
