package platformmcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// skillMIMEType is the media type a served SKILL.md is read as: Markdown
// with YAML frontmatter, as on every other surface that serves skills.
const skillMIMEType = "text/markdown"

// SkillsGate decides whether the external Platform MCP serves the Skills
// extension (SEP-2640) to an organization.
type SkillsGate interface {
	SkillsEnabled(ctx context.Context, organizationID string) bool
}

// FeatureSkillsGate evaluates the skills-over-mcp rollout flag the way the
// hosted and assistant surfaces do: by organization id, with the
// organization's slug as its PostHog group, from locally cached definitions
// only, failing closed. A slug lookup failure degrades to id-only evaluation.
type FeatureSkillsGate struct {
	logger   *slog.Logger
	features feature.Provider
	slugs    OrganizationSlugResolver
}

func NewFeatureSkillsGate(logger *slog.Logger, features feature.Provider, slugs OrganizationSlugResolver) *FeatureSkillsGate {
	return &FeatureSkillsGate{logger: logger, features: features, slugs: slugs}
}

func (g *FeatureSkillsGate) SkillsEnabled(ctx context.Context, organizationID string) bool {
	if g == nil || g.features == nil || organizationID == "" {
		return false
	}

	var slug string
	if g.slugs != nil {
		resolved, err := g.slugs.OrganizationSlug(ctx, organizationID)
		if err != nil {
			g.logger.WarnContext(ctx, "resolve organization slug for skills-over-mcp flag", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		} else {
			slug = resolved
		}
	}

	on, err := g.features.IsFlagEnabledLocal(ctx, feature.FlagSkillsOverMCP, organizationID, feature.OrgProjectGroups(slug, ""), nil)
	if err != nil {
		g.logger.WarnContext(ctx, "skills-over-mcp flag check failed; treating as disabled", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return false
	}
	return on
}

// skillsListParams is the params of skills/list. The catalog fits in one
// page, so a cursor can only name a page that does not exist.
type skillsListParams struct {
	mcp.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

// skillsListResult is the ListSkillsResult of skills/list.
type skillsListResult struct {
	mcp.ResultBase
	mcp.Cacheable
	Skills []mcpskills.Skill `json:"skills"`
}

// skillsGetParams is the params of skills/get.
type skillsGetParams struct {
	mcp.ParamsBase
	URI string `json:"uri"`
}

// skillsGetResult is the GetSkillResult of skills/get.
type skillsGetResult struct {
	mcp.ResultBase
	mcp.Cacheable
	Skill mcpskills.Skill `json:"skill"`
}

// WithSkills serves the embedded Platform MCP skills over the Skills
// extension to callers whose organization the gate admits: the same catalog,
// byte for byte, that the Platform MCP package carries and the assistant
// toolset serves. For any other caller the surface is unchanged: no
// extension is declared, skills/list and skills/get are unknown methods, and
// a skill:// read is an ordinary resource that is not found. Skills are
// never listed by resources/list.
func (r *Runtime) WithSkills(catalog *mcpskills.EmbeddedCatalog, gate SkillsGate) (*Runtime, error) {
	if catalog == nil || gate == nil {
		return r, nil
	}

	if err := mcp.AddReceivingCustomMethod(r.server, mcpversions.MethodSkillsList, func(_ context.Context, _ *mcp.ServerSession, params *skillsListParams) (*skillsListResult, error) {
		if params != nil && params.Cursor != "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "unknown cursor", Data: nil}
		}
		entries := catalog.Entries()
		skills := make([]mcpskills.Skill, 0, len(entries))
		for _, entry := range entries {
			skills = append(skills, entry.Wire())
		}
		return &skillsListResult{Skills: skills, Cacheable: privateCacheable()}, nil //nolint:exhaustruct // MCP SDK metadata is intentionally omitted.
	}); err != nil {
		return nil, fmt.Errorf("register %s: %w", mcpversions.MethodSkillsList, err)
	}

	if err := mcp.AddReceivingCustomMethod(r.server, mcpversions.MethodSkillsGet, func(_ context.Context, _ *mcp.ServerSession, params *skillsGetParams) (*skillsGetResult, error) {
		if params == nil || params.URI == "" {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "skill URI is required", Data: nil}
		}
		entry, ok := catalog.Get(params.URI)
		if !ok {
			return nil, skillNotFound(params.URI)
		}
		return &skillsGetResult{Skill: entry.Wire(), Cacheable: privateCacheable()}, nil //nolint:exhaustruct // MCP SDK metadata is intentionally omitted.
	}); err != nil {
		return nil, fmt.Errorf("register %s: %w", mcpversions.MethodSkillsGet, err)
	}

	r.server.AddReceivingMiddleware(skillsMiddleware(catalog, gate))
	return r, nil
}

// skillsMiddleware gates the extension per request on the caller's
// organization. The SDK's server and its capabilities are shared by every
// caller, so the extension is added to the handshake result here rather than
// to the server options, and the custom methods registered for it answer as
// unknown to a caller the gate does not admit.
func skillsMiddleware(catalog *mcpskills.EmbeddedCatalog, gate SkillsGate) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			switch method {
			case "initialize", "server/discover":
				result, err := next(ctx, method, req)
				if err != nil || !skillsEnabled(ctx, gate) {
					return result, err
				}
				switch res := result.(type) {
				case *mcp.InitializeResult:
					res.Capabilities = withSkillsExtension(res.Capabilities)
				case *mcp.DiscoverResult:
					res.Capabilities = withSkillsExtension(res.Capabilities)
				}
				return result, nil
			case mcpversions.MethodSkillsList, mcpversions.MethodSkillsGet:
				if !skillsEnabled(ctx, gate) {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: fmt.Sprintf("method %q not found", method), Data: nil}
				}
				return next(ctx, method, req)
			case "resources/read":
				read, ok := req.(*mcp.ReadResourceRequest)
				if !ok || read.Params == nil {
					return next(ctx, method, req)
				}
				if _, isSkill := mcpskills.ParseSkillURI(read.Params.URI); !isSkill || !skillsEnabled(ctx, gate) {
					return next(ctx, method, req)
				}
				entry, found := catalog.Get(read.Params.URI)
				if !found {
					return nil, skillNotFound(read.Params.URI)
				}
				return &mcp.ReadResourceResult{ //nolint:exhaustruct // MCP SDK metadata is intentionally omitted.
					Cacheable: privateCacheable(),
					Contents: []*mcp.ResourceContents{{
						URI:      entry.URI(),
						MIMEType: skillMIMEType,
						Text:     entry.Content,
					}},
				}, nil
			default:
				return next(ctx, method, req)
			}
		}
	}
}

// skillsEnabled asks the gate about the authenticated caller's organization.
// A request without a principal is served nothing.
func skillsEnabled(ctx context.Context, gate SkillsGate) bool {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return false
	}
	return gate.SkillsEnabled(ctx, principal.OrganizationID)
}

// withSkillsExtension returns a copy of capabilities that also declares the
// Skills extension. The extension needs the Resources primitive, which this
// server always declares.
func withSkillsExtension(capabilities *mcp.ServerCapabilities) *mcp.ServerCapabilities {
	var out mcp.ServerCapabilities
	if capabilities != nil {
		out = *capabilities
	}
	extensions := make(map[string]any, len(out.Extensions)+1)
	for name, settings := range out.Extensions {
		extensions[name] = settings
	}
	extensions[mcpversions.ExtensionSkills] = map[string]any{}
	out.Extensions = extensions
	return &out
}

// privateCacheable marks a result as private to the caller: whether the
// extension is served at all depends on who asks.
func privateCacheable() mcp.Cacheable {
	return mcp.Cacheable{TTLMs: 0, CacheScope: "private"}
}

// skillNotFound is the -32602 answer SEP-2640 requires when no served skill
// matches a URI, whether on skills/get or resources/read.
func skillNotFound(uri string) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("no skill is served at %s", uri), Data: nil}
}
