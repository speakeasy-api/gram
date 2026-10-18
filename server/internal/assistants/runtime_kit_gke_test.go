package assistants

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func kitGKEInput() KitGKEProvisionInput {
	return KitGKEProvisionInput{
		Identity: KitGKEIdentity{Namespace: "kit-test", AssistantID: "assistant-test", Lineage: "lineage-one", CompatibilityGeneration: "generation-one"},
		Name:     "kit-test-runtime", Image: "example.invalid/kit@sha256:" + strings.Repeat("a", 64), IngressCIDR: "10.1.0.0/16", GuestPort: 8080,
		Command: []string{"/approved/kit-command"}, Args: []string{"caller-supplied-argument"},
		AgentsMD: []byte("caller instructions"), InferenceJSON: []byte(`{"caller":"inference"}`), MCPJSON: []byte(`{"caller":"mcp"}`), GatewayToken: []byte("test-token"),
		WorkSizeLimit: "1Gi", TmpSizeLimit: "128Mi",
		Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}},
	}
}
func kitPodTemplate(t *testing.T, objects KitGKEObjects) corev1.PodTemplateSpec {
	t.Helper()
	m, found, err := unstructured.NestedMap(objects.Template.Object, "spec", "podTemplate")
	require.NoError(t, err)
	require.True(t, found)
	var pod corev1.PodTemplateSpec
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(m, &pod))
	return pod
}
func TestKitGKERenderIsolationAndBootstrap(t *testing.T) {
	in := kitGKEInput()
	objects, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	var secret corev1.Secret
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(objects.Secret.Object, &secret))
	require.True(t, *secret.Immutable)
	require.Equal(t, map[string][]byte{"AGENTS.md": in.AgentsMD, "inference.json": in.InferenceJSON, "mcp.json": in.MCPJSON, "gateway-token": in.GatewayToken}, secret.Data)
	require.Equal(t, in.Identity.labels(), secret.Labels)
	pod := kitPodTemplate(t, objects)
	require.Equal(t, in.Identity.labels(), pod.Labels)
	require.NotContains(t, pod.Annotations, "podsnapshot.gke.io/ps-name")
	require.Equal(t, "false", pod.Annotations["cluster-autoscaler.kubernetes.io/safe-to-evict"])
	require.False(t, *pod.Spec.AutomountServiceAccountToken)
	require.Equal(t, "gvisor", *pod.Spec.RuntimeClassName)
	require.Equal(t, int64(1000), *pod.Spec.SecurityContext.RunAsUser)
	require.Equal(t, int64(1000), *pod.Spec.SecurityContext.RunAsGroup)
	require.Equal(t, int64(1000), *pod.Spec.SecurityContext.FSGroup)
	require.True(t, *pod.Spec.SecurityContext.RunAsNonRoot)
	require.Equal(t, corev1.DNSNone, pod.Spec.DNSPolicy)
	require.Equal(t, []string{"8.8.8.8", "1.1.1.1"}, pod.Spec.DNSConfig.Nameservers)
	require.Equal(t, map[string]string{"sandbox.gke.io/runtime": "gvisor", "workload-type": "sandbox"}, pod.Spec.NodeSelector)
	require.Len(t, pod.Spec.Tolerations, 2)
	require.Len(t, pod.Spec.InitContainers, 1)
	require.Len(t, pod.Spec.Containers, 1)
	init, main := pod.Spec.InitContainers[0], pod.Spec.Containers[0]
	require.Equal(t, in.Command, main.Command)
	require.Equal(t, in.Args, main.Args)
	require.Equal(t, []corev1.EnvVar{{Name: "HOME", Value: "/home/kit"}}, main.Env)
	require.Equal(t, "/home/kit", main.WorkingDir)
	require.Equal(t, []corev1.VolumeMount{{Name: "work", MountPath: "/home/kit"}, {Name: "tmp", MountPath: "/tmp"}}, main.VolumeMounts)
	require.Equal(t, corev1.VolumeMount{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true}, init.VolumeMounts[1])
	require.Equal(t, init.VolumeMounts[0], main.VolumeMounts[0])
	require.Contains(t, init.Args[0], "umask 077")
	require.Contains(t, init.Args[0], "chmod 600 /home/kit/.kit/AGENTS.md /home/kit/.kit/credentials/*")
	for _, container := range []corev1.Container{init, main} {
		require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
		require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	}
	require.Equal(t, corev1.StorageMediumMemory, pod.Spec.Volumes[0].EmptyDir.Medium)
	require.Equal(t, corev1.StorageMediumMemory, pod.Spec.Volumes[1].EmptyDir.Medium)
	require.Equal(t, secret.Name, pod.Spec.Volumes[2].Secret.SecretName)
	require.Equal(t, int32(0440), *pod.Spec.Volumes[2].Secret.DefaultMode)
	require.Len(t, pod.Spec.Volumes[2].Secret.Items, 4)
	policy, found, err := unstructured.NestedMap(objects.Template.Object, "spec", "networkPolicy")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, map[string]any{"ingress": []any{map[string]any{"from": []any{map[string]any{"ipBlock": map[string]any{"cidr": in.IngressCIDR}}}, "ports": []any{map[string]any{"protocol": "TCP", "port": int64(8080)}}}}, "egress": []any{map[string]any{"to": []any{map[string]any{"ipBlock": map[string]any{"cidr": "0.0.0.0/0", "except": []any{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}}}, map[string]any{"ipBlock": map[string]any{"cidr": "::/0", "except": []any{"fc00::/7", "fe80::/10"}}}}}}}, policy)
	same, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.Equal(t, secret.Name, same.Secret.GetName())
	in.GatewayToken = []byte("rotated-test-token")
	changed, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.NotEqual(t, secret.Name, changed.Secret.GetName())
	in = kitGKEInput()
	in.Identity.Lineage = "lineage-two"
	changed, err = RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.NotEqual(t, secret.Name, changed.Secret.GetName())
}
func TestKitGKEProvisionCreateOrderingAndFailure(t *testing.T) {
	for _, failResource := range []string{"", "secrets", "sandboxtemplates", "sandboxclaims"} {
		t.Run(failResource, func(t *testing.T) {
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			if failResource != "" {
				client.PrependReactor("create", failResource, func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("blocked") })
			}
			err := ProvisionKitGKE(t.Context(), client, kitGKEInput())
			expected := []string{"secrets", "sandboxtemplates", "sandboxclaims"}
			if failResource != "" {
				require.Error(t, err)
				for idx, name := range expected {
					if name == failResource {
						expected = expected[:idx+1]
						break
					}
				}
			} else {
				require.NoError(t, err)
			}
			actions := client.Actions()
			require.Len(t, actions, len(expected))
			for idx, action := range actions {
				require.Equal(t, "create", action.GetVerb())
				require.Equal(t, expected[idx], action.GetResource().Resource)
			}
		})
	}
}
func TestKitGKEProvisionValidation(t *testing.T) {
	cases := map[string]func(*KitGKEProvisionInput){
		"legacy":                       func(in *KitGKEProvisionInput) { in.Name = "gram-asst" },
		"missing command":              func(in *KitGKEProvisionInput) { in.Command = nil },
		"unspecified args":             func(in *KitGKEProvisionInput) { in.Args = nil },
		"invalid JSON":                 func(in *KitGKEProvisionInput) { in.MCPJSON = []byte("not JSON") },
		"invalid label":                func(in *KitGKEProvisionInput) { in.Identity.Lineage = "bad/value" },
		"missing token":                func(in *KitGKEProvisionInput) { in.GatewayToken = nil },
		"invalid CIDR":                 func(in *KitGKEProvisionInput) { in.IngressCIDR = "bad" },
		"missing volume limit":         func(in *KitGKEProvisionInput) { in.WorkSizeLimit = "" },
		"reset without prior identity": func(in *KitGKEProvisionInput) { in.ColdReset = true },
		"reset same lineage":           func(in *KitGKEProvisionInput) { in.ColdReset = true; old := in.Identity; in.PreviousIdentity = &old },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := kitGKEInput()
			change(&in)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			require.Error(t, ProvisionKitGKE(t.Context(), client, in))
			require.Empty(t, client.Actions())
		})
	}
	in := kitGKEInput()
	old := in.Identity
	in.PreviousIdentity = &old
	in.ColdReset = true
	in.Identity.Lineage = "new-lineage"
	_, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
}

type kitSnapshotVerifier func(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) error

func (f kitSnapshotVerifier) VerifyAvailable(ctx context.Context, u *unstructured.Unstructured, b KitGKESnapshotBinding) error {
	return f(ctx, u, b)
}
func TestKitGKESnapshotContracts(t *testing.T) {
	in := kitGKEInput()
	binding := KitGKESnapshotBinding{Identity: in.Identity, SourcePodName: "source-pod", SourcePodUID: "source-uid"}
	pod := kitObject("Pod", "v1", binding.SourcePodName, in.Identity)
	pod.SetUID(binding.SourcePodUID)
	labels := pod.GetLabels()
	labels["gram.ai/snapshots"] = "enabled"
	pod.SetLabels(labels)
	_, err := RenderKitGKEManualTrigger(false, "trigger", binding, pod)
	require.Error(t, err)
	trigger, err := RenderKitGKEManualTrigger(true, "trigger", binding, pod)
	require.NoError(t, err)
	require.Equal(t, "PodSnapshotManualTrigger", trigger.GetKind())
	require.Equal(t, "podsnapshot.gke.io/v1", trigger.GetAPIVersion())
	require.Equal(t, map[string]any{"targetPod": "source-pod"}, trigger.Object["spec"])
	pod.SetUID("replaced-uid")
	_, err = RenderKitGKEManualTrigger(true, "trigger", binding, pod)
	require.Error(t, err)
	selected := kitObject("PodSnapshot", "podsnapshot.gke.io/v1", "selected-snapshot", in.Identity)
	_, err = VerifyKitGKESnapshot(t.Context(), selected, binding, nil)
	require.Error(t, err)
	unavailable := kitSnapshotVerifier(func(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) error {
		return errors.New("not available")
	})
	_, err = VerifyKitGKESnapshot(t.Context(), selected, binding, unavailable)
	require.Error(t, err)
	verifier := kitSnapshotVerifier(func(_ context.Context, got *unstructured.Unstructured, b KitGKESnapshotBinding) error {
		require.Equal(t, binding, b)
		require.Equal(t, selected.GetName(), got.GetName())
		return nil
	})
	verified, err := VerifyKitGKESnapshot(t.Context(), selected, binding, verifier)
	require.NoError(t, err)
	require.Equal(t, selected.GetName(), verified.Name())
	in.Restore = verified
	in.RestoreSource = &binding
	_, err = RenderKitGKEProvision(in)
	require.Error(t, err)
	in.SnapshotsEnabled = true
	objects, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.Equal(t, selected.GetName(), kitPodTemplate(t, objects).Annotations["podsnapshot.gke.io/ps-name"])
	for _, field := range []string{"assistant", "namespace", "lineage", "generation", "uid", "pod"} {
		t.Run(field, func(t *testing.T) {
			bad := binding
			switch field {
			case "assistant":
				bad.Identity.AssistantID = "other"
			case "namespace":
				bad.Identity.Namespace = "other"
			case "lineage":
				bad.Identity.Lineage = "other"
			case "generation":
				bad.Identity.CompatibilityGeneration = "other"
			case "uid":
				bad.SourcePodUID = "other"
			case "pod":
				bad.SourcePodName = "other"
			}
			copied := in
			copied.RestoreSource = &bad
			_, err := RenderKitGKEProvision(copied)
			require.Error(t, err)
		})
	}
	for _, label := range []string{"gram.ai/assistant-id", "gram.ai/runtime-version", "gram.ai/runtime-lineage", "gram.ai/compatibility-generation"} {
		t.Run(label, func(t *testing.T) {
			bad := selected.DeepCopy()
			labels := bad.GetLabels()
			labels[label] = "other"
			bad.SetLabels(labels)
			_, err := VerifyKitGKESnapshot(t.Context(), bad, binding, verifier)
			require.Error(t, err)
		})
	}
	selected.SetNamespace("other")
	_, err = VerifyKitGKESnapshot(t.Context(), selected, binding, verifier)
	require.Error(t, err)
	// No restore annotation deliberately retains GKE auto-selection semantics.
	in.Restore = nil
	in.RestoreSource = nil
	objects, err = RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.NotContains(t, kitPodTemplate(t, objects).Annotations, "podsnapshot.gke.io/ps-name")
}

// Keep the test's API object construction exercised through the dynamic client.
func TestKitGKEProvisionDoesNotOverwriteExistingSecret(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	in := kitGKEInput()
	objects, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	_, err = client.Resource(kitSecretGVR).Namespace(in.Identity.Namespace).Create(t.Context(), objects.Secret, metav1.CreateOptions{})
	require.NoError(t, err)
	client.ClearActions()
	require.Error(t, ProvisionKitGKE(t.Context(), client, in))
	require.Len(t, client.Actions(), 1)
}

func TestKitGKERejectsUnboundedOrMutableProvisioning(t *testing.T) {
	cases := map[string]func(*KitGKEProvisionInput){
		"legacy prefix":    func(in *KitGKEProvisionInput) { in.Name = "gram-asst-kit" },
		"mutable image":    func(in *KitGKEProvisionInput) { in.Image = "example.invalid/kit:latest" },
		"missing image":    func(in *KitGKEProvisionInput) { in.Image = "" },
		"short digest":     func(in *KitGKEProvisionInput) { in.Image = "example.invalid/kit@sha256:abcd" },
		"uppercase digest": func(in *KitGKEProvisionInput) { in.Image = "example.invalid/kit@sha256:" + strings.Repeat("A", 64) },
		"nonhex digest":    func(in *KitGKEProvisionInput) { in.Image = "example.invalid/kit@sha256:" + strings.Repeat("z", 64) },
		"empty resources":  func(in *KitGKEProvisionInput) { in.Resources = corev1.ResourceRequirements{} },
	}
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		cases[string(name)+" missing limit"] = func(in *KitGKEProvisionInput) { delete(in.Resources.Limits, name) }
		cases[string(name)+" missing request"] = func(in *KitGKEProvisionInput) { delete(in.Resources.Requests, name) }
		cases[string(name)+" zero limit"] = func(in *KitGKEProvisionInput) { in.Resources.Limits[name] = resource.MustParse("0") }
		cases[string(name)+" negative request"] = func(in *KitGKEProvisionInput) { in.Resources.Requests[name] = resource.MustParse("-1") }
		cases[string(name)+" oversized request"] = func(in *KitGKEProvisionInput) {
			q := in.Resources.Limits[name]
			q.Add(resource.MustParse("1"))
			in.Resources.Requests[name] = q
		}
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := kitGKEInput()
			change(&in)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			require.Error(t, ProvisionKitGKE(t.Context(), client, in))
			require.Empty(t, client.Actions())
		})
	}
}
