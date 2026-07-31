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
//	    "agentActive": { "type": "boolean" },
//	    "agentAttestation": { "type": "string" },
//	    "agentLastSeenAt": { "type": "string" }
//	  }
//	}
//
// Evidence precision: every record states its own attestation strength.
// agentAttestation "device" means that machine's agent reported in (matched
// on hardware serial); "user" means only that its assigned user runs one
// somewhere (matched on email). Both can appear in one push, because a
// machine whose agent cannot read a serial stays user-attested even for an
// org on device-level matching. agentLastSeenAt is omitted (not null, not
// zero) when no agent has ever synced. An auditor consuming a stronger claim
// than we can support is worse than no integration.
//
// Endpoints used (all under the region's public API base URL):
//
//	GET    /public/v2/custom-connections/{id}?expand[]=customResources — connection test + resource discovery
//	GET    /public/v2/custom-connections/{id}/resources/{rid}/sessions?status=IN_PROGRESS — stranded-session sweep
//	POST   /public/v2/custom-connections/{id}/resources/{rid}/sessions/{sid} — batched record upload
//	POST   /public/v2/custom-connections/{id}/resources/{rid}/sessions/{sid}/actions — cancel a strand, then complete the session
//	GET    /public/v2/custom-connections/{id}/resources/{rid}/records — enumerate live records (empty-fleet clear)
//	DELETE /public/v2/custom-connections/{id}/resources/{rid}/records/{recordId} — delete one record (empty-fleet clear)
//
// Sessions give true snapshot-replace semantics: completing a session makes
// it the authoritative dataset and Drata deletes every record not in it, so
// retries can never duplicate records and departed devices never linger as
// stale evidence. An abandoned session is NOT inert, however: Drata permits
// only one IN_PROGRESS session per connection/resource, so a push that dies
// mid-upload would wedge every later push. Each push therefore cancels any
// stranded session before opening its own. Known limitation: completions are
// last-writer-wins with no fencing token, so a zombie push attempt (a worker
// partitioned from Temporal but not from Drata) that completes its session
// after a newer attempt briefly reverts evidence to the older snapshot until
// the next fleet change re-pushes.
package drata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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

	// sessionStatusInProgress is the only session state that blocks a new
	// push: Drata permits one such session per connection/resource.
	sessionStatusInProgress = "IN_PROGRESS"

	fieldRegion       = "region"
	fieldWorkspaceID  = "workspace_id"
	fieldConnectionID = "connection_id"
	fieldAPIKey       = "api_key"

	// provisionConnectionName is the display-name stem of the connection Gram
	// creates and reuses on the customer's behalf. The effective name appends a
	// per-Gram-org suffix (see connectionNameForOrg): find-or-create keys on the
	// full name, so a re-save reuses the same connection, and two Gram orgs that
	// happen to share one Drata tenant each own a distinct connection instead of
	// racing to create — or later clobbering — a single shared one.
	provisionConnectionName = "Speakeasy Device Agent Coverage"

	// defaultWorkspaceID is the workspace a connection is created in when the
	// customer leaves the field blank. Drata requires workspaceIds on create,
	// and single-workspace accounts — the common case — are workspace 1.
	defaultWorkspaceID = 1

	// displayNameKey names the record field Drata shows as each row's label.
	displayNameKey = "hostname"

	// connectionListLimit is the page size for the find-existing lookup.
	connectionListLimit = 200

	// maxConnectionListPages bounds how far find-existing pages before it fails
	// the provision (reaching the cap is treated as unproven-absence, not
	// not-found, so it never silently creates a duplicate). 50 × the page size
	// is far more connections than any tenant realistically has; the bound only
	// exists so a cursor that never clears cannot loop forever.
	maxConnectionListPages = 50
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
			{Key: fieldWorkspaceID, Label: "Workspace ID (usually 1)", Kind: providers.FieldKindText, Secret: false, Required: false},
			// Optional: Gram creates and fills this in on connect. A customer
			// may still supply an existing connection to reuse instead.
			{Key: fieldConnectionID, Label: "Custom Connection ID (optional — created automatically)", Kind: providers.FieldKindText, Secret: false, Required: false},
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

var (
	_ providers.EvidenceSink = (*sink)(nil)
	// The sink also provisions its own Custom Connection during connect.
	_ providers.Provisioner = (*sink)(nil)
)

// target is the resolved, validated push destination: the base URL and the
// connection's path root, derived exactly once per operation so the
// connection-test path and the push path can never drift apart.
type target struct {
	base     string
	connPath string
}

// regionBaseURL resolves the configured region to its API base URL. Split out
// from resolveTarget because provisioning needs the base before a connection
// id exists.
func (s *sink) regionBaseURL(settings providers.Settings) (string, error) {
	region := strings.ToLower(strings.TrimSpace(settings[fieldRegion]))
	if region == "" {
		return "", fmt.Errorf("region is not configured")
	}
	base, ok := s.regions[region]
	if !ok {
		return "", fmt.Errorf("region must be one of us, eu, or apac")
	}
	return base, nil
}

// resolveTarget validates the configured region and connection id.
func (s *sink) resolveTarget(settings providers.Settings) (target, error) {
	base, err := s.regionBaseURL(settings)
	if err != nil {
		return target{}, err
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

// flexID tolerates Drata serializing ids as JSON numbers or strings. The
// reference documents resource ids as numbers, and session listings have been
// observed carrying numeric "id" fields in production — but a string-typed id
// must not permanently brick the integration either way. Only strings,
// numbers, and null decode; any other JSON value fails the surrounding
// decode loudly, because these ids become URL path segments (resource and
// session-cancel requests) and a mangled composite value must not turn into
// a bogus API call.
type flexID string

func (r *flexID) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		*r = ""
		return nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return fmt.Errorf("decode id: %w", err)
		}
		*r = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return fmt.Errorf("decode id: %w", err)
	}
	*r = flexID(n)
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
			ID flexID `json:"id"`
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

// sessionRef is one entry of the session list. Drata does not document this
// endpoint's shapes; the production API has been observed returning a
// {"data": [...], "pagination": {...}} envelope whose items carry a numeric
// "id" alongside the caller-chosen string "sessionId" — hence flexID — so
// both fields are decoded and the id is read from "sessionId" (the field the
// documented action response carries) or "id". The status is required rather
// than assumed.
type sessionRef struct {
	SessionID string `json:"sessionId"`
	ID        flexID `json:"id"`
	Status    string `json:"status"`
}

func (r sessionRef) identifier() string {
	if r.SessionID != "" {
		return r.SessionID
	}
	return string(r.ID)
}

// cancelStrandedSessions cancels every IN_PROGRESS session on the resource,
// which is how a crashed earlier run is cleaned up: Drata allows only one
// such session per connection/resource and documents no recovery path.
//
// A listing failure is fatal rather than ignored. Without the listing we
// cannot know whether a stranded session exists, and the error Drata returns
// for colliding with one is undocumented — so proceeding would trade a clear
// failure here for an unattributable one downstream.
func (s *sink) cancelStrandedSessions(ctx context.Context, creds providers.Credentials, resourcePath string) error {
	body, err := s.doJSON(ctx, creds, http.MethodGet, resourcePath+"/sessions?status=IN_PROGRESS", nil)
	if err != nil {
		return err
	}

	// The observed response is a {"data": [...]} envelope; a bare array is
	// tolerated in case the shape changes. The branch is picked by looking at
	// the payload, not by trying one decode and falling back on error — a
	// fallback would swallow the envelope's real decode error (e.g. an
	// unexpected field type) and report a useless "cannot unmarshal object
	// into []sessionRef" instead.
	var sessions []sessionRef
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &sessions); err != nil {
			return fmt.Errorf("decode session list: %w", err)
		}
	} else {
		var envelope struct {
			Data []sessionRef `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return fmt.Errorf("decode session list: %w", err)
		}
		// A null or absent "data" is an empty sweep, not an error.
		sessions = envelope.Data
	}

	for _, sess := range sessions {
		// The status filter is re-checked here rather than trusted, and that
		// is load-bearing: the production API has been observed ignoring the
		// status query param entirely (returning CANCELED sessions). An API
		// handing back ACTIVE sessions the same way would otherwise get them
		// cancelled — destroying the live evidence this integration exists
		// to publish.
		if sess.Status != sessionStatusInProgress {
			continue
		}
		id := sess.identifier()
		if id == "" {
			return fmt.Errorf("session listed with status %s carried no id", sess.Status)
		}
		if _, err := s.doJSON(ctx, creds, http.MethodPost, resourcePath+"/sessions/"+url.PathEscape(id)+"/actions", map[string]any{"action": "cancel"}); err != nil {
			return fmt.Errorf("cancel stranded session: %w", err)
		}
	}
	return nil
}

// Provision creates (or reuses) the dedicated Custom Connection this config
// pushes into, so the customer never has to hand-craft it against the Drata
// API — the setup that is otherwise a raw curl with an exact record schema and
// the easily-missed `required` list. Idempotent: a no-op when a connection id
// is already configured, and a fresh provision first looks for an existing
// Gram-created connection by name before creating one, so a re-save never
// spawns a duplicate.
func (s *sink) Provision(ctx context.Context, orgID string, creds providers.Credentials, settings providers.Settings) (providers.Settings, error) {
	if strings.TrimSpace(settings[fieldConnectionID]) != "" {
		return settings, nil
	}
	base, err := s.regionBaseURL(settings)
	if err != nil {
		return nil, fmt.Errorf("drata provisioning: %w", err)
	}
	workspaceID, err := parseWorkspaceID(settings)
	if err != nil {
		return nil, fmt.Errorf("drata provisioning: %w", err)
	}

	name := connectionNameForOrg(orgID)
	connID, err := s.findConnectionByName(ctx, creds, base, name)
	if err != nil {
		return nil, fmt.Errorf("drata provisioning: find existing connection: %w", err)
	}
	if connID == "" {
		connID, err = s.createConnection(ctx, creds, base, name, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("drata provisioning: create connection: %w", err)
		}
	}

	out := make(providers.Settings, len(settings)+1)
	maps.Copy(out, settings)
	out[fieldConnectionID] = connID
	return out, nil
}

// connectionNameForOrg is the deterministic connection name for one Gram org.
// Encoding the org into the find-or-create key is what makes provisioning
// correct when two Gram orgs share a single Drata tenant: each owns a distinct
// connection, so they neither race to create a duplicate nor later resolve to —
// and overwrite — each other's evidence. A short hash keeps the customer-facing
// name tidy while staying stable and collision-resistant across orgs.
func connectionNameForOrg(orgID string) string {
	sum := sha256.Sum256([]byte(orgID))
	return fmt.Sprintf("%s (%s)", provisionConnectionName, hex.EncodeToString(sum[:4]))
}

// parseWorkspaceID reads the customer-supplied workspace, defaulting to the
// single-workspace common case when blank. Drata requires workspaceIds on
// create; a bad value fails loudly here rather than as an opaque 400.
func parseWorkspaceID(settings providers.Settings) (int, error) {
	raw := strings.TrimSpace(settings[fieldWorkspaceID])
	if raw == "" {
		return defaultWorkspaceID, nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("workspace_id must be a positive integer")
	}
	return id, nil
}

// findConnectionByName returns the id of an existing custom connection whose
// display name matches, or "" when the scan reaches the genuine last page
// without one. Reusing a prior Gram-created connection is what keeps
// provisioning idempotent across re-saves — so the lookup follows the
// pagination cursor: a customer with more connections than one page could carry
// the Gram one onto a later page, and missing it there would create a duplicate
// on every save.
//
// Only an explicit null cursor (the true end of the list) counts as "not
// found"; a scan that hits the page cap, a cursor that won't advance, or a
// response that omits the cursor altogether leaves the connection's existence
// unproven, so it returns an error rather than "" — reporting "not found" there
// would create a duplicate on every connect, the exact bug the pagination is
// here to prevent.
func (s *sink) findConnectionByName(ctx context.Context, creds providers.Credentials, base, name string) (string, error) {
	cursor := ""
	for range maxConnectionListPages {
		requestURL := base + "/public/v2/custom-connections?limit=" + strconv.Itoa(connectionListLimit)
		if cursor != "" {
			requestURL += "&cursor=" + url.QueryEscape(cursor)
		}
		body, err := s.doJSON(ctx, creds, http.MethodGet, requestURL, nil)
		if err != nil {
			return "", err
		}
		var list struct {
			Data []struct {
				ID flexID `json:"id"`
				// Drata echoes the create-time "name" back as "clientAlias";
				// match either so the lookup is robust to which the list
				// endpoint returns.
				ClientAlias string `json:"clientAlias"`
				Name        string `json:"name"`
			} `json:"data"`
			// RawMessage, not *string, so field presence survives decoding: an
			// omitted cursor and an explicit null both decode a *string to nil,
			// but only the explicit null is Drata's end-of-list signal.
			Pagination struct {
				Cursor json.RawMessage `json:"cursor"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return "", fmt.Errorf("decode connection list: %w", err)
		}
		for _, c := range list.Data {
			if c.ClientAlias == name || c.Name == name {
				return string(c.ID), nil
			}
		}
		raw := bytes.TrimSpace(list.Pagination.Cursor)
		if len(raw) == 0 {
			// The cursor field is absent entirely (missing, or no pagination
			// object): an incomplete response, not a proof of the end. Fail
			// rather than mistake it for end-of-list and create a duplicate.
			return "", fmt.Errorf("connection list response missing pagination cursor")
		}
		if string(raw) == "null" {
			// An explicit null is Drata's only end-of-list signal: no match
			// through the true end means the connection does not exist and the
			// caller creates it.
			return "", nil
		}
		var next string
		if err := json.Unmarshal(raw, &next); err != nil {
			return "", fmt.Errorf("decode pagination cursor: %w", err)
		}
		if next == "" || next == cursor {
			// A cursor that is empty or unchanged can't advance the scan and
			// isn't proof of absence; fail rather than risk a duplicate.
			return "", fmt.Errorf("connection list cursor did not advance")
		}
		cursor = next
	}
	// Ran out of pages before reaching the end: a match could still be beyond
	// the cap, so this is unproven-absence, not not-found. Fail loudly.
	return "", fmt.Errorf("connection list exceeded %d pages", maxConnectionListPages)
}

// createConnection creates the dedicated Custom Connection with the exact
// record schema the push path expects.
func (s *sink) createConnection(ctx context.Context, creds providers.Credentials, base, name string, workspaceID int) (string, error) {
	payload := map[string]any{
		"name":           name,
		"providerTypes":  []string{"CUSTOM"},
		"workspaceIds":   []int{workspaceID},
		"displayNameKey": displayNameKey,
		"schema":         connectionRecordSchema(),
	}
	body, err := s.doJSON(ctx, creds, http.MethodPost, base+"/public/v2/custom-connections", payload)
	if err != nil {
		return "", err
	}
	var created struct {
		ID flexID `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("decode created connection: %w", err)
	}
	if string(created.ID) == "" {
		return "", fmt.Errorf("created connection carried no id")
	}
	return string(created.ID), nil
}

// connectionRecordSchema is the JTD the pushed records must match. The
// `required` list deliberately OMITS agentLastSeenAt: Drata marks every
// property required when the list is absent, which would reject the
// never-seen-agent records the sink emits without that field — the single
// most error-prone step of manual setup, encoded correctly here once.
func connectionRecordSchema() map[string]any {
	str := map[string]string{"type": "string"}
	return map[string]any{
		"type": "object",
		"required": []string{
			"id", "serialNumber", "hostname",
			"assignedUserEmail", "agentActive", "agentAttestation",
		},
		"properties": map[string]any{
			"id":                str,
			"serialNumber":      str,
			"hostname":          str,
			"assignedUserEmail": str,
			"agentActive":       map[string]string{"type": "boolean"},
			"agentAttestation":  str,
			"agentLastSeenAt":   str,
		},
	}
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
	ID                string `json:"id"`
	SerialNumber      string `json:"serialNumber"`
	Hostname          string `json:"hostname"`
	AssignedUserEmail string `json:"assignedUserEmail"`
	AgentActive       bool   `json:"agentActive"`
	// agentAttestation is what keeps agentActive honest per row: "device"
	// means this machine's own agent reported in, "user" means only that its
	// assigned user runs one somewhere. An auditor reading agentActive
	// without it would be reading a claim we may not be making.
	AgentAttestation string  `json:"agentAttestation"`
	AgentLastSeenAt  *string `json:"agentLastSeenAt,omitempty"`
}

func buildRecords(snapshot providers.CoverageSnapshot) []coverageRecord {
	records := make([]coverageRecord, 0, len(snapshot.Devices))
	for _, d := range snapshot.Devices {
		var lastSeen *string
		if !d.AgentLastSeenAt.IsZero() {
			formatted := d.AgentLastSeenAt.UTC().Format(time.RFC3339)
			lastSeen = &formatted
		}
		records = append(records, coverageRecord{
			ID:                d.ExternalID,
			SerialNumber:      d.SerialNumber,
			Hostname:          d.Hostname,
			AssignedUserEmail: d.UserEmail,
			AgentActive:       d.AgentActive,
			AgentAttestation:  string(d.AgentAttestation),
			AgentLastSeenAt:   lastSeen,
		})
	}
	return records
}

// checkUploadResults surfaces per-record rejections hidden inside a 2xx
// upload response. The observed shape is a bare array of per-record results;
// a rejected record carries an "error" object (with the schema-validation
// message) while its top-level "statusCode" still reads 201, so the error
// field is the only reliable discriminator. Unknown response shapes pass:
// the 2xx already says the request itself succeeded, and failing on a shape
// change would break pushes that worked.
func checkUploadResults(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var results []struct {
		ID    string `json:"id"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(trimmed, &results); err != nil {
		return nil
	}
	for _, r := range results {
		if r.Error == nil {
			continue
		}
		// Rejected results have been observed carrying the record only under
		// "data", so fall back there for the id naming the failure.
		id := r.ID
		if id == "" {
			id = r.Data.ID
		}
		return fmt.Errorf("record %q rejected: %s", id, r.Error.Message)
	}
	return nil
}

// clearEvidenceRecords deletes every live record on the resource, one by one.
//
// DESTRUCTIVE — read before calling. This removes the connection's entire
// evidence dataset. It is safe only because of three invariants, and a new
// call site must re-justify all of them:
//
//  1. It expresses a truthful state, not a cleanup: it is called only when a
//     completed inventory sync reported zero managed devices, so "no
//     evidence" IS the correct evidence. Mark-missing is completion-gated
//     upstream, so a failed or partial MDM sync can never produce the empty
//     snapshot that reaches here.
//  2. It only ever touches data this integration owns: resolveResourceID
//     refuses any connection carrying more than one resource, so the
//     resource being cleared holds nothing but records earlier pushes wrote.
//  3. It is the N=0 case of the deletion every ordinary push already
//     performs — completing a session deletes all records not in it — and
//     exists only because the production API refuses to complete an empty
//     session (422 "Cannot complete a session with no data records"), making
//     this the one snapshot size sessions cannot express. A later non-empty
//     push fully restores the dataset, so the operation is self-correcting.
//
// Each pass re-lists the first page and deletes what it finds, so cursor
// semantics never matter: the loop drains the list or reports the first
// failure.
func (s *sink) clearEvidenceRecords(ctx context.Context, creds providers.Credentials, resourcePath string) error {
	deleted := make(map[string]bool)
	for {
		body, err := s.doJSON(ctx, creds, http.MethodGet, resourcePath+"/records", nil)
		if err != nil {
			return err
		}
		var page struct {
			Data []struct {
				ID flexID `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("decode record list: %w", err)
		}
		if len(page.Data) == 0 {
			return nil
		}
		for _, rec := range page.Data {
			id := string(rec.ID)
			if id == "" {
				return fmt.Errorf("record listed with no id")
			}
			// A re-listed id whose delete already returned success means
			// deletes are not taking effect; erroring out beats spinning
			// on the listing forever.
			if deleted[id] {
				return fmt.Errorf("record %q reappeared after deletion", id)
			}
			if _, err := s.doJSON(ctx, creds, http.MethodDelete, resourcePath+"/records/"+url.PathEscape(id), nil); err != nil {
				return fmt.Errorf("delete stale record: %w", err)
			}
			deleted[id] = true
		}
	}
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

	resourcePath := tgt.base + tgt.connPath + "/resources/" + url.PathEscape(resource)

	// Drata permits only one IN_PROGRESS session per connection/resource, so
	// a run that died mid-upload leaves one stranded and every later push
	// collides with it forever. Clearing first makes the push self-healing
	// rather than needing a human to cancel the session by hand. Cancelling
	// (not completing) the stranded session is what discards its partial
	// records: completing would publish half a fleet as authoritative.
	if err := s.cancelStrandedSessions(ctx, creds, resourcePath); err != nil {
		return fmt.Errorf("clear stranded drata session: %w", err)
	}

	// One fresh session per push attempt (Drata allows 3-64
	// alphanumeric/hyphen/underscore chars). Uniqueness per attempt — not
	// per snapshot — matters: a retry must never touch a session an earlier
	// attempt may already have completed. Because completion replaces
	// wholesale, two attempts pushing the same snapshot converge to the same
	// dataset.
	sessionID := "gram-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	sessionPath := resourcePath + "/sessions/" + sessionID

	// Transport-level retries (the shared client resends a batch on 429/5xx
	// whose response was lost) are safe here: Drata matches records by
	// their "id" field — "if a record with that ID already exists it is
	// updated" — so a resent batch upserts rather than duplicates, and
	// completion publishes each id once.
	records := buildRecords(snapshot)
	if len(records) == 0 {
		// Sessions cannot express an empty fleet: the production API refuses
		// the completing action with 422 "Cannot complete a session with no
		// data records". Stale evidence still has to clear — a departed
		// fleet must not keep attesting — so the truthful empty state is
		// reached by deleting the live records directly. See the invariants
		// on clearEvidenceRecords before touching this path.
		if err := s.clearEvidenceRecords(ctx, creds, resourcePath); err != nil {
			return fmt.Errorf("clear evidence for empty fleet: %w", err)
		}
		return nil
	}
	for batch := range slices.Chunk(records, recordBatchSize) {
		body, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath, map[string]any{"data": batch})
		if err != nil {
			return fmt.Errorf("upload evidence batch: %w", err)
		}
		// A 2xx upload can still reject records: the response carries a
		// per-record result whose "error" field reports schema-validation
		// failures. Ignoring those would complete a session missing part of
		// the fleet — or, for a wholly rejected batch, publish an empty one.
		if err := checkUploadResults(body); err != nil {
			return fmt.Errorf("upload evidence batch: %w", err)
		}
	}

	if _, err := s.doJSON(ctx, creds, http.MethodPost, sessionPath+"/actions", map[string]any{"action": "complete"}); err != nil {
		return fmt.Errorf("complete evidence session: %w", err)
	}
	return nil
}
