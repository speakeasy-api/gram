package assistants

// Kit provisioning follows gram-infra PR 4331 at
// b55e684f030da7a9e178bb24ddf229f11dbd9967. It deliberately does not implement
// runtime admission, readiness, fencing, or snapshot status interpretation.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"maps"
	"net"
	"slices"
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

func validKitGKEImage(image string) bool { return kitGKEExplicitImage(image) }

var kitSecretGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}

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
	Model                                          string
	Name, Image, IngressCIDR                       string
	GuestPort                                      int32
	Command, Args                                  []string
	AgentsMD, InferenceJSON, MCPJSON, GatewayToken []byte
	MCPFiles                                       map[string][]byte
	AllowInsecureHTTP                              bool
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
//
//nolint:exhaustruct // Only renderer-defined Kubernetes fields are set; optional fields must retain API defaults.
func RenderKitGKEProvision(in KitGKEProvisionInput) (KitGKEObjects, error) {
	var out KitGKEObjects
	if err := in.Identity.validate(); err != nil {
		return out, err
	}
	if len(validation.IsDNS1123Subdomain(in.Name)) != 0 || strings.HasPrefix(in.Name, "gram-asst") {
		return out, fmt.Errorf("invalid dedicated Kit object name")
	}
	if !kitGKEExplicitImage(in.Image) {
		return out, fmt.Errorf("kit image requires an explicit non-latest version tag or digest")
	}
	if err := validateKitResources(in.Resources); err != nil {
		return out, err
	}
	if len(in.Command) == 0 || in.Command[0] == "" || in.Args == nil {
		return out, fmt.Errorf("kit command and explicit args are required")
	}
	if in.GuestPort < 1 || in.GuestPort > 65535 {
		return out, fmt.Errorf("invalid Kit guest port")
	}
	if _, _, err := net.ParseCIDR(in.IngressCIDR); err != nil {
		return out, fmt.Errorf("invalid Kit ingress CIDR")
	}
	if err := validateKitGatewayCommand(in.Command, in.Args, int(in.GuestPort)); err != nil {
		return out, err
	}
	if err := validateKitBootstrap(KitLocalBootstrap{AgentsMD: in.AgentsMD, InferenceJSON: in.InferenceJSON, MCPJSON: in.MCPJSON, GatewayToken: in.GatewayToken, MCPFiles: in.MCPFiles}); err != nil {
		return out, err
	}
	origin, err := kitInferenceOrigin(in.InferenceJSON)
	if err != nil {
		return out, err
	}
	if strings.HasPrefix(origin, "http:") && !in.AllowInsecureHTTP {
		return out, fmt.Errorf("insecure Kit inference requires explicit HTTP opt-in")
	}
	environment := []corev1.EnvVar{{Name: "HOME", Value: "/home/kit"}, {Name: "SPEAKEASY_ORIGIN", Value: origin}}
	if in.AllowInsecureHTTP {
		environment = append(environment, corev1.EnvVar{Name: "SPEAKEASY_ALLOW_INSECURE_HTTP", Value: "1"})
	}

	if in.ColdReset {
		if in.PreviousIdentity == nil || in.PreviousIdentity.AssistantID != in.Identity.AssistantID || in.PreviousIdentity.Namespace != in.Identity.Namespace || in.PreviousIdentity.Lineage == in.Identity.Lineage || in.PreviousIdentity.Lineage == "" || in.Restore != nil {
			return out, fmt.Errorf("kit cold/reset requires a new lineage for the same assistant and namespace, without restore")
		}
	}
	annotations := map[string]string{"cluster-autoscaler.kubernetes.io/safe-to-evict": "false"}
	if in.Restore != nil {
		if !in.SnapshotsEnabled || in.RestoreSource == nil || *in.RestoreSource != in.Restore.binding || in.Restore.binding.Identity != in.Identity || in.Restore.name == "" {
			return out, fmt.Errorf("kit restore requires a verified snapshot with exact identity and snapshots enabled")
		}
		annotations["podsnapshot.gke.io/ps-name"] = in.Restore.name
	}
	data := map[string][]byte{"AGENTS.md": in.AgentsMD, "config.toml": []byte(kitBootstrapConfig(KitLocalBootstrap{Model: in.Model})), KitInferenceCredentialFilename: in.InferenceJSON, "mcp.json": in.MCPJSON, "gateway-token": in.GatewayToken}
	maps.Copy(data, in.MCPFiles)
	items := make([]corev1.KeyToPath, 0, len(data))
	for _, name := range slices.Sorted(maps.Keys(data)) {
		path := "credentials/" + name
		if name == "AGENTS.md" || name == "config.toml" {
			path = name
		}
		items = append(items, corev1.KeyToPath{Key: name, Path: path})
	}
	// Content-address the immutable generation-specific bootstrap: no credential
	// rotation in place, and no names shared across assistants or runtime lineages.
	encoded, err := json.Marshal(map[string]any{"namespace": in.Identity.Namespace, "identity": in.Identity.labels(), "data": data})
	if err != nil {
		return out, fmt.Errorf("encode Kit bootstrap generation: %w", err)
	}
	secretName := fmt.Sprintf("kit-bootstrap-%x", sha256.Sum256(encoded))[:63]
	yes, no := true, false
	uid := int64(1000)
	mode := int32(0440)
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: in.Identity.Namespace, Labels: in.Identity.labels()}, Immutable: &yes, Type: corev1.SecretTypeOpaque, Data: data}
	secretMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(secret)
	if err != nil {
		return out, fmt.Errorf("convert Kit bootstrap Secret: %w", err)
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
		RuntimeClassName: new("gvisor"), AutomountServiceAccountToken: &no,
		DNSPolicy: corev1.DNSNone, DNSConfig: &corev1.PodDNSConfig{Nameservers: []string{"8.8.8.8", "1.1.1.1"}},
		NodeSelector:    map[string]string{"sandbox.gke.io/runtime": "gvisor", "workload-type": "sandbox"},
		Tolerations:     []corev1.Toleration{{Key: "sandbox.gke.io/runtime", Operator: corev1.TolerationOpEqual, Value: "gvisor", Effect: corev1.TaintEffectNoSchedule}, {Key: "workload-type", Operator: corev1.TolerationOpEqual, Value: "sandbox", Effect: corev1.TaintEffectNoSchedule}},
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid},
		InitContainers:  []corev1.Container{{Name: "provision-kit", Image: in.Image, Command: []string{"/bin/sh", "-ec"}, Args: []string{kitBootstrapScript}, SecurityContext: security, Resources: in.Resources, VolumeMounts: []corev1.VolumeMount{home, {Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true}}}},
		Containers:      []corev1.Container{{Name: "kit", Image: in.Image, Command: in.Command, Args: in.Args, WorkingDir: "/home/kit", Ports: []corev1.ContainerPort{{ContainerPort: in.GuestPort}}, Env: environment, SecurityContext: security, Resources: in.Resources, VolumeMounts: []corev1.VolumeMount{home, {Name: "tmp", MountPath: "/tmp"}}}},
		Volumes:         []corev1.Volume{work, tmp, {Name: "bootstrap", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &mode, Items: items}}}},
	}}
	if in.SnapshotsEnabled {
		pod.Labels["gram.ai/snapshots"] = "enabled"
	}
	podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
	if err != nil {
		return out, fmt.Errorf("convert Kit pod template: %w", err)
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

const kitBootstrapScript = `umask 077
rm -rf /home/kit/.kit/credentials
mkdir -p /home/kit/.kit/credentials /home/kit/workspace
chmod 700 /home/kit/.kit /home/kit/.kit/credentials /home/kit/workspace
cp /bootstrap/AGENTS.md /home/kit/.kit/AGENTS.md
cp /bootstrap/config.toml /home/kit/.kit/config.toml
cp -R /bootstrap/credentials/. /home/kit/.kit/credentials/
chmod 600 /home/kit/.kit/AGENTS.md /home/kit/.kit/config.toml /home/kit/.kit/credentials/*
`

// ProvisionKitGKE is create-only: no adoption, mutation, cleanup, or readiness
// inference. On partial failure the caller must reconcile under its admission
// lock; existing objects are never overwritten or trusted by name alone.
func ProvisionKitGKE(ctx context.Context, client dynamic.Interface, in KitGKEProvisionInput) error {
	if client == nil {
		return fmt.Errorf("kit provisioning requires a dynamic client")
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
			return fmt.Errorf("kit requires positive explicit %s requests and limits with request <= limit", name)
		}
	}
	return nil
}

// PrepareKitGKEStartup replaces any snapshot-derived credential bytes with fresh
// startup authority before rendering a new pod. Caller must hold runtime admission
// and ensure the gateway boots afresh (not resume a process with cached credentials).
// The snapshot identity remains exact; inference authority is rotated for its row.
func PrepareKitGKEStartup(ctx context.Context, manager *assistanttokens.Manager, startup KitStartupBootstrapInput, in KitGKEProvisionInput) (KitGKEProvisionInput, error) {
	if in.Identity.AssistantID != startup.Identity.AssistantID.String() || in.Identity.Lineage != startup.RuntimeID.String() {
		return KitGKEProvisionInput{}, fmt.Errorf("kit GKE startup credentials must match assistant and runtime lineage")
	}
	in.Model = startup.Model
	in.Command, in.Args = KitGatewayCommand(int(in.GuestPort))
	bootstrap, _, err := PrepareKitStartupBootstrap(ctx, manager, startup)
	if err != nil {
		return KitGKEProvisionInput{}, err
	}
	in.AgentsMD, in.InferenceJSON, in.MCPJSON, in.GatewayToken, in.MCPFiles = bootstrap.AgentsMD, bootstrap.InferenceJSON, bootstrap.MCPJSON, bootstrap.GatewayToken, bootstrap.MCPFiles
	return in, nil
}
