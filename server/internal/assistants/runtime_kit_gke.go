package assistants

// Kit provisioning follows gram-infra PR 4331 at
// b55e684f030da7a9e178bb24ddf229f11dbd9967. It deliberately does not implement
// runtime admission, readiness, fencing, or snapshot status interpretation.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

var kitTemplateGVR = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1alpha1", Resource: "sandboxtemplates"}
var kitPinnedImage = regexp.MustCompile(`^[^@\s]+@sha256:[a-f0-9]{64}$`)

var kitSecretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// KitGKEIdentity must come from the authoritative runtime record, not pod labels.
// A cold/reset boot MUST have a new Lineage; absent restore annotations permit
// GKE automatic snapshot selection and do not request a cold boot.
type KitGKEIdentity struct {
	Namespace, AssistantID, Lineage, CompatibilityGeneration string
}

func (i KitGKEIdentity) labels() map[string]string {
	return map[string]string{"gram.ai/assistant-id": i.AssistantID, "gram.ai/runtime-version": "3", "gram.ai/runtime-lineage": i.Lineage, "gram.ai/compatibility-generation": i.CompatibilityGeneration}
}
func (i KitGKEIdentity) validate() error {
	if len(validation.IsDNS1123Label(i.Namespace)) != 0 {
		return fmt.Errorf("invalid Kit namespace")
	}
	for key, value := range i.labels() {
		if value == "" || len(validation.IsValidLabelValue(value)) != 0 {
			return fmt.Errorf("invalid Kit identity label %s", key)
		}
	}
	return nil
}

// KitGKEProvisionInput requires the Kit owner to supply actual configuration
// bytes and executable arguments. This layer does not define Kit's wire format.
// Snapshot-enabled provisioning is reserved for a caller holding the admission
// lock with settled work and fenced prior authority. No warm pool is used.
type KitGKEProvisionInput struct {
	Identity                                       KitGKEIdentity
	Name, Image, IngressCIDR                       string
	GuestPort                                      int32
	Command, Args                                  []string
	AgentsMD, InferenceJSON, MCPJSON, GatewayToken []byte
	Resources                                      corev1.ResourceRequirements
	WorkSizeLimit, TmpSizeLimit                    string
	SnapshotsEnabled                               bool
	// PreviousIdentity is mandatory for an intentional cold/reset transition.
	ColdReset        bool
	PreviousIdentity *KitGKEIdentity
	Restore          *KitGKEVerifiedSnapshot
	// RestoreSource must be freshly read from authoritative runtime state.
	RestoreSource *KitGKESnapshotBinding
}

type KitGKEObjects struct{ Secret, Template, Claim *unstructured.Unstructured }

func kitObject(kind, version, name string, i KitGKEIdentity) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": version, "kind": kind}}
	u.SetName(name)
	u.SetNamespace(i.Namespace)
	u.SetLabels(i.labels())
	return u
}

// RenderKitGKEProvision is pure. Returned objects contain credentials and must
// never be logged. ProvisionKitGKE creates them in Secret -> Template -> Claim order.
func RenderKitGKEProvision(in KitGKEProvisionInput) (KitGKEObjects, error) {
	var out KitGKEObjects
	if err := in.Identity.validate(); err != nil {
		return out, err
	}
	if len(validation.IsDNS1123Subdomain(in.Name)) != 0 || strings.HasPrefix(in.Name, "gram-asst") {
		return out, fmt.Errorf("invalid dedicated Kit object name")
	}
	if !kitPinnedImage.MatchString(in.Image) {
		return out, fmt.Errorf("Kit image must be pinned to a lowercase SHA-256 digest")
	}
	if err := validateKitResources(in.Resources); err != nil {
		return out, err
	}
	if len(in.Command) == 0 || in.Command[0] == "" || in.Args == nil {
		return out, fmt.Errorf("Kit command and explicit args are required")
	}
	if in.GuestPort < 1 || in.GuestPort > 65535 {
		return out, fmt.Errorf("invalid Kit guest port")
	}
	if _, _, err := net.ParseCIDR(in.IngressCIDR); err != nil {
		return out, fmt.Errorf("invalid Kit ingress CIDR")
	}
	if len(in.AgentsMD) == 0 || len(in.GatewayToken) == 0 || !json.Valid(in.InferenceJSON) || !json.Valid(in.MCPJSON) {
		return out, fmt.Errorf("Kit bootstrap requires nonempty instructions/token and valid JSON configurations")
	}
	if in.ColdReset {
		if in.PreviousIdentity == nil || in.PreviousIdentity.AssistantID != in.Identity.AssistantID || in.PreviousIdentity.Namespace != in.Identity.Namespace || in.PreviousIdentity.Lineage == in.Identity.Lineage || in.PreviousIdentity.Lineage == "" || in.Restore != nil {
			return out, fmt.Errorf("Kit cold/reset requires a new lineage for the same assistant and namespace, without restore")
		}
	}
	annotations := map[string]string{"cluster-autoscaler.kubernetes.io/safe-to-evict": "false"}
	if in.Restore != nil {
		if !in.SnapshotsEnabled || in.RestoreSource == nil || *in.RestoreSource != in.Restore.binding || in.Restore.binding.Identity != in.Identity || in.Restore.name == "" {
			return out, fmt.Errorf("Kit restore requires a verified snapshot with exact identity and snapshots enabled")
		}
		annotations["podsnapshot.gke.io/ps-name"] = in.Restore.name
	}
	data := map[string][]byte{"AGENTS.md": in.AgentsMD, "inference.json": in.InferenceJSON, "mcp.json": in.MCPJSON, "gateway-token": in.GatewayToken}
	// Content-address the immutable generation-specific bootstrap: no credential
	// rotation in place, and no names shared across assistants or runtime lineages.
	encoded, err := json.Marshal(struct {
		Identity KitGKEIdentity
		Data     map[string][]byte
	}{in.Identity, data})
	if err != nil {
		return out, err
	}
	secretName := fmt.Sprintf("kit-bootstrap-%x", sha256.Sum256(encoded))[:63]
	yes, no := true, false
	uid := int64(1000)
	mode := int32(0440)
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: in.Identity.Namespace, Labels: in.Identity.labels()}, Immutable: &yes, Type: corev1.SecretTypeOpaque, Data: data}
	secretMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		return out, err
	}
	out.Secret = &unstructured.Unstructured{Object: secretMap}
	work, err := kitMemoryVolume("work", in.WorkSizeLimit)
	if err != nil {
		return out, err
	}
	tmp, err := kitMemoryVolume("tmp", in.TmpSizeLimit)
	if err != nil {
		return out, err
	}
	security := &corev1.SecurityContext{AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	home := corev1.VolumeMount{Name: "work", MountPath: "/home/kit"}
	pod := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: in.Identity.labels(), Annotations: annotations}, Spec: corev1.PodSpec{
		RuntimeClassName: ptrKitString("gvisor"), AutomountServiceAccountToken: &no,
		DNSPolicy: corev1.DNSNone, DNSConfig: &corev1.PodDNSConfig{Nameservers: []string{"8.8.8.8", "1.1.1.1"}},
		NodeSelector:    map[string]string{"sandbox.gke.io/runtime": "gvisor", "workload-type": "sandbox"},
		Tolerations:     []corev1.Toleration{{Key: "sandbox.gke.io/runtime", Operator: corev1.TolerationOpEqual, Value: "gvisor", Effect: corev1.TaintEffectNoSchedule}, {Key: "workload-type", Operator: corev1.TolerationOpEqual, Value: "sandbox", Effect: corev1.TaintEffectNoSchedule}},
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid},
		InitContainers:  []corev1.Container{{Name: "provision-kit", Image: in.Image, Command: []string{"/bin/sh", "-ec"}, Args: []string{kitBootstrapScript}, SecurityContext: security, Resources: in.Resources, VolumeMounts: []corev1.VolumeMount{home, {Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true}}}},
		Containers:      []corev1.Container{{Name: "kit", Image: in.Image, Command: in.Command, Args: in.Args, WorkingDir: "/home/kit", Ports: []corev1.ContainerPort{{ContainerPort: in.GuestPort}}, Env: []corev1.EnvVar{{Name: "HOME", Value: "/home/kit"}}, SecurityContext: security, Resources: in.Resources, VolumeMounts: []corev1.VolumeMount{home, {Name: "tmp", MountPath: "/tmp"}}}},
		Volumes:         []corev1.Volume{work, tmp, {Name: "bootstrap", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &mode, Items: []corev1.KeyToPath{{Key: "AGENTS.md", Path: "AGENTS.md"}, {Key: "inference.json", Path: "inference.json"}, {Key: "mcp.json", Path: "mcp.json"}, {Key: "gateway-token", Path: "gateway-token"}}}}}},
	}}
	if in.SnapshotsEnabled {
		pod.Labels["gram.ai/snapshots"] = "enabled"
	}
	podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
	if err != nil {
		return out, err
	}
	out.Template = kitObject("SandboxTemplate", "extensions.agents.x-k8s.io/v1alpha1", in.Name, in.Identity)
	out.Template.Object["spec"] = map[string]any{"networkPolicyManagement": "Managed", "networkPolicy": map[string]any{
		"ingress": []any{map[string]any{"from": []any{map[string]any{"ipBlock": map[string]any{"cidr": in.IngressCIDR}}}, "ports": []any{map[string]any{"protocol": "TCP", "port": int64(in.GuestPort)}}}},
		"egress":  []any{map[string]any{"to": []any{map[string]any{"ipBlock": map[string]any{"cidr": "0.0.0.0/0", "except": []any{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}}}, map[string]any{"ipBlock": map[string]any{"cidr": "::/0", "except": []any{"fc00::/7", "fe80::/10"}}}}}},
	}, "podTemplate": podMap}
	out.Claim = kitObject("SandboxClaim", "extensions.agents.x-k8s.io/v1alpha1", in.Name, in.Identity)
	out.Claim.Object["spec"] = map[string]any{"sandboxTemplateRef": map[string]any{"name": in.Name}, "lifecycle": map[string]any{"shutdownPolicy": "Delete"}}
	return out, nil
}

func ptrKitString(s string) *string { return &s }

const kitBootstrapScript = `umask 077
mkdir -p /home/kit/.kit /home/kit/.kit/credentials
cp /bootstrap/AGENTS.md /home/kit/.kit/AGENTS.md
cp /bootstrap/inference.json /home/kit/.kit/credentials/inference.json
cp /bootstrap/mcp.json /home/kit/.kit/credentials/mcp.json
cp /bootstrap/gateway-token /home/kit/.kit/credentials/gateway-token
chmod 600 /home/kit/.kit/AGENTS.md /home/kit/.kit/credentials/*
`

// ProvisionKitGKE is create-only: no adoption, mutation, cleanup, or readiness
// inference. On partial failure the caller must reconcile under its admission
// lock; existing objects are never overwritten or trusted by name alone.
func ProvisionKitGKE(ctx context.Context, client dynamic.Interface, in KitGKEProvisionInput) error {
	if client == nil {
		return fmt.Errorf("Kit provisioning requires a dynamic client")
	}
	objects, err := RenderKitGKEProvision(in)
	if err != nil {
		return err
	}
	for _, step := range []struct {
		gvr    schema.GroupVersionResource
		object *unstructured.Unstructured
	}{{kitSecretGVR, objects.Secret}, {kitTemplateGVR, objects.Template}, {gkeSandboxClaimGVR, objects.Claim}} {
		if _, err := client.Resource(step.gvr).Namespace(in.Identity.Namespace).Create(ctx, step.object, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create Kit %s: %w", step.object.GetKind(), err)
		}
	}
	return nil
}

// Require explicit bounds rather than relying on cluster defaulting. These are
// caller-selected budgets, not runtime sizing recommendations.
func validateKitResources(resources corev1.ResourceRequirements) error {
	for _, name := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		limit, hasLimit := resources.Limits[name]
		request, hasRequest := resources.Requests[name]
		if !hasLimit || !hasRequest || limit.Sign() <= 0 || request.Sign() <= 0 || request.Cmp(limit) > 0 {
			return fmt.Errorf("Kit requires positive explicit %s requests and limits with request <= limit", name)
		}
	}
	return nil
}
