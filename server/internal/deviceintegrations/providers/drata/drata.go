// Package drata implements the Drata evidence-sink provider: it pushes
// per-device agent-coverage evidence into a customer's Drata workspace via
// the Custom Connections API, so "AI usage on endpoints is monitored" is a
// continuously-tested control instead of quarterly screenshots.
//
// Customer-side setup, for the docs: create a Custom Connection (Drata
// console or POST /public/v2/custom-connections) with providerTypes
// ["CUSTOM"], displayNameKey "hostname", and this record schema, then paste
// the connection id into Gram:
//
//	{
//	  "type": "object",
//	  "properties": {
//	    "id": { "type": "string" },
//	    "serialNumber": { "type": "string" },
//	    "hostname": { "type": "string" },
//	    "assignedUserEmail": { "type": "string" },
//	    "assignedUserAgentActive": { "type": "boolean" },
//	    "assignedUserAgentLastSeenAt": { "type": "string" }
//	  }
//	}
//
// Evidence precision: agent presence is attested per assigned USER, not per
// device — the field names say exactly that (assignedUserAgentActive, never
// "device_monitored"). An auditor consuming a stronger claim than we can
// support is worse than no integration.
//
// Endpoints used (all under the region's public API base URL):
//
//	GET  /public/v2/custom-connections/{id}?expand[]=customResources — connection test + resource discovery
//	POST /public/v2/custom-connections/{id}/resources/{rid}/sessions/{sid} — batched record upload
//	POST /public/v2/custom-connections/{id}/resources/{rid}/sessions/{sid}/actions — complete the session
//
// Sessions give true snapshot-replace semantics: completing a session makes
// it the authoritative dataset and Drata deletes every record not in it, so
// retries can never duplicate records and departed devices never linger as
// stale evidence. An abandoned (never-completed) session from a failed push
// changes nothing.
package drata

import (
	"bytes"
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
)

const (
	// ProviderID is the registry discriminator stored on config rows.
	ProviderID = "drata"

	// ScheduleEvidence is the provider's single sync pipeline.
	ScheduleEvidence = "drata_evidence"

	// userAgent identifies this integration on every API request.
	userAgent = "Speakeasy-Gram/1.0"

	// recordBatchSize bounds records per upload request. Drata's rate limit
	// is 500 requests/minute per source IP, so batching is mandatory: even a
	// 100k-device fleet stays around 200 requests per push at this size.
	recordBatchSize = 500

	// maxResponseBytes bounds each response body read. Drata responses here
	// are small envelopes (a connection object, upload acknowledgements).
	maxResponseBytes = 1 * 1024 * 1024

	fieldRegion       = "region"
	fieldConnectionID = "connection_id"
	fieldAPIKey       = "api_key"
)

// regionBaseURLs allowlists the customer-selectable Drata regions. Deriving
// the base URL from a closed set — rather than accepting a URL — means a
// config can never point the bearer key at an arbitrary host.
var regionBaseURLs = map[string]string{
	"us":   "https://public-api.drata.com",
	"eu":   "https://public-api.eu.drata.com",
	"apac": "https://public-api.apac.drata.com",
}

func init() {
	providers.Register(providers.Descriptor{
		ID:           ProviderID,
		DisplayName:  "Drata",
		Capabilities: []providers.Capability{providers.CapabilityEvidenceSink},
		Fields: []providers.CredentialField{
			{Key: fieldRegion, Label: "Region (us, eu, or apac)", Kind: providers.FieldKindText, Secret: false, Required: true},
			{Key: fieldConnectionID, Label: "Custom Connection ID", Kind: providers.FieldKindText, Secret: false, Required: true},
			{Key: fieldAPIKey, Label: "API Key", Kind: providers.FieldKindText, Secret: true, Required: true},
		},
		Schedules: []providers.ScheduleSpec{
			{Schedule: ScheduleEvidence, Capability: providers.CapabilityEvidenceSink, Interval: time.Hour},
		},
		NewInventorySource: nil,
		NewEvidenceSink: func(deps providers.Deps) providers.EvidenceSink {
			return &sink{client: deps.Client, baseOverride: ""}
		},
	})
}

// sink is one Drata API session. Auth is a static bearer key, so the sink is
// stateless.
type sink struct {
	client *guardian.HTTPClient
	// baseOverride replaces the region-derived base URL in unit tests, where
	// the endpoint is an httptest server rather than a real region. Region
	// validation still runs first.
	baseOverride string
}

var _ providers.EvidenceSink = (*sink)(nil)

// baseURL resolves the configured region against the allowlist.
func (s *sink) baseURL(settings providers.Settings) (string, error) {
	region := strings.ToLower(strings.TrimSpace(settings[fieldRegion]))
	if region == "" {
		return "", fmt.Errorf("region is not configured")
	}
	base, ok := regionBaseURLs[region]
	if !ok {
		return "", fmt.Errorf("region must be one of us, eu, or apac")
	}
	if s.baseOverride != "" {
		return s.baseOverride, nil
	}
	return base, nil
}

// connectionPath returns the connection's URL path root, validating the
// configured id.
func connectionPath(settings providers.Settings) (string, error) {
	id := strings.TrimSpace(settings[fieldConnectionID])
	if id == "" {
		return "", fmt.Errorf("connection_id is not configured")
	}
	return "/public/v2/custom-connections/" + url.PathEscape(id), nil
}

// doJSON issues one authenticated request and returns the response body.
// Status classification happens before the body is read: the error branches
// never need it, and an oversized error response must not mask a credential
// rejection as a generic read failure.
func (s *sink) doJSON(ctx context.Context, creds providers.Credentials, method, requestURL string, payload any) ([]byte, error) {
	var body *bytes.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds[fieldAPIKey])
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, providers.NewAuthError(fmt.Errorf("request to %s rejected with status %d", req.URL.Path, resp.StatusCode))
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, fmt.Errorf("request to %s failed with status %d", req.URL.Path, resp.StatusCode)
	}

	respBody, err := providers.ReadBoundedBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", req.URL.Path, err)
	}
	return respBody, nil
}

// resolveResourceID fetches the connection with its resources expanded and
// returns the record resource's id. The resource is created by Drata when
// the customer creates the connection, so discovering it here means the
// customer pastes exactly one id into Gram and a connection/resource
// mismatch is impossible.
func (s *sink) resolveResourceID(ctx context.Context, creds providers.Credentials, settings providers.Settings) (string, error) {
	base, err := s.baseURL(settings)
	if err != nil {
		return "", err
	}
	connPath, err := connectionPath(settings)
	if err != nil {
		return "", err
	}

	body, err := s.doJSON(ctx, creds, http.MethodGet, base+connPath+"?expand[]=customResources", nil)
	if err != nil {
		return "", err
	}

	var connection struct {
		CustomResources []struct {
			ID json.Number `json:"id"`
		} `json:"customResources"`
	}
	if err := json.Unmarshal(body, &connection); err != nil {
		return "", fmt.Errorf("decode custom connection: %w", err)
	}
	if len(connection.CustomResources) == 0 {
		return "", fmt.Errorf("custom connection has no resource; recreate it with a record schema")
	}
	resourceID := connection.CustomResources[0].ID.String()
	if resourceID == "" {
		return "", fmt.Errorf("custom connection resource carried no id")
	}
	return resourceID, nil
}

// TestConnection proves the stored credentials with the same read the push
// path depends on: fetching the connection and discovering its resource.
func (s *sink) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	if _, err := s.resolveResourceID(ctx, creds, settings); err != nil {
		return fmt.Errorf("drata connection test: %w", err)
	}
	return nil
}

// coverageRecord is one device's evidence record in the pushed schema. Field
// names deliberately scope the attestation to the assigned user.
type coverageRecord struct {
	ID                          string  `json:"id"`
	SerialNumber                string  `json:"serialNumber"`
	Hostname                    string  `json:"hostname"`
	AssignedUserEmail           string  `json:"assignedUserEmail"`
	AssignedUserAgentActive     bool    `json:"assignedUserAgentActive"`
	AssignedUserAgentLastSeenAt *string `json:"assignedUserAgentLastSeenAt"`
}

func buildRecords(snapshot providers.CoverageSnapshot) []coverageRecord {
	records := make([]coverageRecord, 0, len(snapshot.Devices))
	for _, d := range snapshot.Devices {
		var lastSeen *string
		if !d.AssignedUserAgentLastSeenAt.IsZero() {
			formatted := d.AssignedUserAgentLastSeenAt.UTC().Format(time.RFC3339)
			lastSeen = &formatted
		}
		records = append(records, coverageRecord{
			ID:                          d.ExternalID,
			SerialNumber:                d.SerialNumber,
			Hostname:                    d.Hostname,
			AssignedUserEmail:           d.UserEmail,
			AssignedUserAgentActive:     d.AssignedUserAgentActive,
			AssignedUserAgentLastSeenAt: lastSeen,
		})
	}
	return records
}

// PushCoverage replaces the connection's evidence with the snapshot: batched
// uploads into a fresh session, then a completing action that makes the
// session authoritative (Drata deletes everything not in it). An empty
// snapshot still pushes — completing an empty session clears stale evidence,
// which is the truthful state for an empty fleet.
func (s *sink) PushCoverage(ctx context.Context, creds providers.Credentials, settings providers.Settings, snapshot providers.CoverageSnapshot) error {
	base, err := s.baseURL(settings)
	if err != nil {
		return err
	}
	connPath, err := connectionPath(settings)
	if err != nil {
		return err
	}
	resourceID, err := s.resolveResourceID(ctx, creds, settings)
	if err != nil {
		return fmt.Errorf("resolve drata resource: %w", err)
	}

	// One fresh session per push attempt (Drata allows 3-64
	// alphanumeric/hyphen/underscore chars). Uniqueness per attempt — not
	// per snapshot — matters: a retry must never touch a session an earlier
	// attempt may already have completed. A failed attempt abandons its
	// session harmlessly, and because completion replaces wholesale, two
	// attempts pushing the same snapshot converge to the same dataset.
	sessionID := "gram-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sessionPath := base + connPath + "/resources/" + url.PathEscape(resourceID) + "/sessions/" + sessionID

	records := buildRecords(snapshot)
	for start := 0; start < len(records) || start == 0; start += recordBatchSize {
		batch := records[start:min(start+recordBatchSize, len(records))]
		if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath, map[string]any{"data": batch}); err != nil {
			return fmt.Errorf("upload evidence batch: %w", err)
		}
	}

	if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath+"/actions", map[string]any{"action": "complete"}); err != nil {
		return fmt.Errorf("complete evidence session: %w", err)
	}
	return nil
}
