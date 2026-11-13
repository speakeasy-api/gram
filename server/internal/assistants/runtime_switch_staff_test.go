package assistants

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestStaffSwitchToKitValidatesReasoningBeforeRetirement(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"none", "medium", "high", "null", "empty", "malformed"} {
		t.Run(effort, func(t *testing.T) {
			t.Parallel()
			conn, err := assistantsInfra.CloneTestDatabase(t, "staff_kit_reasoning_"+effort)
			require.NoError(t, err)
			ctx := t.Context()
			core := newProvisioningCore(t, conn)
			projectID := newProvisioningProject(t, conn, "staff-kit-reasoning")
			configuredEffort := effort
			if effort == "null" || effort == "empty" || effort == "malformed" {
				configuredEffort = "medium"
			}
			assistant, err := core.CreateAssistant(ctx, "org-test", projectID, "user-1", "Staff switch", "test-model", "", nil, nil, 300, 1, StatusPaused, true, nil, nil, configuredEffort)
			require.NoError(t, err)
			require.EqualValues(t, 2, assistant.RuntimeVersion)
			_, err = usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
				ProjectID: projectID, OrganizationID: pgtype.Text{String: "org-test", Valid: true}, Slug: "staff-shared",
				AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
				AuthorizationServerMode: pgtype.Text{String: "shared", Valid: true}, UseAuthenticationHost: pgtype.Bool{},
			})
			require.NoError(t, err)
			q := assistantrepo.New(conn)
			if effort == "null" {
				require.NoError(t, q.ClearAssistantReasoningEffortForTest(ctx, assistantrepo.ClearAssistantReasoningEffortForTestParams{ProjectID: projectID, AssistantID: assistant.ID}))
			}

			if effort == "empty" || effort == "malformed" {
				persistedEffort := effort
				if effort == "empty" {
					persistedEffort = ""
				}
				_, err = q.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{ProjectID: projectID, AssistantID: assistant.ID, ReasoningEffort: pgtype.Text{String: persistedEffort, Valid: true}})
				require.NoError(t, err)
			}
			now, runtimeID := time.Now().UTC(), uuid.New()
			require.NoError(t, q.CreateAssistantRuntime(ctx, assistantrepo.CreateAssistantRuntimeParams{
				ID: runtimeID, AssistantThreadID: uuid.New(), AssistantID: assistant.ID, ProjectID: projectID, Backend: "fly",
				BackendMetadataJson: []byte(`{"machine_id":"legacy-machine"}`), State: runtimeStateActive,
				WarmUntil:       pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
				LastHeartbeatAt: pgtype.Timestamptz{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
				EndedAt: pgtype.Timestamptz{}, DeletedAt: pgtype.Timestamptz{},
			}))
			require.NoError(t, q.SetKitRuntimeMetadataFixture(ctx, assistantrepo.SetKitRuntimeMetadataFixtureParams{ProjectID: projectID, RuntimeID: runtimeID, RuntimeVersion: 2, Metadata: []byte(`{"machine_id":"legacy-machine"}`)}))
			runtimeBefore, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: projectID, ID: runtimeID})
			require.NoError(t, err)
			assistantBefore, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: projectID, AssistantID: assistant.ID})
			require.NoError(t, err)
			var outcome KitSwitchOutcome
			var switchErr error
			require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
				outcome, switchErr = StaffSwitchToKit(ctx, tx, "org-test", projectID, assistant.ID, now)
				// Commit even a refusal to prove validation precedes every authority mutation.
				return nil
			}))
			runtimeAfter, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: projectID, ID: runtimeID})
			require.NoError(t, err)
			assistantAfter, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: projectID, AssistantID: assistant.ID})
			require.NoError(t, err)
			if effort == "none" || effort == "malformed" {
				require.NoError(t, switchErr)
				require.Equal(t, KitSwitchIneligible, outcome)
				require.Equal(t, assistantBefore, assistantAfter)
				require.Equal(t, runtimeBefore, runtimeAfter, "refusal must not retire or alter existing runtime authority")
				// The same assistant is otherwise eligible: correcting only effort permits the switch.
				_, err = q.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{ProjectID: projectID, AssistantID: assistant.ID, ReasoningEffort: pgtype.Text{String: "medium", Valid: true}})
				require.NoError(t, err)
				require.NoError(t, pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
					outcome, err = StaffSwitchToKit(ctx, tx, "org-test", projectID, assistant.ID, now)
					return err
				}))
				require.Equal(t, KitSwitchSwitched, outcome)
				return
			}
			require.NoError(t, switchErr)
			require.Equal(t, KitSwitchSwitched, outcome)
			require.EqualValues(t, 3, assistantAfter.RuntimeVersion)
			require.EqualValues(t, 2, runtimeAfter.RuntimeVersion, "retirement never rewrites legacy protocol authority")
			require.Equal(t, "switch_retiring", runtimeAfter.State)
			require.True(t, runtimeAfter.EndedAt.Valid)
			require.Equal(t, runtimeBefore.BackendMetadataJson, runtimeAfter.BackendMetadataJson)
		})
	}
}

func TestRuntimeReasoningEffortValidatesCompleteEnum(t *testing.T) {
	t.Parallel()
	for _, version := range []int16{2, 3} {
		require.NoError(t, validateRuntimeReasoningEffort(version, ""), "legacy empty values mean medium")
		for _, effort := range []string{"low", "medium", "high"} {
			require.NoError(t, validateRuntimeReasoningEffort(version, effort))
		}
		for _, effort := range []string{"malformed", "HIGH", " medium", "default", "minimal"} {
			require.ErrorIs(t, validateRuntimeReasoningEffort(version, effort), errAssistantValidation)
		}
	}
	require.NoError(t, validateRuntimeReasoningEffort(2, "none"))
	require.ErrorIs(t, validateRuntimeReasoningEffort(3, "none"), errAssistantValidation)
}
