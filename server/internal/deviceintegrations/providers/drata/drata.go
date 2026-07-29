// Package drata implements the Drata evidence-sink provider: it pushes
// per-device agent-coverage evidence into a customer's Drata workspace via
// the Custom Connections API, so "AI usage on endpoints is monitored" is a
// continuously-tested control instead of quarterly screenshots.
//
// Customer-side setup, for the docs: create a Custom Connection (Drata
// console or POST /public/v2/custom-connections) with providerTypes
// ["CUSTOM"], displayNameKey "hostname", and this record schema, then paste
// the connection id into Gram. The connection must be dedicated to Gram —
// pushes replace the connection's records wholesale, and the sink refuses
// connections carrying more than one resource:
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
// "device_monitored"), and assignedUserAgentLastSeenAt is omitted (not
// null, not zero) when the assigned user has never synced an agent. An
// auditor consuming a stronger claim than we can support is worse than no
// integration.
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
// changes nothing. Known limitation: completions are last-writer-wins with
// no fencing token, so a zombie push attempt (a worker partitioned from
// Temporal but not from Drata) that completes its session after a newer
// attempt briefly reverts evidence to the older snapshot until the next
// fleet change re-pushes.
package drata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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

// defaultRegions allowlists the customer-selectable Drata regions. Deriving
// the base URL from a closed set — rather than accepting a URL — means a
// config can never point the bearer key at an arbitrary host. The sink
// carries its own copy (tests inject an httptest table), so the allowlist a
// request consults is always exactly the one its constructor was given.
var defaultRegions = map[string]string{
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
			return &sink{client: deps.Client, regions: defaultRegions}
		},
	})
}

// sink is one Drata API session. Auth is a static bearer key, so the sink is
// stateless.
type sink struct {
	client *guardian.HTTPClient
	// regions is the region→base-URL allowlist: defaultRegions in
	// production, an httptest table in unit tests.
	regions map[string]string
}

var _ providers.EvidenceSink = (*sink)(nil)

// target is the resolved, validated push destination: the base URL and the
// connection's path root, derived exactly once per operation so the
// connection-test path and the push path can never drift apart.
type target struct {
	base     string
	connPath string
}

// resolveTarget validates the configured region and connection id.
func (s *sink) resolveTarget(settings providers.Settings) (target, error) {
	region := strings.ToLower(strings.TrimSpace(settings[fieldRegion]))
	if region == "" {
		return target{}, fmt.Errorf("region is not configured")
	}
	base, ok := s.regions[region]
	if !ok {
		return target{}, fmt.Errorf("region must be one of us, eu, or apac")
	}
	id := strings.TrimSpace(settings[fieldConnectionID])
	if id == "" {
		return target{}, fmt.Errorf("connection_id is not configured")
	}
	return target{
		base:     base,
		connPath: "/public/v2/custom-connections/" + url.PathEscape(id),
	}, nil
}

// doJSON issues one authenticated request and returns the response body.
// Status classification happens before the body is read: the error branches
// never need it, and an oversized error response must not mask a credential
// rejection as a generic read failure.
func (s *sink) doJSON(ctx context.Context, creds providers.Credentials, method, requestURL string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request payload: %w", err)
		}
		body = bytes.NewReader(encoded)
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
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

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

// resourceID tolerates Drata serializing resource ids as JSON numbers or
// strings — the reference documents numbers, but a string-typed id must not
// permanently brick the integration.
type resourceID string

func (r *resourceID) UnmarshalJSON(data []byte) error {
	trimmed := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if trimmed == "null" {
		trimmed = ""
	}
	*r = resourceID(trimmed)
	return nil
}

// resolveResourceID fetches the connection with its resources expanded and
// returns the record resource's id. The resource is created by Drata when
// the customer creates the connection, so discovering it here means the
// customer pastes exactly one id into Gram and a connection/resource
// mismatch is impossible. A connection carrying more than one resource is
// refused outright: pushes wholesale-replace a resource's records, and
// guessing among resources risks destroying unrelated data.
func (s *sink) resolveResourceID(ctx context.Context, creds providers.Credentials, tgt target) (string, error) {
	body, err := s.doJSON(ctx, creds, http.MethodGet, tgt.base+tgt.connPath+"?expand[]=customResources", nil)
	if err != nil {
		return "", err
	}

	var connection struct {
		CustomResources []struct {
			ID resourceID `json:"id"`
		} `json:"customResources"`
	}
	if err := json.Unmarshal(body, &connection); err != nil {
		return "", fmt.Errorf("decode custom connection: %w", err)
	}
	if len(connection.CustomResources) == 0 {
		return "", fmt.Errorf("custom connection has no resource; recreate it with a record schema")
	}
	if len(connection.CustomResources) > 1 {
		return "", fmt.Errorf("custom connection has %d resources; use a connection dedicated to Gram with exactly one", len(connection.CustomResources))
	}
	id := string(connection.CustomResources[0].ID)
	if id == "" {
		return "", fmt.Errorf("custom connection resource carried no id")
	}
	return id, nil
}

// TestConnection proves the stored credentials with the same read the push
// path depends on: fetching the connection and discovering its resource.
func (s *sink) TestConnection(ctx context.Context, creds providers.Credentials, settings providers.Settings) error {
	tgt, err := s.resolveTarget(settings)
	if err != nil {
		return fmt.Errorf("drata connection test: %w", err)
	}
	if _, err := s.resolveResourceID(ctx, creds, tgt); err != nil {
		return fmt.Errorf("drata connection test: %w", err)
	}
	return nil
}

// coverageRecord is one device's evidence record in the pushed schema. Field
// names deliberately scope the attestation to the assigned user. The
// last-seen field is omitted entirely for a never-seen agent — the declared
// record schema types it as a plain string, and an explicit null (or a zero
// timestamp masquerading as evidence) would overstate what we can prove.
type coverageRecord struct {
	ID                          string  `json:"id"`
	SerialNumber                string  `json:"serialNumber"`
	Hostname                    string  `json:"hostname"`
	AssignedUserEmail           string  `json:"assignedUserEmail"`
	AssignedUserAgentActive     bool    `json:"assignedUserAgentActive"`
	AssignedUserAgentLastSeenAt *string `json:"assignedUserAgentLastSeenAt,omitempty"`
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
// session authoritative (Drata deletes everything not in it).
func (s *sink) PushCoverage(ctx context.Context, creds providers.Credentials, settings providers.Settings, snapshot providers.CoverageSnapshot) error {
	tgt, err := s.resolveTarget(settings)
	if err != nil {
		return err
	}
	resource, err := s.resolveResourceID(ctx, creds, tgt)
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
	sessionPath := tgt.base + tgt.connPath + "/resources/" + url.PathEscape(resource) + "/sessions/" + sessionID

	// Transport-level retries (the shared client resends a batch on 429/5xx
	// whose response was lost) are safe here: Drata matches records by
	// their "id" field — "if a record with that ID already exists it is
	// updated" — so a resent batch upserts rather than duplicates, and
	// completion publishes each id once.
	records := buildRecords(snapshot)
	if len(records) == 0 {
		// An empty fleet still pushes: sessions are created implicitly by
		// their first upload, so one empty batch makes the session exist,
		// and completing it clears stale evidence — the truthful state.
		if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath, map[string]any{"data": []coverageRecord{}}); err != nil {
			return fmt.Errorf("upload evidence batch: %w", err)
		}
	}
	for batch := range slices.Chunk(records, recordBatchSize) {
		if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath, map[string]any{"data": batch}); err != nil {
			return fmt.Errorf("upload evidence batch: %w", err)
		}
	}

	if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath+"/actions", map[string]any{"action": "complete"}); err != nil {
		return fmt.Errorf("complete evidence session: %w", err)
	}
	return nil
}
