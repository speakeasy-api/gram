package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	require.Equal(t, "open", current.Mode, "a freshly registered MCP reports the default written at create")
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

func TestClientAdmissionProtectsActiveEMABindings(t *testing.T) {
	t.Parallel()
	for _, concurrentPreparation := range []bool{false, true} {
		name := "existing binding"
		if concurrentPreparation {
			name = "concurrent preparation"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_admission_ema")
			require.NoError(t, err)
			principal, project := seedRegistrationLifecycle(t, ctx, conn)
			store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
			require.NoError(t, err)
			request := registrationRequest(project, "admission-ema", "admission-ema-key")
			receipt, err := store.BeginReceipt(ctx, principal, project, request, time.Now().UTC())
			require.NoError(t, err)
			receipt, err = store.ConvergeRegistration(ctx, principal, project, request, receipt)
			require.NoError(t, err)
			receipt, err = store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, "https://reviewed.example.test/admission-ema")
			require.NoError(t, err)
			service := NewClientAdmissionService(conn, audit.NewLogger())
			registrationID := receipt.RegistrationID.UUID
			issuerID, err := service.registrationIssuer(ctx, conn, principal, project, registrationID)
			require.NoError(t, err)
			q := remoterepo.New(conn)
			remote, err := q.CreateRemoteSessionIssuer(ctx, remoterepo.CreateRemoteSessionIssuerParams{
				ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID),
				Slug: "admission-ema-remote", Issuer: "https://provider.example.test",
				ScopesSupported: []string{}, GrantTypesSupported: []string{}, ResponseTypesSupported: []string{},
				TokenEndpointAuthMethodsSupported: []string{}, CodeChallengeMethodsSupported: []string{},
			})
			require.NoError(t, err)
			bindingParams := remoterepo.EnsureEMABindingParams{
				ProjectID: project.ID, OrganizationID: principal.OrganizationID,
				UserSessionIssuerID: issuerID, RemoteSessionIssuerID: remote.ID, Resource: "https://resource.example.test/",
			}
			if concurrentPreparation {
				tx := testenv.BeginTx(t, ctx, conn)
				txq := remoterepo.New(tx)
				_, err := txq.LockEMAUserIssuer(ctx, remoterepo.LockEMAUserIssuerParams{
					ID: issuerID, ProjectID: conv.ToNullUUID(project.ID), OrganizationID: conv.ToPGText(principal.OrganizationID),
				})
				require.NoError(t, err)
				done := make(chan error, 1)
				go func() {
					_, err := service.Set(ctx, principal, project, registrationID, "disabled")
					done <- err
				}()
				require.Eventually(t, func() bool {
					blocked, err := testrepo.New(conn).IsQueryBlockedOnLockFixture(ctx, "%LockEMAUserIssuer :one%")
					return err == nil && blocked
				}, 10*time.Second, 10*time.Millisecond)
				require.Empty(t, done, "mode change must wait for preparation")
				require.NoError(t, txq.EnsureEMABinding(ctx, bindingParams))
				require.NoError(t, tx.Commit(ctx))
				select {
				case err = <-done:
				case <-ctx.Done():
					t.Fatal("mode change did not finish after preparation committed")
				}
			} else {
				// A legacy NULL resolves to open; comparing raw storage would
				// incorrectly reject the same effective mode with a binding.
				require.NoError(t, testrepo.New(conn).SetUserSessionIssuerCIMDAdmissionMode(ctx, testrepo.SetUserSessionIssuerCIMDAdmissionModeParams{
					ID: issuerID, ProjectID: project.ID, ClientIDMetadataAdmissionMode: pgtype.Text{String: "", Valid: false},
				}))
				require.NoError(t, q.EnsureEMABinding(ctx, bindingParams))
				_, err = service.Set(ctx, principal, project, registrationID, "disabled")
			}
			var shared *oops.ShareableError
			require.ErrorAs(t, err, &shared)
			require.Equal(t, oops.CodeConflict, shared.Code)
			stored, err := service.Get(ctx, principal, project, registrationID)
			require.NoError(t, err)
			require.Equal(t, "open", stored.Mode, "rejected mode change preserves admission")
			unchanged, err := service.Set(ctx, principal, project, registrationID, "open")
			require.NoError(t, err, "same-mode writes are allowed with active bindings")
			require.Equal(t, "open", unchanged.Mode)
			binding, err := q.GetEMABinding(ctx, remoterepo.GetEMABindingParams(bindingParams))
			require.NoError(t, err)
			_, err = q.SetEMABinding(ctx, remoterepo.SetEMABindingParams{
				ID: binding.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID,
				ExpectedGeneration: binding.Generation, Generation: binding.Generation + 1,
				State: conv.ToPGText("unlinked"), GrantSource: conv.ToPGText("unknown"), RequestedScopes: []string{},
			})
			require.NoError(t, err)
			updated, err := service.Set(ctx, principal, project, registrationID, "disabled")
			require.NoError(t, err, "explicitly unlinking permits mode changes")
			require.Equal(t, "disabled", updated.Mode)
		})
	}
}
