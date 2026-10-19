package assistants

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

//nolint:exhaustive // Container resource budgets intentionally specify only CPU and memory.
func kitGKEInput() KitGKEProvisionInput {
	cert, key, err := newKitGKETLSCertificate("kit-test-runtime.kit.internal")
	if err != nil {
		panic(err)
	}
	return KitGKEProvisionInput{
		TLSCertificate: cert, TLSPrivateKey: key,
		CheckpointMarker: []byte("11111111-1111-4111-8111-111111111111"),
		Identity:         KitGKEIdentity{Namespace: "kit-test", AssistantID: "assistant-test", Lineage: "lineage-one", CompatibilityGeneration: "generation-one"},
		Name:             "kit-test-runtime", Image: "example.invalid/kit@sha256:" + strings.Repeat("a", 64), IngressCIDR: "10.1.0.0/16", GuestPort: 8080,
		Command: []string{"/usr/local/bin/kit"}, Args: func() []string { _, args := KitGatewayCommand(8080); return args }(),
		AgentsMD: []byte("caller instructions"), InferenceJSON: []byte(`{"api_key":"gram_local_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","project":"test-project","email":null,"organization_id":null}`), MCPJSON: []byte(`{"mcpServers":{}}`), GatewayToken: []byte("test-token"),
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
	t.Parallel()
	in := kitGKEInput()
	in.Model = "openai/gpt-4.1-mini"
	objects, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	var secret corev1.Secret
	require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(objects.Secret.Object, &secret))
	require.True(t, *secret.Immutable)
	require.Equal(t, map[string][]byte{"AGENTS.md": in.AgentsMD, "config.toml": []byte(kitFileConfig + "model = \"openai/gpt-4.1-mini\"\n"), "speakeasy.json": in.InferenceJSON, "mcp.json": in.MCPJSON, "gateway-token": in.GatewayToken, "checkpoint-marker": in.CheckpointMarker, "tls.crt": in.TLSCertificate, "tls.key": in.TLSPrivateKey, "nginx.conf": []byte(kitGKETLSProxyConfig(in.GuestPort))}, secret.Data)
	require.Equal(t, in.Identity.labels(), secret.Labels)
	pod := kitPodTemplate(t, objects)
	foundBootstrap := false
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "bootstrap" {
			foundBootstrap = true
			require.NotNil(t, volume.Secret)
			require.Contains(t, volume.Secret.Items, corev1.KeyToPath{Key: "speakeasy.json", Path: "credentials/" + KitInferenceCredentialFilename})
		}
	}
	require.True(t, foundBootstrap, "bootstrap volume must exist")
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
	require.Empty(t, pod.Spec.InitContainers)
	require.Len(t, pod.Spec.Containers, 2)
	main := pod.Spec.Containers[0]
	require.Equal(t, []string{"/bin/sh", "-ec"}, main.Command)
	require.Equal(t, append(append([]string{}, in.Command...), "gateway", "--listen", "127.0.0.1:17766", "--credential-file", "/home/kit/.kit/credentials/gateway-token", "--project", KitWorkspacePath), main.Args[2:])
	require.Equal(t, kitBootstrapScript+"exec \"$@\"\n", main.Args[0])
	require.Equal(t, []corev1.EnvVar{{Name: "HOME", Value: "/home/kit"}, {Name: "SPEAKEASY_ORIGIN", Value: "https://ai.speakeasy.com"}}, main.Env)
	require.Equal(t, "/home/kit", main.WorkingDir)
	require.Equal(t, []corev1.VolumeMount{{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}}, main.VolumeMounts)
	proxy := pod.Spec.Containers[1]
	require.Equal(t, "acp-tls", proxy.Name)
	require.Equal(t, kitGKETLSProxyImage, proxy.Image)
	require.Equal(t, []string{"nginx"}, proxy.Command)
	require.Equal(t, []string{"-c", "/tls/nginx.conf", "-g", "daemon off;"}, proxy.Args)
	require.Equal(t, []corev1.ContainerPort{{ContainerPort: in.GuestPort}}, proxy.Ports)
	require.Equal(t, []corev1.VolumeMount{{Name: "tls", MountPath: "/tls", ReadOnly: true}, {Name: "tls-tmp", MountPath: "/tmp"}}, proxy.VolumeMounts)
	for _, directive := range []string{
		"access_log off;", "proxy_buffering off;", "proxy_request_buffering off;",
		"proxy_cache off;", "proxy_next_upstream off;", "proxy_http_version 1.1;",
		"proxy_pass http://127.0.0.1:17766;", "listen 8080 ssl;",
		"ssl_certificate /tls/tls.crt;", "ssl_certificate_key /tls/tls.key;",
	} {
		require.Contains(t, string(secret.Data["nginx.conf"]), directive)
	}
	for _, container := range pod.Spec.Containers {
		require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
		require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
	}
	require.Equal(t, corev1.StorageMediumMemory, pod.Spec.Volumes[0].EmptyDir.Medium)
	require.Equal(t, secret.Name, pod.Spec.Volumes[1].Secret.SecretName)
	require.Len(t, pod.Spec.Volumes, 4)
	require.Equal(t, "tls-tmp", pod.Spec.Volumes[2].Name)
	require.Equal(t, corev1.StorageMediumMemory, pod.Spec.Volumes[2].EmptyDir.Medium)
	require.Equal(t, "tls", pod.Spec.Volumes[3].Name)
	require.Equal(t, secret.Name, pod.Spec.Volumes[3].Secret.SecretName)
	require.ElementsMatch(t, []corev1.KeyToPath{{Key: "tls.crt", Path: "tls.crt"}, {Key: "tls.key", Path: "tls.key"}, {Key: "nginx.conf", Path: "nginx.conf"}}, pod.Spec.Volumes[3].Secret.Items)
	require.Equal(t, resource.MustParse(in.WorkSizeLimit), main.Resources.Limits[corev1.ResourceEphemeralStorage])
	require.Equal(t, int32(0440), *pod.Spec.Volumes[1].Secret.DefaultMode)
	require.Len(t, pod.Spec.Volumes[1].Secret.Items, 6)
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
	t.Parallel()
	for _, failResource := range []string{"", "secrets", "sandboxtemplates", "sandboxclaims"} {
		t.Run(failResource, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	cases := map[string]func(*KitGKEProvisionInput){
		"missing TLS key":              func(in *KitGKEProvisionInput) { in.TLSPrivateKey = nil },
		"missing TLS certificate":      func(in *KitGKEProvisionInput) { in.TLSCertificate = nil },
		"wrong TLS identity":           func(in *KitGKEProvisionInput) { in.Name = "another-runtime" },
		"reserved plaintext port":      func(in *KitGKEProvisionInput) { in.GuestPort = kitGKEPlaintextPort },
		"legacy":                       func(in *KitGKEProvisionInput) { in.Name = "gram-asst" },
		"missing command":              func(in *KitGKEProvisionInput) { in.Command = nil },
		"unspecified args":             func(in *KitGKEProvisionInput) { in.Args = nil },
		"invalid JSON":                 func(in *KitGKEProvisionInput) { in.MCPJSON = []byte("not JSON") },
		"invalid label":                func(in *KitGKEProvisionInput) { in.Identity.Lineage = "bad/value" },
		"missing marker":               func(in *KitGKEProvisionInput) { in.CheckpointMarker = nil },
		"invalid marker":               func(in *KitGKEProvisionInput) { in.CheckpointMarker = []byte("invalid") },
		"unexpected restore marker":    func(in *KitGKEProvisionInput) { in.ExpectedCheckpointMarker = in.CheckpointMarker },
		"missing token":                func(in *KitGKEProvisionInput) { in.GatewayToken = nil },
		"invalid CIDR":                 func(in *KitGKEProvisionInput) { in.IngressCIDR = "bad" },
		"missing volume limit":         func(in *KitGKEProvisionInput) { in.WorkSizeLimit = "" },
		"reset without prior identity": func(in *KitGKEProvisionInput) { in.ColdReset = true },
		"reset same lineage":           func(in *KitGKEProvisionInput) { in.ColdReset = true; old := in.Identity; in.PreviousIdentity = &old },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
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
	selected.SetUID("snapshot-uid")
	selected.SetUID("snapshot-uid")
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
	in.ExpectedCheckpointMarker = []byte("22222222-2222-4222-8222-222222222222")
	in.Restore = verified
	in.RestoreSource = &binding
	_, err = RenderKitGKEProvision(in)
	require.Error(t, err)
	in.SnapshotsEnabled = true
	objects, err := RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.Equal(t, selected.GetName(), kitPodTemplate(t, objects).Annotations["podsnapshot.gke.io/ps-name"])
	restoreInput := in
	for _, field := range []string{"assistant", "namespace", "lineage", "generation", "uid", "pod"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
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
			copied := restoreInput
			copied.RestoreSource = &bad
			_, err := RenderKitGKEProvision(copied)
			require.Error(t, err)
		})
	}
	// Controller snapshots need not inherit custom source labels.
	selected.SetLabels(nil)
	_, err = VerifyKitGKESnapshot(t.Context(), selected, binding, verifier)
	require.NoError(t, err)
	selected.SetNamespace("other")
	_, err = VerifyKitGKESnapshot(t.Context(), selected, binding, verifier)
	require.Error(t, err)
	// No restore annotation deliberately retains GKE auto-selection semantics.
	in.ExpectedCheckpointMarker = nil
	in.Restore = nil
	in.RestoreSource = nil
	objects, err = RenderKitGKEProvision(in)
	require.NoError(t, err)
	require.NotContains(t, kitPodTemplate(t, objects).Annotations, "podsnapshot.gke.io/ps-name")
}

// Keep the test's API object construction exercised through the dynamic client.
func TestKitGKEProvisionDoesNotOverwriteExistingSecret(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
			t.Parallel()
			in := kitGKEInput()
			change(&in)
			client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			require.Error(t, ProvisionKitGKE(t.Context(), client, in))
			require.Empty(t, client.Actions())
		})
	}
}

func TestKitGKEAllowsExplicitReleaseTags(t *testing.T) {
	t.Parallel()
	for _, image := range []string{KitIntegrationImage, "registry.invalid:5000/kit:0.2.17"} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			in := kitGKEInput()
			in.Image = image
			objects, err := RenderKitGKEProvision(in)
			require.NoError(t, err)
			pod := kitPodTemplate(t, objects)
			require.Equal(t, image, pod.Spec.Containers[0].Image)
			require.Equal(t, in.Identity.CompatibilityGeneration, pod.Labels["gram.ai/compatibility-generation"])
		})
	}
	for _, image := range []string{"kit", "registry.invalid:5000/kit", "kit:latest", "kit:LATEST", "kit:", "kit:bad tag"} {
		require.False(t, validKitGKEImage(image), image)
	}
}

func TestKitGKEBootstrapClearsRetiredCredentials(t *testing.T) {
	t.Parallel()
	clearIndex := strings.Index(kitBootstrapScript, "rm -rf /home/kit/.kit/credentials")
	create := strings.Index(kitBootstrapScript, "mkdir -p /home/kit/.kit/credentials")
	copyIndex := strings.Index(kitBootstrapScript, "cp -R /bootstrap/credentials/.")
	require.NotEqual(t, -1, clearIndex)
	require.Less(t, clearIndex, create)
	require.Less(t, create, copyIndex)
}

func TestKitGKEBootstrapReplacesCredentialDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bootstrap := filepath.Join(root, "bootstrap")
	credentials := filepath.Join(home, ".kit", "credentials")
	staleTLS := filepath.Join(home, ".kit", "tls")
	require.NoError(t, os.MkdirAll(staleTLS, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(staleTLS, "tls.key"), []byte("retired"), 0600))
	require.NoError(t, os.MkdirAll(credentials, 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(bootstrap, "credentials"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(credentials, "retired-key.pem"), []byte("retired"), 0600))
	for _, file := range []string{"AGENTS.md", "config.toml", "credentials/gateway-token", "checkpoint-marker"} {
		require.NoError(t, os.WriteFile(filepath.Join(bootstrap, file), []byte("fresh"), 0600))
	}
	script := strings.NewReplacer("/home/kit", home, "/bootstrap", bootstrap).Replace(kitBootstrapScript)
	output, err := exec.CommandContext(t.Context(), "/bin/sh", "-ec", script).CombinedOutput()
	require.NoError(t, err, string(output))
	_, err = os.Stat(staleTLS)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(credentials, "retired-key.pem"))
	require.ErrorIs(t, err, os.ErrNotExist)
	fresh, err := os.ReadFile(filepath.Join(credentials, "gateway-token"))
	require.NoError(t, err)
	require.Equal(t, "fresh", string(fresh))
	info, err := os.Stat(filepath.Join(credentials, "gateway-token"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// Run the actual guest shell program, not a replica of its marker semantics.
func TestKitGKEBootstrapRestoreMarkerGate(t *testing.T) {
	t.Parallel()
	for _, saved := range []string{"", "wrong", "expected"} {
		t.Run(saved, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			home, bootstrap := filepath.Join(root, "home"), filepath.Join(root, "bootstrap")
			state := filepath.Join(home, ".kit")
			require.NoError(t, os.MkdirAll(filepath.Join(state, "credentials"), 0700))
			require.NoError(t, os.MkdirAll(filepath.Join(bootstrap, "credentials"), 0700))
			for name, content := range map[string]string{"AGENTS.md": "fresh", "config.toml": "fresh", "credentials/gateway-token": "fresh", "expected-checkpoint-marker": "expected", "checkpoint-marker": "next"} {
				require.NoError(t, os.WriteFile(filepath.Join(bootstrap, name), []byte(content), 0600))
			}
			require.NoError(t, os.WriteFile(filepath.Join(state, "credentials", "gateway-token"), []byte("stale"), 0600))
			require.NoError(t, os.WriteFile(filepath.Join(state, "session.json"), []byte("durable session"), 0600))
			if saved != "" {
				require.NoError(t, os.WriteFile(filepath.Join(state, "checkpoint-marker"), []byte(saved), 0600))
			}
			script := strings.NewReplacer("/home/kit", home, "/bootstrap", bootstrap).Replace(kitBootstrapScript)
			output, err := exec.CommandContext(t.Context(), "/bin/sh", "-ec", script).CombinedOutput()
			if saved != "expected" {
				require.Error(t, err, string(output))
				credential, readErr := os.ReadFile(filepath.Join(state, "credentials", "gateway-token"))
				require.NoError(t, readErr)
				require.Equal(t, "stale", string(credential), "failed restore must not install fresh authority")
			} else {
				require.NoError(t, err, string(output))
				marker, err := os.ReadFile(filepath.Join(state, "checkpoint-marker"))
				require.NoError(t, err)
				require.Equal(t, "next", string(marker))
				credential, err := os.ReadFile(filepath.Join(state, "credentials", "gateway-token"))
				require.NoError(t, err)
				require.Equal(t, "fresh", string(credential))
				// Restarting the already-validated rootfs still refreshes credentials.
				require.NoError(t, os.WriteFile(filepath.Join(state, "credentials", "gateway-token"), []byte("stale again"), 0600))
				output, err = exec.CommandContext(t.Context(), "/bin/sh", "-ec", script).CombinedOutput()
				require.NoError(t, err, string(output))
				credential, err = os.ReadFile(filepath.Join(state, "credentials", "gateway-token"))
				require.NoError(t, err)
				require.Equal(t, "fresh", string(credential))
			}
			session, err := os.ReadFile(filepath.Join(state, "session.json"))
			require.NoError(t, err)
			require.Equal(t, "durable session", string(session))
		})
	}
}
