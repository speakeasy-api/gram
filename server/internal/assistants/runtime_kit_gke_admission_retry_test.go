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
	attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		require.NotEmpty(t, persisted, "ownership must precede network readiness")
		cancel()
		return &kitGKEFailedStatusProtocol{}, nil
	}
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
	for _, failure := range []string{"none", "claim", "template", "secret", "Failed", "Succeeded", "missing-claim", "transient-api"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			var persisted []byte
			ctx := context.WithValue(t.Context(), kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(_ context.Context, raw []byte) error {
				persisted = append([]byte(nil), raw...)
				return nil
			}))
			attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
				require.NotEmpty(t, persisted, "ownership must precede pod readiness")
				cancel()
				return true, &unstructured.UnstructuredList{}, nil
			})
			_, err := k.Ensure(attempt, r)
			require.Error(t, err)
			require.Zero(t, *fences)
			var metadata kitGKEBackendMetadata
			require.NoError(t, json.Unmarshal(persisted, &metadata))
			require.True(t, metadata.Provisioning)
			require.NotEmpty(t, metadata.ClaimUID)
			client.ReactionChain = client.ReactionChain[1:]
			switch failure {
			case "claim", "template", "secret":
				gvr, name := gkeSandboxClaimGVR, kitGKEName(r)
				switch failure {
				case "template":
					gvr = kitTemplateGVR
				case "secret":
					gvr, name = kitSecretGVR, metadata.SecretName
				}
				resource := client.Resource(gvr).Namespace(k.config.GKE.Namespace)
				obj, err := resource.Get(ctx, name, metav1.GetOptions{})
				require.NoError(t, err)
				obj.SetUID("replacement")
				_, err = resource.Update(ctx, obj, metav1.UpdateOptions{})
				require.NoError(t, err)
			case "Failed", "Succeeded":
				resource := client.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace)
				pods, err := resource.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				require.Len(t, pods.Items, 1)
				pod := &pods.Items[0]
				require.NoError(t, unstructured.SetNestedField(pod.Object, failure, "status", "phase"))
				_, err = resource.Update(ctx, pod, metav1.UpdateOptions{})
				require.NoError(t, err)
			case "missing-claim":
				require.NoError(t, client.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).Delete(ctx, kitGKEName(r), metav1.DeleteOptions{}))
			case "transient-api":
				client.PrependReactor("get", "sandboxclaims", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("temporary API outage")
				})
			}
			r.BackendMetadataJSON = persisted
			_, err = k.Ensure(ctx, r)
			switch failure {
			case "none":
				require.NoError(t, err)
				require.Zero(t, *fences)
			case "transient-api":
				require.ErrorContains(t, err, "temporary API outage")
				require.Zero(t, *fences)
				client.ReactionChain = client.ReactionChain[1:]
				_, err = k.Ensure(ctx, r)
				require.NoError(t, err)
				require.Zero(t, *fences)
			default:
				require.Error(t, err)
				switch failure {
				case "Failed", "Succeeded":
					require.ErrorContains(t, err, "process exited")
				case "missing-claim":
					require.ErrorContains(t, err, "not found")
				default:
					require.ErrorContains(t, err, "authority mismatch")
				}
				require.Equal(t, 1, *fences, "terminal failures must revoke startup credentials")
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
