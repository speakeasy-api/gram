package assistants

import (
	"context"
	"errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

type readyKitBackend struct{ *stubRuntimeBackend }

func (*readyKitBackend) RuntimeReadyForAdmission(context.Context) error { return nil }

func TestVersionedRuntimeRoutesImmutableBinding(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	kit := &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendLocal}}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: kit}
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 2
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{}))
	record.RuntimeVersion = 3
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{}))
	require.Equal(t, 1, legacy.runTurnCount)
	require.Equal(t, 1, kit.runTurnCount)
	record.RuntimeVersion = 4
	require.ErrorContains(t, backend.RunTurn(t.Context(), record, runTurnRequest{}), "unsupported assistant runtime version")
	require.Equal(t, 1, legacy.runTurnCount)
}

func TestVersionedRuntimeSwitchRequiresKitSettlementCapability(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	kit := &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendLocal}}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: kit}
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 3
	_, err := backend.PrepareRuntimeSwitch(t.Context(), assistantRecord{}, []assistantRuntimeRecord{record}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	record.RuntimeVersion = 2
	ready, err := backend.PrepareRuntimeSwitch(t.Context(), assistantRecord{}, []assistantRuntimeRecord{record}, 3)
	require.NoError(t, err)
	require.True(t, ready.DestinationReady)
	require.True(t, ready.Settled)
	require.NotNil(t, ready.Release)
	ready.Release()
}

func TestVersionedRuntimeNeverFallsBackForUnconfiguredKit(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: nil}
	_, err := backend.BackendForVersion(3)
	require.ErrorContains(t, err, "version 3 is not configured")
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 3
	require.Error(t, backend.Stop(t.Context(), record))
	require.Zero(t, legacy.stopCount)
}

func TestVersionedRuntimeSwitchProductionKit(t *testing.T) {
	t.Parallel()
	k, provisionClient, row, _ := kitGKEBackendFixture(t)
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gkeSandboxClaimGVR: "SandboxClaimList"})
	k.config.GKE.Dynamic = client
	// Exercise the concrete backend's control-plane readiness without creating a
	// guest. The fake API supplies the same namespaced list response as GKE.
	client.PrependReactor("list", "sandboxclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, &unstructured.UnstructuredList{}, nil
	})
	backend := &versionedRuntimeBackend{legacy: &stubRuntimeBackend{name: runtimeBackendGKE}, kit: k}
	assistant := assistantRecord{ID: row.AssistantID, ProjectID: row.ProjectID}
	ready, err := backend.PrepareRuntimeSwitch(t.Context(), assistant, nil, 3)
	require.NoError(t, err)
	require.True(t, ready.DestinationReady)
	require.True(t, ready.Settled)
	ready.Release()
	k.config.GKE.Dynamic = provisionClient
	result, err := k.Ensure(t.Context(), row)
	require.NoError(t, err)
	row.BackendMetadataJSON = result.BackendMetadataJSON
	row.State = runtimeStateActive
	ready, err = backend.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{row}, 2)
	require.NoError(t, err)
	require.True(t, ready.Settled)
	ready.Release()
	ready.Release()
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &kitGKEBackendProtocol{settled: false}, nil
	}
	_, err = backend.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{row}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchBusy)
	k.config.GKE.Dynamic = client
	client.PrependReactor("list", "sandboxclaims", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("control plane unavailable")
	})
	_, err = backend.PrepareRuntimeSwitch(t.Context(), assistant, nil, 3)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
}
