package assistants

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/stretchr/testify/require"
)

// The GKE sandbox-core-policy prohibits these projections even when the
// hardening policy is customizable. Keep the rendered workload compatible
// with that core isolation requirement rather than overriding admission.
func TestKitGKERenderRespectsCoreCredentialIsolation(t *testing.T) {
	t.Parallel()
	objects, err := RenderKitGKEProvision(kitGKEInput())
	require.NoError(t, err)
	pod := kitPodTemplate(t, objects)
	require.NotNil(t, pod.Spec.AutomountServiceAccountToken)
	require.False(t, *pod.Spec.AutomountServiceAccountToken)
	for _, volume := range pod.Spec.Volumes {
		if volume.Projected == nil {
			continue
		}
		for _, source := range volume.Projected.Sources {
			require.Nil(t, source.ServiceAccountToken, "volume %s is rejected by sandbox-core-policy", volume.Name)
			require.Nil(t, source.ClusterTrustBundle, "volume %s is rejected by sandbox-core-policy", volume.Name)
		}
	}
}

func TestKitGKERenderMetadataRefreshSidecar(t *testing.T) {
	t.Parallel()
	input := kitGKEInput()
	objects, err := RenderKitGKEProvision(input)
	require.NoError(t, err)
	pod := kitPodTemplate(t, objects)
	require.Len(t, pod.Spec.InitContainers, 1)
	sidecar := pod.Spec.InitContainers[0]
	require.Equal(t, input.MetadataImage, sidecar.Image)
	require.Equal(t, "Always", string(*sidecar.RestartPolicy))
	require.Equal(t, []string{"bun", "/usr/local/share/gram/kit-workload-token.mjs"}, sidecar.Command)
	require.Equal(t, []string{"bun", "/usr/local/share/gram/kit-workload-token.mjs", "--ready"}, sidecar.StartupProbe.Exec.Command)
	require.Equal(t, int64(1000), *pod.Spec.SecurityContext.RunAsUser)
	require.False(t, *sidecar.SecurityContext.AllowPrivilegeEscalation)
	require.Equal(t, pod.Spec.Containers[0].SecurityContext, sidecar.SecurityContext)
	env := map[string]string{}
	for _, variable := range sidecar.Env {
		env[variable.Name] = variable.Value
	}
	require.Equal(t, input.WorkloadAudience, env["WORKLOAD_TOKEN_AUDIENCE"])
	require.Equal(t, KitWorkloadAssertionPath, env["WORKLOAD_TOKEN_PATH"])
	require.Equal(t, input.Workload.Subject, env["WORKLOAD_TOKEN_SUBJECT"])
	require.Equal(t, "16Mi", sidecar.Resources.Requests.StorageEphemeral().String())
	require.Equal(t, "64Mi", sidecar.Resources.Limits.StorageEphemeral().String())
	require.Len(t, sidecar.VolumeMounts, 1)
	require.Equal(t, "workload", sidecar.VolumeMounts[0].Name)
	require.False(t, sidecar.VolumeMounts[0].ReadOnly)
	mountIndex := slices.IndexFunc(pod.Spec.Containers[0].VolumeMounts, func(mount corev1.VolumeMount) bool {
		return mount.Name == "workload"
	})
	require.NotEqual(t, -1, mountIndex, "Kit must mount the workload token volume")
	mount := pod.Spec.Containers[0].VolumeMounts[mountIndex]
	require.True(t, mount.ReadOnly)
	require.Empty(t, mount.SubPath)
	require.Empty(t, mount.SubPathExpr)
	require.Equal(t, sidecar.VolumeMounts[0].MountPath, mount.MountPath)
	for _, image := range []string{"", "registry.example/assistant:latest", "registry.example/assistant:v1", "registry.example/assistant@sha256:bad"} {
		input.MetadataImage = image
		_, err := RenderKitGKEProvision(input)
		require.ErrorContains(t, err, "immutable sha256 digest")
	}
}

func TestKitGKERenderExplicitZeroReplicaPool(t *testing.T) {
	t.Parallel()
	objects, err := RenderKitGKEProvision(kitGKEInput())
	require.NoError(t, err)
	for _, object := range []*unstructured.Unstructured{objects.Template, objects.WarmPool, objects.Claim} {
		require.Equal(t, "extensions.agents.x-k8s.io/v1beta1", object.GetAPIVersion())
	}
	require.Equal(t, "SandboxWarmPool", objects.WarmPool.GetKind())
	replicas, found, err := unstructured.NestedInt64(objects.WarmPool.Object, "spec", "replicas")
	require.NoError(t, err)
	require.True(t, found)
	require.Zero(t, replicas, "runtime authority must not be issued to speculative pool pods")
	template, found, err := unstructured.NestedString(objects.WarmPool.Object, "spec", "sandboxTemplateRef", "name")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, objects.Template.GetName(), template)
	pool, found, err := unstructured.NestedString(objects.Claim.Object, "spec", "warmPoolRef", "name")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, objects.WarmPool.GetName(), pool)
	_, found, err = unstructured.NestedFieldNoCopy(objects.Claim.Object, "spec", "sandboxTemplateRef")
	require.NoError(t, err)
	require.False(t, found)
}
