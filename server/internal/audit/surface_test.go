package audit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestLogger_StampsActingSurface writes through the ordinary event API and
// reads the row back, so it covers the whole path rather than the derivation
// alone: an event that never mentions a surface still records one, because the
// logger stamps it for every audited mutation.
func TestLogger_StampsActingSurface(t *testing.T) {
	t.Parallel()

	registeredClient := "client_registered"

	tests := []struct {
		name     string
		actAs    func(ctx context.Context) context.Context
		surface  string
		clientID *string
	}{
		{
			name:     "a dashboard session",
			actAs:    dashboardSession,
			surface:  "dashboard",
			clientID: nil,
		},
		{
			name: "platform mcp with a registered client",
			actAs: func(ctx context.Context) context.Context {
				ctx = contextvalues.SetActingSurface(ctx, "platform_mcp")
				return contextvalues.SetOAuthClientID(ctx, "client_registered")
			},
			surface:  "platform_mcp",
			clientID: &registeredClient,
		},
		{
			name:     "a write with no request identity at all",
			actAs:    func(ctx context.Context) context.Context { return ctx },
			surface:  "unknown",
			clientID: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			conn, err := infra.CloneTestDatabase(t, "testdb")
			require.NoError(t, err)

			orgID := uuid.New().String()
			_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
				ID:          orgID,
				Name:        "Test Org",
				Slug:        "test-org-" + orgID[:8],
				WorkosID:    pgtype.Text{},
				Whitelisted: pgtype.Bool{},
			})
			require.NoError(t, err)

			assetID, err := uuid.NewV7()
			require.NoError(t, err)

			err = audit.NewLogger().LogAssetCreate(tt.actAs(ctx), conn, audit.LogAssetCreateEvent{
				OrganizationID: orgID,
				ProjectID:      uuid.NullUUID{},
				Actor:          urn.NewPrincipal(urn.PrincipalTypeUser, "user_test01"),
				AssetURN:       urn.NewAsset(urn.AssetKindImage, assetID),
				AssetName:      "Test Asset",
			})
			require.NoError(t, err)

			record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionAssetCreate)
			require.NoError(t, err)

			// The column is nullable only so that rows predating it need no
			// backfill. Every row written through the logger carries a value,
			// which is what lets a reader treat null as "older than
			// attribution" rather than "this write was not attributed".
			require.NotNil(t, record.ActingSurface, "a write through the logger must never leave the surface null")
			require.Equal(t, tt.surface, *record.ActingSurface)
			require.Equal(t, tt.clientID, record.ActingClientID)
		})
	}
}

func dashboardSession(ctx context.Context) context.Context {
	sessionID := "session_test"

	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: "org_test",
		UserID:               "user_test",
		SessionID:            &sessionID,
	})
}
