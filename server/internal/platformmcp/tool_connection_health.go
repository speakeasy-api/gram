//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
)

// connectionHealthToolHotspots is how many hotspots the tool returns unless
// asked for fewer.
const connectionHealthToolHotspots = 10

type GatewayConnectionHealthInput struct {
	ProjectID          string `json:"project_id" jsonschema:"exact project ID"`
	GatewayID          string `json:"gateway_id" jsonschema:"exact gateway ID"`
	From               string `json:"from,omitempty" jsonschema:"window start, RFC 3339; defaults to 7 days before to; at most 30 days"`
	To                 string `json:"to,omitempty" jsonschema:"window end, RFC 3339, excluded; defaults to now"`
	HotspotStage       string `json:"hotspot_stage,omitempty" jsonschema:"inbound_authorize, inbound_token, upstream_callback, upstream_refresh, discovery_listing, or discovery_member (default)"`
	HotspotSort        string `json:"hotspot_sort,omitempty" jsonschema:"count (default) ranks by failed attempts; rate by failed attempts over all attempts"`
	HotspotMinAttempts int    `json:"hotspot_min_attempts,omitempty" jsonschema:"leave out callers with fewer attempts; default 5; with caller only 1, the default there"`
	HotspotLimit       int    `json:"hotspot_limit,omitempty" jsonschema:"hotspots to return, 1 to 25; default 10"`

	Key *GatewayConnectionKeyInput `json:"key,omitempty" jsonschema:"one hotspot to count alone, with its timeline, instead of ranking hotspots"`

	Caller *GatewayConnectionCallerInput `json:"caller,omitempty" jsonschema:"one person or agent and registered client: counts each stage for them alone, every provider included and attempts that recorded none too, and ranks their keys in hotspot_stage, one per provider; not with key"`
}

// GatewayConnectionCallerInput is one person or agent and the registered
// client they used, as a hotspot's subject and client keys give them.
type GatewayConnectionCallerInput struct {
	Subject string `json:"subject" jsonschema:"the hotspot subject's key: a person or agent URN"`
	Client  string `json:"client" jsonschema:"the hotspot client's key: a registered client ID"`
}

// GatewayConnectionKeyInput is one hotspot as a hotspot's ids give it.
type GatewayConnectionKeyInput struct {
	Stage      string `json:"stage" jsonschema:"inbound_authorize, inbound_token, upstream_callback, upstream_refresh, discovery_listing, or discovery_member"`
	Subject    string `json:"subject" jsonschema:"the hotspot subject's key, possibly empty"`
	ProviderID string `json:"provider_id" jsonschema:"the hotspot provider's key, possibly empty"`
	Client     string `json:"client" jsonschema:"the hotspot client's key, possibly empty"`
}

type GatewayConnectionOutcome struct {
	Outcome string `json:"outcome"`
	Count   uint64 `json:"count"`
}

type GatewayConnectionStageSummary struct {
	Stage      string                     `json:"stage"`
	Attempts   uint64                     `json:"attempts"`
	Outcomes   []GatewayConnectionOutcome `json:"outcomes"`
	Retries    uint64                     `json:"retries" jsonschema:"extra describe calls members needed within their own listings"`
	Recovered  uint64                     `json:"recovered" jsonschema:"members listed only after a retry within the same listing; not recovery after a later reconnect"`
	LastSeenAt string                     `json:"last_seen_at,omitempty"`
	// LastCodeIssuedAt is included for a keyed or caller token stage and names
	// the latest sign-in code request's start whose token response was written.
	LastCodeIssuedAt string `json:"last_code_issued_at,omitempty" jsonschema:"latest authorization-code request start with a written token response; only for a keyed or caller token stage; not delivery, token use, or provider sign-in completion"`

	// Buckets is the stage's timeline, returned only for a key.
	Buckets []GatewayConnectionBucketSummary `json:"buckets,omitempty"`
}

type GatewayConnectionBucketSummary struct {
	Start    string                     `json:"start"`
	Outcomes []GatewayConnectionOutcome `json:"outcomes"`
}

type GatewayConnectionName struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	ID    string `json:"id,omitempty"`
	// Key is the value to pass in key, possibly empty; nil when this caller,
	// provider, or client is not visible and cannot be selected.
	Key *string `json:"key,omitempty"`
}

type GatewayConnectionReason struct {
	Reason string `json:"reason"`
	Count  uint64 `json:"count"`
}

type GatewayConnectionHotspotSummary struct {
	Subject    GatewayConnectionName      `json:"subject"`
	Provider   GatewayConnectionName      `json:"provider"`
	Client     GatewayConnectionName      `json:"client"`
	Attempts   uint64                     `json:"attempts"`
	Failed     uint64                     `json:"failed"`
	Blocked    uint64                     `json:"blocked"`
	Outcomes   []GatewayConnectionOutcome `json:"outcomes"`
	Reasons    []GatewayConnectionReason  `json:"reasons"`
	LastSeenAt string                     `json:"last_seen_at,omitempty"`
}

type GatewayConnectionNotRecorded struct {
	Stage string `json:"stage"`
	Note  string `json:"note"`
}

type GatewayConnectionHealthOutput struct {
	ProjectID string `json:"project_id"`
	GatewayID string `json:"gateway_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	// IntervalSeconds is the width of a key's timeline buckets, a multiple of
	// 30 minutes aligned to UTC, over the same window as the counts.
	IntervalSeconds int64                           `json:"interval_seconds"`
	Stages          []GatewayConnectionStageSummary `json:"stages"`
	// Ranking is absent when a key asks about one hotspot.
	Ranking     *GatewayConnectionRanking      `json:"ranking,omitempty"`
	NotRecorded []GatewayConnectionNotRecorded `json:"not_recorded"`
	Scope       string                         `json:"scope"`
	// Caller is the caller the counts and ranking are scoped to, set only when
	// caller was given and answered.
	Caller *GatewayConnectionCallerInput `json:"caller,omitempty" jsonschema:"the caller these counts and ranking are scoped to; absent for the whole gateway or a key"`
}

// GatewayConnectionRanking is the hotspots of one stage over the window.
type GatewayConnectionRanking struct {
	Stage       string                            `json:"stage"`
	Sort        string                            `json:"sort"`
	MinAttempts int                               `json:"min_attempts"`
	Hotspots    []GatewayConnectionHotspotSummary `json:"hotspots"`
	// Truncated reports that more keys met the floor than were returned.
	Truncated bool `json:"truncated" jsonschema:"more keys met min_attempts than hotspot_limit returned"`
}

func (r *PostgresReader) WithConnectionHealth(health *telemetry.ConnectionHealth) *PostgresReader {
	if r != nil && health != nil {
		r.connectionHealth = health
	}
	return r
}

// GetGatewayConnectionHealth summarizes one gateway's recorded connection
// stages and its top hotspots through the same reader and authorization as
// the management API.
func (r *PostgresReader) GetGatewayConnectionHealth(ctx context.Context, principal Principal, input GatewayConnectionHealthInput) (GatewayConnectionHealthOutput, error) {
	var none GatewayConnectionHealthOutput
	if r == nil || r.connectionHealth == nil {
		return none, ErrUnavailable
	}
	project, err := r.ResolveProjectLogRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID})
	if err != nil {
		return none, err
	}
	gatewayID, err := uuid.Parse(input.GatewayID)
	if err != nil {
		return none, &ToolRefusalError{Code: "invalid_request", Payload: "gateway_id must be a UUID."}
	}
	window, err := telemetry.ParseConnectionHealthWindow(input.From, input.To, time.Now())
	if err != nil {
		return none, connectionHealthToolError(err)
	}
	stage := input.HotspotStage
	if stage == "" {
		stage = telemetry.ConnectionStageDiscoveryMember
	}
	sort := input.HotspotSort
	if sort == "" {
		sort = "count"
	}
	if sort != "count" && sort != "rate" {
		return none, &ToolRefusalError{Code: "invalid_request", Payload: "hotspot_sort must be count or rate."}
	}
	limit := input.HotspotLimit
	if limit == 0 {
		limit = connectionHealthToolHotspots
	}
	target := telemetry.ConnectionHealthTarget{ProjectID: project.ID, OrganizationID: principal.OrganizationID, GatewayID: gatewayID}
	var scope telemetry.ConnectionHealthScope
	if input.Key != nil {
		scope.Key = &telemetry.ConnectionHealthKey{Stage: input.Key.Stage, Subject: input.Key.Subject, ProviderID: input.Key.ProviderID, ClientKey: input.Key.Client}
	}
	minAttempts := input.HotspotMinAttempts
	if input.Caller != nil {
		// A caller ranking lists every key, so a single successful attempt
		// with a provider is never hidden behind the gateway-wide floor.
		if minAttempts != 0 && minAttempts != 1 {
			return none, &ToolRefusalError{Code: "invalid_request", Payload: "hotspot_min_attempts must be 1 or omitted with caller, so no key of that caller is hidden."}
		}
		minAttempts = 1
		scope.Caller = &telemetry.ConnectionCaller{Subject: input.Caller.Subject, ClientKey: input.Caller.Client}
	}
	key := scope.Key
	summary, err := r.connectionHealth.Summary(ctx, target, window, scope)
	if err != nil {
		return none, connectionHealthToolError(err)
	}
	// A key asks about one hotspot, so no ranking is read for it.
	var hotspots telemetry.ConnectionHotspots
	if key == nil {
		hotspots, err = r.connectionHealth.Hotspots(ctx, target, window, telemetry.ConnectionHotspotQuery{Stage: stage, ByRate: sort == "rate", MinAttempts: minAttempts, Limit: limit, Caller: scope.Caller})
		if err != nil {
			return none, connectionHealthToolError(err)
		}
	}

	output := GatewayConnectionHealthOutput{
		ProjectID:       project.ID.String(),
		GatewayID:       gatewayID.String(),
		From:            window.Start.Format(time.RFC3339Nano),
		To:              window.End.Format(time.RFC3339Nano),
		IntervalSeconds: summary.IntervalSeconds,
		Stages:          make([]GatewayConnectionStageSummary, 0, len(summary.Stages)),
		Ranking:         nil,
		NotRecorded:     make([]GatewayConnectionNotRecorded, 0, len(summary.NotRecorded)),
		Scope:           summary.Scope,
		Caller:          nil,
	}
	if key == nil {
		output.Ranking = &GatewayConnectionRanking{Stage: hotspots.Stage, Sort: sort, MinAttempts: hotspots.MinAttempts, Hotspots: make([]GatewayConnectionHotspotSummary, 0, len(hotspots.Hotspots)), Truncated: hotspots.Truncated}
	}
	for _, stage := range summary.Stages {
		view := GatewayConnectionStageSummary{Stage: stage.Stage, Attempts: stage.Attempts, Outcomes: connectionOutcomes(stage.Outcomes), Retries: stage.Retries, Recovered: stage.Recovered, LastSeenAt: connectionTime(stage.LastSeen), LastCodeIssuedAt: "", Buckets: nil}
		if key != nil || scope.Caller != nil {
			view.LastCodeIssuedAt = connectionTime(stage.LastCodeIssued)
		}
		if key != nil {
			for _, bucket := range stage.Buckets {
				view.Buckets = append(view.Buckets, GatewayConnectionBucketSummary{Start: connectionTime(bucket.Start), Outcomes: connectionOutcomes(bucket.Outcomes)})
			}
		}
		output.Stages = append(output.Stages, view)
	}
	for _, hotspot := range hotspots.Hotspots {
		reasons := make([]GatewayConnectionReason, 0, len(hotspot.Reasons))
		for _, reason := range hotspot.Reasons {
			reasons = append(reasons, GatewayConnectionReason{Reason: reason.Reason, Count: reason.Count})
		}
		output.Ranking.Hotspots = append(output.Ranking.Hotspots, GatewayConnectionHotspotSummary{
			Subject:    connectionName(hotspot.Subject),
			Provider:   connectionName(hotspot.Provider),
			Client:     connectionName(hotspot.Client),
			Attempts:   hotspot.Attempts,
			Failed:     hotspot.Failed,
			Blocked:    hotspot.Blocked,
			Outcomes:   connectionOutcomes(hotspot.Outcomes),
			Reasons:    reasons,
			LastSeenAt: connectionTime(hotspot.LastSeen),
		})
	}
	for _, coverage := range summary.NotRecorded {
		output.NotRecorded = append(output.NotRecorded, GatewayConnectionNotRecorded{Stage: coverage.Stage, Note: coverage.Note})
	}
	if scope.Caller != nil {
		output.Caller = &GatewayConnectionCallerInput{Subject: scope.Caller.Subject, Client: scope.Caller.ClientKey}
	}
	return output, nil
}

// connectionHealthNotFound refuses a gateway or hotspot that is missing or
// that the caller may not see, alike, so neither reveals the other.
func connectionHealthNotFound(message string) error {
	payload, err := json.Marshal(map[string]string{"code": "not_found", "message": message})
	if err != nil {
		payload = []byte(`{"code":"not_found"}`)
	}
	return &ToolRefusalError{Code: "not_found", Payload: string(payload)}
}

func connectionHealthToolError(err error) error {
	switch {
	case isAuthorizationDenied(err):
		return ErrForbidden
	case errors.Is(err, telemetryerrs.ErrLogsDisabled):
		return &ToolRefusalError{Code: "observability_disabled", Payload: "Enable observability for this organization to inspect gateway connection health."}
	case errors.Is(err, telemetry.ErrConnectionHealthGatewayNotFound):
		return connectionHealthNotFound("No gateway with that ID in this project is visible to you. Check the gateway ID and project.")
	case errors.Is(err, telemetry.ErrConnectionHealthKeyNotFound):
		return connectionHealthNotFound("No hotspot with that key is visible to you. Use the key values a ranking returned.")
	case errors.Is(err, telemetry.ErrConnectionHealthCallerNotFound):
		return connectionHealthNotFound("No person or agent with that registered client is visible to you. Use the subject and client keys a ranking returned.")
	case errors.Is(err, telemetry.ErrConnectionHealthInvalid):
		return &ToolRefusalError{Code: "invalid_request", Payload: err.Error()}
	}
	return fmt.Errorf("read gateway connection health: %w", err)
}

func connectionOutcomes(counts []telemetry.ConnectionOutcomeCount) []GatewayConnectionOutcome {
	outcomes := make([]GatewayConnectionOutcome, 0, len(counts))
	for _, count := range counts {
		outcomes = append(outcomes, GatewayConnectionOutcome{Outcome: count.Outcome, Count: count.Count})
	}
	return outcomes
}

func connectionName(label telemetry.ConnectionLabel) GatewayConnectionName {
	name := GatewayConnectionName{Kind: label.Kind, Label: label.Label, ID: label.ID, Key: nil}
	if label.Selectable {
		name.Key = &label.Key
	}
	return name
}

func connectionTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

const gatewayConnectionHealthToolName = "get_gateway_connection_health"

const gatewayConnectionHealthDescription = "Summarize one exact gateway's connection attempts over at most 30 days (default 7): per recorded stage, the attempts and their outcomes, and the callers, providers, and clients with the most failures. " +
	"Each stage is counted on its own. inbound_authorize counts sign-in requests (accepted, policy_blocked, failed); accepted means a redirect to sign-in was attempted, not that it arrived or that sign-in finished. " +
	"inbound_token counts token requests: successful outcomes distinguish code_issued (authorization code), refreshed, refresh_replayed (the same refresh answered again), and assertion_issued (enterprise assertion); policy_blocked and failed are not split by grant. A keyed token read also returns last_code_issued_at when present: the latest sign-in code request's start whose response was written, independent of later refreshes. A written token response is not proof that a client used it or that a connection is new, and refreshes are never sign-ins. " +
	"upstream_callback counts provider sign-in callbacks whose sign-in state named this gateway (stored, retried, failed): stored means the provider credential was saved, not that the person returned to their client; retried means the provider was asked again without the resource. A callback with missing or expired state names no gateway and is not counted. " +
	"upstream_refresh counts provider refreshes that a request to this gateway caused (refreshed, adopted, no_grant, failed, unknown): each is one logical refresh, not a request to the provider; adopted reused another request's refresh, no_grant means there was nothing to refresh, and unknown (in_progress) means the refresh was still running when the request finished. Capture is bounded per request; not_recorded discloses the limit. A failed refresh may still have served a live access token. " +
	"discovery_listing counts Direct tools/list calls (complete, partial, failed) and discovery_member counts the members each listing reached (listed, failed, credential_blocked, consent_blocked). unknown is an attempt whose outcome was not recorded. " +
	"A stage's attempts is the denominator for every outcome in it; blocked attempts (a credential or consent the caller fixes, or an administrator's client or workload policy) are reported apart from failures and never dropped from attempts. " +
	"No observed attempts is no evidence about that stage, never proof that it is healthy. " +
	"not_recorded lists what is not observed, such as client metadata fetches, scheduled and manual provider refreshes, and requests to provider endpoints, so they are unknown, not healthy. " +
	"Reasons are sanitized codes; provider OAuth reasons use provider_oauth_ followed by the registered code, distinguishing them from gateway validation failures. stale_cursor means the client should restart tool discovery (if signing in again leaves the error, remove and add the connection in the client), and consent_not_approved that reconnecting lets the person approve the new server. Later outcomes are what was observed, not proof that an action caused them. " +
	"Names are given only for people in the organization, agents you may read, and providers and clients the project can see; others are labeled by kind without an ID. " +
	"To see one hotspot's own outcomes and timeline, pass key with its stage and the key values of its subject, provider, and client; a value without a key is not visible to you and cannot be selected. " +
	"To diagnose one person or agent, pass caller with a hotspot's subject key and registered client key instead of key: each stage is counted for them alone, with every provider and with attempts that recorded none (sign-in requests are recorded before the caller is known, so not_recorded lists that stage), the token stage returns last_code_issued_at, and the ranking lists their keys in hotspot_stage, one per provider, with no attempt floor so a single successful provider callback is included; ranking.truncated says more keys exist, and caller echoes whom the answer counted. A caller ranking cannot use hotspot_stage inbound_authorize, which is recorded before the caller is known. Call once each with hotspot_stage upstream_callback, upstream_refresh and discovery_member to see each provider's sign-ins, refreshes and member checks. Member checks are grouped by each member's provider, so several member servers can share a row and members without a provider share one; failed, credential_blocked and consent_blocked stay apart, and the stage's recovered counts listed members that needed a retry inside the same listing, never a later reconnect. These count the caller, client and window, not one listing, and are observations beside each other, not causes. Requires project:read and log:read on the project and mcp:read on the gateway."

func registerGatewayConnectionHealthTool(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name: gatewayConnectionHealthToolName, Title: "Check Gateway Connection Health",
		Description: gatewayConnectionHealthDescription,
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryLogRead}, func(ctx context.Context, _ *mcp.CallToolRequest, input GatewayConnectionHealthInput) (*mcp.CallToolResult, GatewayConnectionHealthOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, GatewayConnectionHealthOutput{}, err
		}
		output, err := reader.GetGatewayConnectionHealth(ctx, principal, input)
		return nil, output, err
	})
}

func registerUnavailableGatewayConnectionHealthTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name: gatewayConnectionHealthToolName, Title: "Check Gateway Connection Health",
		Description: "Summarize one gateway's connection attempts. Connection health is not available yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryLogRead}, func(_ context.Context, _ *mcp.CallToolRequest, _ GatewayConnectionHealthInput) (*mcp.CallToolResult, GatewayConnectionHealthOutput, error) {
		return nil, GatewayConnectionHealthOutput{}, &ToolRefusalError{Code: unavailableCode, Payload: "Connection health is not available yet."}
	})
}
