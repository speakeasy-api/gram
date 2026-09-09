package agentmanagement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/audit"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) ListAPIKeys(ctx context.Context, payload *gen.ListAPIKeysPayload) (*gen.ListAPIKeysResult, error) {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return nil, err
	}
	var cursor uuid.NullUUID
	if payload.Cursor != nil {
		id, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid key cursor")
		}
		cursor = uuid.NullUUID{UUID: id, Valid: true}
	}
	human, agent, err := s.authorizer.RequireAgent(ctx, s.db, agentID, OwnedAgentAuthorize)
	if err != nil {
		return nil, s.serviceError(ctx, err, "list agent API keys")
	}
	rows, err := keysrepo.New(s.db).ListAgentAPIKeys(ctx, keysrepo.ListAgentAPIKeysParams{
		OrganizationID: human.Auth.ActiveOrganizationID,
		SubjectUrn:     urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(),
		Cursor:         cursor,
	})
	if err != nil {
		return nil, s.serviceError(ctx, err, "list agent API keys")
	}
	result := &gen.ListAPIKeysResult{Items: make([]*gen.AgentAPIKey, 0, len(rows))}
	if len(rows) > 100 {
		next := rows[99].ID.String()
		result.NextCursor = &next
		rows = rows[:100]
	}
	for _, row := range rows {
		result.Items = append(result.Items, &gen.AgentAPIKey{
			ID: row.ID.String(), Name: row.Name, CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano),
			ExpiresAt: apiKeyTimestamp(row.ExpiresAt), LastAccessedAt: apiKeyTimestamp(row.LastAccessedAt),
		})
	}
	return result, nil
}

func apiKeyTimestamp(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC().Format(time.RFC3339Nano)
	return &result
}

func (s *Service) RevokeAPIKey(ctx context.Context, payload *gen.RevokeAPIKeyPayload) error {
	agentID, err := parseAgentID(payload.AgentID)
	if err != nil {
		return err
	}
	keyID, err := uuid.Parse(payload.KeyID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid key id")
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		human, agent, err := s.authorizer.RequireAgentForUpdate(ctx, tx, agentID, OwnedAgentAuthorize)
		if err != nil {
			return err
		}
		subject := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String()
		keys := keysrepo.New(tx)
		_, err = keys.LockAgentAPIKey(ctx, keysrepo.LockAgentAPIKeyParams{OrganizationID: human.Auth.ActiveOrganizationID, SubjectUrn: subject, ID: keyID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		managed, err := keys.IsAPIKeyManagedByActiveLiteLLMInstance(ctx, keysrepo.IsAPIKeyManagedByActiveLiteLLMInstanceParams{ID: keyID, OrganizationID: human.Auth.ActiveOrganizationID})
		if err != nil {
			return err
		}
		if managed {
			return oops.E(oops.CodeForbidden, nil, "api key is managed by an active LiteLLM instance; revoke the instance instead")
		}
		deleted, err := keys.RevokeAgentAPIKey(ctx, keysrepo.RevokeAgentAPIKeyParams{OrganizationID: human.Auth.ActiveOrganizationID, SubjectUrn: subject, ID: keyID})
		if err != nil {
			return err
		}
		return s.audit.LogKeyRevoke(ctx, tx, audit.LogKeyRevokeEvent{
			OrganizationID: human.Auth.ActiveOrganizationID, ProjectID: deleted.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, human.Auth.UserID), ActorDisplayName: human.Auth.Email,
			KeyURN: urn.NewAPIKey(keyID), KeyName: deleted.Name, Scopes: deleted.Scopes,
		})
	})
	if err != nil {
		return s.serviceError(ctx, err, "revoke agent API key")
	}
	return nil
}
