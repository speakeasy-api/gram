package mcptoolexecution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func insertAgentPrincipal(t *testing.T, conn *pgxpool.Pool, orgID, ownerID string) uuid.UUID {
	t.Helper()
	agent, err := agentrepo.New(conn).CreateAgent(t.Context(), agentrepo.CreateAgentParams{
		OrganizationID: orgID, OwnerUserID: ownerID, Name: "Test Agent " + uuid.NewString(),
	})
	require.NoError(t, err)
	return agent.ID
}

func TestAgentPrincipalAdapterCanonicalize(t *testing.T) {
	t.Parallel()
	adapter := NewAgentPrincipalAdapter(nil)
	require.Equal(t, PrincipalKindAgent, adapter.Kind())
	require.Equal(t, killswitches.PrincipalKind("agent"), adapter.Kind())
	id := uuid.MustParse("abcdefab-1234-5678-9abc-abcdefabcdef")
	for _, input := range []string{id.String(), strings.ToUpper(id.String()), " " + id.String() + " "} {
		result, err := adapter.Canonicalize("org", input)
		require.NoError(t, err)
		key, supported, err := result.Key()
		require.NoError(t, err)
		require.True(t, supported)
		require.Equal(t, killswitches.PrincipalKey(id.String()), key)
	}
	for _, input := range []string{"", "not-an-agent", uuid.Nil.String()} {
		result, err := adapter.Canonicalize("org", input)
		require.NoError(t, err)
		_, supported, err := result.Key()
		require.NoError(t, err)
		require.False(t, supported)
	}
}

func TestAgentPrincipalAdapterDerivationFailsClosed(t *testing.T) {
	t.Parallel()
	conn, orgID := newTestDatabase(t, "ks_agent_adapter")
	ownerID := "user_" + uuid.NewString()
	insertUser(t, conn, ownerID, false)
	insertMembership(t, conn, orgID, ownerID, false)
	id := insertAgentPrincipal(t, conn, orgID, ownerID)
	otherOrg := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrg)
	adapter := NewAgentPrincipalAdapter(conn)
	boundary := mcpidentity.NewValidatorBoundary()
	identity, ok := mcpidentity.FromContext(boundary.StampAgent(t.Context(), id))
	require.True(t, ok)
	organization := killswitches.OrganizationID(orgID)
	result, err := adapter.DeriveCandidates(t.Context(), organization, identity)
	require.NoError(t, err)
	require.Equal(t, []killswitches.PrincipalCandidate{{Kind: PrincipalKindAgent, Key: killswitches.PrincipalKey(id.String())}}, result.Candidates())
	valid, err := adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(id.String()))
	require.NoError(t, err)
	require.True(t, valid)
	// Even a real agent owned by an active user must never become that user.
	result, err = NewAuthenticatedUserPrincipalAdapter(conn).DeriveCandidates(t.Context(), organization, identity)
	require.NoError(t, err)
	require.Equal(t, killswitches.PrincipalCandidateResultUnsupported, result.Kind())
	require.Empty(t, result.Candidates())
	missing, ok := mcpidentity.FromContext(boundary.StampAgent(t.Context(), uuid.New()))
	require.True(t, ok)
	invalid, ok := mcpidentity.FromContext(boundary.StampAgent(t.Context(), uuid.Nil))
	require.True(t, ok)
	for name, source := range map[string]any{
		"missing": missing, "invalid source": id.String(), "nil UUID": invalid,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := adapter.DeriveCandidates(t.Context(), organization, source)
			require.Error(t, err)
			require.Empty(t, result.Candidates())
		})
	}
	_, err = adapter.DeriveCandidates(t.Context(), killswitches.OrganizationID(otherOrg), identity)
	require.Error(t, err)
	valid, err = adapter.ValidateCurrentOrganization(t.Context(), killswitches.OrganizationID(otherOrg), killswitches.PrincipalKey(id.String()))
	require.NoError(t, err)
	require.False(t, valid)
	for _, key := range []string{"invalid", uuid.Nil.String(), uuid.NewString()} {
		valid, err := adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(key))
		require.NoError(t, err)
		require.False(t, valid)
	}
	fixtures := testrepo.New(conn)
	for name, mutate := range map[string]func(context.Context, uuid.UUID) error{
		"suspended":            fixtures.SetAgentSuspendedFixture,
		"revoked":              fixtures.SetAgentRevokedFixture,
		"reassignment latched": fixtures.SetAgentOwnerLatchFixture,
	} {
		agentID := insertAgentPrincipal(t, conn, orgID, ownerID)
		valid, err := adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(agentID.String()))
		require.NoError(t, err)
		require.True(t, valid, "%s agent was current before its lifecycle mutation", name)
		require.NoError(t, mutate(t.Context(), agentID))
		valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(agentID.String()))
		require.NoError(t, err)
		require.False(t, valid, "inactive and reassignment-latched identities are not current principals: %s", name)
	}
	err = testrepo.New(conn).SoftDeleteAgentFixture(t.Context(), id)
	require.NoError(t, err)
	_, err = adapter.DeriveCandidates(t.Context(), organization, identity)
	require.Error(t, err)
	valid, err = adapter.ValidateCurrentOrganization(t.Context(), organization, killswitches.PrincipalKey(id.String()))
	require.NoError(t, err)
	require.False(t, valid)
}

func TestAgentCheckpointsUseRealEvaluator(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"private", "hosted"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			conn, orgID := newTestDatabase(t, "ks_agent_"+surface)
			ownerID := "user_" + uuid.NewString()
			insertUser(t, conn, ownerID, false)
			insertMembership(t, conn, orgID, ownerID, false)
			id := insertAgentPrincipal(t, conn, orgID, ownerID)
			otherAgentID := insertAgentPrincipal(t, conn, orgID, ownerID)
			otherOrg := "org_" + uuid.NewString()
			insertOrganization(t, conn, otherOrg)
			insertMembership(t, conn, otherOrg, ownerID, false)
			foreignID := insertAgentPrincipal(t, conn, otherOrg, ownerID)
			projectID := insertProject(t, conn, orgID, "agent-project", false)
			serverID := insertMCPServer(t, conn, orgID, projectID, false)
			registry, err := NewRegistry(conn)
			require.NoError(t, err)
			evaluator, err := killswitches.NewEvaluator(conn, registry, time.Second, nil, testenv.NewLogger(t))
			require.NoError(t, err)
			private, err := newCheckpoint(registry, evaluator, time.Second)
			require.NoError(t, err)
			hosted, err := NewHostedCheckpoint(conn, nil, testenv.NewLogger(t), nil)
			require.NoError(t, err)
			evaluate := func(ctx context.Context) (killswitches.TransportDisposition, error) {
				if surface == "hosted" {
					return hosted.Evaluate(ctx, orgID, ServerSource{FrontingServerID: uuid.NullUUID{UUID: serverID, Valid: true}})
				}
				return private.Evaluate(ctx, orgID, serverID.String())
			}
			boundary := mcpidentity.NewValidatorBoundary()
			contexts := []context.Context{
				boundary.StampAgent(t.Context(), id),
				stampValidatedSession(t, boundary, urn.NewAgentSubject(id)),
			}
			// An existing owner-user rule applies to the owner, not to their agent.
			insertPrescription(t, conn, orgID, prescriptionFixture{ID: uuid.New(), PrincipalKey: ownerID, Scope: "all", ExternalNote: "User paused."})
			userDisposition, err := evaluate(testIdentityContext(t, mcpidentity.KindUserSession, ownerID))
			require.NoError(t, err)
			require.Equal(t, killswitches.TransportDispositionMatchedDenial, userDisposition.Kind())
			for _, ctx := range contexts {
				disposition, err := evaluate(ctx)
				require.NoError(t, err)
				require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
			}
			insertPrescription(t, conn, orgID, prescriptionFixture{ID: uuid.New(), PrincipalKind: PrincipalKindAgent, PrincipalKey: id.String(), Scope: "selected", Resources: []string{serverID.String()}, ExternalNote: "Agent paused."})
			for _, ctx := range contexts {
				disposition, err := evaluate(ctx)
				require.NoError(t, err)
				require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())
				note, ok := disposition.ExternalNote()
				require.True(t, ok)
				require.Equal(t, "Agent paused.", note)
			}
			// Agent prescriptions are concrete, not owner-wide or tenant-wide.
			disposition, err := evaluate(boundary.StampAgent(t.Context(), otherAgentID))
			require.NoError(t, err)
			require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())
			// Resolution infrastructure failures never turn an agent into no-match.
			for _, ctx := range contexts {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				disposition, err := evaluate(cancelled)
				if surface == "hosted" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
			}
			// The same stamped identity must be revalidated after deletion, not cached.
			err = testrepo.New(conn).SoftDeleteAgentFixture(t.Context(), id)
			require.NoError(t, err)
			for name, ctx := range map[string]context.Context{
				"stale API key": contexts[0], "stale session": contexts[1],
				"invalid nil UUID":   boundary.StampAgent(t.Context(), uuid.Nil),
				"missing":            boundary.StampAgent(t.Context(), uuid.New()),
				"other organization": boundary.StampAgent(t.Context(), foreignID),
			} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					disposition, err := evaluate(ctx)
					if surface == "hosted" {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
					require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind())
				})
			}
		})
	}
}
