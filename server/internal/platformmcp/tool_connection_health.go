//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
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
	HotspotStage       string `json:"hotspot_stage,omitempty" jsonschema:"discovery_member (default) or discovery_listing"`
	HotspotSort        string `json:"hotspot_sort,omitempty" jsonschema:"count (default) ranks by failed attempts; rate by failed attempts over all attempts"`
	HotspotMinAttempts int    `json:"hotspot_min_attempts,omitempty" jsonschema:"leave out callers with fewer attempts; default 5"`
	HotspotLimit       int    `json:"hotspot_limit,omitempty" jsonschema:"hotspots to return, 1 to 25; default 10"`

	Key *GatewayConnectionKeyInput `json:"key,omitempty" jsonschema:"one hotspot to count alone, with its timeline, instead of ranking hotspots"`
}

// GatewayConnectionKeyInput is one hotspot as a hotspot's ids give it.
type GatewayConnectionKeyInput struct {
	Stage      string `json:"stage" jsonschema:"discovery_listing or discovery_member"`
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
	ProjectID          string                            `json:"project_id"`
	GatewayID          string                            `json:"gateway_id"`
	From               string                            `json:"from"`
	To                 string                            `json:"to"`
	Stages             []GatewayConnectionStageSummary   `json:"stages"`
	HotspotStage       string                            `json:"hotspot_stage"`
	HotspotSort        string                            `json:"hotspot_sort"`
	HotspotMinAttempts int                               `json:"hotspot_min_attempts"`
	Hotspots           []GatewayConnectionHotspotSummary `json:"hotspots"`
	NotRecorded        []GatewayConnectionNotRecorded    `json:"not_recorded"`
	Scope              string                            `json:"scope"`
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
	var key *telemetry.ConnectionHealthKey
	if input.Key != nil {
		key = &telemetry.ConnectionHealthKey{Stage: input.Key.Stage, Subject: input.Key.Subject, ProviderID: input.Key.ProviderID, ClientKey: input.Key.Client}
	}
	summary, err := r.connectionHealth.Summary(ctx, target, window, key)
	if err != nil {
		return none, connectionHealthToolError(err)
	}
	// A key asks about one hotspot, so no ranking is read for it.
	var hotspots telemetry.ConnectionHotspots
	if key == nil {
		hotspots, err = r.connectionHealth.Hotspots(ctx, target, window, telemetry.ConnectionHotspotQuery{Stage: stage, ByRate: sort == "rate", MinAttempts: input.HotspotMinAttempts, Limit: limit})
		if err != nil {
			return none, connectionHealthToolError(err)
		}
	}

	output := GatewayConnectionHealthOutput{
		ProjectID:          project.ID.String(),
		GatewayID:          gatewayID.String(),
		From:               window.Start.Format(time.RFC3339Nano),
		To:                 window.End.Format(time.RFC3339Nano),
		Stages:             make([]GatewayConnectionStageSummary, 0, len(summary.Stages)),
		HotspotStage:       hotspots.Stage,
		HotspotSort:        sort,
		HotspotMinAttempts: hotspots.MinAttempts,
		Hotspots:           make([]GatewayConnectionHotspotSummary, 0, len(hotspots.Hotspots)),
		NotRecorded:        make([]GatewayConnectionNotRecorded, 0, len(summary.NotRecorded)),
		Scope:              summary.Scope,
	}
	for _, stage := range summary.Stages {
		view := GatewayConnectionStageSummary{Stage: stage.Stage, Attempts: stage.Attempts, Outcomes: connectionOutcomes(stage.Outcomes), Retries: stage.Retries, Recovered: stage.Recovered, LastSeenAt: connectionTime(stage.LastSeen), Buckets: nil}
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
		output.Hotspots = append(output.Hotspots, GatewayConnectionHotspotSummary{
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
	return output, nil
}

func connectionHealthToolError(err error) error {
	switch {
	case errors.Is(err, telemetryerrs.ErrLogsDisabled):
		return &ToolRefusalError{Code: "observability_disabled", Payload: "Enable observability for this organization to inspect gateway connection health."}
	case errors.Is(err, telemetry.ErrConnectionHealthGatewayNotFound):
		return ErrMCPConnectionSettingsNotFound
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
	"Each stage is counted on its own: discovery_listing counts Direct tools/list calls (complete, partial, failed) and discovery_member counts the members each listing reached (listed, failed, credential_blocked, consent_blocked). " +
	"A stage's attempts is the denominator for every outcome in it; blocked attempts are a credential or consent the caller fixes, reported apart from failures and never dropped from attempts. " +
	"No observed attempts is no evidence about that stage, never proof that it is healthy. " +
	"not_recorded lists stages that are not observed yet, including sign-in, so they are unknown, not healthy: a listing failure does not show whether sign-in succeeded. " +
	"Reasons are sanitized codes; stale_cursor means the client should reconnect to restart its tool listing, and consent_not_approved that reconnecting lets the person approve the new server. Later outcomes are what was observed, not proof that an action caused them. " +
	"Names are given only for people in the organization, agents you may read, and providers and clients the project can see; others are labeled by kind without an ID. " +
	"To see one hotspot's own outcomes and timeline, pass key with its stage and the key values of its subject, provider, and client; a value without a key is not visible to you and cannot be selected. Requires project:read and log:read on the project and mcp:read on the gateway."

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
