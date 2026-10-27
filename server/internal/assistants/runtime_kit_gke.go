package assistants

// Kit provisioning follows gram-infra PR 4331 at
// b55e684f030da7a9e178bb24ddf229f11dbd9967. It deliberately does not implement
// runtime admission, readiness, fencing, or snapshot status interpretation.
import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
)

var kitTemplateGVR = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxtemplates"}

var kitWarmPoolGVR = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxwarmpools"}
var kitClaimGVR = schema.GroupVersionResource{Group: "extensions.agents.x-k8s.io", Version: "v1beta1", Resource: "sandboxclaims"}

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
// lock with settled work and fenced prior authority. The explicit pool has zero speculative replicas.
type KitGKEProvisionInput struct {
	Workload                                 *KitWorkloadBinding
	InferenceEndpoint                        string
	WorkloadServiceAccount, WorkloadAudience string
	Identity                                 KitGKEIdentity
	Model                                    string
	Name, Image, MetadataImage, IngressCIDR  string
	GuestPort                                int32
	Command, Args                            []string
	AgentsMD, MCPJSON, GatewayToken          []byte
	TLSCertificate, TLSPrivateKey            []byte
	AllowInsecureHTTP                        bool
	Resources                                corev1.ResourceRequirements
	WorkSizeLimit, TmpSizeLimit              string
	SnapshotsEnabled                         bool
	// PreviousIdentity is mandatory for an intentional cold/reset transition.
	ColdReset        bool
	PreviousIdentity *KitGKEIdentity
	Restore          *KitGKEVerifiedSnapshot
	// RestoreSource must be freshly read from authoritative runtime state.
	RestoreSource *KitGKESnapshotBinding
	// CheckpointMarker is a fresh UUID for this bootstrap generation. A restore
	// must also supply the previous generation's authoritative rootfs marker.
	CheckpointMarker, ExpectedCheckpointMarker []byte
}

type KitGKEObjects struct{ Secret, Template, WarmPool, Claim *unstructured.Unstructured }

func kitObject(kind, version, name string, i KitGKEIdentity) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": version, "kind": kind}}
	u.SetName(name)
	u.SetNamespace(i.Namespace)
	u.SetLabels(i.labels())
	return u
}

// RenderKitGKEProvision is pure. Returned objects contain credentials and must
// never be logged. KitGKERuntimeBackend.Ensure creates them in Secret -> Template -> WarmPool -> Claim order.
//
//nolint:exhaustruct,exhaustive // Only renderer-defined Kubernetes fields and CPU/memory budgets are set; optional fields retain API defaults.
func RenderKitGKEProvision(in KitGKEProvisionInput) (KitGKEObjects, error) {
	var out KitGKEObjects
	if err := in.Identity.validate(); err != nil {
		return out, err
	}
	if len(validation.IsDNS1123Subdomain(in.Name)) != 0 || strings.HasPrefix(in.Name, "gram-asst") {
		return out, fmt.Errorf("invalid dedicated Kit object name")
	}
	if len(validation.IsDNS1123Subdomain(in.WorkloadServiceAccount)) != 0 {
		return out, fmt.Errorf("invalid Kit workload service account")
	}
	if strings.TrimSpace(in.WorkloadAudience) == "" {
		return out, fmt.Errorf("kit workload audience is required")
	}
	if !kitGKEExplicitImage(in.Image) {
		return out, fmt.Errorf("kit image requires an explicit non-latest version tag or digest")
	}
	if !regexp.MustCompile(`^[^@\s]+@sha256:[a-f0-9]{64}$`).MatchString(in.MetadataImage) {
		return out, fmt.Errorf("kit metadata image requires an immutable sha256 digest")
	}
	if err := validateKitResources(in.Resources); err != nil {
		return out, err
	}
	if len(in.Command) == 0 || in.Command[0] == "" || in.Args == nil {
		return out, fmt.Errorf("kit command and explicit args are required")
	}
	if in.GuestPort < 1 || in.GuestPort > 65535 || in.GuestPort == kitGKEPlaintextPort {
		return out, fmt.Errorf("invalid Kit guest port")
	}
	if _, _, err := net.ParseCIDR(in.IngressCIDR); err != nil {
		return out, fmt.Errorf("invalid Kit ingress CIDR")
	}
	if err := validateKitGatewayCommand(in.Command, in.Args, int(in.GuestPort)); err != nil {
		return out, err
	}
	bootstrap := KitLocalBootstrap{Model: in.Model, Workload: in.Workload, AssertionFile: KitWorkloadAssertionPath, InferenceEndpoint: in.InferenceEndpoint, AgentsMD: in.AgentsMD, MCPJSON: in.MCPJSON, GatewayToken: in.GatewayToken}
	if err := validateKitBootstrap(bootstrap); err != nil {
		return out, err
	}
	if in.Workload.ServiceAccount != in.WorkloadServiceAccount || in.Workload.Audience != in.WorkloadAudience {
		return out, fmt.Errorf("kit workload metadata binding mismatch")
	}
	if _, err := tls.X509KeyPair(in.TLSCertificate, in.TLSPrivateKey); err != nil {
		return out, fmt.Errorf("kit GKE requires fresh TLS key pair")
	}
	if _, err := kitGKETLSConfig(in.TLSCertificate, in.Name+".kit.internal"); err != nil {
		return out, err
	}
	environment := []corev1.EnvVar{{Name: "HOME", Value: "/home/kit"}}
	if in.AllowInsecureHTTP {
		environment = append(environment, corev1.EnvVar{Name: "SPEAKEASY_ALLOW_INSECURE_HTTP", Value: "1"})
	}

	if in.ColdReset {
		if in.PreviousIdentity == nil || in.PreviousIdentity.AssistantID != in.Identity.AssistantID || in.PreviousIdentity.Namespace != in.Identity.Namespace || in.PreviousIdentity.Lineage == in.Identity.Lineage || in.PreviousIdentity.Lineage == "" || in.Restore != nil {
			return out, fmt.Errorf("kit cold/reset requires a new lineage for the same assistant and namespace, without restore")
		}
	}
	if marker, err := uuid.Parse(string(in.CheckpointMarker)); err != nil || marker.String() != string(in.CheckpointMarker) {
		return out, fmt.Errorf("kit bootstrap requires a canonical checkpoint marker UUID")
	}
	if in.Restore != nil {
		if marker, err := uuid.Parse(string(in.ExpectedCheckpointMarker)); err != nil || marker.String() != string(in.ExpectedCheckpointMarker) || string(in.ExpectedCheckpointMarker) == string(in.CheckpointMarker) {
			return out, fmt.Errorf("kit restore requires a distinct authoritative checkpoint marker")
		}
	} else if len(in.ExpectedCheckpointMarker) != 0 {
		return out, fmt.Errorf("kit cold bootstrap cannot expect a checkpoint marker")
	}
	annotations := map[string]string{"cluster-autoscaler.kubernetes.io/safe-to-evict": "false"}
	if in.Restore != nil {
		if !in.SnapshotsEnabled || in.RestoreSource == nil || *in.RestoreSource != in.Restore.binding || in.Restore.binding.Identity != in.Identity || in.Restore.name == "" {
			return out, fmt.Errorf("kit restore requires a verified snapshot with exact identity and snapshots enabled")
		}
		annotations["podsnapshot.gke.io/ps-name"] = in.Restore.name
	}
	data := map[string][]byte{"AGENTS.md": in.AgentsMD, "config.toml": []byte(kitBootstrapConfig(bootstrap)), "mcp.json": in.MCPJSON, "gateway-token": in.GatewayToken}
	data["tls.crt"], data["tls.key"] = in.TLSCertificate, in.TLSPrivateKey
	data["nginx.conf"] = []byte(kitGKETLSProxyConfig(in.GuestPort))
	data["checkpoint-marker"] = in.CheckpointMarker
	if in.Restore != nil {
		data["expected-checkpoint-marker"] = in.ExpectedCheckpointMarker
	}
	items := make([]corev1.KeyToPath, 0, len(data))
	for _, name := range slices.Sorted(maps.Keys(data)) {
		if name == "tls.crt" || name == "tls.key" || name == "nginx.conf" {
			continue
		}
		path := "credentials/" + name
		if name == "AGENTS.md" || name == "config.toml" || name == "checkpoint-marker" || name == "expected-checkpoint-marker" {
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
	workLimit, err := resource.ParseQuantity(in.WorkSizeLimit)
	if err != nil || workLimit.Sign() <= 0 {
		return out, fmt.Errorf("kit writable rootfs requires a positive ephemeral storage limit")
	}
	resources := *in.Resources.DeepCopy()
	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{}
	}
	resources.Limits[corev1.ResourceEphemeralStorage] = workLimit
	tmp, err := kitMemoryVolume("tmp", in.TmpSizeLimit)
	if err != nil {
		return out, err
	}
	security := &corev1.SecurityContext{AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	// The native Kit image owns /home/kit as UID 1000. Durable Kit state must
	// remain on the container rootfs, not on a mount excluded from snapshots.
	// Main-process bootstrap runs on each fresh process, in the same rootfs.
	args := append([]string{kitBootstrapScript + "exec \"$@\"\n", "kit-bootstrap"}, in.Command...)
	args = append(args, "gateway", "--listen", "127.0.0.1:"+strconv.Itoa(kitGKEPlaintextPort), "--credential-file", "/home/kit/.kit/credentials/gateway-token", "--project", KitWorkspacePath)
	pod := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: in.Identity.labels(), Annotations: annotations}, Spec: corev1.PodSpec{
		RuntimeClassName: new("gvisor"), AutomountServiceAccountToken: &no, ServiceAccountName: in.WorkloadServiceAccount,
		DNSPolicy: corev1.DNSNone, DNSConfig: &corev1.PodDNSConfig{Nameservers: []string{"8.8.8.8", "1.1.1.1"}},
		NodeSelector:    map[string]string{"sandbox.gke.io/runtime": "gvisor", "workload-type": "sandbox"},
		Tolerations:     []corev1.Toleration{{Key: "sandbox.gke.io/runtime", Operator: corev1.TolerationOpEqual, Value: "gvisor", Effect: corev1.TaintEffectNoSchedule}, {Key: "workload-type", Operator: corev1.TolerationOpEqual, Value: "sandbox", Effect: corev1.TaintEffectNoSchedule}},
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid},
		Containers:      []corev1.Container{{Name: "kit", Image: in.Image, Command: []string{"/bin/sh", "-ec"}, Args: args, WorkingDir: "/home/kit", Env: environment, SecurityContext: security, Resources: resources, VolumeMounts: []corev1.VolumeMount{{Name: "bootstrap", MountPath: "/bootstrap", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}, {Name: "workload", MountPath: "/var/run/secrets/speakeasy/workload", ReadOnly: true}}}},
		Volumes:         []corev1.Volume{tmp, {Name: "bootstrap", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &mode, Items: items}}}},
	}}
	proxyTmp, err := kitMemoryVolume("tls-tmp", "32Mi")
	if err != nil {
		return out, err
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, proxyTmp, corev1.Volume{Name: "tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: &mode, Items: []corev1.KeyToPath{{Key: "tls.crt", Path: "tls.crt"}, {Key: "tls.key", Path: "tls.key"}, {Key: "nginx.conf", Path: "nginx.conf"}}}}})
	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "acp-tls", Image: kitGKETLSProxyImage, Command: []string{"nginx"}, Args: []string{"-c", "/tls/nginx.conf", "-g", "daemon off;"}, Ports: []corev1.ContainerPort{{ContainerPort: in.GuestPort}}, SecurityContext: security, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")}}, VolumeMounts: []corev1.VolumeMount{{Name: "tls", MountPath: "/tls", ReadOnly: true}, {Name: "tls-tmp", MountPath: "/tmp"}}})
	// Metadata credentials rotate in memory, outside the snapshotted rootfs.
	workload, err := kitMemoryVolume("workload", "1Mi")
	if err != nil {
		return out, err
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, workload)
	metadataEnv := []corev1.EnvVar{
		{Name: "WORKLOAD_TOKEN_AUDIENCE", Value: in.WorkloadAudience},
		{Name: "WORKLOAD_TOKEN_PATH", Value: KitWorkloadAssertionPath},
		{Name: "WORKLOAD_TOKEN_SUBJECT", Value: in.Workload.Subject},
	}
	// A native sidecar's startup probe gates the Kit process on fresh authority.
	pod.Spec.InitContainers = []corev1.Container{{
		Name: "workload-token", Image: in.MetadataImage,
		Command: []string{"bun", "/usr/local/share/gram/kit-workload-token.mjs"},
		Env:     metadataEnv, RestartPolicy: new(corev1.ContainerRestartPolicyAlways),
		StartupProbe:    &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"bun", "/usr/local/share/gram/kit-workload-token.mjs", "--ready"}}}, PeriodSeconds: 2, TimeoutSeconds: 5, FailureThreshold: 60},
		SecurityContext: security,
		Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}},
		VolumeMounts:    []corev1.VolumeMount{{Name: "workload", MountPath: "/var/run/secrets/speakeasy/workload"}},
	}}
	if in.SnapshotsEnabled {
		pod.Labels["gram.ai/snapshots"] = "enabled"
	}
	podMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
	if err != nil {
		return out, fmt.Errorf("convert Kit pod template: %w", err)
	}
	out.Template = kitObject("SandboxTemplate", "extensions.agents.x-k8s.io/v1beta1", kitGKETemplateName(in.Name), in.Identity)
	out.Template.Object["spec"] = map[string]any{"networkPolicyManagement": "Managed", "networkPolicy": map[string]any{
		"ingress": []any{map[string]any{"from": []any{map[string]any{"ipBlock": map[string]any{"cidr": in.IngressCIDR}}}, "ports": []any{map[string]any{"protocol": "TCP", "port": int64(in.GuestPort)}}}},
		"egress": []any{map[string]any{"to": []any{map[string]any{"ipBlock": map[string]any{"cidr": "0.0.0.0/0", "except": []any{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}}}, map[string]any{"ipBlock": map[string]any{"cidr": "::/0", "except": []any{"fc00::/7", "fe80::/10"}}}}},
			// GKE metadata identity is the only link-local egress exception.
			map[string]any{"to": []any{map[string]any{"ipBlock": map[string]any{"cidr": "169.254.169.254/32"}}}, "ports": []any{map[string]any{"protocol": "TCP", "port": int64(80)}}}},
	}, "podTemplate": podMap}
	// Zero replicas prohibit speculative pods carrying this runtime's credentials.
	// Claims cold-create from the pool's template when no warm sandbox exists.
	out.WarmPool = kitObject("SandboxWarmPool", "extensions.agents.x-k8s.io/v1beta1", kitGKEWarmPoolName(in.Name), in.Identity)
	out.WarmPool.Object["spec"] = map[string]any{"replicas": int64(0), "sandboxTemplateRef": map[string]any{"name": out.Template.GetName()}}
	out.Claim = kitObject("SandboxClaim", "extensions.agents.x-k8s.io/v1beta1", in.Name, in.Identity)
	out.Claim.Object["spec"] = map[string]any{"warmPoolRef": map[string]any{"name": out.WarmPool.GetName()}, "lifecycle": map[string]any{"shutdownPolicy": "Delete"}}
	return out, nil
}

const kitBootstrapScript = `umask 077
# Never follow a restored symlink when installing fresh authority or checking state.
test ! -L /home/kit/.kit
test ! -L /home/kit/.kit/checkpoint-marker
if [ -f /bootstrap/expected-checkpoint-marker ]; then
  test -f /home/kit/.kit/checkpoint-marker
  # A container restart may retain the rootfs after this generation already
  # passed the restore gate. Only its fresh nonce may stand in for the source.
  cmp -s /bootstrap/expected-checkpoint-marker /home/kit/.kit/checkpoint-marker ||
    cmp -s /bootstrap/checkpoint-marker /home/kit/.kit/checkpoint-marker
fi
rm -rf /home/kit/.kit/tls
rm -rf /home/kit/.kit/credentials
mkdir -p /home/kit/.kit/credentials /home/kit/workspace
chmod 700 /home/kit/.kit /home/kit/.kit/credentials /home/kit/workspace
rm -f /home/kit/.kit/AGENTS.md /home/kit/.kit/config.toml
cp /bootstrap/AGENTS.md /home/kit/.kit/AGENTS.md
cp /bootstrap/config.toml /home/kit/.kit/config.toml
cp -R /bootstrap/credentials/. /home/kit/.kit/credentials/
chmod 600 /home/kit/.kit/AGENTS.md /home/kit/.kit/config.toml /home/kit/.kit/credentials/*
cp /bootstrap/checkpoint-marker /home/kit/.kit/checkpoint-marker
chmod 600 /home/kit/.kit/checkpoint-marker
`

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

// Templates and claims each create a managed network policy named after themselves.
// Distinct names prevent their controllers from competing for the same policy.
func kitGKETemplateName(runtimeName string) string { return runtimeName + "-template" }

func kitGKEWarmPoolName(runtimeName string) string { return runtimeName + "-pool" }
