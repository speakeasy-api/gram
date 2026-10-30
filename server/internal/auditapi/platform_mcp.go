package auditapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// PlatformMCPAuditRequest intersects investigation filters with the transport's trusted user.
type PlatformMCPAuditRequest struct {
	// OrganizationID is the authenticated connection's organization, not tool input.
	OrganizationID string

	// UserID is the authenticated canonical user, not an actor filter.
	UserID string

	// Cursor is an optional position in descending sequence order.
	Cursor string

	// ProjectID narrows history to an exact same-organization project, including deleted projects.
	ProjectID uuid.NullUUID

	// ActorID optionally narrows persisted attribution without granting visibility.
	ActorID string

	// Action matches exact recorded action vocabulary.
	Action string

	// SubjectType selects an exact resource type; absent excludes assistant subjects.
	SubjectType string

	// SubjectID matches an exact recorded resource identifier.
	SubjectID string

	// ActingSurface matches recorded vocabulary, with unknown including null surfaces.
	ActingSurface string

	// From is an optional inclusive UTC creation-time bound.
	From *time.Time

	// To is an optional exclusive UTC creation-time bound.
	To *time.Time
}

// PlatformMCPAuditPage contains customer-masked details and applied visibility.
type PlatformMCPAuditPage struct {
	// Visibility is self or organization, recomputed for this request.
	Visibility string

	// Logs retains the customer API's full detail projection.
	Logs []*gen.AuditLog

	// NextCursor continues after the last returned event without retaining authority.
	NextCursor *string
}

// ListPlatformMCPAuditLogs reads authorized history using the prepared request grants.
func (s *Service) ListPlatformMCPAuditLogs(ctx context.Context, request PlatformMCPAuditRequest) (*PlatformMCPAuditPage, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if !ok || authCtx == nil || request.OrganizationID == "" || request.UserID == "" ||
		authCtx.ActiveOrganizationID != request.OrganizationID || authCtx.UserID != request.UserID ||
		!hasActor || actor.Type != urn.PrincipalTypeUser || actor.ID != request.UserID {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	enforce, err := s.authz.ShouldEnforce(ctx)
	if err != nil {
		return nil, fmt.Errorf("check audit enforcement: %w", err)
	}
	if !enforce {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, loaded := authz.GrantsFromContext(ctx); !loaded {
		return nil, oops.C(oops.CodeUnexpected)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: request.OrganizationID, Dimensions: nil}); err != nil {
		return nil, fmt.Errorf("require audit organization read: %w", err)
	}
	admin, err := s.authz.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: request.OrganizationID, Dimensions: nil})
	if err != nil {
		return nil, fmt.Errorf("evaluate audit organization admin: %w", err)
	}
	if !admin && request.ActorID != "" && request.ActorID != request.UserID {
		return nil, oops.C(oops.CodeForbidden)
	}
	visibility := "self"
	params := repo.ListAuditLogsParams{
		OrganizationID: request.OrganizationID,
		ProjectID:      request.ProjectID,
		CursorSeq:      pgtype.Int8{Int64: 0, Valid: false},
		ActorID:        conv.ToPGText(request.UserID), ActorType: conv.ToPGText(string(urn.PrincipalTypeUser)),
		Action: conv.ToPGTextEmpty(request.Action), SubjectType: conv.ToPGTextEmpty(request.SubjectType),
		SubjectID: conv.ToPGTextEmpty(request.SubjectID), SubjectIds: nil,
		ActingSurface: conv.ToPGTextEmpty(request.ActingSurface), IncludeAssistantEvents: false,
		CreatedFrom: conv.PtrToPGTimestamptz(request.From),
		CreatedTo:   conv.PtrToPGTimestamptz(request.To),
	}
	if admin {
		visibility = "organization"
		params.ActorID = conv.ToPGTextEmpty(request.ActorID)
		params.ActorType = pgtype.Text{String: "", Valid: false}
	}
	if request.Cursor != "" {
		seq, err := audit.DecodeCursor(request.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogWarn(ctx, s.logger)
		}
		params.CursorSeq = pgtype.Int8{Int64: seq, Valid: true}
	}
	if request.ProjectID.Valid {
		belongs, err := repo.New(s.db).AuditProjectBelongsToOrganization(ctx, repo.AuditProjectBelongsToOrganizationParams{
			ProjectID: request.ProjectID.UUID, OrganizationID: request.OrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error validating audit project").LogError(ctx, s.logger)
		}
		if !belongs {
			return nil, oops.C(oops.CodeNotFound)
		}
	}
	rows, err := repo.New(s.db).ListAuditLogs(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing audit logs").LogError(ctx, s.logger)
	}
	page, err := s.customerPage(ctx, request.OrganizationID, rows)
	if err != nil {
		return nil, err
	}
	return &PlatformMCPAuditPage{Visibility: visibility, Logs: page.Logs, NextCursor: page.NextCursor}, nil
}
