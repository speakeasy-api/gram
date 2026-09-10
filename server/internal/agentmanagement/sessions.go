package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type sessionTokenRevoker interface {
	RevokeToken(context.Context, string) error
}
type agentSessionRevoker interface {
	SoftDeleteAgentSubjectSessions(context.Context, remoterepo.DBTX, urn.SessionSubject, uuid.UUID, uuid.UUID, string) ([]remotesessions.RevokedCredentials, error)
	RevokeAllDetached(context.Context, []remotesessions.RevokedCredentials)
}

func (s *Service) ListSessions(ctx context.Context, payload *gen.ListSessionsPayload) (*gen.ListSessionsResult, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	human, agent, err := s.authorizer.RequireAgent(ctx, s.db, agentID, OwnedAgentAuthorize)
	if err != nil {
		return nil, err
	}
	cursor := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid session cursor")
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	limit := payload.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, oops.C(oops.CodeBadRequest)
	}
	rows, err := repo.New(s.db).ListManagedAgentSessions(ctx, repo.ListManagedAgentSessionsParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		AgentSubject:   urn.NewAgentSubject(agent.ID).String(),
		Cursor:         cursor, LimitValue: int32(limit + 1),
	})
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	result := &gen.ListSessionsResult{Items: make([]*gen.AgentSession, 0, len(rows)), NextCursor: nil}
	if len(rows) > limit {
		rows = rows[:limit]
		value := rows[len(rows)-1].ID.String()
		result.NextCursor = &value
	}
	for _, row := range rows {
		item := &gen.AgentSession{
			ID: row.ID.String(), IssuerID: row.UserSessionIssuerID.String(), IssuerSlug: row.IssuerSlug,
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339Nano), ExpiresAt: row.ExpiresAt.Time.Format(time.RFC3339Nano),
			RefreshExpiresAt: row.RefreshExpiresAt.Time.Format(time.RFC3339Nano),
			ProjectID:        nil, ClientName: nil, AuthorizerUserID: nil, LastUsedAt: nil,
		}
		if row.ProjectID.Valid {
			value := row.ProjectID.UUID.String()
			item.ProjectID = &value
		}
		if row.ClientName.Valid {
			item.ClientName = &row.ClientName.String
		}
		if row.AuthorizerUserID.Valid {
			item.AuthorizerUserID = &row.AuthorizerUserID.String
		}
		if row.LastUsedAt.Valid {
			value := row.LastUsedAt.Time.Format(time.RFC3339Nano)
			item.LastUsedAt = &value
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Service) RevokeSession(ctx context.Context, payload *gen.RevokeSessionPayload) error {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return err
	}
	sessionID, err := uuid.Parse(payload.SessionID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid session id")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin agent session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentAuthorize)
	if err != nil {
		return err
	}
	if s.sessionTokens == nil || s.sessionRevoker == nil {
		return oops.C(oops.CodeUnexpected)
	}
	subject := urn.NewAgentSubject(agent.ID)
	row, err := repo.New(tx).RevokeManagedAgentSession(ctx, repo.RevokeManagedAgentSessionParams{
		OrganizationID: human.Auth.ActiveOrganizationID, AgentSubject: subject.String(), ID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return fmt.Errorf("revoke agent session: %w", err)
	}
	projectID := uuid.Nil
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID
	}
	if !row.AlreadyRevoked {
		if err := s.audit.LogUserSessionRevoke(ctx, tx, audit.LogUserSessionRevokeEvent{
			OrganizationID: human.Auth.ActiveOrganizationID, ProjectID: projectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID), ActorDisplayName: human.Auth.Email, ActorSlug: nil,
			UserSessionURN: urn.NewUserSession(row.ID), Principal: subject, Jti: row.Jti,
		}); err != nil {
			return fmt.Errorf("audit agent session revocation: %w", err)
		}
	}
	upstream, err := s.sessionRevoker.SoftDeleteAgentSubjectSessions(ctx, tx, subject, row.UserSessionIssuerID, projectID, human.Auth.ActiveOrganizationID)
	if err != nil {
		return fmt.Errorf("revoke agent upstream sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit agent session revocation: %w", err)
	}
	// Preserve the runtime cascade's order: commit, invalidate JWT, then make
	// best-effort upstream RFC 7009 calls even if the cache push failed.
	pushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	pushErr := s.sessionTokens.RevokeToken(pushCtx, row.Jti)
	s.sessionRevoker.RevokeAllDetached(ctx, upstream)
	if pushErr != nil {
		return oops.E(oops.CodeUnexpected, pushErr, "push agent session revocation")
	}
	return nil
}
