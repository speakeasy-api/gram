package mcptoolexecution

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
)

type coverageObservation struct {
	surface  mcpmetrics.KillswitchCoverageSurface
	identity mcpmetrics.KillswitchIdentityClass
	resource mcpmetrics.KillswitchResourceClass
}

type coverageRecorder struct {
	observations []coverageObservation
}

func (r *coverageRecorder) RecordKillswitchIdentityCoverage(_ context.Context, surface mcpmetrics.KillswitchCoverageSurface, identity mcpmetrics.KillswitchIdentityClass, resource mcpmetrics.KillswitchResourceClass) {
	r.observations = append(r.observations, coverageObservation{surface: surface, identity: identity, resource: resource})
}

func TestIdentityCoverageCheckpoint_RevalidatesEachCall(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_coverage_checkpoint")
	userID := "user_" + uuid.NewString()
	insertUser(t, conn, userID, nil)
	insertMembership(t, conn, orgID, userID, nil)
	projectID := insertProject(t, conn, orgID, "coverage-"+uuid.NewString()[:8], nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)

	recorder := &coverageRecorder{}
	checkpoint := NewIdentityCoverageCheckpoint(conn, recorder)
	ctx := mcpidentity.WithIdentity(t.Context(), mcpidentity.AuthenticatedUser(userID))
	source := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}}

	checkpoint.Record(ctx, orgID, mcpmetrics.KillswitchSurfaceHosted, source)
	require.Equal(t, coverageObservation{
		surface:  mcpmetrics.KillswitchSurfaceHosted,
		identity: mcpmetrics.KillswitchIdentityActiveUser,
		resource: mcpmetrics.KillswitchResourceCanonicalServer,
	}, recorder.observations[0])

	_, err := conn.Exec(t.Context(), `
		UPDATE organization_user_relationships
		SET deleted_at = clock_timestamp()
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, userID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = $1`, serverID)
	require.NoError(t, err)

	checkpoint.Record(ctx, orgID, mcpmetrics.KillswitchSurfaceHosted, source)
	require.Equal(t, coverageObservation{
		surface:  mcpmetrics.KillswitchSurfaceHosted,
		identity: mcpmetrics.KillswitchIdentityInactiveUser,
		resource: mcpmetrics.KillswitchResourceInvalidOwner,
	}, recorder.observations[1])
}

func TestIdentityCoverageCheckpoint_UsesOnlyStampedProvenance(t *testing.T) {
	t.Parallel()

	conn, orgID := newTestDatabase(t, "ks_coverage_provenance")
	projectID := insertProject(t, conn, orgID, "coverage-"+uuid.NewString()[:8], nil)
	serverID := insertMCPServer(t, conn, orgID, projectID, nil)
	source := ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}}
	recorder := &coverageRecorder{}
	checkpoint := NewIdentityCoverageCheckpoint(conn, recorder)

	checkpoint.Record(t.Context(), orgID, mcpmetrics.KillswitchSurfacePrivateProxy, source)
	checkpoint.Record(
		mcpidentity.WithIdentity(t.Context(), mcpidentity.Identity{Kind: mcpidentity.KindAPIKey}),
		orgID,
		mcpmetrics.KillswitchSurfacePrivateProxy,
		source,
	)

	require.Equal(t, mcpmetrics.KillswitchIdentityUnattributed, recorder.observations[0].identity)
	require.Equal(t, mcpmetrics.KillswitchIdentityAPIKey, recorder.observations[1].identity)
	require.Equal(t, mcpmetrics.KillswitchResourceCanonicalServer, recorder.observations[0].resource)
	require.Equal(t, mcpmetrics.KillswitchResourceCanonicalServer, recorder.observations[1].resource)
}
