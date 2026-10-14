package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
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

// hostedSkillScope resolves the endpoint and caller a skills request is
// answered for. The endpoint is the described toolset together with its
// fronting server when the request came through one, so a plugin containing
// either carries skills to it. The caller's delivery principals decide which
// plugins are assigned to them; a caller who is not an authenticated member
// of the owning organization, or one the endpoint never resolved a toolset
// for, has none and so is served no skills.
func hostedSkillScope(ctx context.Context, db *pgxpool.Pool, payload *mcpInputs) (mcpskills.Scope, error) {
	scope := mcpskills.Scope{
		ProjectID:     payload.projectID,
		ToolsetID:     payload.toolsetID.UUID,
		MCPServerID:   payload.mcpServerID,
		PrincipalURNs: nil,
	}
	if !payload.toolsetID.Valid || !payload.authenticated || payload.userID == "" {
		return scope, nil
	}

	principals, err := mcpskills.PrincipalURNsForCaller(ctx, db, payload.organizationID, payload.userID)
	if err != nil {
		return scope, fmt.Errorf("resolve skill recipient principals: %w", err)
	}
	scope.PrincipalURNs = principals
	return scope, nil
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
	if _, ok := mcpskills.ParseSkillURI(read.URI); !ok {
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
