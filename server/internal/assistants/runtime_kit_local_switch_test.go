package assistants

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeKitLocalFenceProtocol struct {
	fakeKitLocalProtocol
	prepare func(context.Context, assistantRecord, []assistantRuntimeRecord, int16) (runtimeSwitchReadiness, error)
}

func (p *fakeKitLocalFenceProtocol) PrepareRuntimeSwitch(ctx context.Context, a assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	return p.prepare(ctx, a, rows, destination)
}

type kitLocalMissingImageEngine struct{ *fakeKitLocalEngine }

func (e *kitLocalMissingImageEngine) ImageID(context.Context, string) (string, error) {
	return "", errLocalImageNotFound
}

func TestKitLocalSwitchEmptyRequiresImageReadiness(t *testing.T) {
	t.Parallel()
	k, engine, r, _ := kitLocalFixture(t)
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	result, err := k.PrepareRuntimeSwitch(t.Context(), a, nil, 3)
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.True(t, result.DestinationReady)
	require.NotNil(t, result.Release)
	result.Release()
	require.Empty(t, engine.calls)
	_, err = k.PrepareRuntimeSwitch(t.Context(), a, nil, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	k.engine = &kitLocalMissingImageEngine{engine}
	require.ErrorIs(t, k.RuntimeReadyForAdmission(t.Context()), errRuntimeSwitchUnavailable)
	result, err = k.PrepareRuntimeSwitch(t.Context(), a, nil, 3)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	require.False(t, result.Settled)
	require.Empty(t, engine.calls)
}
func TestKitLocalSwitchNeverUsesForegroundIdle(t *testing.T) {
	t.Parallel()
	k, _, r, p := kitLocalFixture(t)
	r.State = runtimeStateActive
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON
	result, err := k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	require.False(t, result.Settled)
	require.Zero(t, p.statuses)
	require.NotNil(t, result.Release)
}
func TestKitLocalSwitchDelegatesFenceAndReturnsPartialCleanup(t *testing.T) {
	t.Parallel()
	k, _, r, _ := kitLocalFixture(t)
	r.State = runtimeStateActive
	a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
	ensured, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = ensured.BackendMetadataJSON
	calls := 0
	released := []int{}
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &fakeKitLocalFenceProtocol{prepare: func(_ context.Context, got assistantRecord, rows []assistantRuntimeRecord, dest int16) (runtimeSwitchReadiness, error) {
			require.Equal(t, a.ID, got.ID)
			require.Len(t, rows, 1)
			require.Equal(t, int16(2), dest)
			calls++
			index := calls
			fence := runtimeSwitchReadiness{Settled: true, Release: func() { released = append(released, index) }}
			if calls == 2 {
				return fence, errors.New("fence failed")
			}
			return fence, nil
		}}, nil
	}
	// Two independent delegate acquisitions exercise cleanup of both successful
	// and partially acquired fences without inventing a guest wire method.
	result, err := k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r, r}, 2)
	require.ErrorContains(t, err, "fence failed")
	require.False(t, result.Settled)
	require.Empty(t, released)
	result.Release()
	result.Release()
	require.Equal(t, []int{2, 1}, released)
}
func TestKitLocalSwitchChecksStoppedContainerAuthority(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "stale-stopped-row"}[running], func(t *testing.T) {
			t.Parallel()
			k, engine, r, _ := kitLocalFixture(t)
			a := assistantRecord{ID: r.AssistantID, ProjectID: r.ProjectID}
			ensured, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = ensured.BackendMetadataJSON
			r.State = runtimeStateStopped
			engine.info.Running = running
			result, err := k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
			require.False(t, result.Settled)
			for _, state := range []string{runtimeStateStopped, runtimeStateFailed} {
				r.State = state
				engine.info.Running = false
				result, err = k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
				require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
				require.False(t, result.Settled, "process exit does not settle session history")
			}
			engine.info.Labels["gram.ai/assistant-id"] = uuid.NewString()
			_, err = k.PrepareRuntimeSwitch(t.Context(), a, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
		})
	}
}
