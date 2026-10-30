package assistants

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"maps"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/google/uuid"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var kitGKETriggerGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshotmanualtriggers"}
var kitGKESnapshotGVR = schema.GroupVersionResource{Group: "podsnapshot.gke.io", Version: "v1", Resource: "podsnapshots"}

// KitGKECheckpointDriver enforces the filesystem/fresh-process lifecycle contract.
// Managed rootfs-only snapshots preserve writable filesystem state, not process
// memory. A fresh projected bootstrap checks the previous boot's marker before
// launching Kit. The backend authenticates the fresh gateway before publication.
// Provider Ready conditions are documented at:
// https://docs.cloud.google.com/kubernetes-engine/docs/how-to/pod-snapshots
type KitGKECheckpointDriver interface {
	KitGKESnapshotAvailabilityVerifier
	Select(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) (*unstructured.Unstructured, error)
	VerifyRestored(context.Context, *unstructured.Unstructured, *unstructured.Unstructured, KitGKESnapshotBinding) error
}

type kitGKECheckpointRef struct {
	TriggerName  string    `json:"TriggerName"`
	TriggerUID   types.UID `json:"TriggerUID"`
	SnapshotName string    `json:"SnapshotName"`
	SnapshotUID  types.UID `json:"SnapshotUID"`
}

// These tagged persistence types preserve the existing authority JSON format
// without coupling the provisioning input structs to storage serialization.
type kitGKECheckpointIdentity struct {
	Namespace               string `json:"Namespace"`
	AssistantID             string `json:"AssistantID"`
	Lineage                 string `json:"Lineage"`
	CompatibilityGeneration string `json:"CompatibilityGeneration"`
}
type kitGKECheckpointSource struct {
	PolicyName       string                   `json:"policy_name"`
	PolicyUID        types.UID                `json:"policy_uid"`
	PolicyGeneration int64                    `json:"policy_generation"`
	CheckpointMarker string                   `json:"CheckpointMarker"`
	Identity         kitGKECheckpointIdentity `json:"Identity"`
	SourcePodName    string                   `json:"SourcePodName"`
	SourcePodUID     types.UID                `json:"SourcePodUID"`
}

func (s kitGKECheckpointSource) binding() KitGKESnapshotBinding {
	return KitGKESnapshotBinding{PolicyName: s.PolicyName, PolicyUID: s.PolicyUID, PolicyGeneration: s.PolicyGeneration, Identity: KitGKEIdentity(s.Identity), SourcePodName: s.SourcePodName, SourcePodUID: s.SourcePodUID}
}

type kitGKEAuthority struct {
	PriorCheckpoints []kitGKECheckpointRef   `json:"prior_checkpoints,omitempty"`
	Phase            string                  `json:"phase"`
	Metadata         kitGKEBackendMetadata   `json:"metadata"`
	TriggerName      string                  `json:"trigger_name,omitempty"`
	TriggerUID       types.UID               `json:"trigger_uid,omitempty"`
	SnapshotName     string                  `json:"snapshot_name,omitempty"`
	SnapshotUID      types.UID               `json:"snapshot_uid,omitempty"`
	Source           *kitGKECheckpointSource `json:"source,omitempty"`
}

// KitGKEAuthorityStore persists checkpoint state in the runtime row. Writes use
// the caller's lifecycle transaction, never an independent connection.
type KitGKEAuthorityStore interface {
	Load(context.Context, pgx.Tx, assistantRuntimeRecord) ([]byte, error)
	Save(context.Context, pgx.Tx, assistantRuntimeRecord, []byte, []byte) error
}

type kitGKEPostgresAuthorityStore struct{ pool assistantrepo.DBTX }

func NewKitGKEPostgresAuthorityStore(pool assistantrepo.DBTX) KitGKEAuthorityStore {
	return &kitGKEPostgresAuthorityStore{pool: pool}
}
func (s *kitGKEPostgresAuthorityStore) Load(ctx context.Context, tx pgx.Tx, r assistantRuntimeRecord) ([]byte, error) {
	db := s.pool
	if tx != nil {
		db = tx
	}
	if db == nil {
		return nil, fmt.Errorf("kit GKE authority database unavailable")
	}
	row, err := assistantrepo.New(db).GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: r.ID, ProjectID: r.ProjectID})
	if err != nil {
		return nil, fmt.Errorf("get Kit GKE authority runtime: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != r.RuntimeVersion || row.Backend != r.Backend {
		return nil, fmt.Errorf("kit GKE authority identity changed")
	}
	return row.BackendMetadataJson, nil
}
func (s *kitGKEPostgresAuthorityStore) Save(ctx context.Context, tx pgx.Tx, r assistantRuntimeRecord, expected, next []byte) error {
	if tx == nil {
		return fmt.Errorf("kit GKE authority writes require a lifecycle transaction")
	}
	q := assistantrepo.New(tx)
	row, err := q.GetAssistantRuntimeForAdmission(ctx, assistantrepo.GetAssistantRuntimeForAdmissionParams{ID: r.ID, ProjectID: r.ProjectID})
	if err != nil {
		return fmt.Errorf("get Kit GKE authority admission: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != r.RuntimeVersion || row.Backend != r.Backend {
		return fmt.Errorf("kit GKE authority identity changed")
	}
	if !kitGKEEqualMetadata(row.BackendMetadataJson, expected) {
		return fmt.Errorf("kit GKE checkpoint authority changed")
	}
	// Publication must precede the external effect: a process crash cannot
	// roll back the closing fence or the resource identity needed for cleanup.
	publisher, ok := tx.(interface{ Publish(context.Context) error })
	if !ok {
		return fmt.Errorf("kit lifecycle authority requires durable phase publication")
	}
	if err := q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{RuntimeID: r.ID, ProjectID: r.ProjectID, BackendMetadataJson: next}); err != nil {
		return fmt.Errorf("update Kit GKE authority metadata: %w", err)
	}
	if err := publisher.Publish(ctx); err != nil {
		return fmt.Errorf("publish Kit GKE authority: %w", err)
	}
	return nil
}
func kitGKEEqualMetadata(a, b []byte) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

type kitGKEAuthorityHandle struct {
	record assistantRuntimeRecord
	raw    []byte
}

// checkpoint contains only lifecycle state; resource identity remains in the
// top-level metadata so ordinary metadata updates cannot recursively nest it.
func kitGKEAuthorityMetadata(state kitGKEAuthority) (kitGKEBackendMetadata, error) {
	m := state.Metadata
	state.Metadata = kitGKEBackendMetadata{WarmPoolName: "", WarmPoolUID: "", TemplateName: "", Workload: nil, ServiceAccountUID: "", Checkpoint: nil, TLSCertificate: nil, Provisioning: false, CheckpointMarker: "", ProxyImageDigest: "", ImageDigest: "", ContainerID: "", AuthorityUID: "", RuntimeID: uuid.Nil, AssistantID: uuid.Nil, ProjectID: uuid.Nil, Version: 0, Namespace: "", Fingerprint: "", ClaimUID: "", TemplateUID: "", SecretUID: "", SecretName: "", PodName: "", PodUID: ""}
	raw, err := json.Marshal(state)
	if err != nil {
		return m, fmt.Errorf("marshal Kit GKE authority state: %w", err)
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return m, fmt.Errorf("decode Kit GKE authority envelope: %w", err)
	}
	delete(envelope, "metadata")
	m.Checkpoint, err = json.Marshal(envelope)
	if err != nil {
		return m, fmt.Errorf("marshal Kit GKE checkpoint: %w", err)
	}
	return m, nil
}
func (k *KitGKERuntimeBackend) createAuthority(ctx context.Context, r assistantRuntimeRecord, m kitGKEBackendMetadata) (kitGKEBackendMetadata, error) {
	if k.config.AuthorityStore == nil {
		return m, fmt.Errorf("kit GKE requires a Postgres authority store")
	}
	expected, err := k.config.AuthorityStore.Load(ctx, r.LifecycleTx, r)
	if err != nil {
		return m, fmt.Errorf("load prior Kit GKE authority: %w", err)
	}
	var prior kitGKEBackendMetadata
	if err = json.Unmarshal(expected, &prior); err != nil {
		return m, fmt.Errorf("decode prior Kit GKE authority: %w", err)
	}
	if len(prior.Checkpoint) != 0 {
		return m, fmt.Errorf("kit GKE checkpoint authority already exists")
	}
	m.AuthorityUID = types.UID(uuid.NewString())
	m, err = kitGKEAuthorityMetadata(kitGKEAuthority{PriorCheckpoints: nil, TriggerName: "", TriggerUID: "", SnapshotName: "", SnapshotUID: "", Source: nil, Phase: "active", Metadata: m})
	if err != nil {
		return m, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return m, fmt.Errorf("marshal new Kit GKE authority: %w", err)
	}
	if err := k.config.AuthorityStore.Save(ctx, r.LifecycleTx, r, expected, raw); err != nil {
		return m, fmt.Errorf("save new Kit GKE authority: %w", err)
	}
	return m, nil
}
func (k *KitGKERuntimeBackend) loadAuthority(ctx context.Context, r assistantRuntimeRecord) (kitGKEAuthority, *kitGKEAuthorityHandle, error) {
	var state kitGKEAuthority
	if k.config.AuthorityStore == nil {
		return state, nil, fmt.Errorf("kit GKE requires a Postgres authority store")
	}
	raw, err := k.config.AuthorityStore.Load(ctx, r.LifecycleTx, r)
	if err != nil {
		return state, nil, fmt.Errorf("load Kit GKE authority: %w", err)
	}
	record := r
	record.BackendMetadataJSON = raw
	m, err := k.metadata(record)
	if err != nil {
		return state, nil, err
	}
	if m.AuthorityUID == "" || len(m.Checkpoint) == 0 {
		return state, nil, fmt.Errorf("kit GKE runtime has no persisted checkpoint authority")
	}
	if err = json.Unmarshal(m.Checkpoint, &state); err != nil {
		return state, nil, fmt.Errorf("kit GKE checkpoint authority malformed: %w", err)
	}
	state.Metadata = m
	switch state.Phase {
	case "active", "closing", "checkpointing", "checkpointed", "stopped", "restoring", "retired":
	default:
		return state, nil, fmt.Errorf("unknown Kit GKE checkpoint authority phase")
	}
	return state, &kitGKEAuthorityHandle{record: r, raw: raw}, nil
}
func (k *KitGKERuntimeBackend) saveAuthority(ctx context.Context, obj *kitGKEAuthorityHandle, state kitGKEAuthority) (*kitGKEAuthorityHandle, error) {
	if obj == nil || k.config.AuthorityStore == nil {
		return nil, fmt.Errorf("kit GKE authority handle unavailable")
	}
	m, err := kitGKEAuthorityMetadata(state)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal updated Kit GKE authority: %w", err)
	}
	if err = k.config.AuthorityStore.Save(ctx, obj.record.LifecycleTx, obj.record, obj.raw, raw); err != nil {
		return nil, fmt.Errorf("save Kit GKE authority: %w", err)
	}
	return &kitGKEAuthorityHandle{record: obj.record, raw: raw}, nil
}

func (k *KitGKERuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	// Idle expiry may already hold admission without passing through Lifecycle.
	// Bridge the explicit transaction for legacy protocol settlement helpers.
	if r.LifecycleTx != nil {
		ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, assistantrepo.New(r.LifecycleTx))
	}
	if k.config.Lifecycle != nil && r.LifecycleTx == nil {
		return k.config.Lifecycle(ctx, r, k.Stop)
	}
	if !k.config.Provision.SnapshotsEnabled || k.config.Snapshots == nil {
		return fmt.Errorf("%s", kitGKECheckpointUnavailable)
	}
	state, obj, err := k.loadAuthority(ctx, r)
	if err != nil {
		return err
	}
	if state.Phase == "stopped" {
		_, _, err = k.gcSupersededCheckpoints(ctx, state, obj)
		return err
	}
	if state.Phase == "closing" {
		return fmt.Errorf("kit GKE session close acknowledgement is uncertain; explicit reconciliation required")
	}
	if state.Phase == "restoring" || state.Phase == "retired" {
		return fmt.Errorf("cannot checkpoint Kit GKE phase %s", state.Phase)
	}
	if state.Phase == "active" {
		r.BackendMetadataJSON, _ = json.Marshal(state.Metadata)
		p, err := k.protocolForLifecycle(ctx, r, true)
		if err != nil {
			return err
		}
		coordinator, ok := p.(runtimeSwitchCoordinator)
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		var assistant assistantRecord
		assistant.ID = r.AssistantID
		assistant.ProjectID = r.ProjectID
		readiness, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, []assistantRuntimeRecord{r}, 3)
		if readiness.Release != nil {
			defer readiness.Release()
		}
		if err != nil {
			return fmt.Errorf("kit GKE Stop: %w", err)
		}
		if readiness.Release == nil {
			return errRuntimeSwitchUnavailable
		}
		if !readiness.Settled {
			return errRuntimeSwitchBusy
		}
		pod, e := k.get(ctx, r, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID)
		if e != nil {
			return e
		}
		if e = k.checkPod(r, pod, state.Metadata); e != nil {
			return e
		}
		settlement, ok := p.(interface {
			VerifyCheckpointSettled(context.Context, assistantRuntimeRecord) error
		})
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		if err := settlement.VerifyCheckpointSettled(ctx, r); err != nil {
			return fmt.Errorf("verify Kit checkpoint settlement: %w", err)
		}
		// Persist the admission fence before revoking credentials or triggering a
		// snapshot. Retried operations cannot re-admit a checkpointing process.
		if state.SnapshotName != "" {
			state.PriorCheckpoints = append(state.PriorCheckpoints, kitGKECheckpointRef{state.TriggerName, state.TriggerUID, state.SnapshotName, state.SnapshotUID})
		}
		state.TriggerUID = ""
		state.SnapshotName = ""
		state.SnapshotUID = ""
		state.Phase = "closing"
		state.TriggerName = "kit-checkpoint-" + uuid.NewString()
		state.Source = &kitGKECheckpointSource{PolicyName: "", PolicyUID: "", PolicyGeneration: 0, CheckpointMarker: state.Metadata.CheckpointMarker, Identity: kitGKECheckpointIdentity(k.identity(r)), SourcePodName: state.Metadata.PodName, SourcePodUID: state.Metadata.PodUID}
		if driver, ok := k.config.Snapshots.(*KitGKEManagedCheckpointDriver); ok {
			policy, err := driver.checkpointPolicy(ctx, pod)
			if err != nil {
				return err
			}
			state.Source.PolicyName, state.Source.PolicyUID, state.Source.PolicyGeneration = policy.GetName(), policy.GetUID(), policy.GetGeneration()
		}
		closer, ok := p.(interface {
			CloseCheckpointSessions(context.Context, assistantRuntimeRecord) error
		})
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		// A lost close acknowledgement cannot be distinguished from an old
		// restorable transcript. Persist a non-retryable phase before sending
		// close; only a successful acknowledgement permits checkpointing.
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
		if err = closer.CloseCheckpointSessions(ctx, r); err != nil {
			return fmt.Errorf("durably close Kit checkpoint sessions: %w", err)
		}
		state.Phase = "checkpointing"
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
	}
	if state.Phase == "checkpointing" {
		if state.Source == nil || state.TriggerName == "" {
			return fmt.Errorf("kit GKE checkpoint source authority missing")
		}
		if err = k.config.Fence(ctx, r); err != nil {
			return err
		}
		pod, err := k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID)
		if err != nil {
			return err
		}
		trigger, err := RenderKitGKEManualTrigger(true, state.TriggerName, state.Source.binding(), pod)
		if err != nil {
			return err
		}
		resource := k.config.GKE.Dynamic.Resource(kitGKETriggerGVR).Namespace(k.config.GKE.Namespace)
		if state.TriggerUID == "" {
			// A crash after Create before CAS deliberately requires operator repair;
			// an unrecorded trigger may not be adopted on labels alone.
			trigger, err = resource.Create(ctx, trigger, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("kit GKE Stop: %w", err)
			}
			state.TriggerUID = trigger.GetUID()
			if state.TriggerUID == "" {
				return fmt.Errorf("kit GKE checkpoint trigger has no UID")
			}
			obj, err = k.saveAuthority(ctx, obj, state)
			if err != nil {
				return err
			}
		} else {
			trigger, err = resource.Get(ctx, state.TriggerName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("kit GKE Stop: %w", err)
			}
			if trigger.GetUID() != state.TriggerUID {
				return fmt.Errorf("kit GKE trigger authority mismatch")
			}
		}
		selected, err := k.config.Snapshots.Select(ctx, trigger, state.Source.binding())
		if err != nil {
			return fmt.Errorf("kit GKE Stop: %w", err)
		}
		verified, err := VerifyKitGKESnapshot(ctx, selected, state.Source.binding(), k.config.Snapshots)
		if err != nil {
			return err
		}
		if _, err = k.get(ctx, r, gkePodGVR, state.Source.SourcePodName, state.Source.SourcePodUID); err != nil {
			return err
		}
		if selected.GetUID() == "" {
			return fmt.Errorf("kit GKE selected snapshot has no UID")
		}
		state.SnapshotName = verified.Name()
		state.SnapshotUID = selected.GetUID()
		state.Phase = "checkpointed"
		obj, err = k.saveAuthority(ctx, obj, state)
		if err != nil {
			return err
		}
	}
	// Revalidate selected backing data even on retries after a prior checkpoint.
	if state.Source == nil || state.SnapshotName == "" || state.SnapshotUID == "" {
		return fmt.Errorf("kit GKE checkpoint evidence missing")
	}
	selected, e := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if e != nil {
		return fmt.Errorf("kit GKE Stop: %w", e)
	}
	if selected.GetUID() != state.SnapshotUID {
		return fmt.Errorf("kit GKE checkpoint snapshot replaced")
	}
	if _, e = VerifyKitGKESnapshot(ctx, selected, state.Source.binding(), k.config.Snapshots); e != nil {
		return e
	}
	state, obj, err = k.gcSupersededCheckpoints(ctx, state, obj)
	if err != nil {
		return err
	}
	// Durable verified authority precedes deletion. Foreground deletion is waited
	// through; no success is returned while the source process still exists.
	if err = k.deleteOwned(ctx, kitClaimGVR, kitGKEName(r), state.Metadata.ClaimUID); err != nil {
		return err
	}
	if err = k.deleteOwned(ctx, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID); err != nil {
		return err
	}
	state.Phase = "stopped"
	_, err = k.saveAuthority(ctx, obj, state)
	return err
}

func (k *KitGKERuntimeBackend) restore(ctx context.Context, r assistantRuntimeRecord, state kitGKEAuthority, obj *kitGKEAuthorityHandle) (_ RuntimeBackendEnsureResult, returnErr error) {
	var empty RuntimeBackendEnsureResult
	var gcErr error
	state, obj, gcErr = k.gcSupersededCheckpoints(ctx, state, obj)
	if gcErr != nil {
		return empty, gcErr
	}
	if k.config.Snapshots == nil {
		return empty, fmt.Errorf("%s", kitGKECheckpointUnavailable)
	}
	if state.Phase != "stopped" {
		return empty, fmt.Errorf("kit GKE phase %s is not safely restorable; finish checkpoint or reconcile fenced attempt", state.Phase)
	}
	if state.Source == nil || state.SnapshotUID == "" || state.SnapshotName == "" {
		return empty, fmt.Errorf("kit GKE restore authority missing")
	}
	snapshot, err := k.config.GKE.Dynamic.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(ctx, state.SnapshotName, metav1.GetOptions{})
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if snapshot.GetUID() != state.SnapshotUID {
		return empty, fmt.Errorf("kit GKE restore snapshot UID mismatch")
	}
	verified, err := VerifyKitGKESnapshot(ctx, snapshot, state.Source.binding(), k.config.Snapshots)
	if err != nil {
		return empty, err
	}
	if _, err = k.config.GKE.Dynamic.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Get(ctx, kitGKEName(r), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		if err != nil {
			return empty, fmt.Errorf("check kit GKE source claim deletion: %w", err)
		}
		return empty, fmt.Errorf("kit GKE restore requires source claim deletion")
	}
	if _, err = k.get(ctx, r, kitWarmPoolGVR, state.Metadata.WarmPoolName, state.Metadata.WarmPoolUID); err != nil {
		return empty, err
	}
	_, err = k.get(ctx, r, kitTemplateGVR, state.Metadata.templateName(r), state.Metadata.TemplateUID)
	if err != nil {
		return empty, err
	}
	if state.Source.CheckpointMarker == "" {
		return empty, fmt.Errorf("checkpoint has no rootfs marker; legacy restore is unsupported")
	}
	if _, err = k.get(ctx, r, kitServiceAccountGVR, state.Metadata.Workload.ServiceAccount, state.Metadata.ServiceAccountUID); err != nil {
		return empty, err
	}
	var createdSAName string
	var createdSAUID types.UID
	serviceAccountPersisted := false
	// Stopped already fences admission and the source claim is gone. Keep that
	// retryable phase through preparation: no Kubernetes resource has changed.
	// Bootstrap may mint authority even when its result is an error. Revoke that
	// authority on every failed restore, including failures after the fresh Pod
	// starts and caller cancellation. Keep the durable phase fenced for recovery.
	defer func() {
		if returnErr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if fenceErr := k.config.Fence(cleanupCtx, r); fenceErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("revoke failed restore bootstrap: %w", fenceErr))
			}
			if createdSAUID != "" && !serviceAccountPersisted {
				returnErr = errors.Join(returnErr, k.deleteOwned(cleanupCtx, kitServiceAccountGVR, createdSAName, createdSAUID))
			}
		}
	}()
	b, err := k.config.Bootstrap(ctx, r)
	if b.Workload != nil {
		r.BackendMetadataJSON, _ = json.Marshal(map[string]any{"workload": b.Workload})
	}
	if err != nil {
		return empty, err
	}
	r.BackendMetadataJSON, _ = json.Marshal(map[string]any{"workload": b.Workload})
	if b.Workload == nil || state.Metadata.Workload == nil || b.Workload.ServiceAccount == state.Metadata.Workload.ServiceAccount {
		return empty, fmt.Errorf("restore requires a fresh workload service account")
	}
	if err = validateKitBootstrap(b); err != nil {
		return empty, err
	}
	previousBootstrap, err := k.get(ctx, r, kitSecretGVR, state.Metadata.SecretName, state.Metadata.SecretUID)
	if err != nil {
		return empty, err
	}
	oldGateway, found, err := unstructured.NestedString(previousBootstrap.Object, "data", "gateway-token")
	if err != nil || !found || oldGateway == base64.StdEncoding.EncodeToString(b.GatewayToken) {
		return empty, fmt.Errorf("restore requires a fresh gateway authentication token")
	}
	in := k.config.Provision
	in.WorkloadServiceAccount, in.WorkloadAudience = b.Workload.ServiceAccount, b.Workload.Audience
	in.Workload, in.InferenceEndpoint = b.Workload, b.InferenceEndpoint
	in.Identity, in.Name, in.Model = k.identity(r), kitGKEName(r), b.Model
	in.AgentsMD, in.MCPJSON, in.GatewayToken = b.AgentsMD, b.MCPJSON, b.GatewayToken
	in.CheckpointMarker = []byte(uuid.NewString())
	in.ExpectedCheckpointMarker = []byte(state.Source.CheckpointMarker)
	in.Restore, in.RestoreSource = verified, &verified.binding
	in.TLSCertificate, in.TLSPrivateKey, err = newKitGKETLSCertificate(in.Name + ".kit.internal")
	if err != nil {
		return empty, err
	}
	objects, err := RenderKitGKEProvision(in)
	if err != nil {
		return empty, err
	}
	labels := k.identity(r).labels()
	labels["gram.ai/project-id"] = r.ProjectID.String()
	if in.SnapshotsEnabled {
		labels["gram.ai/snapshots"] = "enabled"
	}
	annotations := map[string]string{"speakeasy.com/kit-spec": k.fingerprint}
	objects.Secret.SetLabels(labels)
	objects.Secret.SetAnnotations(annotations)
	// Cross the irreversible-resource boundary only after preparation succeeds.
	// A failed preparation leaves the exact source snapshot/UIDs unchanged and
	// the next Ensure can safely mint a different fresh bootstrap and retry.
	state.Phase = "restoring"
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	sa := k.workloadServiceAccount(in)
	sa.SetLabels(labels)
	saAnnotations := sa.GetAnnotations()
	if saAnnotations == nil {
		saAnnotations = map[string]string{}
	}
	maps.Copy(saAnnotations, annotations)
	sa.SetAnnotations(saAnnotations)
	sa, err = k.config.GKE.Dynamic.Resource(kitServiceAccountGVR).Namespace(k.config.GKE.Namespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create restore workload service account: %w", err)
	}
	if sa.GetUID() == "" {
		return empty, fmt.Errorf("restore workload service account lacks UID")
	}
	createdSAName, createdSAUID = sa.GetName(), sa.GetUID()
	oldWorkload, oldSAUID := state.Metadata.Workload, state.Metadata.ServiceAccountUID
	state.Metadata.Workload, state.Metadata.ServiceAccountUID = b.Workload, sa.GetUID()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	// Once published, the exact UID is durable recovery/reap authority.
	serviceAccountPersisted = true
	if err = k.deleteOwned(ctx, kitServiceAccountGVR, oldWorkload.ServiceAccount, oldSAUID); err != nil {
		return empty, err
	}
	secret, err := k.config.GKE.Dynamic.Resource(kitSecretGVR).Namespace(k.config.GKE.Namespace).Create(ctx, objects.Secret, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create fresh restore bootstrap: %w", err)
	}
	if secret.GetUID() == "" {
		return empty, fmt.Errorf("fresh restore bootstrap lacks UID")
	}
	oldSecretName, oldSecretUID := state.Metadata.SecretName, state.Metadata.SecretUID
	state.Metadata.SecretName, state.Metadata.SecretUID = secret.GetName(), secret.GetUID()
	state.Metadata.CheckpointMarker = string(in.CheckpointMarker)
	state.Metadata.TLSCertificate = in.TLSCertificate
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	if err = k.deleteOwned(ctx, kitSecretGVR, oldSecretName, oldSecretUID); err != nil {
		return empty, err
	}
	// SandboxTemplate's spec is immutable. Replace it only after its exact UID
	// has been deleted; the restoring fence prevents concurrent admission.
	if err = k.deleteOwned(ctx, kitWarmPoolGVR, state.Metadata.WarmPoolName, state.Metadata.WarmPoolUID); err != nil {
		return empty, err
	}
	if err = k.deleteOwned(ctx, kitTemplateGVR, state.Metadata.templateName(r), state.Metadata.TemplateUID); err != nil {
		return empty, err
	}
	template := objects.Template
	template.SetLabels(labels)
	template.SetAnnotations(annotations)
	if err = unstructured.SetNestedStringMap(template.Object, labels, "spec", "podTemplate", "metadata", "labels"); err != nil {
		return empty, fmt.Errorf("set restored pod labels: %w", err)
	}
	if err = unstructured.SetNestedField(template.Object, k.fingerprint, "spec", "podTemplate", "metadata", "annotations", "speakeasy.com/kit-spec"); err != nil {
		return empty, fmt.Errorf("set restored pod fingerprint: %w", err)
	}
	template, err = k.config.GKE.Dynamic.Resource(kitTemplateGVR).Namespace(k.config.GKE.Namespace).Create(ctx, template, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create fresh restore template: %w", err)
	}
	if template.GetUID() == "" {
		return empty, fmt.Errorf("fresh restore template lacks UID")
	}
	state.Metadata.TemplateUID = template.GetUID()
	state.Metadata.TemplateName = template.GetName()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	pool := objects.WarmPool
	pool.SetLabels(labels)
	pool.SetAnnotations(annotations)
	pool, err = k.config.GKE.Dynamic.Resource(kitWarmPoolGVR).Namespace(k.config.GKE.Namespace).Create(ctx, pool, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("create fresh restore warm pool: %w", err)
	}
	if pool.GetUID() == "" {
		return empty, fmt.Errorf("fresh restore warm pool lacks UID")
	}
	state.Metadata.WarmPoolName, state.Metadata.WarmPoolUID = pool.GetName(), pool.GetUID()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	claim := objects.Claim
	claim.SetLabels(labels)
	claim.SetAnnotations(annotations)
	claim, err = k.config.GKE.Dynamic.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if claim.GetUID() == "" {
		return empty, fmt.Errorf("kit GKE restored claim lacks UID")
	}
	state.Metadata.ClaimUID = claim.GetUID()
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	pending := state.Metadata
	pending.PodUID = ""
	pending.ContainerID = ""
	pending.PodName = ""
	readyCtx, cancel := context.WithTimeout(ctx, gkeRuntimeReadyTimeout)
	defer cancel()
	pod, err := k.waitPod(readyCtx, r, pending)
	if err != nil {
		return empty, err
	}
	state.Metadata.PodName = pod.GetName()
	state.Metadata.PodUID = pod.GetUID()
	_, state.Metadata.ContainerID, err = kitGKEProcessIdentity(pod)
	if err != nil {
		return empty, err
	}
	obj, err = k.saveAuthority(ctx, obj, state)
	if err != nil {
		return empty, err
	}
	if err = k.config.Snapshots.VerifyRestored(ctx, pod, snapshot, state.Source.binding()); err != nil {
		return empty, fmt.Errorf("kit GKE restore: %w", err)
	}
	if _, err = VerifyKitGKESnapshot(ctx, snapshot, state.Source.binding(), k.config.Snapshots); err != nil {
		return empty, err
	}
	pod, err = k.get(ctx, r, gkePodGVR, state.Metadata.PodName, state.Metadata.PodUID)
	if err != nil {
		return empty, err
	}
	if err = k.checkPod(r, pod, state.Metadata); err != nil {
		return empty, err
	}
	digest, _, e := kitGKEProcessIdentity(pod)
	if e != nil {
		return empty, e
	}
	if digest != state.Metadata.ImageDigest {
		return empty, fmt.Errorf("cold restored image changed")
	}
	_, state.Metadata.ContainerID, err = kitGKEProcessIdentity(pod)
	if err != nil {
		return empty, err
	}
	state.Metadata.PodName = pod.GetName()
	state.Metadata.PodUID = pod.GetUID()
	raw, err := json.Marshal(state.Metadata)
	if err != nil {
		return empty, fmt.Errorf("kit GKE restore metadata: %w", err)
	}
	r.BackendMetadataJSON = raw
	ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
	if net.ParseIP(ip) == nil {
		return empty, fmt.Errorf("restored Pod has no valid IP")
	}
	protocol, err := k.config.Protocol(readyCtx, r, &url.URL{Scheme: "https", Host: net.JoinHostPort(ip, strconv.Itoa(int(k.config.Provision.GuestPort))), Path: "/acp/v2"})
	if err != nil {
		return empty, err
	}
	if protocol == nil {
		return empty, fmt.Errorf("fresh restore protocol unavailable")
	}
	// Running precedes the entrypoint's marker check and gateway listener.
	// Retry initialization under the restore fence, never publish on factory success.
	for {
		status, statusErr := protocol.Status(readyCtx, r)
		if statusErr == nil && status.Configured {
			break
		}
		select {
		case <-readyCtx.Done():
			if statusErr != nil {
				return empty, fmt.Errorf("authenticate fresh restored gateway: %w", statusErr)
			}
			return empty, fmt.Errorf("fresh restored gateway is not configured: %w", readyCtx.Err())
		case <-time.After(gkeRuntimePollInterval):
		}
	}
	// Only authenticated startup proves the marker gate ran. Until this point
	// all turn admission remains fenced, including silent cold-start fallback.
	state.Phase = "active"
	if obj, err = k.saveAuthority(ctx, obj, state); err != nil {
		return empty, err
	}
	return RuntimeBackendEnsureResult{ColdStart: false, BackendMetadataJSON: obj.raw}, nil
}
