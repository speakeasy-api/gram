package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// The Skills extension (SEP-2640) wire shapes the hosted surface answers
// with. Entries are rendered by mcpskills, which owns the Skill shape; these
// are the request params and result envelopes around them.

// skillsListParams is the PaginatedRequest params of skills/list.
type skillsListParams struct {
	// Cursor is the opaque pagination token from a previous result. The
	// hosted surface answers every listing in one page and issues no cursor,
	// so an empty value is the only one it accepts.
	Cursor string `json:"cursor"`
}

// skillsListResult is the ListSkillsResult of skills/list. The caching
// members every CacheableResult carries are spliced in at marshal time.
type skillsListResult struct {
	// Skills holds one entry per served skill, sorted by name. It is an
	// empty array, never null, when the caller is served nothing.
	Skills []mcpskills.Skill `json:"skills"`

	// NextCursor is the token for the following page. Omitted: the whole
	// listing fits in one page.
	NextCursor *string `json:"nextCursor,omitempty"`
}

// skillsGetParams is the params of skills/get.
type skillsGetParams struct {
	// URI is the resource URI of the skill's SKILL.md.
	URI string `json:"uri"`
}

// skillsGetResult is the GetSkillResult of skills/get.
type skillsGetResult struct {
	// Skill is the entry for the requested URI.
	Skill mcpskills.Skill `json:"skill"`
}

// skillMimeType is the media type resources/read reports for a served
// SKILL.md, which is Markdown with YAML frontmatter.
const skillMimeType = "text/markdown"

// parseSkillsListParams decodes the params of skills/list. Absent or null
// params are an unpaginated request. Every surface answers a listing in one
// page and issues no cursor, so a non-empty cursor names a page that does not
// exist and is an invalid param.
func parseSkillsListParams(ctx context.Context, logger *slog.Logger, req *rawRequest) (skillsListParams, error) {
	var params skillsListParams
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return params, oops.E(oops.CodeBadRequest, err, "failed to parse skills/list request").LogWarn(ctx, logger)
		}
	}

	if params.Cursor != "" {
		return params, oops.E(oops.CodeInvalid, nil, "unknown cursor").LogWarn(ctx, logger)
	}
	return params, nil
}

// parseSkillsGetParams decodes the params of skills/get, which must name a
// URI.
func parseSkillsGetParams(ctx context.Context, logger *slog.Logger, req *rawRequest) (skillsGetParams, error) {
	var params skillsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return params, oops.E(oops.CodeBadRequest, err, "failed to parse skills/get request").LogWarn(ctx, logger)
	}

	if params.URI == "" {
		return params, oops.E(oops.CodeInvalid, nil, "skill URI is required").LogWarn(ctx, logger)
	}
	return params, nil
}

// marshalSkillsList renders a skills/list response holding every entry, in
// one page, under the given server identity. The listing is private to the
// caller on every surface: what is served depends on who asks.
func marshalSkillsList(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, entries []mcpskills.Entry, identity serverInfo) (json.RawMessage, error) {
	skills := make([]mcpskills.Skill, 0, len(entries))
	for _, entry := range entries {
		skills = append(skills, entry.Wire())
	}

	bs, err := json.Marshal(result[skillsListResult]{
		ID: id,
		Result: skillsListResult{
			Skills:     skills,
			NextCursor: nil,
		},
		serverIdentity: identity,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize skills/list response").LogError(ctx, logger)
	}
	return bs, nil
}

// marshalSkillsGet renders a skills/get response for one entry under the
// given server identity, private to the caller like the listing.
func marshalSkillsGet(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, entry mcpskills.Entry, identity serverInfo) (json.RawMessage, error) {
	bs, err := json.Marshal(result[skillsGetResult]{
		ID:             id,
		Result:         skillsGetResult{Skill: entry.Wire()},
		serverIdentity: identity,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize skills/get response").LogError(ctx, logger)
	}
	return bs, nil
}

// marshalSkillRead renders a resources/read response carrying an entry's
// SKILL.md as Markdown text: exactly the bytes the entry's digest covers.
func marshalSkillRead(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, entry mcpskills.Entry, identity serverInfo) (json.RawMessage, error) {
	bs, err := json.Marshal(result[resourceReadResult]{
		ID: id,
		Result: resourceReadResult{
			Contents: []resourceContent{{
				URI:      entry.URI(),
				Name:     nil,
				MimeType: new(skillMimeType),
				Text:     entry.Content,
				Blob:     "",
				Meta:     nil,
			}},
		},
		serverIdentity: identity,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/read result").LogError(ctx, logger)
	}
	return bs, nil
}

// hostedSkillScope resolves the surface and caller a skills request is
// answered for. The surface is the MCP server serving the endpoint: the
// fronting server when the request came through one, otherwise the single
// enabled server wrapping a legacy toolset. Skills are linked to servers, so
// an endpoint with neither serves none and ok is false. Resolving the wrapper
// is best effort: a failure is logged and treated as no server. The resolver
// serves a caller only while they are a live member of the owning
// organization; an unauthenticated caller has no user id and so is served no
// skills.
//
// An API key is never a recipient. A legacy key carries its creator's user
// id in the auth context, but the key is the actor, not that person: serving
// the creator's organization skills to every agent holding the key, and
// recording each read as the creator's activation, would be wrong on both
// counts. The RBAC engine refuses the same identification (an API key is not
// a user principal), so the skill scope follows it.
func hostedSkillScope(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs) (scope mcpskills.Scope, ok bool) {
	// The dispatcher leaves attribution to the handlers, and it must not turn
	// an otherwise valid skills request into an error.
	if payload.toolsetID.Valid {
		if err := payload.resolveServingAttribution(ctx, logger, db, payload.toolsetID.UUID); err != nil {
			logger.WarnContext(ctx, "failed to resolve skill serving attribution", attr.SlogError(err))
		}
	}

	serverID := payload.servingServerID()
	if !payload.toolsetID.Valid || serverID == nil {
		return scope, false
	}

	userID := ""
	if payload.authenticated && payload.apiKeyID == "" {
		userID = payload.userID
	}
	return mcpskills.Scope{
		OrganizationID:     payload.organizationID,
		Target:             mcpskills.MCPServerTarget(*serverID),
		MemberMCPServerIDs: nil,
		UserID:             userID,
	}, true
}

// skillResourceURI reports the skill URI a resources/read request names, when
// its params decode and the URI has the shape of a served skill's SKILL.md.
// Any other request, including one whose params are malformed, is left to
// the toolset resource path to answer.
func skillResourceURI(params json.RawMessage) (string, bool) {
	var read resourceReadParams
	if err := json.Unmarshal(params, &read); err != nil {
		return "", false
	}
	if _, _, ok := mcpskills.ParseSkillURI(read.URI); !ok {
		return "", false
	}
	return read.URI, true
}

// skillNotFound is the -32602 answer SEP-2640 requires when no served skill
// matches a URI, whether on skills/get or resources/read. The message names
// the URI, as the extension's example does.
func skillNotFound(ctx context.Context, logger *slog.Logger, err error, uri string) error {
	return oops.E(oops.CodeInvalid, err, "no skill is served at %s", uri).LogWarn(ctx, logger)
}
