// Package vanta implements the Vanta evidence-sink provider: it pushes
// per-device agent-coverage evidence into a customer's Vanta tenant via a
// private integration, so agent coverage becomes continuously-tested
// compliance evidence (a Custom Test over the pushed properties fails when a
// device's assigned user has no live agent).
//
// Customer-side setup, for the docs: create a private integration in the
// Vanta Developer Console with the connectors.self:write-resource scope,
// define a Custom Resource whose schema mirrors the customProperties below,
// copy its resource id from the Resources tab, then define a Custom Test
// over agent_active mapped to the relevant controls.
//
// Evidence precision: every resource states its own attestation strength.
// agent_attestation "device" means that machine's agent reported in (matched
// on hardware serial); "user" means only that its assigned user runs one
// somewhere (matched on email). Both can appear in one push — a machine whose
// agent cannot read a serial stays user-attested even for an org on
// device-level matching — so the strength cannot be stated once for the whole
// resource set. agent_last_seen_at is an empty string when no agent has ever
// synced: Vanta's console cannot author a JTD optionalProperties schema, so
// the customer-defined record schema marks every property required and an
// omitted field is rejected at sync; an empty string reads as "unknown"
// without fabricating a heartbeat. The ticket suggested Vanta's built-in
// Computer resource kind, but built-in kinds carry fixed schemas that cannot
// express these properties; a Custom Resource is what lets the Custom Test
// attest exactly what we can prove. Every record must also carry a top-level
// externalUrl — Vanta's base schema requires it alongside uniqueId and
// displayName.
//
// Endpoints used (Vanta documents a single global API host):
//
//	POST /oauth/token — client-credentials mint; tokens live one hour and
//	Vanta allows ONE active token per application (a new mint revokes the
//	previous), so the token is minted once per push run and cached.
//	PUT /v1/resources/custom_resource — full-state sync of the fleet.
//
// Vanta's sync is full-state PUT: each request is the complete resource set
// and any previously pushed uniqueId omitted from a later PUT is marked
// gone. That is exactly PushCoverage's snapshot-replace contract — and it
// also means the whole fleet MUST travel in one request; splitting across
// requests would mark each earlier batch gone.
package vanta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ProviderID is the registry discriminator stored on config rows.
	ProviderID = "vanta"

	// ScheduleEvidence is the provider's single sync pipeline.
	ScheduleEvidence = "vanta_evidence"

	// userAgent identifies this integration on every API request.
	userAgent = "Speakeasy-Gram/1.0"

	// defaultBaseURL is Vanta's canonical API host. Regional hosts exist
	// (api.eu.vanta.com, api.aus.vanta.com) but permanently redirect every
	// path here — verified empirically — so a region setting would be dead
	// weight. Fixed rather than customer-supplied so a config can never
	// point the OAuth credentials at an arbitrary host.
	defaultBaseURL = "https://api.vanta.com"

	// tokenScope is the least privilege the sync needs: writing the
	// integration's own resources.
	tokenScope = "connectors.self:write-resource"

	// tokenExpirySlack renews the cached token this long before its actual
	// expiry so in-flight requests never race the cutoff. Vanta tokens live
	// an hour, far above the slack.
	tokenExpirySlack = 30 * time.Second

	// maxResponseBytes bounds each response body read. Vanta responses here
	// are small envelopes (a token, accepted/rejected counts).
	maxResponseBytes = 1 * 1024 * 1024

	fieldClientID     = "client_id"
	fieldClientSecret = "client_secret"
	fieldResourceID   = "resource_id"
)

func init() {
	providers.Register(providers.Descriptor{
		ID:           ProviderID,
		DisplayName:  "Vanta",
		Capabilities: []providers.Capability{providers.CapabilityEvidenceSink},
		Fields: []providers.CredentialField{
			// The client id is technically public, but it rotates together
			// with the secret as one credential pair (matching Jamf's API
			// client id), so both live in the write-only blob.
			{Key: fieldClientID, Label: "OAuth Client ID", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: fieldClientSecret, Label: "OAuth Client Secret", Kind: providers.FieldKindText, Secret: true, Required: true},
			{Key: fieldResourceID, Label: "Custom Resource ID", Kind: providers.FieldKindText, Secret: false, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: ScheduleEvidence, Capability: providers.CapabilityEvidenceSink, Interval: time.Hour},
		},
		NewInventorySource: nil,
		NewEvidenceSink: func(deps providers.Deps) providers.EvidenceSink {
			return newSink(deps.Client, defaultBaseURL)
		},
	})
}

// sink is one Vanta API session. The sync runner constructs a fresh sink per
// run, so the token cache spans one full push — deliberate, because Vanta
// allows a single active token per application and minting per request would
// revoke the token out from under concurrent calls.
type sink struct {
	client *guardian.HTTPClient
	// baseURL is defaultBaseURL in production and an httptest server in
	// unit tests.
	baseURL string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

var _ providers.EvidenceSink = (*sink)(nil)

// newSink is the one construction site for both production wiring and unit
// tests, so a future sink field cannot be zero-valued in tests only
// (test files are exempt from exhaustruct).
func newSink(client *guardian.HTTPClient, baseURL string) *sink {
	return &sink{client: client, baseURL: baseURL, mu: sync.Mutex{}, token: "", tokenExpiry: time.Time{}}
}

// bearerToken returns a cached OAuth token, minting one via the
// client-credentials grant when absent or near expiry.
func (s *sink) bearerToken(ctx context.Context, creds providers.Credentials) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExpiry) {
		return s.token, nil
	}

	payload, err := json.Marshal(map[string]string{
		"client_id":     creds[fieldClientID],
		"client_secret": creds[fieldClientSecret],
		"grant_type":    "client_credentials",
		"scope":         tokenScope,
	})
	if err != nil {
		return "", fmt.Errorf("encode token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/oauth/token", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", fmt.Errorf("token request failed with status %d", resp.StatusCode)
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
		// A missing or nonsensical expires_in must not poison the cache
		// into minting per call — under the one-active-token rule every
		// extra mint revokes a token someone may be using. Vanta documents
		// hour-long tokens; ten minutes is a safe floor.
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

// resourceID validates the configured Custom Resource id.
func resourceID(settings providers.Settings) (string, error) {
	id := strings.TrimSpace(settings[fieldResourceID])
	if id == "" {
		return "", fmt.Errorf("resource_id is not configured")
	}
	return id, nil
}

// TestConnection proves the stored OAuth credentials by minting a token.
// The resource id has no cheap read to validate against under the
// write-only scope; a typo there surfaces as a rejected first push. Note
// that minting revokes any token a concurrently running push holds — the
// push's single in-run re-mint retry absorbs one such revocation, but the
// OAuth app should be dedicated to this integration.
func (s *sink) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	if _, err := resourceID(settings); err != nil {
		return fmt.Errorf("vanta connection test: %w", err)
	}
	if _, err := s.bearerToken(ctx, creds); err != nil {
		return fmt.Errorf("vanta connection test: %w", err)
	}
	return nil
}

// externalURL is the required base-field link on every pushed resource.
// Vanta's CustomResource base schema mandates uniqueId, displayName, AND
// externalUrl — an omitted externalUrl is rejected with 400 "must have
// property 'externalUrl'". There is no per-device dashboard deep link
// available in the snapshot (no org slug), so every record carries the same
// product URL; uniqueId, not this field, is the dedup key.
const externalURL = "https://app.getgram.ai"

// coverageResource is one device in the pushed full-state set. Base fields
// (uniqueId, displayName, externalUrl) sit at the top level per Vanta's
// resource envelope; the evidence properties live in customProperties, named
// to scope the attestation to the assigned user.
type coverageResource struct {
	UniqueID         string           `json:"uniqueId"`
	DisplayName      string           `json:"displayName"`
	ExternalURL      string           `json:"externalUrl"`
	CustomProperties coverageProperty `json:"customProperties"`
}

type coverageProperty struct {
	SerialNumber      string `json:"serial_number"`
	Hostname          string `json:"hostname"`
	AssignedUserEmail string `json:"assigned_user_email"`
	AgentActive       bool   `json:"agent_active"`
	// agent_attestation is what keeps agent_active honest per row: "device"
	// means this machine's own agent reported in, "user" means only that its
	// assigned user runs one somewhere. Both can appear in one push, so the
	// strength cannot be stated once for the whole resource set.
	AgentAttestation string `json:"agent_attestation"`
	// AgentLastSeenAt is always present, empty string when no agent has ever
	// synced. It is NOT omitted for a never-seen agent: Vanta's console
	// cannot express a JTD optionalProperties schema, so the customer-defined
	// record schema marks every property required, and an omitted field is
	// rejected. An empty string is honestly "unknown" — unlike a null or a
	// zero timestamp, it fabricates no heartbeat — so it satisfies the
	// required-property rule without overstating coverage.
	AgentLastSeenAt string `json:"agent_last_seen_at"`
}

func buildResources(snapshot providers.CoverageSnapshot) ([]coverageResource, error) {
	resources := make([]coverageResource, 0, len(snapshot.Devices))
	seen := make(map[string]bool, len(snapshot.Devices))
	for _, d := range snapshot.Devices {
		// uniqueId anchors Vanta's idempotent upsert: an empty id cannot be
		// tracked and a duplicate would silently overwrite a sibling's
		// evidence with accepted counts still adding up — both fail loudly.
		if strings.TrimSpace(d.ExternalID) == "" {
			return nil, fmt.Errorf("coverage device carried no external id")
		}
		if seen[d.ExternalID] {
			return nil, fmt.Errorf("duplicate device external id %q in coverage snapshot", d.ExternalID)
		}
		seen[d.ExternalID] = true
		// Empty string, not a zero timestamp, when no agent has ever synced:
		// the field is required by the customer's schema but must not imply a
		// heartbeat that never happened.
		lastSeen := ""
		if !d.AgentLastSeenAt.IsZero() {
			lastSeen = d.AgentLastSeenAt.UTC().Format(time.RFC3339)
		}
		displayName := d.Hostname
		if displayName == "" {
			displayName = d.ExternalID
		}
		resources = append(resources, coverageResource{
			UniqueID:    d.ExternalID,
			DisplayName: displayName,
			ExternalURL: externalURL,
			CustomProperties: coverageProperty{
				SerialNumber:      d.SerialNumber,
				Hostname:          d.Hostname,
				AssignedUserEmail: d.UserEmail,
				AgentActive:       d.AgentActive,
				AgentAttestation:  string(d.AgentAttestation),
				AgentLastSeenAt:   lastSeen,
			},
		})
	}
	return resources, nil
}

// putResources performs one full-state PUT attempt and classifies the
// outcome. On 401/403 the token cache is dropped before returning the auth
// error so the caller can re-mint.
func (s *sink) putResources(ctx context.Context, token string, resource string, payload []byte, recordCount int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.baseURL+"/v1/resources/custom_resource", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build resource sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("request resource sync: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// Vanta's single-active-token rule means an external mint (a
		// connection test, another consumer of the same OAuth app) can
		// revoke ours between mint and PUT. Drop the cache so the caller's
		// retry re-mints.
		s.mu.Lock()
		s.token = ""
		s.mu.Unlock()
		return providers.NewAuthError(fmt.Errorf("resource sync rejected with status %d", resp.StatusCode))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// Include the record count: a 413 on a huge single-request
		// full-state payload should read as a size ceiling, not a mystery.
		return fmt.Errorf("resource sync of %d records failed with status %d", recordCount, resp.StatusCode)
	}

	body, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return fmt.Errorf("read resource sync response: %w", err)
	}
	// Vanta's full-state PUT is all-or-nothing: a valid set returns 200
	// {"success": true}, and any schema violation fails the whole request
	// with a 4xx {"error": ...} (already handled by the status switch above)
	// — there is no per-record accepted/rejected accounting, verified against
	// the live API. Success is confirmed by the body rather than the bare
	// 2xx: a success field present and false, or a drifted envelope missing
	// it, must not be read as a completed sync.
	var result struct {
		Success *bool `json:"success"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode resource sync response: %w", err)
	}
	if result.Success == nil || !*result.Success {
		return fmt.Errorf("resource sync of %d records did not report success", recordCount)
	}
	return nil
}

// PushCoverage replaces the integration's resource set with the snapshot in
// one full-state PUT. Vanta marks any previously pushed uniqueId omitted
// here as gone, so an empty fleet truthfully clears stale evidence — and
// the whole fleet must travel in this single request (splitting across
// requests would mark each earlier part gone). The PUT is idempotent on
// uniqueId, so retries cannot duplicate — including the single in-run
// re-mint retry below, which absorbs one external token revocation (the
// one-active-token rule means any other mint on the same OAuth app revokes
// ours) without booking a spurious auth rejection.
func (s *sink) PushCoverage(ctx context.Context, creds providers.Credentials, settings providers.Settings, snapshot providers.CoverageSnapshot) error {
	resource, err := resourceID(settings)
	if err != nil {
		return err
	}
	resources, err := buildResources(snapshot)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"resourceId": resource,
		"resources":  resources,
	})
	if err != nil {
		return fmt.Errorf("encode resource sync: %w", err)
	}

	token, err := s.bearerToken(ctx, creds)
	if err != nil {
		return err
	}
	err = s.putResources(ctx, token, resource, payload, len(resources))
	if !providers.IsAuthError(err) {
		return err
	}
	// One revocation is absorbed; a second rejection is a real credential
	// problem and surfaces as the auth error it is.
	token, mintErr := s.bearerToken(ctx, creds)
	if mintErr != nil {
		return mintErr
	}
	return s.putResources(ctx, token, resource, payload, len(resources))
}
