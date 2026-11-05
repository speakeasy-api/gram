package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpskillsrepo "github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/skilllink"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// WithFeatureProvider sets the flag provider that gates the skill link
// methods behind the skills-over-mcp rollout. Without one those methods
// refuse every request.
func (s *Service) WithFeatureProvider(flags feature.Provider) *Service {
	s.flags = flags
	return s
}

// ErrSkillsOverMCPDisabled is the cause carried by the refusal the skill link
// methods return when the organization is not in the skills-over-mcp rollout,
// so other surfaces can tell it apart from a missing permission.
var ErrSkillsOverMCPDisabled = errors.New("skills over mcp is not enabled for this organization")

// requireSkillsOverMCP refuses the request unless the organization is in the
// skills-over-mcp rollout. It fails closed: a missing provider or a flag
// evaluation error reads as off.
func (s *Service) requireSkillsOverMCP(ctx context.Context, authCtx *contextvalues.AuthContext, logger *slog.Logger) error {
	disabled := oops.E(oops.CodeForbidden, ErrSkillsOverMCPDisabled, "serving skills over MCP is not enabled for this organization")
	if s.flags == nil {
		return disabled
	}
	// The flag targets the organization group by slug. Callers that build
	// their own auth context, such as the Platform MCP, may not carry it.
	orgSlug := authCtx.OrganizationSlug
	if orgSlug == "" {
		org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			logger.WarnContext(ctx, "resolve organization slug for skills-over-mcp flag", attr.SlogError(err))
		} else {
			orgSlug = org.Slug
		}
	}
	groups := feature.OrgProjectGroups(orgSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	enabled, err := s.flags.IsFlagEnabled(ctx, feature.FlagSkillsOverMCP, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		logger.WarnContext(ctx, "skills-over-mcp flag check failed; treating as disabled", attr.SlogError(err))
		return disabled
	}
	if !enabled {
		return disabled
	}
	return nil
}

// mcpGrantResourceID is the resource id mcp grants name for a stored MCP
// server: its toolset when it fronts one, otherwise the server itself. It
// matches the key the MCP servers service checks.
func mcpGrantResourceID(serverID uuid.UUID, toolsetID uuid.NullUUID) string {
	if toolsetID.Valid {
		return toolsetID.UUID.String()
	}
	return serverID.String()
}

// parseLinkTarget turns the request's target fields into a link target. The
// id that fits the kind is required and the other id is refused, so a request
// always names exactly one surface.
func parseLinkTarget(kind string, mcpServerID, metaMCPServerID *string) (skilllink.Target, error) {
	parseID := func(value *string, field string) (uuid.UUID, error) {
		if value == nil {
			return uuid.Nil, oops.E(oops.CodeBadRequest, nil, "%s is required when target_kind is %s", field, kind)
		}
		id, err := uuid.Parse(*value)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, oops.E(oops.CodeBadRequest, nil, "invalid %s", field)
		}
		return id, nil
	}

	var target skilllink.Target
	switch skilllink.TargetKind(kind) {
	case skilllink.TargetMCPServer:
		if metaMCPServerID != nil {
			return skilllink.Target{}, oops.E(oops.CodeBadRequest, nil, "meta_mcp_server_id is not allowed when target_kind is mcp_server")
		}
		id, err := parseID(mcpServerID, "mcp_server_id")
		if err != nil {
			return skilllink.Target{}, err
		}
		target = skilllink.MCPServerTarget(id)
	case skilllink.TargetMetaMCPServer:
		if mcpServerID != nil {
			return skilllink.Target{}, oops.E(oops.CodeBadRequest, nil, "mcp_server_id is not allowed when target_kind is meta_mcp_server")
		}
		id, err := parseID(metaMCPServerID, "meta_mcp_server_id")
		if err != nil {
			return skilllink.Target{}, err
		}
		target = skilllink.MetaMCPServerTarget(id)
	case skilllink.TargetOrganizationGateway:
		if mcpServerID != nil || metaMCPServerID != nil {
			return skilllink.Target{}, oops.E(oops.CodeBadRequest, nil, "the organization gateway takes no server id")
		}
		target = skilllink.OrganizationGatewayTarget()
	default:
		return skilllink.Target{}, oops.E(oops.CodeBadRequest, nil, "unknown target_kind %q", kind)
	}
	if err := target.Validate(); err != nil {
		return skilllink.Target{}, oops.E(oops.CodeBadRequest, err, "invalid skill link target")
	}
	return target, nil
}

// requireLinkTargetWrite resolves a link target in the caller's project and
// checks that the caller may change what it serves: mcp:write on a stored MCP
// server, project-level mcp:write for a gateway (the check the gateway write
// APIs make), and project write for the organization gateway. It returns the target's display name, nil for the
// organization gateway.
func (s *Service) requireLinkTargetWrite(ctx context.Context, authCtx *contextvalues.AuthContext, logger *slog.Logger, target skilllink.Target) (*string, error) {
	queries := mcpskillsrepo.New(s.db)
	projectID := *authCtx.ProjectID
	switch target.Kind {
	case skilllink.TargetMCPServer:
		server, err := queries.GetLinkableMCPServer(ctx, mcpskillsrepo.GetLinkableMCPServerParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, ID: target.ID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, nil, "mcp server not found in project")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "resolve mcp server for skill link").LogError(ctx, logger)
		}
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, mcpGrantResourceID(server.ID, server.ToolsetID), projectID.String())); err != nil {
			return nil, err
		}
		return &server.Name, nil
	case skilllink.TargetMetaMCPServer:
		server, err := queries.GetLinkableMetaMCPServer(ctx, mcpskillsrepo.GetLinkableMetaMCPServerParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, ID: target.ID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeNotFound, nil, "gateway not found in project")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "resolve gateway for skill link").LogError(ctx, logger)
		}
		if err := s.requireProjectMCPWrite(ctx, projectID); err != nil {
			return nil, err
		}
		return &server.Name, nil
	case skilllink.TargetOrganizationGateway:
		if err := s.requireProjectWrite(ctx, projectID); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown target_kind %q", target.Kind)
	}
}

// requireStoredLinkTargetWrite checks write access to the target of a link
// that already exists, whose target row may since have been removed.
func (s *Service) requireStoredLinkTargetWrite(ctx context.Context, projectID uuid.UUID, link mcpskillsrepo.McpSkillLink, toolsetID uuid.NullUUID) error {
	if link.TargetKind == string(skilllink.TargetMCPServer) && link.McpServerID.Valid {
		return s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, mcpGrantResourceID(link.McpServerID.UUID, toolsetID), projectID.String()))
	}
	if link.TargetKind == string(skilllink.TargetMetaMCPServer) {
		return s.requireProjectMCPWrite(ctx, projectID)
	}
	return s.requireProjectWrite(ctx, projectID)
}

// requireProjectMCPWrite matches the check the gateway write APIs make.
func (s *Service) requireProjectMCPWrite(ctx context.Context, projectID uuid.UUID) error {
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil})
}

func (s *Service) requireProjectWrite(ctx context.Context, projectID uuid.UUID) error {
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil})
}

func buildSkillLinkAuditSnapshot(link mcpskillsrepo.McpSkillLink) *audit.SkillLinkSnapshot {
	return &audit.SkillLinkSnapshot{
		ID:              link.ID.String(),
		ProjectID:       link.ProjectID.String(),
		SkillID:         link.SkillID.String(),
		TargetKind:      link.TargetKind,
		MCPServerID:     conv.FromNullableUUID(link.McpServerID),
		MetaMCPServerID: conv.FromNullableUUID(link.MetaMcpServerID),
		PinnedVersionID: conv.FromNullableUUID(link.PinnedVersionID),
		CreatedByUserID: link.CreatedByUserID,
		CreatedAt:       conv.FromPGTimestamptz(link.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(link.UpdatedAt),
		DeletedAt:       conv.PtrEmpty(conv.FromPGTimestamptz(link.DeletedAt)),
	}
}

func (s *Service) Link(ctx context.Context, payload *gen.LinkPayload) (*types.SkillLink, error) {
	authCtx, logger, err := s.requireSkillAccess(ctx, authz.ScopeSkillRead, payload.ID)
	if err != nil {
		return nil, err
	}
	if err := s.requireSkillsOverMCP(ctx, authCtx, logger); err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "linking a skill requires a user identity")
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	target, err := parseLinkTarget(payload.TargetKind, payload.McpServerID, payload.MetaMcpServerID)
	if err != nil {
		return nil, err
	}
	pinnedVersionID, err := conv.PtrToNullUUID(payload.PinnedVersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid pinned version id")
	}
	targetName, err := s.requireLinkTargetWrite(ctx, authCtx, logger, target)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin link skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	linkQueries := mcpskillsrepo.New(dbtx)

	// Locking the skill serializes link writes for it, so the live-link read
	// below and the upsert that follows see the same state.
	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{ProjectID: *authCtx.ProjectID, ID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill for link").LogError(ctx, logger)
	}

	if pinnedVersionID.Valid {
		_, versionErr := queries.GetValidSkillVersion(ctx, repo.GetValidSkillVersionParams{
			ProjectID: *authCtx.ProjectID,
			SkillID:   skill.ID,
			VersionID: pinnedVersionID.UUID,
		})
		if errors.Is(versionErr, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "pinned version must be a valid version of the skill")
		}
		if versionErr != nil {
			return nil, oops.E(oops.CodeUnexpected, versionErr, "validate pinned skill version").LogError(ctx, logger)
		}
	}

	mcpServerID, metaMCPServerID := target.Columns()
	existing, err := linkQueries.GetLiveMCPSkillLinkForTarget(ctx, mcpskillsrepo.GetLiveMCPSkillLinkForTargetParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skill.ID,
		TargetKind:      string(target.Kind),
		McpServerID:     mcpServerID,
		MetaMcpServerID: metaMCPServerID,
	})
	hasExisting := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get live skill link").LogError(ctx, logger)
	}
	if hasExisting && existing.PinnedVersionID == pinnedVersionID {
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "commit unchanged skill link transaction").LogError(ctx, logger)
		}
		return mv.BuildSkillLinkView(existing, skill.Name, skill.DisplayName, targetName), nil
	}

	var pinned *uuid.UUID
	if pinnedVersionID.Valid {
		pinned = &pinnedVersionID.UUID
	}
	link, err := skilllink.UpsertLink(ctx, dbtx, skilllink.NewLink{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skill.ID,
		PinnedVersionID: pinned,
		Target:          target,
		CreatedByUserID: authCtx.UserID,
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
		return nil, oops.E(oops.CodeBadRequest, err, "skill link target not found in project")
	}
	if errors.Is(err, skilllink.ErrInvalidTarget) {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid skill link target")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert skill link").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if hasExisting {
		err = s.audit.LogSkillUpdateLink(ctx, dbtx, audit.LogSkillUpdateLinkEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              actor,
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			SkillURN:           urn.NewSkill(skill.ID),
			SkillName:          skill.Name,
			SkillDisplayName:   skill.DisplayName,
			LinkSnapshotBefore: buildSkillLinkAuditSnapshot(existing),
			LinkSnapshotAfter:  buildSkillLinkAuditSnapshot(link),
		})
	} else {
		err = s.audit.LogSkillLink(ctx, dbtx, audit.LogSkillLinkEvent{
			OrganizationID:    authCtx.ActiveOrganizationID,
			ProjectID:         *authCtx.ProjectID,
			Actor:             actor,
			ActorDisplayName:  authCtx.Email,
			ActorSlug:         nil,
			SkillURN:          urn.NewSkill(skill.ID),
			SkillName:         skill.Name,
			SkillDisplayName:  skill.DisplayName,
			LinkSnapshotAfter: buildSkillLinkAuditSnapshot(link),
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill link").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit link skill transaction").LogError(ctx, logger)
	}

	return mv.BuildSkillLinkView(link, skill.Name, skill.DisplayName, targetName), nil
}

func (s *Service) Unlink(ctx context.Context, payload *gen.UnlinkPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireSkillsOverMCP(ctx, authCtx, logger); err != nil {
		return err
	}
	linkID, err := uuid.Parse(payload.LinkID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, nil, "invalid skill link id")
	}

	// The link is read before authorization because the skill and target it
	// names decide which grants apply. A removed link is still returned, so a
	// repeat is authorized the same way before it answers as a no-op.
	current, err := mcpskillsrepo.New(s.db).GetMCPSkillLink(ctx, mcpskillsrepo.GetMCPSkillLinkParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, ID: linkID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Nothing by this id exists in the project, so there is nothing to
		// remove and nothing to reveal.
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get skill link").LogError(ctx, logger)
	}
	if _, _, err := s.requireSkillAccess(ctx, authz.ScopeSkillRead, current.McpSkillLink.SkillID.String()); err != nil {
		return err
	}
	if err := s.requireStoredLinkTargetWrite(ctx, *authCtx.ProjectID, current.McpSkillLink, current.McpServerToolsetID); err != nil {
		return err
	}
	if authCtx.UserID == "" {
		return oops.E(oops.CodeUnauthorized, nil, "unlinking a skill requires a user identity")
	}
	if current.McpSkillLink.Deleted {
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin unlink skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	removed, err := skilllink.DeleteLink(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, linkID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete skill link").LogError(ctx, logger)
	}
	if !removed {
		// A concurrent unlink got there first and recorded the removal.
		if err := dbtx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit missing skill link transaction").LogError(ctx, logger)
		}
		return nil
	}
	after, err := mcpskillsrepo.New(dbtx).GetMCPSkillLink(ctx, mcpskillsrepo.GetMCPSkillLinkParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, ID: linkID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read removed skill link").LogError(ctx, logger)
	}
	if err := s.audit.LogSkillUnlink(ctx, dbtx, audit.LogSkillUnlinkEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		SkillURN:           urn.NewSkill(current.McpSkillLink.SkillID),
		SkillName:          current.SkillName,
		SkillDisplayName:   current.SkillDisplayName,
		LinkSnapshotBefore: buildSkillLinkAuditSnapshot(current.McpSkillLink),
		LinkSnapshotAfter:  buildSkillLinkAuditSnapshot(after.McpSkillLink),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log skill unlink").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit unlink skill transaction").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) ListLinks(ctx context.Context, payload *gen.ListLinksPayload) (*gen.ListSkillLinksResult, error) {
	authCtx, logger, err := s.requireSkillAccess(ctx, authz.ScopeSkillRead, conv.PtrValOr(payload.SkillID, ""))
	if err != nil {
		return nil, err
	}
	if err := s.requireSkillsOverMCP(ctx, authCtx, logger); err != nil {
		return nil, err
	}

	skillID, err := conv.PtrToNullUUID(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	mcpServerID, err := conv.PtrToNullUUID(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid mcp server id")
	}
	metaMCPServerID, err := conv.PtrToNullUUID(payload.MetaMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid meta mcp server id")
	}

	cursorCreatedAt := conv.PtrToPGTimestamptz(nil)
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		createdAt, id, decodeErr := decodeCreatedAtIDCursor(*payload.Cursor)
		if decodeErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill link cursor")
		}
		cursorCreatedAt = conv.ToPGTimestamptz(createdAt)
		cursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	rows, err := mcpskillsrepo.New(s.db).ListMCPSkillLinks(ctx, mcpskillsrepo.ListMCPSkillLinksParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skillID,
		McpServerID:     mcpServerID,
		MetaMcpServerID: metaMCPServerID,
		TargetKind:      conv.PtrToPGTextEmpty(payload.TargetKind),
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skill links").LogError(ctx, logger)
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}
	var nextCursor *string
	if hasMore {
		last := rows[len(rows)-1]
		encoded := encodeCreatedAtIDCursor(last.McpSkillLink.CreatedAt.Time, last.McpSkillLink.ID)
		nextCursor = &encoded
	}

	readable, err := s.readableLinkTargets(ctx, *authCtx.ProjectID, rows)
	if err != nil {
		return nil, err
	}
	links := make([]*types.SkillLink, 0, len(rows))
	for i, row := range rows {
		if !readable[i] {
			continue
		}
		targetName := mv.SkillLinkTargetName(row.MetaMcpServerName, row.McpServerName, row.ToolsetName, row.McpServerSlug)
		links = append(links, mv.BuildSkillLinkView(row.McpSkillLink, row.SkillName, row.SkillDisplayName, targetName))
	}

	// The cursor follows the last row of the page before access filtering, so
	// a page can hold fewer links than the limit while more remain.
	return &gen.ListSkillLinksResult{
		Links:      links,
		NextCursor: nextCursor,
	}, nil
}

// readableLinkTargets reports, per row, whether the caller may read the
// link's target: mcp:read on a stored MCP server, and mcp:read on the project
// for a gateway, as the gateway read APIs check. The organization gateway has
// no row to grant on, so skill read is enough.
func (s *Service) readableLinkTargets(ctx context.Context, projectID uuid.UUID, rows []mcpskillsrepo.ListMCPSkillLinksRow) ([]bool, error) {
	readable := make([]bool, len(rows))
	checks := make([]authz.Check, 0, len(rows))
	checkRows := make([]int, 0, len(rows))
	for i, row := range rows {
		link := row.McpSkillLink
		switch {
		case link.TargetKind == string(skilllink.TargetMCPServer) && link.McpServerID.Valid:
			checks = append(checks, authz.MCPCheck(authz.ScopeMCPRead, mcpGrantResourceID(link.McpServerID.UUID, row.McpServerToolsetID), projectID.String()))
			checkRows = append(checkRows, i)
		case link.TargetKind == string(skilllink.TargetMetaMCPServer) && link.MetaMcpServerID.Valid:
			checks = append(checks, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: projectID.String(), Dimensions: nil})
			checkRows = append(checkRows, i)
		default:
			readable[i] = true
		}
	}
	if len(checks) == 0 {
		return readable, nil
	}
	matched, err := s.authz.FindMatched(ctx, checks)
	if err != nil {
		return nil, fmt.Errorf("filter skill links by readable target: %w", err)
	}
	for j, i := range checkRows {
		readable[i] = matched[j]
	}
	return readable, nil
}
