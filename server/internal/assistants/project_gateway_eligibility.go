package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
)

// projectGatewayRuntimeServerID is the runner-side server identifier for the
// single Project Gateway MCP entry when an assistant turn is gateway-eligible.
const projectGatewayRuntimeServerID = "gateway"

// projectGatewayEligibilityCacheTTL bounds the in-memory eligibility cache so
// grant, feature-flag, or gateway-toggle changes take effect within seconds
// while avoiding repeated per-turn member snapshots on bursty threads (such as
// turn dispatch immediately followed by runner bootstrap).
const (
	projectGatewayEligibilityCacheTTL     = 3 * time.Second
	projectGatewayEligibilityCacheMaxSize = 1024
)

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

type projectGatewayEligibilityCacheEntry struct {
	result    ProjectGatewayEligibility
	expiresAt time.Time
}

type projectGatewayEligibilityCache struct {
	mu      sync.Mutex
	entries map[string]projectGatewayEligibilityCacheEntry
}

func newProjectGatewayEligibilityCache() *projectGatewayEligibilityCache {
	return &projectGatewayEligibilityCache{
		mu:      sync.Mutex{},
		entries: make(map[string]projectGatewayEligibilityCacheEntry),
	}
}

func (c *projectGatewayEligibilityCache) get(key string, now time.Time) (ProjectGatewayEligibility, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !now.Before(entry.expiresAt) {
		delete(c.entries, key)
		return ProjectGatewayEligibility{Eligible: false, GatewayURL: "", Reason: ""}, false
	}
	return entry.result, true
}

func (c *projectGatewayEligibilityCache) set(key string, result ProjectGatewayEligibility, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= projectGatewayEligibilityCacheMaxSize {
		for k, v := range c.entries {
			if !now.Before(v.expiresAt) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= projectGatewayEligibilityCacheMaxSize {
			clear(c.entries)
		}
	}
	c.entries[key] = projectGatewayEligibilityCacheEntry{
		result:    result,
		expiresAt: now.Add(projectGatewayEligibilityCacheTTL),
	}
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
// admission, and member resolution to the Project Gateway's own code path so
// eligibility can never disagree with admission, then verifies strict set
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

	cacheKey := projectGatewayEligibilityCacheKey(ctx, organizationID, projectID, assistantID, serverURL, turnToken, attached, platformSlugs)
	now := time.Now()
	if s.projectGatewayCache != nil && cacheKey != "" {
		if cached, ok := s.projectGatewayCache.get(cacheKey, now); ok {
			return cached
		}
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
		res := ineligible(eval.Reason)
		if s.projectGatewayCache != nil && cacheKey != "" {
			s.projectGatewayCache.set(cacheKey, res, now)
		}
		return res
	}

	if reason := matchProjectGatewayMembers(attached, platformSlugs, eval.Members); reason != "" {
		res := ineligible(reason)
		if s.projectGatewayCache != nil && cacheKey != "" {
			s.projectGatewayCache.set(cacheKey, res, now)
		}
		return res
	}

	res := ProjectGatewayEligibility{
		Eligible:   true,
		GatewayURL: eval.GatewayURL,
		Reason:     "",
	}
	if s.projectGatewayCache != nil && cacheKey != "" {
		s.projectGatewayCache.set(cacheKey, res, now)
	}
	return res
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

func projectGatewayEligibilityCacheKey(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	assistantID uuid.UUID,
	serverURL *url.URL,
	turnToken string,
	attached []assistantMCPServerRow,
	platformSlugs []string,
) string {
	var b strings.Builder
	b.WriteString(organizationID)
	b.WriteByte('|')
	b.WriteString(projectID.String())
	b.WriteByte('|')
	b.WriteString(assistantID.String())
	b.WriteByte('|')
	b.WriteString(serverURL.String())
	b.WriteByte('|')
	if cred, ok := principalcredential.FromContext(ctx); ok {
		b.WriteString(cred.Credential.Principal.String())
		b.WriteByte('|')
		b.WriteString(cred.Credential.AuthorizerUserID)
		b.WriteByte('|')
		if cred.Credential.SelectionProfileVersionID.Valid {
			b.WriteString(cred.Credential.SelectionProfileVersionID.UUID.String())
		}
	} else {
		sum := sha256.Sum256([]byte(turnToken))
		b.WriteString(hex.EncodeToString(sum[:16]))
	}
	b.WriteByte('|')
	sortedSlugs := append([]string(nil), platformSlugs...)
	sort.Strings(sortedSlugs)
	for _, slug := range sortedSlugs {
		b.WriteString(slug)
		b.WriteByte(',')
	}
	b.WriteByte('|')
	parts := make([]string, 0, len(attached))
	for _, m := range attached {
		parts = append(parts, m.MCPServerID.String()+":"+m.Visibility+":"+m.EndpointSlug+":"+assistantMCPEnvironment(m))
	}
	sort.Strings(parts)
	for _, part := range parts {
		b.WriteString(part)
		b.WriteByte(',')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
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
