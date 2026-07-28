// Package intune implements the Microsoft Intune inventory-source provider:
// it pulls the managed-device fleet from a customer's Intune tenant via
// Microsoft Graph so Gram can compute agent coverage.
//
// Customer-side setup, for the docs: create an Entra ID app registration,
// grant it only the DeviceManagementManagedDevices.Read.All APPLICATION
// permission (admin consent required), create a client secret, and enter
// the directory (tenant) id plus the client id/secret in the dashboard.
//
// Endpoints used — both hosts are fixed, never customer-supplied:
//
//	POST login.microsoftonline.com/{tenant}/oauth2/v2.0/token — client
//	credentials mint (scope https://graph.microsoft.com/.default). Entra
//	reports bad client credentials as HTTP 400 with an error code such as
//	"invalid_client", which must classify as a credential rejection.
//	GET  graph.microsoft.com/v1.0/deviceManagement/managedDevices —
//	field-selected ($select) device pages.
//
// Pagination follows Graph's server-driven @odata.nextLink cursor: the
// client does no offset arithmetic, so there is no client-side page-shift
// hazard — though Graph makes no snapshot-isolation promise, so mid-pull
// churn behavior is whatever its continuation tokens provide (see the
// snapshot-consistency note on providers.InventorySource). The cursor
// round-trips through the framework as an opaque string, so the followed
// URL is validated to stay on the Graph host.
package intune

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ProviderID is the registry discriminator stored on config rows.
	ProviderID = "intune"

	// ScheduleInventory is the provider's single sync pipeline.
	ScheduleInventory = "intune_inventory"

	// userAgent identifies this integration on every API request.
	userAgent = "Speakeasy-Gram/1.0"

	// maxResponseBytes bounds each response body read so a misbehaving
	// tenant cannot force unbounded allocations. Page size is server-driven
	// (up to 1000 records) and Intune only partially honors $select on some
	// properties, so this is sized for full-fat records, not the selected
	// subset.
	maxResponseBytes = 16 * 1024 * 1024

	// tokenExpirySlack renews the cached token this long before its actual
	// expiry so in-flight requests never race the cutoff.
	tokenExpirySlack = 30 * time.Second

	// selectFields keeps pages to exactly the mapped fields — full
	// managedDevice payloads carry dozens of properties we never read.
	selectFields = "id,deviceName,serialNumber,operatingSystem,osVersion,userPrincipalName,emailAddress,lastSyncDateTime"

	fieldTenantID     = "tenant_id"
	fieldClientID     = "client_id"
	fieldClientSecret = "client_secret"
)

// The identity and Graph hosts are constants; tests inject fakes via the
// source fields.
const (
	defaultLoginBaseURL = "https://login.microsoftonline.com"
	defaultGraphBaseURL = "https://graph.microsoft.com"
)

func init() {
	providers.Register(providers.Descriptor{
		ID:           ProviderID,
		DisplayName:  "Microsoft Intune",
		Capabilities: []providers.Capability{providers.CapabilityInventorySource},
		Fields: []providers.CredentialField{
			{Key: fieldTenantID, Label: "Directory (tenant) ID", Kind: providers.FieldKindText, Secret: false, Required: true},
			// The client id is technically public, but it rotates together
			// with the secret as one credential pair (matching the Jamf and
			// Vanta providers), so both live in the write-only blob.
			{Key: fieldClientID, Label: "Application (client) ID", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: fieldClientSecret, Label: "Client Secret", Kind: providers.FieldKindText, Secret: true, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: ScheduleInventory, Capability: providers.CapabilityInventorySource, Interval: time.Hour},
		},
		NewInventorySource: func(deps providers.Deps) providers.InventorySource {
			return newSource(deps.Client, defaultLoginBaseURL, defaultGraphBaseURL)
		},
		NewEvidenceSink: nil,
	})
}

// source is one Graph API session. The sync runner constructs a fresh source
// per run, so the token cache spans one full paginated pull.
type source struct {
	client *guardian.HTTPClient
	// loginBaseURL/graphBaseURL are the fixed production hosts, or httptest
	// servers in unit tests; graphHost is graphBaseURL's hostname, computed
	// once for cursor validation.
	loginBaseURL string
	graphBaseURL string
	graphHost    string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

var _ providers.InventorySource = (*source)(nil)

// newSource is the one construction site for both production wiring and
// unit tests, so a future field cannot be zero-valued in tests only (test
// files are exempt from exhaustruct).
func newSource(client *guardian.HTTPClient, loginBaseURL string, graphBaseURL string) *source {
	graphHost := ""
	if parsed, err := url.Parse(graphBaseURL); err == nil {
		graphHost = strings.ToLower(parsed.Hostname())
	}
	return &source{
		client:       client,
		loginBaseURL: loginBaseURL,
		graphBaseURL: graphBaseURL,
		graphHost:    graphHost,
		mu:           sync.Mutex{},
		token:        "",
		tokenExpiry:  time.Time{},
	}
}

// tenantID validates the configured directory id. Entra tenant ids are
// UUIDs (or a verified domain); the id becomes a URL path segment, so it
// must at least be a single clean segment.
func tenantID(settings providers.Settings) (string, error) {
	id := strings.TrimSpace(settings[fieldTenantID])
	if id == "" {
		return "", fmt.Errorf("tenant_id is not configured")
	}
	switch strings.ToLower(id) {
	case ".", "..", "common", "organizations", "consumers":
		// Multi-tenant aliases from Entra docs (and path tricks) are not a
		// directory: client_credentials requires a real tenant.
		return "", fmt.Errorf("tenant_id must be your directory id or verified domain, not %q", id)
	}
	if strings.ContainsAny(id, "/?#%") {
		return "", fmt.Errorf("tenant_id must be a directory id or verified domain, e.g. 00000000-0000-0000-0000-000000000000")
	}
	return id, nil
}

// bearerToken returns a cached Graph token, minting one via the
// client-credentials grant when absent or near expiry.
func (s *source) bearerToken(ctx context.Context, creds providers.Credentials, settings providers.Settings) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExpiry) {
		return s.token, nil
	}

	tenant, err := tenantID(settings)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds[fieldClientID])
	form.Set("client_secret", creds[fieldClientSecret])
	form.Set("scope", s.graphBaseURL+"/.default")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.loginBaseURL+"/"+url.PathEscape(tenant)+"/oauth2/v2.0/token", strings.NewReader(form.Encode()))
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

	// Classify plain rejections by status before touching the body — an
	// oversized error response must not mask a credential rejection.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", providers.NewAuthError(fmt.Errorf("token request rejected with status %d", resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Entra reports bad client credentials as 400 with a JSON error
		// code, not 401 — read a bounded body to classify, or a revoked
		// secret would retry forever instead of auto-pausing.
		body, readErr := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
		if readErr != nil {
			return "", fmt.Errorf("token request failed with status %d: %w", resp.StatusCode, readErr)
		}
		var oauthErr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &oauthErr)
		switch oauthErr.Error {
		case "invalid_client", "unauthorized_client", "invalid_grant", "access_denied":
			return "", providers.NewAuthError(fmt.Errorf("token request rejected: %s (status %d)", oauthErr.Error, resp.StatusCode))
		case "":
			return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
		}
		// Name the Entra code (e.g. invalid_request for a tenant that does
		// not exist) — a bare status 400 is undiagnosable from the
		// dashboard. Deliberately not auth-classified: transient 400 shapes
		// exist, and a visible named error retrying on schedule beats
		// auto-pausing a healthy config on a transient.
		return "", fmt.Errorf("token request failed with status %d (%s)", resp.StatusCode, oauthErr.Error)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("token response carried no access token")
	}

	ttl := time.Duration(token.ExpiresIn) * time.Second
	if ttl <= 0 {
		// A missing expires_in must not poison the cache into minting per
		// page; Entra tokens live about an hour, ten minutes is safe.
		ttl = 10 * time.Minute
	}
	slack := tokenExpirySlack
	if ttl <= 2*tokenExpirySlack {
		slack = ttl / 2
	}
	s.token = token.AccessToken
	s.tokenExpiry = time.Now().Add(ttl - slack)
	return s.token, nil
}

// managedDevice mirrors the $select-ed fields of a Graph managedDevice.
type managedDevice struct {
	ID                string `json:"id"`
	DeviceName        string `json:"deviceName"`
	SerialNumber      string `json:"serialNumber"`
	OperatingSystem   string `json:"operatingSystem"`
	OSVersion         string `json:"osVersion"`
	UserPrincipalName string `json:"userPrincipalName"`
	EmailAddress      string `json:"emailAddress"`
	LastSyncDateTime  string `json:"lastSyncDateTime"`
}

type devicePage struct {
	NextLink string            `json:"@odata.nextLink"`
	Value    []json.RawMessage `json:"value"`
}

// pageURL resolves the request URL for a pull step: the first page from the
// fixed Graph host, later pages from Graph's own nextLink — validated to
// stay on the Graph host, because the cursor round-trips through the
// framework as an opaque string. Page size is deliberately server-driven
// (Intune defaults to 1000 records per page): the Intune Graph endpoints
// support a restricted OData subset, and $select is the only parameter the
// pull depends on.
func (s *source) pageURL(cursor string) (string, error) {
	if cursor == "" {
		query := url.Values{}
		query.Set("$select", selectFields)
		return s.graphBaseURL + "/v1.0/deviceManagement/managedDevices?" + query.Encode(), nil
	}
	parsed, err := url.Parse(cursor)
	if err != nil {
		return "", fmt.Errorf("invalid intune cursor: %w", err)
	}
	// Compare hostnames case-insensitively and ignore an explicit default
	// port: a semantically identical nextLink (Graph.Microsoft.Com,
	// :443) must not abort page two of every large pull.
	if parsed.Scheme != "https" || s.graphHost == "" || strings.ToLower(parsed.Hostname()) != s.graphHost {
		return "", fmt.Errorf("intune cursor does not point at the Graph host")
	}
	return cursor, nil
}

// fetchPageOnce performs one authenticated page request. On 401/403 the
// token cache is dropped before returning the auth error so the caller can
// re-mint.
func (s *source) fetchPageOnce(ctx context.Context, token string, requestURL string) (devicePage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return devicePage{}, fmt.Errorf("build devices request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return devicePage{}, fmt.Errorf("request devices: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	// Classify by status before touching the body: the error branches never
	// need it, and an oversized error response must not mask a credential
	// rejection as a generic read failure.
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		s.mu.Lock()
		s.token = ""
		s.mu.Unlock()
		return devicePage{}, providers.NewAuthError(fmt.Errorf("devices request rejected with status %d", resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return devicePage{}, fmt.Errorf("devices request failed with status %d", resp.StatusCode)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return devicePage{}, fmt.Errorf("read devices response: %w", err)
	}
	var page devicePage
	if err := json.Unmarshal(body, &page); err != nil {
		return devicePage{}, fmt.Errorf("decode devices response: %w", err)
	}
	return page, nil
}

// fetchDevicesPage requests one device page, absorbing exactly one 401/403
// by re-minting and retrying: a token that crossed expiry during a slow or
// throttled request must not book a spurious credential rejection toward
// auto-pause. A second rejection is a real credential problem and surfaces
// as the auth error it is.
func (s *source) fetchDevicesPage(ctx context.Context, creds providers.Credentials, settings providers.Settings, cursor string) (devicePage, error) {
	requestURL, err := s.pageURL(cursor)
	if err != nil {
		return devicePage{}, err
	}
	token, err := s.bearerToken(ctx, creds, settings)
	if err != nil {
		return devicePage{}, err
	}
	page, err := s.fetchPageOnce(ctx, token, requestURL)
	if !providers.IsAuthError(err) {
		return page, err
	}
	token, mintErr := s.bearerToken(ctx, creds, settings)
	if mintErr != nil {
		return devicePage{}, mintErr
	}
	return s.fetchPageOnce(ctx, token, requestURL)
}

// TestConnection proves the stored credentials against one field-selected
// device page. This validates the tenant id, the client credentials, and
// the admin-consented DeviceManagementManagedDevices.Read.All permission in
// one probe. ($top is deliberately not sent — Intune's restricted OData
// subset makes the server-sized page the only shape guaranteed to work,
// and a field-selected page is small.)
func (s *source) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	if _, err := s.fetchDevicesPage(ctx, creds, settings, ""); err != nil {
		return fmt.Errorf("intune connection test: %w", err)
	}
	return nil
}

// userEmail picks the attribution email: the Intune-recorded email address
// when present, else the user principal name (which is usually, but not
// always, the primary email). Both empty means unassigned.
func (d managedDevice) userEmail() string {
	if email := strings.TrimSpace(d.EmailAddress); email != "" {
		return email
	}
	return strings.TrimSpace(d.UserPrincipalName)
}

// ListDevices returns one page of the tenant's managed devices. The cursor
// is Graph's own @odata.nextLink URL; an empty cursor requests the first
// page and an absent nextLink ends the listing — the server owns pagination
// consistency, so there is no offset-shift churn hazard here.
func (s *source) ListDevices(ctx context.Context, creds providers.Credentials, settings providers.Settings, cursor string) (providers.DevicePage, error) {
	page, err := s.fetchDevicesPage(ctx, creds, settings, cursor)
	if err != nil {
		return providers.DevicePage{Devices: nil, NextCursor: ""}, err
	}

	devices := make([]providers.Device, 0, len(page.Value))
	for _, raw := range page.Value {
		var d managedDevice
		if err := json.Unmarshal(raw, &d); err != nil {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("decode managed device: %w", err)
		}
		// The Graph id anchors upserts (external_id is unique per config);
		// a record without one cannot be tracked, so fail loudly rather
		// than silently churn a phantom device.
		if strings.TrimSpace(d.ID) == "" {
			return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("managed device carried no id")
		}
		var lastSync time.Time
		if d.LastSyncDateTime != "" {
			// Graph emits RFC3339. An unparseable value fails loudly: a
			// format drift would otherwise silently NULL every stored
			// check-in on the next pull, fleet-wide.
			parsed, err := time.Parse(time.RFC3339, d.LastSyncDateTime)
			if err != nil {
				return providers.DevicePage{Devices: nil, NextCursor: ""}, fmt.Errorf("device %s carried unparseable lastSyncDateTime %q", d.ID, d.LastSyncDateTime)
			}
			lastSync = parsed.UTC()
		}
		devices = append(devices, providers.Device{
			ExternalID:    d.ID,
			SerialNumber:  d.SerialNumber,
			Hostname:      d.DeviceName,
			OSName:        d.OperatingSystem,
			OSVersion:     d.OSVersion,
			UserEmail:     d.userEmail(),
			LastCheckInAt: lastSync,
			Raw:           raw,
		})
	}

	return providers.DevicePage{Devices: devices, NextCursor: page.NextLink}, nil
}
