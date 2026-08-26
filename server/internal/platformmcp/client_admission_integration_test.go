package platformmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The CIMD admission mode of a registered MCP is reachable and writable by the
// managed project assistant, which holds no OAuth connection: the whole point
// of putting the last setup step on a tool surface is that the assistant can
// finish it.
func TestClientAdmissionRoundTripsWithoutAConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_client_admission")
	require.NoError(t, err)

	connected, project := seedRegistrationLifecycle(t, ctx, conn)
	assistant := Principal{
		UserID:         connected.UserID,
		OrganizationID: connected.OrganizationID,
		ClientID:       AssistantClientID,
		Surface:        SurfaceProjectAssistant,
	}
	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)

	request := registrationRequest(project, "client-admission", "client-admission-key")
	receipt, err := store.BeginReceipt(ctx, assistant, project, request, time.Now().UTC())
	require.NoError(t, err)
	receipt, err = store.ConvergeRegistration(ctx, assistant, project, request, receipt)
	require.NoError(t, err)
	receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, assistant, project, request, receipt, "https://reviewed.example.test/client-admission")
	require.NoError(t, err)
	require.True(t, receipt.RegistrationID.Valid)
	registrationID := receipt.RegistrationID.UUID

	service := NewClientAdmissionService(conn, audit.NewLogger())

	current, err := service.Get(ctx, assistant, project, registrationID)
	require.NoError(t, err)
	require.Equal(t, "reporting", current.Mode, "a freshly registered MCP reports the unconfigured default")
	require.Equal(t, []string{"disabled", "presets", "open"}, current.AllowedModes)
	require.Empty(t, current.CustomClientURLs)

	updated, err := service.Set(ctx, assistant, project, registrationID, "presets")
	require.NoError(t, err)
	require.Equal(t, "presets", updated.Mode)

	stored, err := service.Get(ctx, assistant, project, registrationID)
	require.NoError(t, err)
	require.Equal(t, "presets", stored.Mode)

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, connected.OrganizationID, auditRecord.OrganizationID)

	// The connected external caller reads the same issuer state the assistant
	// wrote: admission is a property of the MCP, not of who configured it.
	external, err := service.Get(ctx, connected, project, registrationID)
	require.NoError(t, err)
	require.Equal(t, "presets", external.Mode)

	_, err = service.Set(ctx, assistant, project, registrationID, "reporting")
	require.ErrorIs(t, err, ErrClientAdmissionInvalid, "reporting is a deployment default, never a caller-selectable mode")

	_, err = service.Get(ctx, assistant, project, uuid.New())
	require.ErrorIs(t, err, ErrRegistrationInvalid, "an unknown registration is not a readable target")

	foreign := assistant
	foreign.UserID = "user_" + uuid.NewString()
	_, err = service.Set(ctx, foreign, project, registrationID, "open")
	require.ErrorIs(t, err, ErrRegistrationInvalid, "another user's registration is not a writable target")

	issuerID, err := service.registrationIssuer(ctx, conn, assistant, project, registrationID)
	require.NoError(t, err)
	issuer, err := usersessionsrepo.New(conn).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "presets", issuer.ClientIDMetadataAdmissionMode.String, "the rejected writes left the stored mode alone")
}
