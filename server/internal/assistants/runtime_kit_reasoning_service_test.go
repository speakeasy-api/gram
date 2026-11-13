package assistants

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestKitReasoningCreateRejectsNoneWithoutChangingConfiguration(t *testing.T) {
	t.Parallel()
	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "kit_reasoning_create")
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	svc.core.newAssistantRuntimeVersion = 3
	_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Unsupported", Model: "test-model", ReasoningEffort: "none"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
	require.NoError(t, err)
	require.Empty(t, listed.Assistants)
	require.EqualValues(t, 3, svc.core.newAssistantRuntimeVersion)
	// Off remains supported by the default runtime, including explicit creation.
	svc.core.newAssistantRuntimeVersion = 2
	created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Supported", Model: "test-model", ReasoningEffort: "none", Status: new(StatusPaused)})
	require.NoError(t, err)
	require.Equal(t, "none", created.ReasoningEffort)
	require.Equal(t, 2, created.RuntimeVersion)
}

func TestKitReasoningRejectedUpdatesPreserveRuntimeAuthority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		initialVersion  int16
		initialEffort   string
		switchVersion   *int
		expectedVersion *int
		effort          *string
	}{
		{"update", 3, "medium", nil, nil, new("none")},
		{"switch persisted off", 2, "none", new(3), new(2), nil},
		{"switch simultaneous off", 2, "high", new(3), new(2), new("none")},
		{"same runtime off", 3, "medium", new(3), new(3), new("none")},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, ctx, projectID, conn := newRBACServiceWithConn(t, fmt.Sprintf("kit_reasoning_reject_%d", i))
			ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
			svc.core.newAssistantRuntimeVersion = tc.initialVersion
			created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Original", Model: "test-model", ReasoningEffort: tc.initialEffort, Status: new(StatusPaused)})
			require.NoError(t, err)
			assistantID, chatID := uuid.MustParse(created.ID), uuid.New()
			q := assistantrepo.New(conn)
			require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: projectID, OrganizationID: "org-test", Title: pgtype.Text{}}))
			threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: assistantID, ProjectID: projectID, CorrelationID: "reasoning", ChatID: chatID, SourceKind: sourceKindSlack, SourceRefJson: []byte(`{}`)})
			require.NoError(t, err)
			require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{AssistantID: assistantID, AssistantThreadID: threadID, ProjectID: projectID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
			before, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID})
			require.NoError(t, err)
			backend := &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}
			svc.core.runtime = backend
			_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, Name: new("Changed"), Model: new("changed-model"), RuntimeVersion: tc.switchVersion, ExpectedRuntimeVersion: tc.expectedVersion, ReasoningEffort: tc.effort})
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.Zero(t, backend.preparations, "unsupported settings must fail before runtime fencing or retirement")
			require.Zero(t, backend.stopCount)
			after, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID})
			require.NoError(t, err)
			require.Equal(t, before, after)
			current, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: created.ID})
			require.NoError(t, err)
			require.Equal(t, created.RuntimeVersion, current.RuntimeVersion)
			require.Equal(t, created.ReasoningEffort, current.ReasoningEffort)
			require.Equal(t, created.Model, current.Model)
			require.Equal(t, created.Name, current.Name)
		})
	}
}

func TestKitReasoningSwitchToV2AllowsNone(t *testing.T) {
	t.Parallel()
	svc, ctx, projectID := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	svc.core.newAssistantRuntimeVersion = 3
	created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Reasoning", Model: "test-model", ReasoningEffort: "medium", Status: new(StatusPaused)})
	require.NoError(t, err)
	svc.core.runtime = &switchBackend{stubRuntimeBackend: &stubRuntimeBackend{}, readiness: runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}}
	updated, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, RuntimeVersion: new(2), ExpectedRuntimeVersion: new(3), ReasoningEffort: new("none")})
	require.NoError(t, err)
	require.Equal(t, 2, updated.RuntimeVersion)
	require.Equal(t, "none", updated.ReasoningEffort)
	// A simultaneous switch and supported setting uses the effective new
	// configuration, not the v2 assistant's previously persisted Off value.
	updated, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, RuntimeVersion: new(3), ExpectedRuntimeVersion: new(2), ReasoningEffort: new("high")})
	require.NoError(t, err)
	require.Equal(t, 3, updated.RuntimeVersion)
	require.Equal(t, "high", updated.ReasoningEffort)
}

func TestKitReasoningRuntimeValidation(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"none", "low", "medium", "high"} {
		require.NoError(t, validateRuntimeReasoningEffort(2, effort))
		if effort == "none" {
			require.ErrorIs(t, validateRuntimeReasoningEffort(3, effort), errAssistantValidation)
		} else {
			require.NoError(t, validateRuntimeReasoningEffort(3, effort))
		}
	}
}
