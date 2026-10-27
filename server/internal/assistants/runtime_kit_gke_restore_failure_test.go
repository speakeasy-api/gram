package assistants

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKitGKEBackendFailedRestoreRevokesFreshAuthority(t *testing.T) {
	t.Parallel()
	for _, fenceFails := range []bool{false, true} {
		name := "successful fence"
		if fenceFails {
			name = "failed fence"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			k.config.Provision.SnapshotsEnabled = true
			k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			require.NoError(t, k.Stop(t.Context(), r))
			before, _, err := k.loadAuthority(t.Context(), r)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			restoreErr := errors.New("injected fresh gateway failure")
			fenceErr := errors.New("injected credential revocation failure")
			k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
				// The replacement Pod and bootstrap already exist at this point.
				cancel()
				return nil, restoreErr
			}
			k.config.Fence = func(cleanupCtx context.Context, _ assistantRuntimeRecord) error {
				*fences++
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.NoError(t, cleanupCtx.Err(), "revocation must survive caller cancellation")
				deadline, ok := cleanupCtx.Deadline()
				require.True(t, ok, "revocation must be bounded")
				require.Positive(t, time.Until(deadline))
				require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
				if fenceFails {
					return fenceErr
				}
				return nil
			}

			_, err = k.Ensure(ctx, r)
			require.ErrorIs(t, err, restoreErr)
			if fenceFails {
				require.ErrorIs(t, err, fenceErr)
			}
			require.Equal(t, 2, *fences, "fresh restore authority must be revoked after startup failure")
			state, _, err := k.loadAuthority(t.Context(), r)
			require.NoError(t, err)
			require.Equal(t, "restoring", state.Phase)
			require.NotEqual(t, before.Metadata.SecretName, state.Metadata.SecretName)
			pod, err := k.get(t.Context(), r, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID)
			require.NoError(t, err)
			phase, _, err := unstructured.NestedString(pod.Object, "status", "phase")
			require.NoError(t, err)
			require.Equal(t, "Running", phase)
			_, err = k.Status(t.Context(), r)
			require.ErrorContains(t, err, "fenced")
			_, err = k.Ensure(t.Context(), r)
			require.ErrorContains(t, err, "not safely restorable")
			require.ErrorContains(t, k.Stop(t.Context(), r), "cannot checkpoint")
			require.Equal(t, 2, *fences, "retry must not bootstrap or publish the failed attempt")
		})
	}
}
