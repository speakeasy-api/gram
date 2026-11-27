package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

func TestKitATESteerByAnotherUserPreservesInitiatorAndAuthorizer(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		userA    = "user-2"
		userB    = "user-test"
		catalog  = `{"configOptions":[{"configId":"model","type":"select","currentValue":"speakeasy:openai/gpt-4o-mini","options":[{"value":"speakeasy:openai/gpt-4o-mini"}]},{"configId":"reasoning_effort","type":"select","currentValue":"medium","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]}`
	)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	var mu sync.Mutex
	streams := map[string]chan string{}
	insertedPrompts := []string{}
	insertedDeliveries := []string{}
	settled := false
	firstPromptRunning := make(chan struct{})
	secondPromptInserted := make(chan struct{})
	releaseTurn := make(chan struct{})

	acpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := r.Header.Get("Acp-Session-Id")
		switch r.Method {
		case http.MethodPost:
			var msg struct {
				ID     json.RawMessage            `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.NewDecoder(r.Body).Decode(&msg) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if msg.Method == "initialize" {
				w.Header().Set("Acp-Connection-Id", "conn-ate-steer")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{"_meta":{"kit/session/status":{}},"session":{"prompt":{"delivery":{"steer":{},"queue":{}}}}}}}`, msg.ID)
				return
			}
			mu.Lock()
			ch := streams[session]
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
			switch msg.Method {
			case "session/new":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", msg.ID, strings.TrimSuffix(catalog, "}")+`,"sessionId":"ate-steer-session"}`)
			case "session/prompt":
				var delivery string
				_ = json.Unmarshal(msg.Params["delivery"], &delivery)
				rawPrompt := string(msg.Params["prompt"])
				mu.Lock()
				insertedDeliveries = append(insertedDeliveries, delivery)
				insertedPrompts = append(insertedPrompts, rawPrompt)
				n := len(insertedPrompts)
				mu.Unlock()
				msgID := fmt.Sprintf("u%d", n)
				ch <- fmt.Sprintf(`data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ate-steer-session","update":{"sessionUpdate":"user_message","messageId":%q,"content":[{"type":"text","text":"msg-%d"}]}}}`+"\n\n", msgID, n)
				if n == 1 {
					ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ate-steer-session","update":{"sessionUpdate":"state_update","state":"running"}}}` + "\n\n"
				}
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"messageId\":%q}}\n\n", msg.ID, msgID)
				if n == 1 {
					close(firstPromptRunning)
				}
				if n == 2 {
					close(secondPromptInserted)
					go func() {
						select {
						case <-releaseTurn:
						case <-ctx.Done():
							return
						}
						mu.Lock()
						settled = true
						mu.Unlock()
						ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ate-steer-session","update":{"sessionUpdate":"agent_message_chunk","messageId":"a1","content":{"type":"text","text":"handled both prompts"}}}}` + "\n\n"
						ch <- `data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"ate-steer-session","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}}` + "\n\n"
					}()
				}
			case "kit/session/status":
				mu.Lock()
				done := settled
				mu.Unlock()
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"sessionId\":\"ate-steer-session\",\"settled\":%t,\"foregroundActive\":%t,\"backgroundActive\":false,\"pendingBackgroundTerminals\":false,\"activeSubagents\":0}}\n\n", msg.ID, done, !done)
			case "session/close":
				ch <- fmt.Sprintf("data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{}}\n\n", msg.ID)
			}
		case http.MethodGet:
			ch := make(chan string, 32)
			mu.Lock()
			streams[session] = ch
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flushKitTestResponse(t, w)
			for {
				select {
				case data := <-ch:
					_, _ = fmt.Fprint(w, data)
					flushKitTestResponse(t, w)
				case <-r.Context().Done():
					return
				}
			}
		case http.MethodDelete:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer acpServer.Close()

	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	seedProjectRead(t, db, userA, runtimeSeed.ProjectID)
	seedProjectRead(t, db, userB, runtimeSeed.ProjectID)
	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	core.assistantTokens = assistanttokens.New("test-jwt-secret", db, core.authz, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))

	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}))

	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID: chatID, ProjectID: runtimeSeed.ProjectID, OrganizationID: "org-test", Title: pgtype.Text{},
	}))
	threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		CorrelationID: "ate-steer-authz", ChatID: chatID, SourceKind: sourceKindDashboard, SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	writer, shutdown := chat.NewChatMessageWriter(logger, db, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	acpEndpoint, err := url.Parse(acpServer.URL + "/acp")
	require.NoError(t, err)
	ateBackend, _ := newCutoverATEBackend(t, core, localCfg, logger, nil)
	ateBackend.config.Protocol = func(_ context.Context, r assistantRuntimeRecord, thread uuid.UUID, _ string, _ *url.URL, _ *guardian.HTTPClient, token string) (runtimeProtocol, error) {
		key := kitRuntimeSessionKey{ProjectID: r.ProjectID, AssistantID: r.AssistantID, RuntimeID: r.ID, ThreadID: thread}
		projector := NewKitTranscriptProjector(db, writer, nil, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
			_, err := core.ValidateKitRuntimeSessionInTx(ctx, tx, key, b.SessionID)
			return err
		})
		return NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
			Core:         core,
			Endpoint:     acpEndpoint,
			HTTPClient:   kitProtocolTestHTTPClient(t),
			Projector:    projector,
			CWD:          "/workspace",
			GatewayToken: token,
		})
	}
	require.NoError(t, core.installKitRuntime(ateBackend))

	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		Backend: runtimeBackendATE, State: runtimeStateActive,
	}))
	rtRow, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	rec := assistantRuntimeRecord{
		ID: rtRow.ID, AssistantThreadID: rtRow.AssistantThreadID,
		AssistantID: rtRow.AssistantID, ProjectID: rtRow.ProjectID,
		RuntimeVersion: rtRow.RuntimeVersion, Backend: rtRow.Backend,
		BackendMetadataJSON: rtRow.BackendMetadataJson, State: rtRow.State, WarmUntil: rtRow.WarmUntil,
	}
	ensureRes, err := ateBackend.Ensure(ctx, rec)
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
		ProjectID: runtimeSeed.ProjectID, RuntimeID: rtRow.ID, BackendMetadataJson: ensureRes.BackendMetadataJSON,
	}))

	insertDashboardMsg := func(senderUserID, text string) {
		t.Helper()
		msgID, err := testrepo.New(db).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
			ProjectID: uuid.NullUUID{UUID: runtimeSeed.ProjectID, Valid: true},
			ChatID:    chatID,
			Role:      "user",
			Content:   text,
		})
		require.NoError(t, err)
		norm, err := json.Marshal(dashboardEventPayload{Text: text, UserID: senderUserID})
		require.NoError(t, err)
		source, err := json.Marshal(map[string]string{"idempotency_key": msgID.String()})
		require.NoError(t, err)
		_, err = q.InsertAssistantThreadEvent(ctx, assistantrepo.InsertAssistantThreadEventParams{
			AssistantThreadID:     threadID,
			AssistantID:           runtimeSeed.AssistantID,
			ProjectID:             runtimeSeed.ProjectID,
			EventID:               uuid.NewString(),
			CorrelationID:         chatID.String(),
			Status:                eventStatusPending,
			NormalizedPayloadJson: norm,
			SourcePayloadJson:     source,
		})
		require.NoError(t, err)
	}

	assertAuthorizesAsUserA := func() {
		t.Helper()
		actorRow, err := q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: rtRow.ID, ThreadID: threadID,
		})
		require.NoError(t, err)
		require.True(t, actorRow.InitiatingUserID.Valid)
		require.Equal(t, userA, actorRow.InitiatingUserID.String)
		require.Equal(t, ATEInitiatorDashboardSender, actorRow.InitiatingUserResolution)

		var binding KitWorkloadBinding
		require.NoError(t, json.Unmarshal(actorRow.WorkloadBindingJson, &binding))
		actorClaim, err := json.Marshal(map[string]string{
			"atespace":  atespace,
			"actorName": actorRow.ActorName,
			"actorUID":  actorRow.ActorUid,
		})
		require.NoError(t, err)

		res, err := NewATEWorkloadAuthorizer(db).Resolve(ctx, ATEWorkloadAuthorizerRequest{
			OrganizationID: "org-test",
			ProjectID:      runtimeSeed.ProjectID,
			IssuerID:       binding.IssuerID,
			Subject:        binding.Subject,
			Claims:         map[string]json.RawMessage{ateActorClaimKey: actorClaim},
		})
		require.NoError(t, err)
		require.True(t, res.Bound)
		require.Equal(t, userA, res.AuthorizerUserID)
		require.Equal(t, runtimeSeed.AssistantID, res.AssistantID)
		require.Equal(t, threadID, res.ThreadID)
		require.Equal(t, chatID, res.ChatID)
	}

	// User A starts the thread and turn.
	insertDashboardMsg(userA, "check deploy status")

	turnDone := make(chan error, 1)
	go func() {
		_, err := core.ProcessThreadEvents(ctx, runtimeSeed.ProjectID, threadID)
		turnDone <- err
	}()

	select {
	case <-firstPromptRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for User A turn to start")
	}

	// While User A's turn is running, User B sends a follow-up message on the same thread;
	// it must steer into the running turn without altering the stored initiating user or authorizer.
	insertDashboardMsg(userB, "also check staging logs")

	select {
	case <-secondPromptInserted:
	case <-ctx.Done():
		t.Fatal("timed out waiting for User B steer prompt to join running turn")
	}

	// Mid-turn check after User B steered into the running turn: actor still authorizes strictly as User A.
	assertAuthorizesAsUserA()

	close(releaseTurn)
	require.NoError(t, <-turnDone)

	mu.Lock()
	require.Equal(t, []string{kitPromptDeliverySteer, kitPromptDeliverySteer}, insertedDeliveries)
	require.Len(t, insertedPrompts, 2)
	require.Contains(t, insertedPrompts[0], "check deploy status")
	require.Contains(t, insertedPrompts[1], "also check staging logs")
	mu.Unlock()

	// Post-turn check: both events settled and requests from the thread actor still authorize as User A.
	busy, err := q.AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	require.True(t, busy.Valid)
	require.False(t, busy.Bool)

	assertAuthorizesAsUserA()
}
