package assistants

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/google/uuid"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// KitGKERuntimeConfig composes dedicated Kit sandboxes, never legacy warm pools.
// All lifecycle calls require the caller's shared runtime admission lock.
// Bootstrap retires old inference authority before minting startup credentials.
type KitGKERuntimeConfig struct {
	GKE            GKERuntimeConfig
	Provision      KitGKEProvisionInput
	Bootstrap      func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error)
	Protocol       func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error)
	Fence          func(context.Context, assistantRuntimeRecord) error
	Snapshots      KitGKECheckpointDriver
	AuthorityStore KitGKEAuthorityStore
	// Lifecycle acquires the assistant advisory fence and reloads authoritative state.
	Lifecycle func(context.Context, assistantRuntimeRecord, func(context.Context, assistantRuntimeRecord) error) error
}

type KitGKERuntimeBackend struct {
	config      KitGKERuntimeConfig
	fingerprint string
}

var _ RuntimeBackend = (*KitGKERuntimeBackend)(nil)

// Trigger creation and Pod Running never prove a checkpoint.
const kitGKECheckpointUnavailable = "Kit GKE checkpoint/restore unavailable: snapshots are disabled or no driver is installed"

func NewKitGKERuntimeBackend(config KitGKERuntimeConfig) (*KitGKERuntimeBackend, error) {
	if config.GKE.Dynamic == nil || config.GKE.Namespace == "" || config.GKE.ServerURL == nil || config.Bootstrap == nil || config.Protocol == nil || config.Fence == nil || config.AuthorityStore == nil {
		return nil, fmt.Errorf("kit GKE requires dynamic client, namespace, server URL, bootstrap, fence, authority store and ACP protocol")
	}
	if config.GKE.ServerURL.Scheme != "https" || config.GKE.ServerURL.Host == "" || config.GKE.ServerURL.User != nil {
		return nil, fmt.Errorf("kit GKE requires an HTTPS management URL")
	}
	p := config.Provision
	if p.AllowInsecureHTTP {
		return nil, fmt.Errorf("kit GKE requires HTTPS inference without insecure overrides")
	}
	if p.GuestPort == 0 {
		if config.GKE.GuestPort < 0 || config.GKE.GuestPort > 65535 {
			return nil, fmt.Errorf("invalid kit GKE port")
		}
		p.GuestPort = int32(config.GKE.GuestPort)
		if p.GuestPort == 0 {
			p.GuestPort = 8081
		}
	}
	if len(p.Command) == 0 && p.Args == nil {
		p.Command, p.Args = KitGatewayCommand(int(p.GuestPort))
	}
	if p.Identity.CompatibilityGeneration == "" {
		p.Identity.CompatibilityGeneration = "kit-acp-v2-rootfs-workload-v4"
	} else if !strings.HasSuffix(p.Identity.CompatibilityGeneration, "-workload-v4") && !strings.HasPrefix(p.Identity.CompatibilityGeneration, "kit-workload-v4-") {
		// Scope even caller-defined generations away from legacy automatic snapshots.
		p.Identity.CompatibilityGeneration = fmt.Sprintf("kit-workload-v4-%x", sha256.Sum256([]byte(p.Identity.CompatibilityGeneration)))[:63]
	}
	p.Command = slices.Clone(p.Command)
	p.Args = slices.Clone(p.Args)
	p.Resources = *p.Resources.DeepCopy()
	serverURL := *config.GKE.ServerURL
	config.GKE.ServerURL = &serverURL
	config.Provision = p
	if p.SnapshotsEnabled && config.Snapshots == nil {
		driver, err := NewKitGKEManagedCheckpointDriver(config.GKE.Dynamic)
		if err != nil {
			return nil, err
		}
		config.Snapshots = driver
	}
	if p.Restore != nil || p.RestoreSource != nil || p.ColdReset || p.PreviousIdentity != nil {
		return nil, fmt.Errorf("restore/reset input must come from persisted runtime authority")
	}
	if p.Identity.CompatibilityGeneration == "" || !kitGKEExplicitImage(p.Image) {
		return nil, fmt.Errorf("kit GKE requires explicit compatibility generation and tagged or digest-pinned image")
	}
	if p.GuestPort < 1 || p.GuestPort > 65535 || p.GuestPort == kitGKEPlaintextPort {
		return nil, fmt.Errorf("invalid Kit GKE port")
	}
	if err := validateKitGatewayCommand(p.Command, p.Args, int(p.GuestPort)); err != nil {
		return nil, err
	}
	if err := validateKitResources(p.Resources); err != nil {
		return nil, err
	}
	if _, _, err := net.ParseCIDR(p.IngressCIDR); err != nil {
		return nil, fmt.Errorf("invalid Kit GKE ingress CIDR")
	}
	p.Identity = KitGKEIdentity{Namespace: config.GKE.Namespace, AssistantID: "", Lineage: "", CompatibilityGeneration: p.Identity.CompatibilityGeneration}
	p.Name = ""
	p.Model = ""
	p.AgentsMD = nil
	p.MCPJSON = nil
	p.GatewayToken = nil
	p.TLSCertificate, p.TLSPrivateKey = nil, nil
	raw, err := json.Marshal(map[string]any{
		"tls_proxy_image": kitGKETLSProxyImage, "tls_transport": 1, "workload_generation": 4,
		"identity": p.Identity.labels(), "namespace": p.Identity.Namespace, "image": p.Image,
		"ingress_cidr": p.IngressCIDR, "guest_port": p.GuestPort, "command": p.Command, "args": p.Args,
		"allow_insecure_http": p.AllowInsecureHTTP, "resources": p.Resources, "work_size_limit": p.WorkSizeLimit,
		"tmp_size_limit": p.TmpSizeLimit, "snapshots_enabled": p.SnapshotsEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("kit GKE NewKitGKERuntimeBackend: %w", err)
	}
	return &KitGKERuntimeBackend{config: config, fingerprint: fmt.Sprintf("%x", sha256.Sum256(raw))}, nil
}
func (k *KitGKERuntimeBackend) Backend() string               { return runtimeBackendGKE }
func (k *KitGKERuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendGKE }
func (k *KitGKERuntimeBackend) ServerURL() *url.URL           { return k.config.GKE.ServerURL }
func (k *KitGKERuntimeBackend) ImageRef() string              { return k.config.Provision.Image }
func (k *KitGKERuntimeBackend) identity(r assistantRuntimeRecord) KitGKEIdentity {
	return KitGKEIdentity{Namespace: k.config.GKE.Namespace, AssistantID: r.AssistantID.String(), Lineage: r.ID.String(), CompatibilityGeneration: k.config.Provision.Identity.CompatibilityGeneration}
}
func kitGKEName(r assistantRuntimeRecord) string { return "kit-" + r.ID.String() }
func (k *KitGKERuntimeBackend) validate(r assistantRuntimeRecord) error {
	if r.RuntimeVersion != 3 || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil || !k.SupportsBackend(r.Backend) {
		return fmt.Errorf("invalid authoritative Kit GKE runtime binding")
	}
	return k.identity(r).validate()
}

type kitGKEBackendMetadata struct {
	Workload          *KitWorkloadBinding `json:"workload,omitempty"`
	ServiceAccountUID types.UID           `json:"service_account_uid,omitempty"`
	Checkpoint        json.RawMessage     `json:"checkpoint,omitempty"`
	TLSCertificate    []byte              `json:"tls_certificate"`
	Provisioning      bool                `json:"provisioning,omitempty"`
	CheckpointMarker  string              `json:"checkpoint_marker"`
	ProxyImageDigest  string              `json:"proxy_image_digest"`
	ImageDigest       string              `json:"image_digest"`
	ContainerID       string              `json:"container_id"`
	AuthorityUID      types.UID           `json:"authority_uid,omitempty"`
	RuntimeID         uuid.UUID           `json:"runtime_id"`
	AssistantID       uuid.UUID           `json:"assistant_id"`
	ProjectID         uuid.UUID           `json:"project_id"`
	Version           int16               `json:"runtime_version"`
	Namespace         string              `json:"namespace"`
	Fingerprint       string              `json:"compatibility"`
	ClaimUID          types.UID           `json:"claim_uid"`
	TemplateUID       types.UID           `json:"template_uid"`
	SecretUID         types.UID           `json:"secret_uid"`
	SecretName        string              `json:"secret_name"`
	PodName           string              `json:"pod_name"`
	PodUID            types.UID           `json:"pod_uid"`
}

func (k *KitGKERuntimeBackend) metadata(r assistantRuntimeRecord) (kitGKEBackendMetadata, error) {
	var m kitGKEBackendMetadata
	if err := k.validate(r); err != nil {
		return m, err
	}
	if err := json.Unmarshal(r.BackendMetadataJSON, &m); err != nil {
		return m, fmt.Errorf("decode Kit GKE authority: %w", err)
	}
	if m.CheckpointMarker == "" || m.Workload == nil || m.Workload.ServiceAccount == "" || m.Workload.Audience == "" || m.Workload.IssuerID == uuid.Nil || m.Workload.Subject == "" || m.ServiceAccountUID == "" || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.Version != 3 || m.Namespace != k.config.GKE.Namespace || m.Fingerprint != k.fingerprint || m.ClaimUID == "" || m.TemplateUID == "" || m.SecretUID == "" || m.SecretName == "" || (!m.Provisioning && (m.ImageDigest == "" || m.ProxyImageDigest == "" || m.ContainerID == "" || m.PodUID == "" || m.PodName == "")) {
		return m, fmt.Errorf("kit GKE authority or exact compatibility mismatch; explicit fenced reset required")
	}
	return m, nil
}
func (k *KitGKERuntimeBackend) checkObject(r assistantRuntimeRecord, o *unstructured.Unstructured, uid types.UID) error {
	if err := kitCheckIdentity(o, k.identity(r)); err != nil {
		return err
	}
	if o.GetUID() == "" || (uid != "" && uid != o.GetUID()) || o.GetAnnotations()["speakeasy.com/kit-spec"] != k.fingerprint || o.GetLabels()["gram.ai/project-id"] != r.ProjectID.String() {
		return fmt.Errorf("kit GKE resource authority mismatch")
	}
	if o.GetDeletionTimestamp() != nil {
		return fmt.Errorf("kit GKE resource is terminating")
	}
	return nil
}
func (k *KitGKERuntimeBackend) get(ctx context.Context, r assistantRuntimeRecord, gvr schema.GroupVersionResource, name string, uid types.UID) (*unstructured.Unstructured, error) {
	o, err := k.config.GKE.Dynamic.Resource(gvr).Namespace(k.config.GKE.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, kitGKEAdmissionTerminalError{err}
		}
		return nil, fmt.Errorf("kit GKE get: %w", err)
	}
	if err = k.checkObject(r, o, uid); err != nil {
		return nil, kitGKEAdmissionTerminalError{err}
	}
	return o, nil
}

func (k *KitGKERuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (result RuntimeBackendEnsureResult, ensureErr error) {
	if k.config.Lifecycle != nil && r.LifecycleTx == nil {
		err := k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
			var err error
			result, err = k.Ensure(ctx, locked)
			return err
		})
		return result, err
	}
	if err := k.validate(r); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	if r.Retired {
		return RuntimeBackendEnsureResult{}, ErrKitRuntimeBinding
	}
	if !kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		var prior kitGKEBackendMetadata
		if json.Unmarshal(r.BackendMetadataJSON, &prior) == nil && prior.Workload == nil && prior.RuntimeID == r.ID {
			if err := k.invalidateLegacy(ctx, r, prior); err != nil {
				return RuntimeBackendEnsureResult{}, err
			}
			r.BackendMetadataJSON = []byte("{}")
			return k.Ensure(ctx, r)
		}
		m, err := k.metadata(r)
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		if m.Provisioning {
			return k.finishColdProvision(ctx, r, m, true)
		}
		state, obj, e := k.loadAuthority(ctx, r)
		if e != nil {
			return RuntimeBackendEnsureResult{}, e
		}
		if state.Phase != "active" {
			return k.restore(ctx, r, state, obj)
		}
		renewTLS, err := kitGKETLSRenewalDue(state.Metadata.TLSCertificate, kitGKEName(r)+".kit.internal", time.Now())
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		if renewTLS {
			// Renew the actual gateway certificate while settlement can still
			// authenticate. Projected workload token rotation never restarts Kit.
			if err := k.Stop(ctx, r); err != nil {
				return RuntimeBackendEnsureResult{}, fmt.Errorf("checkpoint expiring Kit TLS certificate: %w", err)
			}
			state, obj, err = k.loadAuthority(ctx, r)
			if err != nil {
				return RuntimeBackendEnsureResult{}, err
			}
			return k.restore(ctx, r, state, obj)
		}
		state, obj, err = k.gcSupersededCheckpoints(ctx, state, obj)
		if err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		r.BackendMetadataJSON = obj.raw
		if _, err := k.protocol(ctx, r); err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
		return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: r.BackendMetadataJSON}, nil
	}
	name := kitGKEName(r)
	for _, step := range []struct {
		gvr  schema.GroupVersionResource
		name string
	}{{gkeSandboxClaimGVR, name}, {kitTemplateGVR, name}} {
		_, err := k.config.GKE.Dynamic.Resource(step.gvr).Namespace(k.config.GKE.Namespace).Get(ctx, step.name, metav1.GetOptions{})
		if err == nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("unrecorded Kit GKE resources require fenced operator reconciliation")
		}
		if !apierrors.IsNotFound(err) {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("kit GKE Ensure: %w", err)
		}
	}
	// Once bootstrap may have minted credentials, failures without a durable
	// resume point must revoke them. Durable readiness failures deliberately keep
	// the same credentials: retry authenticates the same owned process, not a
	// replacement process with revoked startup files.
	createdResources := []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{}
	resumable := false
	defer func() {
		if ensureErr != nil && !resumable {
			result = RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: nil}
			fenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := k.config.Fence(fenceCtx, r); err != nil {
				ensureErr = errors.Join(ensureErr, fmt.Errorf("fence failed Kit cold admission: %w", err))
			}
			for _, created := range slices.Backward(createdResources) {
				if err := k.deleteOwned(fenceCtx, created.gvr, created.name, created.uid); err != nil {
					ensureErr = errors.Join(ensureErr, err)
				}
			}
		}
	}()
	b, err := k.config.Bootstrap(ctx, r)
	if b.Workload != nil {
		r.BackendMetadataJSON, _ = json.Marshal(map[string]any{"workload": b.Workload})
	}
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	r.BackendMetadataJSON, _ = json.Marshal(map[string]any{"workload": b.Workload})
	if b.Workload == nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("kit workload binding is required")
	}
	in := k.config.Provision
	in.WorkloadServiceAccount, in.WorkloadAudience = b.Workload.ServiceAccount, b.Workload.Audience
	in.Workload, in.InferenceEndpoint = b.Workload, b.InferenceEndpoint
	in.Identity = k.identity(r)
	in.Name = name
	in.Model = b.Model
	in.AgentsMD = b.AgentsMD
	in.MCPJSON = b.MCPJSON
	in.GatewayToken = b.GatewayToken
	in.CheckpointMarker = []byte(uuid.NewString())
	in.TLSCertificate, in.TLSPrivateKey, err = newKitGKETLSCertificate(in.Name + ".kit.internal")
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	objects, err := RenderKitGKEProvision(in)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	labels := in.Identity.labels()
	if in.SnapshotsEnabled {
		labels["gram.ai/snapshots"] = "enabled"
	}
	labels["gram.ai/project-id"] = r.ProjectID.String()
	annotations := map[string]string{"speakeasy.com/kit-spec": k.fingerprint}
	_ = unstructured.SetNestedStringMap(objects.Template.Object, labels, "spec", "podTemplate", "metadata", "labels")
	podAnnotations, _, _ := unstructured.NestedStringMap(objects.Template.Object, "spec", "podTemplate", "metadata", "annotations")
	if podAnnotations == nil {
		podAnnotations = map[string]string{}
	}
	maps.Copy(podAnnotations, annotations)
	_ = unstructured.SetNestedStringMap(objects.Template.Object, podAnnotations, "spec", "podTemplate", "metadata", "annotations")
	m := kitGKEBackendMetadata{ServiceAccountUID: "", Checkpoint: nil, TLSCertificate: in.TLSCertificate, Provisioning: false, CheckpointMarker: string(in.CheckpointMarker), ImageDigest: "", ProxyImageDigest: "", ContainerID: "", AuthorityUID: "", ClaimUID: "", TemplateUID: "", SecretUID: "", PodName: "", PodUID: "", Workload: b.Workload, SecretName: objects.Secret.GetName(), RuntimeID: r.ID, AssistantID: r.AssistantID, ProjectID: r.ProjectID, Version: 3, Namespace: in.Identity.Namespace, Fingerprint: k.fingerprint}
	for _, step := range []struct {
		gvr schema.GroupVersionResource
		obj *unstructured.Unstructured
		uid *types.UID
	}{{kitServiceAccountGVR, kitWorkloadServiceAccount(in), &m.ServiceAccountUID}, {kitSecretGVR, objects.Secret, &m.SecretUID}, {kitTemplateGVR, objects.Template, &m.TemplateUID}, {gkeSandboxClaimGVR, objects.Claim, &m.ClaimUID}} {
		step.obj.SetLabels(labels)
		step.obj.SetAnnotations(annotations)
		created, e := k.config.GKE.Dynamic.Resource(step.gvr).Namespace(in.Identity.Namespace).Create(ctx, step.obj, metav1.CreateOptions{})
		if e != nil {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("create Kit GKE %s (partial resources require reconciliation): %w", step.obj.GetKind(), e)
		}
		*step.uid = created.GetUID()
		if *step.uid != "" {
			createdResources = append(createdResources, struct {
				gvr  schema.GroupVersionResource
				name string
				uid  types.UID
			}{gvr: step.gvr, name: created.GetName(), uid: *step.uid})
		}
		if *step.uid == "" {
			return RuntimeBackendEnsureResult{}, fmt.Errorf("kit GKE create returned no authoritative UID")
		}
	}
	m.Provisioning = true
	if err := persistKitAdmissionProgress(ctx, m); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	// finishColdProvision owns fencing after this handoff.
	resumable = true
	_, persisted := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter)
	return k.finishColdProvision(ctx, r, m, persisted)
}

// Terminal failures cannot reuse the recorded resources or their startup authority.
type kitGKEAdmissionTerminalError struct{ error }

func (e kitGKEAdmissionTerminalError) Unwrap() error { return e.error }

// The caller persists exact create-response UIDs before waiting for a process.
// Retrying this phase never adopts resources discovered by labels or names.
func (k *KitGKERuntimeBackend) finishColdProvision(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata, resumable bool) (result RuntimeBackendEnsureResult, ensureErr error) {
	r.BackendMetadataJSON, _ = json.Marshal(m)
	defer func() {
		var terminal kitGKEAdmissionTerminalError
		if ensureErr != nil && (!resumable || errors.As(ensureErr, &terminal)) {
			result = RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: nil}
			ensureErr = k.fenceColdAdmissionFailure(ctx, r, ensureErr)
		}
	}()
	for _, step := range []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{{gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID}, {kitTemplateGVR, kitGKEName(r), m.TemplateUID}, {kitSecretGVR, m.SecretName, m.SecretUID}, {kitServiceAccountGVR, m.Workload.ServiceAccount, m.ServiceAccountUID}} {
		if _, err := k.get(ctx, r, step.gvr, step.name, step.uid); err != nil {
			return RuntimeBackendEnsureResult{}, err
		}
	}
	readyCtx, cancel := context.WithTimeout(ctx, gkeRuntimeReadyTimeout)
	defer cancel()
	pod, err := k.waitPod(readyCtx, r, m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	m.ImageDigest, m.ContainerID, err = kitGKEProcessIdentity(pod)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	m.ProxyImageDigest, err = kitGKEProxyImageDigest(pod)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	m.PodName = pod.GetName()
	m.PodUID = pod.GetUID()
	m.Provisioning = false
	m, err = k.createAuthority(readyCtx, r, m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, kitGKEAdmissionTerminalError{err}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("kit GKE Ensure: %w", err)
	}
	if err := persistKitAdmissionProgress(ctx, m); err != nil {
		return RuntimeBackendEnsureResult{}, kitGKEAdmissionTerminalError{err}
	}
	r.BackendMetadataJSON = raw
	for {
		if _, err = k.protocol(readyCtx, r); err == nil {
			break
		}
		select {
		case <-readyCtx.Done():
			return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: raw}, fmt.Errorf("wait for authenticated Kit gateway: %w", err)
		case <-time.After(gkeRuntimePollInterval):
		}
	}
	return RuntimeBackendEnsureResult{ColdStart: true, BackendMetadataJSON: raw}, nil
}

func (k *KitGKERuntimeBackend) waitPod(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata) (*unstructured.Unstructured, error) {
	for {
		if _, err := k.get(ctx, r, gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID); err != nil {
			return nil, err
		}
		pods, err := k.config.GKE.Dynamic.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace).List(ctx, metav1.ListOptions{LabelSelector: gkeClaimUIDLabel + "=" + string(m.ClaimUID)})
		if err != nil {
			return nil, fmt.Errorf("kit GKE waitPod: %w", err)
		}
		if len(pods.Items) > 1 {
			return nil, kitGKEAdmissionTerminalError{fmt.Errorf("ambiguous Kit GKE claim pod authority")}
		}
		if len(pods.Items) == 1 {
			p := &pods.Items[0]
			if err := k.checkObject(r, p, m.PodUID); err != nil {
				return nil, kitGKEAdmissionTerminalError{err}
			}
			statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
			for _, raw := range statuses {
				status, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if restarts, ok := status["restartCount"].(int64); ok && restarts > 0 {
					return nil, kitGKEAdmissionTerminalError{fmt.Errorf("kit GKE process restarted outside admission")}
				}
			}
			phase, _, _ := unstructured.NestedString(p.Object, "status", "phase")
			ip, _, _ := unstructured.NestedString(p.Object, "status", "podIP")
			if phase == "Running" && net.ParseIP(ip) != nil {
				if _, _, e := kitGKEProcessIdentity(p); e == nil {
					if e = k.checkPod(r, p, m); e != nil {
						return nil, kitGKEAdmissionTerminalError{e}
					}
					return p, nil
				}
			}
			if phase == "Failed" || phase == "Succeeded" {
				return nil, kitGKEAdmissionTerminalError{fmt.Errorf("kit GKE process exited; cold replacement requires explicit fenced reset")}
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait Kit GKE pod: %w", ctx.Err())
		case <-time.After(gkeRuntimePollInterval):
		}
	}
}
func (k *KitGKERuntimeBackend) checkPod(r assistantRuntimeRecord, p *unstructured.Unstructured, m kitGKEBackendMetadata) error {
	digest, containerID, err := kitGKEProcessIdentity(p)
	if err != nil {
		return err
	}
	proxyDigest, err := kitGKEProxyImageDigest(p)
	if err != nil {
		return err
	}
	if m.ProxyImageDigest != "" && m.ProxyImageDigest != proxyDigest {
		return fmt.Errorf("kit GKE observed TLS proxy image digest compatibility mismatch")
	}
	if m.ImageDigest != "" && m.ImageDigest != digest {
		return fmt.Errorf("kit GKE observed image digest compatibility mismatch")
	}
	if m.ContainerID != "" && m.ContainerID != containerID {
		return fmt.Errorf("kit GKE process was replaced outside admission")
	}

	if err := k.checkObject(r, p, m.PodUID); err != nil {
		return err
	}
	if (m.PodName != "" && p.GetName() != m.PodName) || p.GetLabels()[gkeClaimUIDLabel] != string(m.ClaimUID) {
		return fmt.Errorf("kit GKE source pod was replaced outside admission")
	}
	serviceAccount, _, _ := unstructured.NestedString(p.Object, "spec", "serviceAccountName")
	automount, found, _ := unstructured.NestedBool(p.Object, "spec", "automountServiceAccountToken")
	if m.Workload == nil || serviceAccount != m.Workload.ServiceAccount || !found || automount {
		return fmt.Errorf("kit workload pod identity mismatch")
	}
	containers, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
	if len(containers) != 2 {
		return fmt.Errorf("unexpected Kit GKE pod containers")
	}
	c, ok := containers[0].(map[string]any)
	if !ok || c["name"] != "kit" || c["image"] != k.ImageRef() {
		return fmt.Errorf("kit GKE pod image compatibility mismatch")
	}
	proxy, ok := containers[1].(map[string]any)
	if !ok || proxy["name"] != "acp-tls" || proxy["image"] != kitGKETLSProxyImage {
		return fmt.Errorf("kit GKE TLS proxy compatibility mismatch")
	}
	return nil
}
func (k *KitGKERuntimeBackend) protocol(ctx context.Context, r assistantRuntimeRecord) (runtimeProtocol, error) {
	return k.protocolForLifecycle(ctx, r, false)
}

// Workload tokens rotate independently of gateway process authority.
func (k *KitGKERuntimeBackend) protocolForLifecycle(ctx context.Context, r assistantRuntimeRecord, _ bool) (runtimeProtocol, error) {
	state, _, err := k.loadAuthority(ctx, r)
	if err != nil {
		return nil, err
	}
	if state.Phase != "active" {
		return nil, fmt.Errorf("kit GKE authority is fenced in phase %s", state.Phase)
	}
	m := state.Metadata
	for _, s := range []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{{gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID}, {kitTemplateGVR, kitGKEName(r), m.TemplateUID}, {kitSecretGVR, m.SecretName, m.SecretUID}, {kitServiceAccountGVR, m.Workload.ServiceAccount, m.ServiceAccountUID}} {
		if _, err = k.get(ctx, r, s.gvr, s.name, s.uid); err != nil {
			return nil, err
		}
	}
	pod, err := k.get(ctx, r, gkePodGVR, m.PodName, m.PodUID)
	if err != nil {
		return nil, err
	}
	if err = k.checkPod(r, pod, m); err != nil {
		return nil, err
	}
	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
	if phase != "Running" || net.ParseIP(ip) == nil {
		return nil, fmt.Errorf("%w: Kit GKE pod is not running", ErrRuntimeUnhealthy)
	}
	p, err := k.config.Protocol(ctx, r, &url.URL{Scheme: "https", Host: net.JoinHostPort(ip, strconv.Itoa(int(k.config.Provision.GuestPort))), Path: "/acp/v2"})
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("kit GKE ACP protocol unavailable")
	}
	// Status authenticates and initializes ACP; Pod Running is not readiness.
	status, err := p.Status(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("kit GKE protocol: %w", err)
	}
	if !status.Configured {
		return nil, fmt.Errorf("kit GKE ACP protocol is not configured")
	}
	return p, nil
}
func (k *KitGKERuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, t runTurnRequest) error {
	p, e := k.protocol(ctx, r)
	if e != nil {
		return e
	}
	if err := p.RunTurn(ctx, r, t); err != nil {
		return fmt.Errorf("run kit GKE turn: %w", err)
	}
	return nil
}
func (k *KitGKERuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, id uuid.UUID) (bool, error) {
	p, e := k.protocol(ctx, r)
	if e != nil {
		return false, e
	}
	interrupted, err := p.InterruptTurn(ctx, r, id)
	if err != nil {
		return false, fmt.Errorf("interrupt kit GKE turn: %w", err)
	}
	return interrupted, nil
}
func (k *KitGKERuntimeBackend) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	p, e := k.protocol(ctx, r)
	if e != nil {
		return RuntimeBackendStatus{}, e
	}
	status, err := p.Status(ctx, r)
	if err != nil {
		return RuntimeBackendStatus{}, fmt.Errorf("get kit GKE status: %w", err)
	}
	return status, nil
}
func (k *KitGKERuntimeBackend) RecycleImage(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	if _, e := k.metadata(r); e != nil {
		return RuntimeBackendRecycleResult{}, e
	}
	if _, e := k.protocol(ctx, r); e != nil {
		return RuntimeBackendRecycleResult{}, e
	}
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}

// kitGKEExplicitImage accepts explicit deployment tags as well as digests.
// Exact live spec and object UIDs remain required on every admission.
func kitGKEExplicitImage(image string) bool {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return false
	}
	// A digest selects immutable content even when a mutable tag is also present.
	if _, digested := named.(reference.Digested); digested {
		return true
	}
	tag, tagged := named.(reference.Tagged)
	return tagged && !strings.EqualFold(tag.Tag(), "latest")
}

// Container identity, not a mutable tag or Pod phase, proves process continuity.
func kitGKEProcessIdentity(p *unstructured.Unstructured) (string, string, error) {
	statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	if len(statuses) != 2 {
		return "", "", fmt.Errorf("kit GKE container readiness is unknown")
	}
	// Kubernetes does not guarantee container status ordering.
	var main, proxy map[string]any
	for _, raw := range statuses {
		status, ok := raw.(map[string]any)
		if !ok {
			return "", "", fmt.Errorf("kit GKE container status invalid")
		}
		switch status["name"] {
		case "kit":
			main = status
		case "acp-tls":
			proxy = status
		}
	}
	if proxy == nil || proxy["ready"] != true || proxy["restartCount"] != int64(0) {
		return "", "", fmt.Errorf("kit GKE TLS proxy is not ready or restarted")
	}
	status, ok := main, main != nil
	if !ok || status["name"] != "kit" || status["ready"] != true {
		return "", "", fmt.Errorf("kit GKE container is not ready")
	}
	restarts, ok := status["restartCount"].(int64)
	if !ok || restarts != 0 {
		return "", "", fmt.Errorf("kit GKE process restarted outside admission")
	}
	digest, err := kitGKEImageDigest(status)
	if err != nil {
		return "", "", err
	}
	container, ok := status["containerID"].(string)
	if !ok || container == "" {
		return "", "", fmt.Errorf("kit GKE process identity missing")
	}
	return digest, container, nil
}

// Explicit release tags are accepted; observed identities are retained across restore.
func kitGKEProxyImageDigest(p *unstructured.Unstructured) (string, error) {
	statuses, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	for _, raw := range statuses {
		status, ok := raw.(map[string]any)
		if ok && status["name"] == "acp-tls" {
			return kitGKEImageDigest(status)
		}
	}
	return "", fmt.Errorf("kit GKE TLS proxy image identity missing")
}

func kitGKEImageDigest(status map[string]any) (string, error) {
	image, ok := status["imageID"].(string)
	if !ok {
		return "", fmt.Errorf("kit GKE observed image identity missing")
	}
	start := strings.LastIndex(image, "sha256:")
	if start < 0 {
		return "", fmt.Errorf("kit GKE observed image has no digest")
	}
	digest := image[start:]
	if len(digest) != 71 {
		return "", fmt.Errorf("kit GKE observed image digest invalid")
	}
	if _, err := hex.DecodeString(digest[7:]); err != nil {
		return "", fmt.Errorf("kit GKE observed image digest invalid")
	}
	return digest, nil
}

// kitAdmissionProgressKey carries the admission owner's durable metadata writer.
// It is deliberately not a discovery/adoption mechanism: only successful create
// responses enter this callback, before readiness can fail or time out.
type kitAdmissionProgressKey struct{}
type kitAdmissionProgressWriter func(context.Context, []byte) error

func persistKitAdmissionProgress(ctx context.Context, m kitGKEBackendMetadata) error {
	write, ok := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter)
	if !ok {
		return nil // Standalone backend callers consume Ensure's result themselves.
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode Kit admission progress: %w", err)
	}
	if err := write(ctx, raw); err != nil {
		return fmt.Errorf("persist Kit admission progress: %w", err)
	}
	return nil
}

func (k *KitGKERuntimeBackend) fenceColdAdmissionFailure(ctx context.Context, r assistantRuntimeRecord, cause error) error {
	fenceCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := k.config.Fence(fenceCtx, r); err != nil {
		return errors.Join(cause, fmt.Errorf("fence failed Kit cold admission: %w", err))
	}
	return cause
}

var kitServiceAccountGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}

func kitWorkloadServiceAccount(in KitGKEProvisionInput) *unstructured.Unstructured {
	o := kitObject("ServiceAccount", "v1", in.WorkloadServiceAccount, in.Identity)
	o.Object["automountServiceAccountToken"] = false
	return o
}

// Legacy startup bearer credentials cannot be admitted as workload authority.
// Verify every recorded UID before fencing and deleting; never discover or adopt
// resources by name alone. The authority CAS makes a completed reset retryable.
func (k *KitGKERuntimeBackend) invalidateLegacy(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata) error {
	if m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.Namespace != k.config.GKE.Namespace || m.Version != 3 || m.Fingerprint == "" {
		return ErrKitRuntimeBinding
	}
	raw, err := k.config.AuthorityStore.Load(ctx, r.LifecycleTx, r)
	if err != nil {
		return fmt.Errorf("load legacy Kit authority: %w", err)
	}
	if !kitGKEEqualMetadata(raw, r.BackendMetadataJSON) {
		return ErrKitRuntimeBinding
	}
	steps := []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{
		{gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID}, {gkePodGVR, m.PodName, m.PodUID},
		{kitTemplateGVR, kitGKEName(r), m.TemplateUID}, {kitSecretGVR, m.SecretName, m.SecretUID},
	}
	for _, step := range steps {
		if step.uid == "" {
			continue
		}
		o, e := k.config.GKE.Dynamic.Resource(step.gvr).Namespace(m.Namespace).Get(ctx, step.name, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			continue
		}
		if e != nil {
			return fmt.Errorf("read legacy Kit resource: %w", e)
		}
		labels := o.GetLabels()
		if o.GetUID() != step.uid || labels["gram.ai/assistant-id"] != r.AssistantID.String() || labels["gram.ai/runtime-lineage"] != r.ID.String() || labels["gram.ai/project-id"] != r.ProjectID.String() || o.GetAnnotations()["speakeasy.com/kit-spec"] != m.Fingerprint {
			return ErrKitRuntimeBinding
		}
	}
	if err = k.config.Fence(ctx, r); err != nil {
		return err
	}
	for _, step := range steps {
		if step.uid != "" {
			if err = k.deleteOwned(ctx, step.gvr, step.name, step.uid); err != nil {
				return err
			}
		}
	}
	var checkpoint kitGKEAuthority
	if len(m.Checkpoint) != 0 {
		if err = json.Unmarshal(m.Checkpoint, &checkpoint); err != nil {
			return fmt.Errorf("decode legacy Kit checkpoint: %w", err)
		}
		refs := slices.Clone(checkpoint.PriorCheckpoints)
		refs = append(refs, kitGKECheckpointRef{TriggerName: checkpoint.TriggerName, TriggerUID: checkpoint.TriggerUID, SnapshotName: checkpoint.SnapshotName, SnapshotUID: checkpoint.SnapshotUID})
		for _, ref := range refs {
			if ref.TriggerUID != "" {
				if err = k.deleteOwned(ctx, kitGKETriggerGVR, ref.TriggerName, ref.TriggerUID); err != nil {
					return err
				}
			}
			if ref.SnapshotUID != "" {
				if err = k.deleteOwned(ctx, kitGKESnapshotGVR, ref.SnapshotName, ref.SnapshotUID); err != nil {
					return err
				}
			}
		}
	}
	if err := k.config.AuthorityStore.Save(ctx, r.LifecycleTx, r, raw, []byte("{}")); err != nil {
		return fmt.Errorf("reset legacy Kit authority: %w", err)
	}
	return nil
}

// The trusted persisted certificate, not inference-token lifetime, controls
// gateway renewal. Already expired or invalid certificates remain fail-closed.
func kitGKETLSRenewalDue(certPEM []byte, serverName string, now time.Time) (bool, error) {
	if _, err := kitGKETLSConfig(certPEM, serverName); err != nil {
		return false, err
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("parse Kit TLS renewal certificate: %w", err)
	}
	return !now.Add(24 * time.Hour).Before(cert.NotAfter), nil
}
