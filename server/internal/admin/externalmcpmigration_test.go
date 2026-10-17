package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func withExternalMCPMigrator(t *testing.T, svc *Service) {
	t.Helper()
	tracerProvider := testenv.NewTracerProvider(t)
	svc.SetExternalMCPMigrator(migration.NewMigrator(
		testenv.NewLogger(t), tracerProvider, svc.db, audit.NewLogger(), guardian.NewDefaultPolicy(tracerProvider), testenv.NewEncryptionClient(t),
		plugins.PublicationRequests{Enabled: false}, nil,
	))
}

func TestExternalMcpMigrationUnavailableWithoutMigrator(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.ListExternalMcpMigrationCandidates(ctx, &gen.ListExternalMcpMigrationCandidatesPayload{AdminSessionToken: nil})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestListExternalMcpMigrationCandidatesEmpty(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	withExternalMCPMigrator(t, svc)

	result, err := svc.ListExternalMcpMigrationCandidates(ctx, &gen.ListExternalMcpMigrationCandidatesPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, result.Candidates)
	require.Equal(t, 30, result.ToolCallWindowDays)
}

func TestMigrateExternalMcpServersRequiresStaffSession(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	withExternalMCPMigrator(t, svc)

	_, err := svc.MigrateExternalMcpServers(ctx, &gen.MigrateExternalMcpServersPayload{
		AdminSessionToken: nil,
		Servers:           []*gen.AdminExternalMcpMigrationRequest{{ToolsetID: uuid.NewString()}},
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestPreviewExternalMcpMigrationReportsUnknownServer(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	withExternalMCPMigrator(t, svc)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-external-mcp", Email: "operator@example.test", OIDCSubject: "oidc-subject-external-mcp", Name: "Test Operator", HD: "example.test",
	})

	result, err := svc.PreviewExternalMcpMigration(ctx, &gen.PreviewExternalMcpMigrationPayload{
		AdminSessionToken: nil,
		Servers:           []*gen.AdminExternalMcpMigrationRequest{{ToolsetID: uuid.NewString()}},
	})
	require.NoError(t, err)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, "failed", result.Outcomes[0].MigrationStatus)
	require.NotNil(t, result.Outcomes[0].Reason)
}

func TestMigrateExternalMcpServersRejectsDuplicates(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	withExternalMCPMigrator(t, svc)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-external-mcp", Email: "operator@example.test", OIDCSubject: "oidc-subject-external-mcp", Name: "Test Operator", HD: "example.test",
	})
	id := uuid.NewString()

	_, err := svc.MigrateExternalMcpServers(ctx, &gen.MigrateExternalMcpServersPayload{
		AdminSessionToken: nil,
		Servers: []*gen.AdminExternalMcpMigrationRequest{
			{ToolsetID: id},
			{ToolsetID: id},
		},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
