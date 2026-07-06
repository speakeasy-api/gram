package usersessions

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const maxLimit = 100

type listInput struct {
	Status              string `json:"status,omitempty" jsonschema:"Filter by status: active, expired, revoked, or all. Defaults to live sessions."`
	UserSessionIssuerID string `json:"user_session_issuer_id,omitempty" jsonschema:"Filter to one issuer/server id (UUID)."`
	SubjectURN          string `json:"subject_urn,omitempty" jsonschema:"Exact subject URN to filter by (e.g. user:<id>)."`
	ClientID            string `json:"client_id,omitempty" jsonschema:"Filter to one connecting client id (UUID)."`
	Cursor              string `json:"cursor,omitempty" jsonschema:"Pagination cursor: id of the last item from the previous page."`
	Limit               int    `json:"limit,omitempty" jsonschema:"Page size (default 50, max 100)."`
}

type getInput struct {
	ID string `json:"id" jsonschema:"The user session id (UUID)."`
}

type listResult struct {
	Items      []*types.UserSession `json:"items"`
	NextCursor *string              `json:"next_cursor,omitempty"`
}

// buildView converts a DB row to the API type. This mirrors mv.BuildUserSessionView
// but is inlined here to avoid an import cycle (mv imports platformtools).
func buildView(row repo.ListUserSessionsByProjectIDRow) *types.UserSession {
	subjectType := string(row.SubjectUrn.Kind)

	var subjectName *string
	switch row.SubjectUrn.Kind {
	case urn.SessionSubjectKindUser:
		if name := conv.FromPGText[string](row.UserDisplayName); name != nil && *name != "" {
			subjectName = name
		} else {
			subjectName = conv.FromPGText[string](row.UserEmail)
		}
	case urn.SessionSubjectKindAPIKey:
		subjectName = conv.FromPGText[string](row.ApiKeyName)
	case urn.SessionSubjectKindAnonymous:
		// anonymous subjects have no resolved display name
	}

	var revokedAt *string
	if row.Deleted && row.DeletedAt.Valid {
		s := row.DeletedAt.Time.Format(time.RFC3339)
		revokedAt = &s
	}

	return &types.UserSession{
		ID:                  row.ID.String(),
		UserSessionIssuerID: row.UserSessionIssuerID.String(),
		SubjectUrn:          row.SubjectUrn.String(),
		Jti:                 row.Jti,
		RefreshExpiresAt:    row.RefreshExpiresAt.Time.Format(time.RFC3339),
		ExpiresAt:           row.ExpiresAt.Time.Format(time.RFC3339),
		CreatedAt:           row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.Format(time.RFC3339),
		IssuerSlug:          row.IssuerSlug,
		ClientName:          conv.FromPGText[string](row.ClientName),
		SubjectType:         subjectType,
		SubjectDisplayName:  subjectName,
		RevokedAt:           revokedAt,
	}
}

func projectID(ctx context.Context) (uuid.UUID, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return uuid.Nil, oops.C(oops.CodeUnauthorized)
	}
	return *authCtx.ProjectID, nil
}

func parseNullUUID(s string, field string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, oops.E(oops.CodeBadRequest, err, "invalid %s", field)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// ListTool returns a page of user sessions for the authenticated project.
type ListTool struct{ db *pgxpool.Pool }

func NewListUserSessionsTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &ListTool{db: db} }

func (t *ListTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "list",
		Name:        "platform_list_user_sessions",
		Description: "List user sessions (clients connected into this project's MCP toolsets) with optional filters. Read-only.",
		InputSchema: core.BuildInputSchema[listInput](
			core.WithPropertyEnum("status", "active", "expired", "revoked", "all"),
			core.WithPropertyNumberRange("limit", 1, maxLimit),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, err := projectID(ctx)
	if err != nil {
		return err
	}

	var in listInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}

	switch in.Status {
	case "", "active", "expired", "revoked", "all":
	default:
		return oops.E(oops.CodeBadRequest, nil, "invalid status %q: must be active, expired, revoked, or all", in.Status)
	}

	limit := int32(50)
	if in.Limit != 0 {
		if in.Limit < 1 || in.Limit > maxLimit {
			return oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and %d", maxLimit)
		}
		limit = int32(in.Limit)
	}

	issuer, err := parseNullUUID(in.UserSessionIssuerID, "user_session_issuer_id")
	if err != nil {
		return err
	}
	client, err := parseNullUUID(in.ClientID, "client_id")
	if err != nil {
		return err
	}
	cursor, err := parseNullUUID(in.Cursor, "cursor")
	if err != nil {
		return err
	}

	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           pid,
		Status:              conv.ToPGTextEmpty(in.Status),
		SubjectUrn:          conv.ToPGTextEmpty(in.SubjectURN),
		UserSessionIssuerID: issuer,
		ClientID:            client,
		ID:                  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Cursor:              cursor,
		LimitValue:          limit,
	})
	if err != nil {
		return fmt.Errorf("list user sessions: %w", err)
	}

	items := make([]*types.UserSession, len(rows))
	for i, row := range rows {
		items[i] = buildView(row)
	}

	var next *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		next = &c
	}

	return core.EncodeResult(wr, listResult{Items: items, NextCursor: next})
}

// GetTool fetches a single user session by ID for the authenticated project.
type GetTool struct{ db *pgxpool.Pool }

func NewGetUserSessionTool(db *pgxpool.Pool) core.PlatformToolExecutor { return &GetTool{db: db} }

func (t *GetTool) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "user-sessions",
		HandlerName: "get",
		Name:        "platform_get_user_session",
		Description: "Get a single user session by id (read-only).",
		InputSchema: core.BuildInputSchema[getInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *GetTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	pid, err := projectID(ctx)
	if err != nil {
		return err
	}

	var in getInput
	if err := core.DecodeInput(payload, &in); err != nil {
		return err
	}

	id, err := parseNullUUID(in.ID, "id")
	if err != nil {
		return err
	}
	if !id.Valid {
		return oops.E(oops.CodeBadRequest, nil, "id is required")
	}

	// status "all" ensures revoked sessions are visible by ID too.
	rows, err := repo.New(t.db).ListUserSessionsByProjectID(ctx, repo.ListUserSessionsByProjectIDParams{
		ProjectID:           pid,
		Status:              conv.ToPGTextEmpty("all"),
		SubjectUrn:          conv.ToPGTextEmpty(""),
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ID:                  id,
		Cursor:              uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		LimitValue:          1,
	})
	if err != nil {
		return fmt.Errorf("get user session: %w", err)
	}
	if len(rows) == 0 {
		return oops.E(oops.CodeNotFound, nil, "user session not found")
	}

	return core.EncodeResult(wr, buildView(rows[0]))
}
