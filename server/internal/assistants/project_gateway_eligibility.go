package assistants

import (
	"context"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
)

// projectGatewayRuntimeServerID is the runner-side server identifier for the
// single Project Gateway MCP entry when an assistant turn is gateway-eligible.
const projectGatewayRuntimeServerID = "gateway"

// ProjectGatewayEvaluator runs the Project Gateway's own availability,
// machine-access, turn-credential admission, and derived member resolution
// path. Implemented by *mcp.Service and *mcp.ProjectGatewayEvaluator.
type ProjectGatewayEvaluator interface {
	EvaluateProjectGatewayForTurnCredential(
		ctx context.Context,
		organizationID string,
		projectID uuid.UUID,
		turnToken string,
		serverURL *url.URL,
	) (orggateway.ProjectGatewayEvaluation, error)
}

// ProjectGatewayEligibility is the shared per-turn eligibility decision used
// by assistant runtimes (Default runtime now; reusable by Kit).
type ProjectGatewayEligibility struct {
	Eligible   bool
	GatewayURL string
	Reason     string
}

// SetProjectGatewayEvaluator wires the gateway admission and member-resolution
// evaluator used to decide whether a turn routes through the Project Gateway.
// When unset, turns stay on the per-server fallback.
func (s *ServiceCore) SetProjectGatewayEvaluator(evaluator ProjectGatewayEvaluator) {
	s.projectGatewayEvaluator = evaluator
}

// EvaluateProjectGatewayEligibility decides whether an assistant turn carrying
// turnToken should reach all of its MCP servers and built-ins through the
// single Project Gateway endpoint.
//
// The check delegates availability, HTTPS URL, machine-access flag, credential
// admission, and live member resolution to the Project Gateway's own code path
// so eligibility can never disagree with admission, then verifies strict set
// equality (including effective environment slug) between the gateway's
// admitted members and the assistant's attached servers plus required
// built-ins. Any mismatch or error falls back to per-server mode.
func (s *ServiceCore) EvaluateProjectGatewayEligibility(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	serverURL *url.URL,
	turnToken string,
	attached []assistantMCPServerRow,
	platformSlugs []string,
) ProjectGatewayEligibility {
	ineligible := func(reason string) ProjectGatewayEligibility {
		return ProjectGatewayEligibility{Eligible: false, GatewayURL: "", Reason: reason}
	}
	if s.projectGatewayEvaluator == nil || serverURL == nil || turnToken == "" {
		return ineligible("evaluator_unavailable")
	}

	eval, err := s.projectGatewayEvaluator.EvaluateProjectGatewayForTurnCredential(ctx, organizationID, projectID, turnToken, serverURL)
	if err != nil {
		s.logger.WarnContext(ctx, "project gateway eligibility check failed; falling back to per-server MCP",
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID.String()),
			attr.SlogAssistantID(assistantID.String()),
			attr.SlogError(err),
		)
		return ineligible("evaluation_error")
	}
	if !eval.Admitted {
		return ineligible(eval.Reason)
	}

	if reason := matchProjectGatewayMembers(attached, platformSlugs, eval.Members); reason != "" {
		return ineligible(reason)
	}

	return ProjectGatewayEligibility{
		Eligible:   true,
		GatewayURL: eval.GatewayURL,
		Reason:     "",
	}
}

// matchProjectGatewayMembers enforces strict set equality between the members
// admitted by the Project Gateway and the assistant's enabled attached servers
// plus its required built-in platform toolsets, including effective environment
// equality for every attached server.
func matchProjectGatewayMembers(
	attached []assistantMCPServerRow,
	platformSlugs []string,
	admitted []orggateway.ProjectGatewayMember,
) string {
	// Mirror resolveAssistantMCPServers' reachability filter and runtime-ID
	// deduplication so "what the assistant reaches today" matches the exact
	// per-server list.
	expectedServers := make(map[uuid.UUID]string, len(attached))
	seenRuntimeIDs := make(map[string]struct{}, len(attached))
	for _, m := range attached {
		if m.EndpointSlug == "" || m.Visibility == visibility.Disabled {
			continue
		}
		id := m.ToolsetSlug.String
		if !m.ToolsetSlug.Valid {
			id = m.EndpointSlug
			if m.ServerSlug.Valid && m.ServerSlug.String != "" {
				id = m.ServerSlug.String
			}
			id = capRuntimeMCPServerID(id)
		}
		if _, dup := seenRuntimeIDs[id]; dup {
			continue
		}
		seenRuntimeIDs[id] = struct{}{}
		expectedServers[m.MCPServerID] = assistantMCPEnvironment(m)
	}

	expectedPlatform := make(map[string]struct{}, len(platformSlugs))
	for _, slug := range platformSlugs {
		expectedPlatform[slug] = struct{}{}
	}

	admittedServers := make(map[uuid.UUID]string, len(admitted))
	admittedPlatform := make(map[string]struct{}, len(platformSlugs))
	for _, member := range admitted {
		if member.PlatformToolsetSlug != "" {
			admittedPlatform[member.PlatformToolsetSlug] = struct{}{}
			continue
		}
		admittedServers[member.ServerID] = member.EnvironmentSlug
	}

	if len(admittedPlatform) != len(expectedPlatform) {
		return "platform_toolset_mismatch"
	}
	for slug := range expectedPlatform {
		if _, ok := admittedPlatform[slug]; !ok {
			return "platform_toolset_mismatch"
		}
	}

	if len(admittedServers) != len(expectedServers) {
		return "server_set_mismatch"
	}
	for serverID, wantEnv := range expectedServers {
		gotEnv, ok := admittedServers[serverID]
		if !ok {
			return "server_set_mismatch"
		}
		if gotEnv != wantEnv {
			return "environment_override_mismatch"
		}
	}
	return ""
}

// projectGatewayRuntimeServer builds the single MCP entry sent to the runner
// when a turn is eligible for the Project Gateway.
func projectGatewayRuntimeServer(gatewayURL string) []runtimeMCPServer {
	return []runtimeMCPServer{{
		ID:        projectGatewayRuntimeServerID,
		URL:       gatewayURL,
		Headers:   map[string]string{},
		IsGateway: true,
	}}
}

type bootstrapRuntimeTokenKey struct{}

// withBootstrapRuntimeToken carries the caller's raw bearer token from
// handleGetThreadBootstrap into BuildThreadBootstrap so bootstrap evaluates
// Project Gateway eligibility with the exact credential the runner presents.
func withBootstrapRuntimeToken(ctx context.Context, rawToken string) context.Context {
	return context.WithValue(ctx, bootstrapRuntimeTokenKey{}, rawToken)
}

func bootstrapRuntimeToken(ctx context.Context) string {
	token, _ := ctx.Value(bootstrapRuntimeTokenKey{}).(string)
	return token
}
