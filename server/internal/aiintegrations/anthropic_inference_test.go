package aiintegrations

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

func TestInferenceSetupLifecycle(t *testing.T) {
	t.Parallel()
	ctx, conn, service, orgID := newInferenceTestService(t)
	empty, err := service.GetAnthropicInferenceConfig(ctx, &gen.GetAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	require.Nil(t, empty.ID)
	pending, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	require.NotNil(t, pending.ID)
	require.False(t, pending.Enabled)
	require.False(t, pending.HasSigningSecret)
	resolver := NewAnthropicInferenceResolver(conn, service.store.enc)
	binding, err := resolver.Resolve(ctx, *pending.ID)
	require.NoError(t, err)
	require.Equal(t, orgID, binding.OrganizationID)
	require.Empty(t, binding.SigningSecrets)
	repeated, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	require.Equal(t, pending.ID, repeated.ID)
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte("EXAMPLE-inference-secret-one"))
	enabled := true
	saved, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{SigningSecret: &secret, Enabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, pending.ID, saved.ID)
	require.True(t, saved.Enabled)
	require.True(t, saved.HasSigningSecret)
	row, err := service.store.repo.GetAnthropicInferenceConfig(ctx, orgID)
	require.NoError(t, err)
	require.NotContains(t, row.ApiKeyEncrypted, secret)
	require.NotContains(t, *saved.WebhookPath, secret)
	binding, err = resolver.Resolve(ctx, *saved.ID)
	require.NoError(t, err)
	require.Equal(t, []string{secret}, binding.SigningSecrets)
	schedules, err := service.store.repo.ListSyncSchedules(ctx, row.ID)
	require.NoError(t, err)
	require.Empty(t, schedules, "push hooks must not create polling schedules")
	events, err := auditrepo.New(conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{OrganizationID: orgID, Action: pgtype.Text{String: "ai_integration:upsert", Valid: true}})
	require.NoError(t, err)
	require.Len(t, events, 3)
	for _, event := range events {
		require.NotContains(t, string(event.BeforeSnapshot), secret)
		require.NotContains(t, string(event.AfterSnapshot), secret)
	}
	require.NoError(t, service.DeleteAnthropicInferenceConfig(ctx, &gen.DeleteAnthropicInferenceConfigPayload{}))
	_, err = resolver.Resolve(ctx, *saved.ID)
	require.Error(t, err)
	fresh, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	require.NotEqual(t, saved.ID, fresh.ID)
}

func TestInferenceRotationExpiresPreviousSecret(t *testing.T) {
	t.Parallel()
	ctx, conn, service, orgID := newInferenceTestService(t)
	first := "whsec_" + base64.StdEncoding.EncodeToString([]byte("EXAMPLE-inference-secret-one"))
	second := "whsec_" + base64.StdEncoding.EncodeToString([]byte("EXAMPLE-inference-secret-two"))
	enabled := true
	saved, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{SigningSecret: &first, Enabled: &enabled})
	require.NoError(t, err)
	rotated, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{SigningSecret: &second})
	require.NoError(t, err)
	require.Equal(t, saved.ID, rotated.ID)
	resolver := NewAnthropicInferenceResolver(conn, service.store.enc)
	binding, err := resolver.Resolve(ctx, *saved.ID)
	require.NoError(t, err)
	require.Equal(t, []string{second, first}, binding.SigningSecrets)
	row, err := service.store.repo.GetAnthropicInferenceConfig(ctx, orgID)
	require.NoError(t, err)
	secrets, err := decryptInferenceSecrets(service.store.enc, row.ApiKeyEncrypted)
	require.NoError(t, err)
	secrets.PreviousExpiresAt = time.Now().Add(-time.Minute)
	plaintext, err := json.Marshal(secrets)
	require.NoError(t, err)
	ciphertext, err := service.store.enc.Encrypt(plaintext)
	require.NoError(t, err)
	_, err = service.store.repo.UpdateAnthropicInferenceConfig(ctx, repo.UpdateAnthropicInferenceConfigParams{ID: row.ID, OrganizationID: orgID, ProjectID: row.ProjectID, ApiKeyEncrypted: ciphertext, Enabled: true})
	require.NoError(t, err)
	binding, err = resolver.Resolve(ctx, *saved.ID)
	require.NoError(t, err)
	require.Equal(t, []string{second}, binding.SigningSecrets)
}

func TestInferenceConfigRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, _, service, orgID := newInferenceTestService(t)
	reader := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgRead, orgID)})
	_, err := service.GetAnthropicInferenceConfig(reader, &gen.GetAnthropicInferenceConfigPayload{})
	require.Error(t, err)
	_, err = service.UpsertAnthropicInferenceConfig(reader, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.Error(t, err)
	require.Error(t, service.DeleteAnthropicInferenceConfig(reader, &gen.DeleteAnthropicInferenceConfigPayload{}))
	foreign := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, "org_other")})
	_, err = service.UpsertAnthropicInferenceConfig(foreign, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.Error(t, err)
}

func TestInferenceConfigRejectsInvalidAndMissingSecrets(t *testing.T) {
	t.Parallel()
	ctx, _, service, _ := newInferenceTestService(t)
	enabled := true
	_, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{Enabled: &enabled})
	require.Error(t, err)
	_, err = service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{SigningSecret: conv.PtrEmpty("EXAMPLE-invalid-secret")})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "EXAMPLE-invalid-secret")
	config, err := service.GetAnthropicInferenceConfig(ctx, &gen.GetAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	require.Nil(t, config.ID)
}

func TestInferenceResolverRejectsDeletedProject(t *testing.T) {
	t.Parallel()
	ctx, conn, service, _ := newInferenceTestService(t)
	saved, err := service.UpsertAnthropicInferenceConfig(ctx, &gen.UpsertAnthropicInferenceConfigPayload{})
	require.NoError(t, err)
	resolver := NewAnthropicInferenceResolver(conn, service.store.enc)
	binding, err := resolver.Resolve(ctx, *saved.ID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, binding.ProjectID)
	_, err = projectsrepo.New(conn).DeleteProject(ctx, binding.ProjectID)
	require.NoError(t, err)
	_, err = resolver.Resolve(ctx, *saved.ID)
	require.Error(t, err)
}
