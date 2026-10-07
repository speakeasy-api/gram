package platformmcp

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"errors"
	"github.com/speakeasy-api/gram/server/internal/celeval"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
)

type findSignalsInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID"`
	Query     string `json:"query,omitempty" jsonschema:"case-insensitive name or slug substring, at most 200 characters"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"next_cursor from the preceding page"`
	Limit     int    `json:"limit,omitempty" jsonschema:"page size from 1 to 100; defaults to 20"`
}

type findSignalsOutput struct {
	Signals    []signalConfiguration `json:"signals"`
	NextCursor string                `json:"next_cursor,omitempty"`
	Version    string                `json:"version"`
}

type findSensorsOutput struct {
	Sensors    []sensorSummary `json:"sensors"`
	NextCursor string          `json:"next_cursor,omitempty"`
	Version    string          `json:"version"`
}

type sensorSummary struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	Mode            string   `json:"mode"`
	MatchExpression string   `json:"match_expression"`
	SignalCount     int      `json:"signal_count"`
	Ready           bool     `json:"configuration_ready"`
	Issues          []string `json:"configuration_issues"`
}

type getSensorInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID"`
	SensorID  string `json:"sensor_id" jsonschema:"exact sensor UUID returned by find_sensors"`
}

type previewSensorMatchInput struct {
	ProjectID  string               `json:"project_id" jsonschema:"exact project UUID"`
	Expression string               `json:"match_expression" jsonschema:"boolean CEL expression over message.role; at most 4096 bytes"`
	Examples   []sensorMatchExample `json:"examples" jsonschema:"up to 20 caller-supplied metadata examples; never fetches transcripts or runs inference"`
}

type sensorMatchExample struct {
	Role *string `json:"role,omitempty" jsonschema:"user, assistant, system, or tool; omit to test absent message context"`
}

type previewSensorMatchOutput struct {
	Contract string   `json:"matching_contract"`
	Results  []string `json:"results"`
}

func signalManifest(name, title, description string, read bool) *mcp.Tool {
	return &mcp.Tool{Meta: nil, Name: name, Title: title, Description: description, InputSchema: nil, OutputSchema: nil, Icons: nil, Annotations: &mcp.ToolAnnotations{Title: "", ReadOnlyHint: read, DestructiveHint: new(!read), IdempotentHint: true, OpenWorldHint: new(false)}}
}

func registerSignalTools(reg *Registrar, service *SignalAuthoringService) {
	readMeta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}
	writeMeta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectWrite}

	for _, operation := range []string{"create_sensor", "create_signal", "update_sensor", "update_signal"} {
		addTool(reg, signalManifest(operation, strings.ReplaceAll(operation, "_", " "), "Author project-scoped signals intelligence. Call confirmed:false to validate and preview. Creation IDs in previews are provisional. Show the complete normalized proposal and affected sensors, obtain explicit confirmation, then resend the identical proposal with expected_version, preview_token, confirmed:true and a stable idempotency_key. Sensor creation atomically creates inline signals and ordered memberships; updates preserve omitted fields and replace supplied memberships. Shared signal changes affect every referencing sensor. Version covers the entire bounded project configuration. Returns current post-commit state; receipt replay does not prove a target still exists. Requires signals_intelligence and project:write plus project:read. Readiness is configuration only, never proof of active evaluation. External users only.", false), writeMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input signalAuthoringInput) (*mcp.CallToolResult, signalAuthoringOutput, error) {
			return principalToolCall(ctx, signalToolError, func(principal Principal) (signalAuthoringOutput, error) {
				return service.author(ctx, principal, operation, input)
			})
		})
	}
	addTool(reg, signalManifest("find_signals", "Find Reusable Signals", "Find reusable signals by name or slug in an explicit project before creating duplicates. Returns criteria and referencing sensor counts. UUID-ordered bounded pagination; candidates are not automatic selections. Requires signals_intelligence and project:read. Authoring supports at most 1000 active signals and 1000 active sensors per project.", true), readMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input findSignalsInput) (*mcp.CallToolResult, findSignalsOutput, error) {
		return principalToolCall(ctx, signalToolError, func(principal Principal) (findSignalsOutput, error) {
			var out findSignalsOutput

			if err := validateSignalSearch(&input); err != nil {
				return out, err
			}

			_, _, state, err := service.scope(ctx, principal, input.ProjectID, false)
			if err != nil {
				return out, err
			}

			out.Signals, out.Version = []signalConfiguration{}, state.Version
			projections := signalProjections(state)

			for _, signal := range state.Signals {
				if signal.ID <= input.Cursor || !signalSearchMatches(input.Query, signal.Name, string(signal.Slug)) {
					continue
				}
				if len(out.Signals) == input.Limit {
					out.NextCursor = out.Signals[len(out.Signals)-1].ID
					break
				}

				out.Signals = append(out.Signals, projections[signal.ID])
			}

			return out, nil
		})
	})
	addTool(reg, signalManifest("find_sensors", "Find Sensors", "Find project sensor summaries by name or slug, with matching expressions, signal counts and configuration readiness. Use get_sensor for ordered expanded definitions. Requires signals_intelligence and project:read. Pagination is UUID ordered; readiness does not prove evaluation is running. Bounded to projects with at most 1000 active sensors and 1000 active signals.", true), readMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input findSignalsInput) (*mcp.CallToolResult, findSensorsOutput, error) {
		return principalToolCall(ctx, signalToolError, func(principal Principal) (findSensorsOutput, error) {
			var out findSensorsOutput

			if err := validateSignalSearch(&input); err != nil {
				return out, err
			}

			_, _, state, err := service.scope(ctx, principal, input.ProjectID, false)
			if err != nil {
				return out, err
			}

			out.Sensors, out.Version = []sensorSummary{}, state.Version
			projections := signalProjections(state)

			for _, sensor := range state.Sensors {
				if sensor.ID <= input.Cursor || !signalSearchMatches(input.Query, sensor.Name, string(sensor.Slug)) {
					continue
				}
				if len(out.Sensors) == input.Limit {
					out.NextCursor = out.Sensors[len(out.Sensors)-1].ID
					break
				}
				value := projectSensor(sensor, projections)
				out.Sensors = append(out.Sensors, sensorSummary{ID: value.ID, Name: value.Name, Slug: value.Slug, Mode: value.Mode, MatchExpression: value.MatchExpression, SignalCount: len(value.Signals), Ready: value.Ready, Issues: value.Issues})
			}

			return out, nil
		})
	})
	addTool(reg, signalManifest("get_sensor", "Inspect Sensor Configuration", "Read one exact project sensor, its ordered expanded signals, matching expression, configuration readiness, project configuration version and dashboard path. Requires signals_intelligence and project:read.", true), readMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input getSensorInput) (*mcp.CallToolResult, signalAuthoringOutput, error) {
		return principalToolCall(ctx, signalToolError, func(principal Principal) (signalAuthoringOutput, error) {
			ctx, project, state, err := service.scope(ctx, principal, input.ProjectID, false)
			if err != nil {
				return signalAuthoringOutput{}, err
			}

			for _, sensor := range state.Sensors {
				if sensor.ID == input.SensorID {
					return projectAuthoringResult(ctx, project, sigint.AuthoringResult{Sensor: sensor, Signal: nil, State: state}), nil
				}
			}

			return signalAuthoringOutput{}, oops.C(oops.CodeNotFound)
		})
	})
	addTool(reg, signalManifest("preview_sensor_match", "Preview Sensor Matching", "Validate a boolean CEL predicate and evaluate up to 20 caller-supplied role examples without retrieving messages or running inference. Returns matched, not_matched, evaluation_error, or cost_limit per example. The message context currently exposes only role; absence is unbound, not an empty role. Configuration predicates do not broaden ingestion: conversation evaluation currently accepts only user/assistant creation events. Requires signals_intelligence and project:read.", true), readMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input previewSensorMatchInput) (*mcp.CallToolResult, previewSensorMatchOutput, error) {
		return principalToolCall(ctx, signalToolError, func(principal Principal) (previewSensorMatchOutput, error) {
			var out previewSensorMatchOutput

			if _, _, _, err := service.scope(ctx, principal, input.ProjectID, false); err != nil {
				return out, err
			}

			if len(input.Examples) > 20 {
				return out, oops.C(oops.CodeBadRequest)
			}

			if err := matching.Validate(input.Expression); err != nil {
				return out, oops.E(oops.CodeBadRequest, err, "invalid matching expression")
			}

			out.Contract, out.Results = matching.Version, []string{}

			for _, example := range input.Examples {
				var message *matching.Message
				if example.Role != nil {
					switch *example.Role {
					case "user", "assistant", "tool", "system":
					default:
						return out, oops.C(oops.CodeBadRequest)
					}
					message = &matching.Message{Role: *example.Role}
				}
				matched, err := matching.Match(ctx, input.Expression, message)
				result := "not_matched"
				switch {
				case ctx.Err() != nil:
					return out, ctx.Err()
				case errors.Is(err, celeval.ErrCostLimit):
					result = "cost_limit"
				case err != nil:
					result = "evaluation_error"
				case matched:
					result = "matched"
				}

				out.Results = append(out.Results, result)
			}

			return out, nil
		})
	})
}

func validateSignalSearch(input *findSignalsInput) error {
	if input.Limit == 0 {
		input.Limit = 20
	}

	if input.Limit < 1 || input.Limit > 100 || len(input.Query) > 200 {
		return oops.C(oops.CodeBadRequest)
	}

	if input.Cursor != "" {
		if _, err := uuid.Parse(input.Cursor); err != nil {
			return oops.C(oops.CodeBadRequest)
		}
	}

	return nil
}

func signalSearchMatches(query, name, slug string) bool {
	query = strings.ToLower(query)
	return strings.Contains(strings.ToLower(name), query) || strings.Contains(strings.ToLower(slug), query)
}
