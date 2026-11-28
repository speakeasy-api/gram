package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSelectTurnUserWithResolution_CoversAllInitiatorSources(t *testing.T) {
	t.Parallel()
	assistant := assistantRecord{
		OrganizationID:  "org-test",
		CreatedByUserID: "user-owner",
	}
	slackLookup := func(_ context.Context, p slackrepo.ResolveSlackMappingUserParams) (string, error) {
		if p.SlackTeamID == "T1" && p.SlackUserID == "U-MAPPED" {
			return "user-slack-mapped", nil
		}
		return "", nil
	}

	cases := []struct {
		name           string
		source         string
		payload        string
		wantUser       string
		wantKnown      bool
		wantResolution string
		wantInitiator  ateThreadInitiator
	}{
		{
			name:           "dashboard sender",
			source:         sourceKindDashboard,
			payload:        `{"user_id":"user-dash","content":"hi"}`,
			wantUser:       "user-dash",
			wantKnown:      true,
			wantResolution: ATEInitiatorDashboardSender,
			wantInitiator:  ateThreadInitiator{UserID: "user-dash", Resolution: ATEInitiatorDashboardSender},
		},
		{
			name:           "mapped Slack user",
			source:         sourceKindSlack,
			payload:        `{"team_id":"T1","user_id":"U-MAPPED","text":"hi"}`,
			wantUser:       "user-slack-mapped",
			wantKnown:      true,
			wantResolution: ATEInitiatorSlackMapping,
			wantInitiator:  ateThreadInitiator{UserID: "user-slack-mapped", Resolution: ATEInitiatorSlackMapping},
		},
		{
			name:           "captured wake requester v1",
			source:         sourceKindWake,
			payload:        `{"identity_version":1,"requester_user_id":"user-wake"}`,
			wantUser:       "user-wake",
			wantKnown:      true,
			wantResolution: ATEInitiatorWakeRequester,
			wantInitiator:  ateThreadInitiator{UserID: "user-wake", Resolution: ATEInitiatorWakeRequester},
		},
		{
			name:           "MCP OAuth resume user",
			source:         sourceKindDashboard,
			payload:        `{"` + mcpAuthEventKindKey + `":"` + mcpAuthEventKind + `","` + mcpAuthResumeUserIDKey + `":"user-oauth"}`,
			wantUser:       "user-oauth",
			wantKnown:      true,
			wantResolution: ATEInitiatorMCPAuthResume,
			wantInitiator:  ateThreadInitiator{UserID: "user-oauth", Resolution: ATEInitiatorMCPAuthResume},
		},
		{
			name:           "cron trigger records explicit no_user",
			source:         sourceKindCron,
			payload:        `{"scheduled_at":"2026-01-01T00:00:00Z"}`,
			wantUser:       "user-owner",
			wantKnown:      false,
			wantResolution: ATEInitiatorNoUser,
			wantInitiator:  ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser},
		},
		{
			name:           "webhook trigger records explicit no_user",
			source:         sourceKindGithub,
			payload:        `{"user_id":"spoofed-webhook-user","event":"deploy"}`,
			wantUser:       "user-owner",
			wantKnown:      false,
			wantResolution: ATEInitiatorNoUser,
			wantInitiator:  ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser},
		},
		{
			name:           "unmapped non-Speakeasy Slack records explicit no_user",
			source:         sourceKindSlack,
			payload:        `{"team_id":"T1","user_id":"U-UNKNOWN","text":"hi"}`,
			wantUser:       "user-owner",
			wantKnown:      false,
			wantResolution: ATEInitiatorNoUser,
			wantInitiator:  ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser},
		},
		{
			name:           "unversioned wake records explicit no_user",
			source:         sourceKindWake,
			payload:        `{"identity_version":0}`,
			wantUser:       "user-owner",
			wantKnown:      false,
			wantResolution: ATEInitiatorNoUser,
			wantInitiator:  ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			user, known, resolution, err := selectTurnUserWithResolution(
				t.Context(),
				assistant,
				tc.source,
				assistantThreadEventRecord{NormalizedPayloadJSON: []byte(tc.payload)},
				slackLookup,
			)
			require.NoError(t, err)
			require.Equal(t, tc.wantUser, user)
			require.Equal(t, tc.wantKnown, known)
			require.Equal(t, tc.wantResolution, resolution)

			gotInitiator := ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser}
			if known {
				gotInitiator = ateThreadInitiator{UserID: user, Resolution: resolution}
			}
			require.Equal(t, tc.wantInitiator, gotInitiator)
			require.NoError(t, validateATEThreadInitiator(gotInitiator))
		})
	}
}

func TestKitATEInitiator_RecordedOnceAndImmutableAcrossTurnsCrashesAndRuntimeReplacements(t *testing.T) {
	t.Parallel()

	newFixture := func(t *testing.T) (*ServiceCore, *assistantrepo.Queries, assistantRuntimeRecord, *KitATERuntimeBackend, *fakeATECluster, uuid.UUID, func(uuid.UUID) assistantRuntimeRecord) {
		t.Helper()
		ctx := t.Context()
		core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
		q := assistantrepo.New(db)
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
			RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		}))
		ateBackend, cluster := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
		require.NoError(t, core.installKitRuntime(ateBackend))

		reserveAndEnsure := func(threadID uuid.UUID) assistantRuntimeRecord {
			require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
				AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
				Backend: runtimeBackendATE, State: runtimeStateActive,
			}))
			row, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
				ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
			})
			require.NoError(t, err)
			rec := assistantRuntimeRecord{
				ID: row.ID, AssistantThreadID: row.AssistantThreadID,
				AssistantID: row.AssistantID, ProjectID: row.ProjectID,
				RuntimeVersion: row.RuntimeVersion, Backend: row.Backend,
				BackendMetadataJSON: row.BackendMetadataJson, State: row.State, WarmUntil: row.WarmUntil,
			}
			ensureRes, err := ateBackend.Ensure(ctx, rec)
			require.NoError(t, err)
			require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
				ProjectID: runtimeSeed.ProjectID, RuntimeID: row.ID, BackendMetadataJson: ensureRes.BackendMetadataJSON,
			}))
			rec.BackendMetadataJSON = ensureRes.BackendMetadataJSON
			return rec
		}
		return core, q, runtimeSeed, ateBackend, cluster, runtimeSeed.ProjectID, reserveAndEnsure
	}

	t.Run("rejects unset or not_recorded initiator", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, _, runtimeSeed, ateBackend, _, _, reserveAndEnsure := newFixture(t)
		threadBad := insertATEThreadFixture(t, core.db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "initiator-invalid")
		rec := reserveAndEnsure(threadBad)

		err := ateBackend.RunTurn(ctx, rec, runTurnRequest{
			ThreadID: threadBad, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{},
		})
		require.ErrorContains(t, err, "validate ATE thread initiator")

		err = ateBackend.RunTurn(ctx, rec, runTurnRequest{
			ThreadID: threadBad, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNotRecorded},
		})
		require.ErrorContains(t, err, "validate ATE thread initiator")

		err = ateBackend.RunTurn(ctx, rec, runTurnRequest{
			ThreadID: threadBad, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-a", Resolution: ATEInitiatorNoUser},
		})
		require.ErrorContains(t, err, "must not carry a user ID")
	})

	t.Run("human initiator persists and survives later user turns, crash recovery, and runtime replacement", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, q, runtimeSeed, ateBackend, cluster, _, reserveAndEnsure := newFixture(t)
		threadHuman := insertATEThreadFixture(t, core.db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "initiator-human")
		rec1 := reserveAndEnsure(threadHuman)

		// 1. First turn by User A (dashboard sender) records User A.
		require.NoError(t, ateBackend.RunTurn(ctx, rec1, runTurnRequest{
			ThreadID: threadHuman, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-alice", Resolution: ATEInitiatorDashboardSender},
		}))
		row1, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec1.ID, ThreadID: threadHuman,
		})
		require.NoError(t, err)
		require.True(t, row1.InitiatingUserID.Valid)
		require.Equal(t, "user-alice", row1.InitiatingUserID.String)
		require.Equal(t, ATEInitiatorDashboardSender, row1.InitiatingUserResolution)

		// 2. Second turn on the same thread by User B does NOT alter the recorded initiator.
		require.NoError(t, ateBackend.RunTurn(ctx, rec1, runTurnRequest{
			ThreadID: threadHuman, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-bob", Resolution: ATEInitiatorSlackMapping},
		}))
		row2, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec1.ID, ThreadID: threadHuman,
		})
		require.NoError(t, err)
		require.Equal(t, row1.ActorUid, row2.ActorUid)
		require.True(t, row2.InitiatingUserID.Valid)
		require.Equal(t, "user-alice", row2.InitiatingUserID.String)
		require.Equal(t, ATEInitiatorDashboardSender, row2.InitiatingUserResolution)

		// 3. Crash without durable snapshot -> replacement actor triggered by User B
		// MUST inherit User A (not User B).
		actorKey := "prod-assistants/" + row2.ActorName
		cluster.mu.Lock()
		cluster.actors[actorKey].actor.Status.State = ateapiv1.ActorState_ACTOR_STATE_CRASHED
		cluster.actors[actorKey].actor.Status.Snapshots = nil
		cluster.mu.Unlock()

		require.NoError(t, ateBackend.RunTurn(ctx, rec1, runTurnRequest{
			ThreadID: threadHuman, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-bob", Resolution: ATEInitiatorDashboardSender},
		}))
		rowAfterCrash, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec1.ID, ThreadID: threadHuman,
		})
		require.NoError(t, err)
		require.NotEqual(t, row1.ActorUid, rowAfterCrash.ActorUid, "crashed actor must be replaced")
		require.True(t, rowAfterCrash.InitiatingUserID.Valid)
		require.Equal(t, "user-alice", rowAfterCrash.InitiatingUserID.String, "crash replacement must inherit original initiator")
		require.Equal(t, ATEInitiatorDashboardSender, rowAfterCrash.InitiatingUserResolution)

		// 4. Runtime replacement (retire old runtime + soft-delete its actors,
		// reserve brand new runtime_id) triggered by User Charlie MUST still
		// inherit User A from the thread's earliest recorded row in assistant_ate_actors.
		require.NoError(t, q.RetireAssistantRuntimesForSwitch(ctx, assistantrepo.RetireAssistantRuntimesForSwitchParams{
			ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
		}))
		require.NoError(t, core.finishRuntimeSwitchRetirement(ctx, rec1))

		rec2 := reserveAndEnsure(threadHuman)
		require.NotEqual(t, rec1.ID, rec2.ID)
		require.NoError(t, ateBackend.RunTurn(ctx, rec2, runTurnRequest{
			ThreadID: threadHuman, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-charlie", Resolution: ATEInitiatorWakeRequester},
		}))
		rowAfterRuntimeSwitch, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec2.ID, ThreadID: threadHuman,
		})
		require.NoError(t, err)
		require.NotEqual(t, rowAfterCrash.ActorUid, rowAfterRuntimeSwitch.ActorUid)
		require.True(t, rowAfterRuntimeSwitch.InitiatingUserID.Valid)
		require.Equal(t, "user-alice", rowAfterRuntimeSwitch.InitiatingUserID.String, "new runtime replacement must inherit original initiator")
		require.Equal(t, ATEInitiatorDashboardSender, rowAfterRuntimeSwitch.InitiatingUserResolution)
	})

	t.Run("no_user initiator stays NULL and no_user even if a human triggers replacement", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, q, runtimeSeed, ateBackend, _, _, reserveAndEnsure := newFixture(t)
		threadCron := insertATEThreadFixture(t, core.db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "initiator-cron")
		rec1 := reserveAndEnsure(threadCron)

		require.NoError(t, ateBackend.RunTurn(ctx, rec1, runTurnRequest{
			ThreadID: threadCron, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "", Resolution: ATEInitiatorNoUser},
		}))
		row1, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec1.ID, ThreadID: threadCron,
		})
		require.NoError(t, err)
		require.False(t, row1.InitiatingUserID.Valid)
		require.Equal(t, ATEInitiatorNoUser, row1.InitiatingUserResolution)

		// Retire runtime and replace on a turn sent by a human: must still inherit no_user.
		require.NoError(t, q.RetireAssistantRuntimesForSwitch(ctx, assistantrepo.RetireAssistantRuntimesForSwitchParams{
			ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
		}))
		require.NoError(t, core.finishRuntimeSwitchRetirement(ctx, rec1))

		rec2 := reserveAndEnsure(threadCron)
		require.NoError(t, ateBackend.RunTurn(ctx, rec2, runTurnRequest{
			ThreadID: threadCron, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-human-followup", Resolution: ATEInitiatorDashboardSender},
		}))
		row2, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rec2.ID, ThreadID: threadCron,
		})
		require.NoError(t, err)
		require.False(t, row2.InitiatingUserID.Valid, "userless thread must remain userless on replacement")
		require.Equal(t, ATEInitiatorNoUser, row2.InitiatingUserResolution)
	})

	t.Run("unrecorded actor row fails closed on replacement", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, q, runtimeSeed, ateBackend, _, _, reserveAndEnsure := newFixture(t)
		threadUnrecorded := insertATEThreadFixture(t, core.db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "initiator-unrecorded")
		rec1 := reserveAndEnsure(threadUnrecorded)

		// Seed a soft-deleted actor row whose initiating_user_resolution is not_recorded.
		unrecordedRow, err := q.InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
			ActorName:                "spk-kit-unrecorded-actor",
			ActorUid:                 "uid-unrecorded-" + uuid.NewString(),
			State:                    ateActorStateRunning,
			Configured:               true,
			InitiatingUserResolution: ATEInitiatorNotRecorded,
			RuntimeID:                rec1.ID,
			ProjectID:                runtimeSeed.ProjectID,
			AssistantID:              runtimeSeed.AssistantID,
			ThreadID:                 threadUnrecorded,
		})
		require.NoError(t, err)
		_, err = q.SoftDeleteAssistantATEActor(ctx, assistantrepo.SoftDeleteAssistantATEActorParams{
			RetiredState: ateActorStateRetired,
			ProjectID:    runtimeSeed.ProjectID,
			ID:           unrecordedRow.ID,
		})
		require.NoError(t, err)

		// Attempting to create a replacement actor on a later turn by another sender must fail closed.
		err = ateBackend.RunTurn(ctx, rec1, runTurnRequest{
			ThreadID: threadUnrecorded, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
			Initiator: ateThreadInitiator{UserID: "user-later-sender", Resolution: ATEInitiatorDashboardSender},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "validate earliest ATE thread initiator")
	})
}
