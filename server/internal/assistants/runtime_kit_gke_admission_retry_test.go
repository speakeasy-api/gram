package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEColdAdmissionRetry(t *testing.T) {
	t.Parallel()
	k, _, r, fences := kitGKEBackendFixture(t)
	var persisted []byte
	ctx := context.WithValue(t.Context(), kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(_ context.Context, raw []byte) error {
		persisted = append([]byte(nil), raw...)
		return nil
	}))
	bootstrap := k.config.Bootstrap
	bootstraps := 0
	k.config.Bootstrap = func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
		bootstraps++
		return bootstrap(ctx, r)
	}
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		require.NotEmpty(t, persisted, "ownership must precede network readiness")
		return &kitGKEFailedStatusProtocol{}, nil
	}
	attempt, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	_, err := k.Ensure(attempt, r)
	require.ErrorContains(t, err, "wait for authenticated Kit gateway")
	require.Zero(t, *fences, "resumable readiness failure retains the owned process credentials")
	var metadata kitGKEBackendMetadata
	require.NoError(t, json.Unmarshal(persisted, &metadata))
	require.NotEmpty(t, metadata.AuthorityUID)
	require.False(t, metadata.Provisioning)
	r.BackendMetadataJSON = persisted
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &kitGKEBackendProtocol{settled: true}, nil
	}
	result, err := k.Ensure(ctx, r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.Equal(t, 1, bootstraps)
	require.JSONEq(t, string(persisted), string(result.BackendMetadataJSON))
}

func TestKitGKEColdAdmissionPodWaitRetryRejectsReplacement(t *testing.T) {
	t.Parallel()
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprint(replaced), func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			var persisted []byte
			ctx := context.WithValue(t.Context(), kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(_ context.Context, raw []byte) error {
				persisted = append([]byte(nil), raw...)
				return nil
			}))
			client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, &unstructured.UnstructuredList{}, nil
			})
			attempt, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
			defer cancel()
			_, err := k.Ensure(attempt, r)
			require.Error(t, err)
			require.Zero(t, *fences)
			var metadata kitGKEBackendMetadata
			require.NoError(t, json.Unmarshal(persisted, &metadata))
			require.True(t, metadata.Provisioning)
			require.NotEmpty(t, metadata.ClaimUID)
			client.ReactionChain = client.ReactionChain[1:]
			if replaced {
				resource := client.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace)
				claim, err := resource.Get(ctx, kitGKEName(r), metav1.GetOptions{})
				require.NoError(t, err)
				claim.SetUID("replacement")
				_, err = resource.Update(ctx, claim, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			r.BackendMetadataJSON = persisted
			_, err = k.Ensure(ctx, r)
			if replaced {
				require.ErrorContains(t, err, "authority mismatch")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestKitGKEColdAdmissionUnresumableFailureFences(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"bootstrap", "create", "persist", "authority-persist"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			ctx := t.Context()
			switch failure {
			case "bootstrap":
				k.config.Bootstrap = func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error) {
					return KitLocalBootstrap{}, fmt.Errorf("bootstrap failed after mint")
				}
			case "create":
				client.PrependReactor("create", "sandboxtemplates", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("template write failed")
				})
			case "persist":
				ctx = context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(context.Context, []byte) error { return fmt.Errorf("DB unavailable") }))
			case "authority-persist":
				writes := 0
				ctx = context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(context.Context, []byte) error {
					writes++
					if writes == 2 {
						return fmt.Errorf("authority UID persistence failed")
					}
					return nil
				}))
			}
			_, err := k.Ensure(ctx, r)
			require.Error(t, err)
			require.Equal(t, 1, *fences)
		})
	}
}

func TestKitGKEColdAdmissionRetriesReadinessWithinAttempt(t *testing.T) {
	t.Parallel()
	k, _, r, _ := kitGKEBackendFixture(t)
	calls := 0
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		calls++
		if calls == 1 {
			return &kitGKEFailedStatusProtocol{}, nil
		}
		return &kitGKEBackendProtocol{settled: true}, nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := k.Ensure(ctx, r)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
