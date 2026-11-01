package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEFailedProjectedGenerationCleanup(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"replace", "retire", "mismatched UID", "fence failure", "delete retry", "unrecorded pod", "assigned claim"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			m := kitGKEBackendMetadata{
				RuntimeID: r.ID, AssistantID: r.AssistantID, ProjectID: r.ProjectID,
				Version: 3, Namespace: k.config.GKE.Namespace, Fingerprint: "old-projected-generation", Provisioning: true,
				ClaimUID: "old-claim", TemplateName: kitGKEName(r) + "-template", TemplateUID: "old-template",
				SecretName: kitGKEName(r) + "-secret", SecretUID: "old-secret", ServiceAccountUID: "old-ksa",
				Workload: &KitWorkloadBinding{IssuerID: uuid.New(), ServiceAccount: "kit-old-generation", Subject: "system:serviceaccount:" + k.config.GKE.Namespace + ":kit-old-generation"},
			}
			claim, template := kitClaimGVR, kitTemplateGVR
			claim.Version, template.Version = "v1alpha1", "v1alpha1"
			for _, step := range []struct {
				gvr        schema.GroupVersionResource
				kind, name string
				uid        types.UID
			}{
				{claim, "SandboxClaim", kitGKEName(r), m.ClaimUID},
				{template, "SandboxTemplate", m.TemplateName, m.TemplateUID},
				{kitSecretGVR, "Secret", m.SecretName, m.SecretUID},
				{kitServiceAccountGVR, "ServiceAccount", m.Workload.ServiceAccount, m.ServiceAccountUID},
			} {
				o := kitObject(step.kind, step.gvr.GroupVersion().String(), step.name, k.identity(r))
				labels := o.GetLabels()
				labels["gram.ai/project-id"] = r.ProjectID.String()
				o.SetLabels(labels)
				o.SetAnnotations(map[string]string{"speakeasy.com/kit-spec": m.Fingerprint})
				o.SetUID(step.uid)
				if mode == "assigned claim" && step.gvr == claim {
					o.Object["status"] = map[string]any{"sandbox": map[string]any{"name": "assigned-sandbox"}}
				}

				if mode == "mismatched UID" && step.gvr == claim {
					o.SetUID("replacement")
				}
				require.NoError(t, client.Tracker().Create(step.gvr, o, m.Namespace))
			}
			if mode == "unrecorded pod" {
				pod := kitObject("Pod", "v1", "unrecorded-pod", k.identity(r))
				pod.SetLabels(map[string]string{gkeClaimUIDLabel: string(m.ClaimUID)})
				require.NoError(t, client.Tracker().Create(gkePodGVR, pod, m.Namespace))
			}
			raw, err := json.Marshal(m)
			require.NoError(t, err)
			require.NoError(t, k.config.AuthorityStore.Save(t.Context(), r.LifecycleTx, r, r.BackendMetadataJSON, raw))
			r.BackendMetadataJSON = raw
			if mode == "fence failure" {
				k.config.Fence = func(context.Context, assistantRuntimeRecord) error { return errors.New("fence unavailable") }
			}
			if mode == "delete retry" {
				failed := false
				client.PrependReactor("delete", "secrets", func(ktesting.Action) (bool, runtime.Object, error) {
					if !failed {
						failed = true
						return true, nil, errors.New("delete unavailable")
					}
					return false, nil, nil
				})
				r.Retired = true
				require.ErrorContains(t, k.Reap(t.Context(), r), "delete unavailable")
				persisted, err := k.config.AuthorityStore.Load(t.Context(), r.LifecycleTx, r)
				require.NoError(t, err)
				require.JSONEq(t, string(raw), string(persisted))
			}
			if mode == "replace" {
				result, err := k.Ensure(t.Context(), r)
				require.NoError(t, err)
				require.True(t, result.ColdStart)
			} else {
				r.Retired = true
				err = k.Reap(t.Context(), r)
				if mode == "retire" || mode == "delete retry" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					for _, action := range client.Actions() {
						require.NotEqual(t, "delete", action.GetVerb())
					}
					return
				}
			}
			if r.Retired {
				// Simulate failed MarkReaped bookkeeping after successful cleanup.
				// Both a stale caller and a freshly loaded row must be retryable.
				client.ClearActions()
				require.NoError(t, k.Reap(t.Context(), r))
				r.BackendMetadataJSON = []byte("{}")
				require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
				require.Empty(t, client.Actions())
			}
			if mode == "delete retry" {
				require.Equal(t, 4, *fences)
			} else if r.Retired {
				require.Equal(t, 3, *fences)
			} else {
				require.Equal(t, 1, *fences)
			}
			_, err = client.Resource(claim).Namespace(m.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
			require.True(t, apierrors.IsNotFound(err))
			_, err = client.Resource(kitServiceAccountGVR).Namespace(m.Namespace).Get(t.Context(), m.Workload.ServiceAccount, metav1.GetOptions{})
			require.True(t, apierrors.IsNotFound(err))
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" && action.GetResource().Resource == "sandboxclaims" {
					require.Equal(t, "v1alpha1", action.GetResource().Version)
				}
			}
		})
	}
}

func TestKitGKEReapClearedAuthorityRequiresRetiredEmptyObject(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "null", "[]", "{", `{"unknown":true}`, `{"runtime_version":3}`, "{}"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			// Even an empty object must not bypass validation for a live row.
			r.Retired = raw != "{}"
			r.BackendMetadataJSON = []byte(raw)
			require.Error(t, k.Reap(t.Context(), r))
			require.Empty(t, client.Actions())
			require.Zero(t, *fences)
		})
	}
}
