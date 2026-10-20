package mcp

import (
	"context"
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// platformSkillsEnabled reports whether the platform toolset serves the
// Skills extension to the request's organization. The surface answers only
// assistant-token callers, so the organization is the one the token was
// issued for; a request with no auth context, as a dispatch without
// authorization produces, is served nothing.
func (s *Service) platformSkillsEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if authCtx == nil {
		return false
	}
	return s.skillsOverMCPEnabledForOrganization(ctx, authCtx.ActiveOrganizationID)
}

// platformExtensions lists the extension capability labels the platform
// toolset declares to this request, which is also the set whose methods
// dispatch admits.
func (s *Service) platformExtensions(ctx context.Context, authCtx *contextvalues.AuthContext) []string {
	if !s.platformSkillsEnabled(ctx, authCtx) {
		return nil
	}
	return []string{mcpversions.ExtensionSkills}
}

// listPlatformSkills answers skills/list with the whole embedded catalog in
// one page. The catalog is static, but the listing stays private to the
// caller: whether this surface serves skills at all depends on the caller's
// organization.
func (s *Service) listPlatformSkills(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	if _, err := parseSkillsListParams(ctx, s.logger, req); err != nil {
		return nil, err
	}

	return marshalSkillsList(ctx, s.logger, req.ID, s.platformSkills.Entries(), serverInfoPlatformToolset)
}

// getPlatformSkill answers skills/get with the catalog entry for one URI, or
// the invalid-params error the extension requires for a URI that names no
// served skill.
func (s *Service) getPlatformSkill(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	params, err := parseSkillsGetParams(ctx, s.logger, req)
	if err != nil {
		return nil, err
	}

	entry, ok := s.platformSkills.Get(params.URI)
	if !ok {
		return nil, skillNotFound(ctx, s.logger, mcpskills.ErrSkillNotFound, params.URI)
	}

	return marshalSkillsGet(ctx, s.logger, req.ID, entry, serverInfoPlatformToolset)
}

// readPlatformResource answers resources/read while the platform toolset
// declares the Skills extension. An embedded skill's SKILL.md is read from the
// catalog; any other URI, skill:// or not, names no resource this surface
// serves and is answered with InvalidParams (-32602), as SEP-2640 requires.
func (s *Service) readPlatformResource(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	var params resourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil || params.URI == "" {
		return nil, oops.E(oops.CodeInvalid, err, "resource URI is required").LogWarn(ctx, s.logger)
	}
	if _, ok := mcpskills.ParseSkillURI(params.URI); !ok {
		return nil, oops.E(oops.CodeInvalid, nil, "no resource is served at %s", params.URI).LogWarn(ctx, s.logger)
	}
	return s.readPlatformSkill(ctx, req, params.URI)
}

// readPlatformSkill answers resources/read for an embedded skill's SKILL.md
// with the exact embedded bytes. Embedded skills are reviewed Speakeasy
// content rather than a customer's managed skill, so a read is counted on the
// skill read metric but recorded as no activation: there is no stored version
// to observe. uri is the skill URI the request named, already known to have
// the shape of a served skill.
func (s *Service) readPlatformSkill(ctx context.Context, req *rawRequest, uri string) (json.RawMessage, error) {
	entry, ok := s.platformSkills.Get(uri)
	if !ok {
		return nil, skillNotFound(ctx, s.logger, mcpskills.ErrSkillNotFound, uri)
	}

	bs, err := marshalSkillRead(ctx, s.logger, req.ID, entry, serverInfoPlatformToolset)
	if err != nil {
		return nil, err
	}
	s.metrics.RecordSkillRead(ctx, mcpmetrics.SurfacePlatform, mcpmetrics.SkillOriginEmbedded)
	return bs, nil
}

// listPlatformResources answers resources/list with no resources. The
// platform toolset declares the resources capability only to carry skill
// reads, and skills are listed through skills/list rather than as resources.
func (s *Service) listPlatformResources(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	bs, err := json.Marshal(result[resourcesListResult]{
		ID:             req.ID,
		Result:         resourcesListResult{Resources: make([]*resourceListEntry, 0)},
		serverIdentity: serverInfoPlatformToolset,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/list response").LogError(ctx, s.logger)
	}
	return bs, nil
}
