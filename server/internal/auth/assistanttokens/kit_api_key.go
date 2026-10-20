package assistanttokens

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/auth"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type KitAPIKey struct {
	ID          uuid.UUID
	Key         string
	ProjectSlug string
}

// IssueKitAPIKey uses the existing agent API-key format and authorization path.
// The runtime binding stays in the control plane, not in the guest credential.
// The caller holds runtime startup admission and rotates previous credentials.
func (m *Manager) IssueKitAPIKey(ctx context.Context, in InferenceIssueInput, environment string) (KitAPIKey, error) {
	if in.RuntimeID == uuid.Nil || in.Lifetime <= 0 || in.Lifetime > MaxInferenceLifetime {
		return KitAPIKey{}, fmt.Errorf("invalid Kit API-key runtime or lifetime")
	}
	tx, err := m.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable, BeginQuery: "", CommitQuery: ""})
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("begin Kit API-key issuance: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	key, err := m.IssueKitAPIKeyInTx(ctx, tx, in, environment)
	if err != nil {
		return KitAPIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return KitAPIKey{}, fmt.Errorf("commit Kit API key: %w", err)
	}
	return key, nil
}

// IssueKitAPIKeyInTx issues a native key on the caller's lifecycle transaction.
// The caller must use REPEATABLE READ or SERIALIZABLE, owns commit/rollback,
// and must not publish the key before commit.
func (m *Manager) IssueKitAPIKeyInTx(ctx context.Context, tx pgx.Tx, in InferenceIssueInput, environment string) (KitAPIKey, error) {
	if tx == nil || in.RuntimeID == uuid.Nil || in.Lifetime <= 0 || in.Lifetime > MaxInferenceLifetime {
		return KitAPIKey{}, fmt.Errorf("invalid Kit API-key transaction, runtime or lifetime")
	}
	ceiling, err := m.snapshotInferenceCeiling(ctx, tx, in.Identity)
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("resolve Kit workload authority: %w", err)
	}
	project, err := projectsrepo.New(tx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: in.Identity.ProjectID, OrganizationID: in.Identity.OrganizationID})
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("load Kit project: %w", err)
	}
	identityJSON, err := json.Marshal(in.Identity)
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("encode Kit authority: %w", err)
	}
	raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix(environment))
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("generate Kit API key: %w", err)
	}
	agent, err := agentrepo.New(tx).GetAgentByIDForUpdate(ctx, agentrepo.GetAgentByIDForUpdateParams{OrganizationID: in.Identity.OrganizationID, ID: in.Identity.AgentID})
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("load Kit agent: %w", err)
	}
	expires := pgtype.Timestamptz{InfinityModifier: pgtype.Finite, Time: time.Now().Add(in.Lifetime), Valid: true}
	key, err := tokenrepo.New(tx).CreateKitAPIKey(ctx, tokenrepo.CreateKitAPIKeyParams{
		ProjectID:      uuid.NullUUID{UUID: in.Identity.ProjectID, Valid: true},
		OrganizationID: in.Identity.OrganizationID, CreatedByUserID: agent.OwnerUserID,
		Name: "assistant-runtime-" + in.RuntimeID.String(), KeyPrefix: prefix, KeyHash: hash,
		SubjectUrn:      pgtype.Text{String: urn.NewPrincipal(urn.PrincipalTypeAgent, in.Identity.AgentID.String()).String(), Valid: true},
		DelegatedGrants: ceiling.Policy, DelegatedGrantsVersion: pgtype.Int4{Int32: int32(ceiling.EncodingVersion), Valid: true}, ExpiresAt: expires,
	})
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("create Kit agent API key: %w", err)
	}
	_, err = tokenrepo.New(tx).IssueKitAPIKeyBinding(ctx, tokenrepo.IssueKitAPIKeyBindingParams{
		ApiKeyID: key.ID, ProjectID: in.Identity.ProjectID, AssistantID: in.Identity.AssistantID, RuntimeID: in.RuntimeID,
		IdentityJson: identityJSON,
	})
	if err != nil {
		return KitAPIKey{}, fmt.Errorf("bind Kit API key to runtime: %w", err)
	}
	return KitAPIKey{ID: key.ID, Key: raw, ProjectSlug: project.Slug}, nil
}

// AuthorizeKitAPIKey adds runtime authority only after ordinary API-key auth.
// A known but revoked/stale runtime key fails closed, never becoming an ordinary
// captured chat request. No caller-supplied attribution establishes ownership.
func (m *Manager) AuthorizeKitAPIKey(ctx context.Context) (context.Context, bool, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.APIKeyID == "" {
		return ctx, false, nil
	}
	keyID, err := uuid.Parse(ac.APIKeyID)
	if err != nil {
		return ctx, false, oops.C(oops.CodeUnauthorized)
	}
	managed, err := m.tokens.IsManagedKitAPIKey(ctx, keyID)
	if err != nil {
		return ctx, false, fmt.Errorf("check Kit API-key binding: %w", err)
	}
	if !managed {
		return ctx, false, nil
	}
	row, err := m.tokens.ReadKitAPIKeyBinding(ctx, keyID)
	if err != nil || row.ApiKeyID != keyID || ac.ProjectID == nil || *ac.ProjectID != row.ProjectID {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	var identity assistantidentity.Identity
	if json.Unmarshal(row.IdentityJson, &identity) != nil || identity.ProjectID != row.ProjectID || identity.AssistantID != row.AssistantID {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor != urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String()) {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	if err := new(assistantidentity.Service).Validate(ctx, m.db, identity); err != nil {
		return ctx, true, oops.C(oops.CodeUnauthorized)
	}
	ctx = context.WithValue(ctx, inferenceContextKey{}, identity)
	return context.WithValue(ctx, inferenceRuntimeContextKey{}, row.RuntimeID), true, nil
}
