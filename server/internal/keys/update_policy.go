package keys

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// UpdateKeyPolicy replaces an ordinary key's own policy. Writing it takes the
// policy over from the default auth permission backfill, which then never
// adds to it.
func (s *Service) UpdateKeyPolicy(ctx context.Context, payload *gen.UpdateKeyPolicyPayload) (*gen.Key, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	keyID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid key ID format")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing api keys").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	kr := s.repo.WithTx(dbtx)
	key, err := kr.GetAPIKeyByIDForUpdate(ctx, repo.GetAPIKeyByIDForUpdateParams{ID: keyID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && key.DeletedAt.Valid) {
		return nil, oops.E(oops.CodeNotFound, nil, "api key not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load api key").LogError(ctx, s.logger)
	}
	if key.SubjectUrn.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "agent keys keep their delegated grants; policy_grants apply to ordinary keys")
	}
	if len(payload.PolicyGrants) > 0 && !slices.ContainsFunc(key.Scopes, func(scope string) bool {
		return scope == auth.APIKeyScopeConsumer.String() || scope == auth.APIKeyScopeProducer.String() || scope == auth.APIKeyScopeChat.String()
	}) {
		return nil, oops.E(oops.CodeBadRequest, nil, "policy_grants need a consumer, producer or chat scope")
	}

	policy, err := s.validatedKeyPolicy(ctx, authCtx.ActiveOrganizationID, payload.PolicyGrants, key.ProjectID)
	if err != nil {
		return nil, err
	}

	principal := urn.NewAPIKeyPrincipal(key.ID)
	previous, err := authz.LoadGrants(ctx, dbtx, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load api key policy").LogError(ctx, s.logger)
	}
	previousAudit, err := policyAuditPayload(grantsToRoleGrants(previous))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare api key audit").LogError(ctx, s.logger)
	}
	policyAudit, err := policyAuditPayload(policy)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "prepare api key audit").LogError(ctx, s.logger)
	}

	if _, err := accessrepo.New(dbtx).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   principal,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "remove api key policy").LogError(ctx, s.logger)
	}
	if err := authz.PatchPrincipalGrants(ctx, dbtx, authCtx.ActiveOrganizationID, principal, policy, nil); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save api key policy").LogError(ctx, s.logger)
	}
	if err := authz.TakeOverPrincipalPolicyTx(ctx, dbtx, authCtx.ActiveOrganizationID, principal); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "take over api key policy").LogError(ctx, s.logger)
	}
	updatedAt, err := kr.TouchAPIKeyUpdatedAt(ctx, repo.TouchAPIKeyUpdatedAtParams{ID: key.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "touch api key").LogError(ctx, s.logger)
	}

	if err := s.audit.LogKeyPolicyUpdate(ctx, dbtx, audit.LogKeyPolicyUpdateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          key.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		KeyURN:             urn.NewAPIKey(key.ID),
		KeyName:            key.Name,
		PolicyGrantsBefore: previousAudit,
		PolicyGrantsAfter:  policyAudit,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log api key policy update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save api key policy").LogError(ctx, s.logger)
	}

	var lastAccessedAt *string
	if key.LastAccessedAt.Valid {
		lastAccessedAt = new(key.LastAccessedAt.Time.Truncate(time.Minute).Format(time.RFC3339))
	}
	return &gen.Key{
		ID:                     key.ID.String(),
		Name:                   key.Name,
		OrganizationID:         key.OrganizationID,
		ProjectID:              conv.FromNullableUUID(key.ProjectID),
		Key:                    nil,
		KeyPrefix:              key.KeyPrefix,
		Scopes:                 key.Scopes,
		CreatedByUserID:        key.CreatedByUserID,
		SubjectUrn:             nil,
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: nil,
		ExpiresAt:              nil,
		PolicyGrants:           policyModel(policy),
		CreatedAt:              key.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              updatedAt.Time.Format(time.RFC3339),
		LastAccessedAt:         lastAccessedAt,
	}, nil
}
