package assistants

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	runtimeBackendATE = "ate"

	// ateInitPort is the port served by kit-ate-init inside the actor sandbox.
	ateInitPort = 8082

	// ateACPPort is the port served by kit serve --remote-acp inside the actor sandbox.
	ateACPPort = 8081

	// ateHelperReadyTimeout bounds how long prepare waits for kit-ate-init or kit serve to become ready.
	ateHelperReadyTimeout = 3 * time.Minute

	// ateHelperPollInterval is the polling cadence while waiting for kit-ate-init readiness endpoints.
	ateHelperPollInterval = 200 * time.Millisecond

	// maxATEHelperErrorBodyBytes bounds drained response bodies from kit-ate-init.
	maxATEHelperErrorBodyBytes = 4 << 10 // 4 KiB
)

type kitATEControl interface {
	GetAtespace(context.Context, *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error)
	GetActorTemplate(context.Context, *ateapiv1.GetActorTemplateRequest) (*ateapiv1.ActorTemplate, error)
	CreateActorTemplate(context.Context, *ateapiv1.CreateActorTemplateRequest) (*ateapiv1.ActorTemplate, error)
	GetActor(context.Context, *ateapiv1.GetActorRequest) (*ateapiv1.Actor, error)
	CreateActor(context.Context, *ateapiv1.CreateActorRequest) (*ateapiv1.Actor, error)
	SuspendActor(context.Context, *ateapiv1.SuspendActorRequest) (*ateapiv1.SuspendActorResponse, error)
	DeleteActor(context.Context, *ateapiv1.DeleteActorRequest) (*ateapiv1.Actor, error)
	GetActorEgressPolicy(context.Context, *ateapiv1.GetActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error)
	CreateActorEgressPolicy(context.Context, *ateapiv1.CreateActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error)
	ActorHTTPClient(string, uint16) (*guardian.HTTPClient, error)
}

var _ kitATEControl = (*ateclient.Client)(nil)

// KitATERuntimeConfig configures the ATE-backed Kit runtime.
type KitATERuntimeConfig struct {
	// EgressHosts are operator-approved DNS hostnames allowed for actor egress.
	EgressHosts []string

	// WorkerSelector holds the label selector applied to created ActorTemplates.
	WorkerSelector map[string]string

	// Revoke withdraws a thread actor's workload admission during reap.
	Revoke func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error

	// CheckSettled verifies the assistant has no unsettled events or wakes before suspend.
	CheckSettled func(context.Context, assistantRuntimeRecord) error

	// Image is the digest-pinned OCI image ref for the ATE Kit runtime.
	Image string

	// Atespace is the pre-created ATE namespace owning templates and thread actors.
	Atespace string

	// Template is the ATE ActorTemplate base name (versioned by image digest).
	Template string

	// ServerURL is the deployment's base URL.
	ServerURL *url.URL

	// Client talks to the ATE control API and actor ingress router.
	Client kitATEControl

	// Bootstrap provisions workload admission and produces the non-secret bootstrap config + ACP token.
	Bootstrap func(context.Context, assistantRuntimeRecord, uuid.UUID, string) (KitLocalBootstrap, error)

	// Token derives the per-thread ACP bearer token for authenticating calls to kit serve.
	Token func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error)

	// Protocol constructs the native Kit ACP protocol driver for a thread actor.
	Protocol func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error)

	// Lifecycle acquires the assistant runtime lock and commits durable metadata progress.
	Lifecycle func(context.Context, assistantRuntimeRecord, func(context.Context, assistantRuntimeRecord) error) error

	// HelperPollInterval overrides the readiness polling interval when positive (for tests).
	HelperPollInterval time.Duration
}

// KitATERuntimeBackend runs one ATE actor per assistant thread over native Kit ACP.
type KitATERuntimeBackend struct {
	config      KitATERuntimeConfig
	imageDigest string
}

type kitATEThread struct {
	ActorName  string              `json:"actor_name"`
	ActorUID   string              `json:"actor_uid"`
	Workload   *KitWorkloadBinding `json:"workload,omitempty"`
	Configured bool                `json:"configured"`
}

type kitATEMetadata struct {
	RuntimeID   uuid.UUID               `json:"runtime_id"`
	AssistantID uuid.UUID               `json:"assistant_id"`
	ProjectID   uuid.UUID               `json:"project_id"`
	ImageDigest string                  `json:"image_digest"`
	Atespace    string                  `json:"atespace"`
	Template    string                  `json:"template"`
	EgressHosts []string                `json:"egress_hosts"`
	Threads     map[string]kitATEThread `json:"threads"`
}

var _ RuntimeBackend = (*KitATERuntimeBackend)(nil)

// NewKitATERuntimeBackend validates configuration and builds a KitATERuntimeBackend.
func NewKitATERuntimeBackend(c KitATERuntimeConfig) (*KitATERuntimeBackend, error) {
	imageDigest, err := kitATEImageDigest(c.Image)
	if err != nil || !kitATENamePattern.MatchString(c.Atespace) || len(c.Atespace) > maxATEDNSLabelLen || !kitATENamePattern.MatchString(c.Template) || len(c.Template) > maxATETemplateBaseLen || c.ServerURL == nil || c.Client == nil || c.Bootstrap == nil || c.Token == nil || c.Protocol == nil || c.Lifecycle == nil || c.Revoke == nil || c.CheckSettled == nil {
		return nil, errors.New("ATE runtime requires a digest-pinned image, actor scope, worker selector, authenticated client, and lifecycle callbacks")
	}
	if err := validateKitATEWorkerSelector(c.WorkerSelector); err != nil {
		return nil, err
	}
	c.WorkerSelector = maps.Clone(c.WorkerSelector)

	c.EgressHosts = append(append([]string(nil), c.EgressHosts...), c.ServerURL.Hostname())
	for i, host := range c.EgressHosts {
		host = strings.ToLower(host)
		if !kitATETrustedDNSHost(host) {
			return nil, errors.New("ATE egress requires explicit DNS hosts without IPs or wildcards")
		}
		c.EgressHosts[i] = host
	}
	slices.Sort(c.EgressHosts)
	c.EgressHosts = slices.Compact(c.EgressHosts)

	if c.HelperPollInterval <= 0 {
		c.HelperPollInterval = ateHelperPollInterval
	}

	return &KitATERuntimeBackend{config: c, imageDigest: imageDigest}, nil
}

func (k *KitATERuntimeBackend) Backend() string               { return runtimeBackendATE }
func (k *KitATERuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendATE }
func (k *KitATERuntimeBackend) ImageRef() string              { return k.config.Image }
func (k *KitATERuntimeBackend) ServerURL() *url.URL           { u := *k.config.ServerURL; return &u }

func (k *KitATERuntimeBackend) templateVersionName() string {
	return kitATETemplateVersionName(k.config.Template, k.imageDigest)
}

func kitATEActorName(r assistantRuntimeRecord, thread uuid.UUID) string {
	sum := sha256.Sum256([]byte(thread.String()))
	return "spk-kit-" + strings.ReplaceAll(r.ID.String(), "-", "") + "-" + hex.EncodeToString(sum[:8])
}

func (k *KitATERuntimeBackend) metadata(r assistantRuntimeRecord) (kitATEMetadata, error) {
	var m kitATEMetadata
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return m, ErrKitRuntimeBinding
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return kitATEMetadata{
			RuntimeID:   r.ID,
			AssistantID: r.AssistantID,
			ProjectID:   r.ProjectID,
			ImageDigest: k.imageDigest,
			Atespace:    k.config.Atespace,
			Template:    k.config.Template,
			EgressHosts: append([]string(nil), k.config.EgressHosts...),
			Threads:     map[string]kitATEThread{},
		}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.Atespace != k.config.Atespace || m.Template != k.config.Template || !ateImageDigestPattern.MatchString(m.ImageDigest) || !slices.Equal(m.EgressHosts, k.config.EgressHosts) || m.Threads == nil {
		return m, ErrKitRuntimeBinding
	}
	for id, t := range m.Threads {
		thread, err := uuid.Parse(id)
		if err != nil || thread == uuid.Nil || t.ActorName != kitATEActorName(r, thread) || t.ActorUID == "" {
			return m, ErrKitRuntimeBinding
		}
	}
	return m, nil
}

// metadataForReap verifies only runtime ownership (runtime, assistant, project
// IDs) so Reap can still revoke workload bindings and delete recorded ATE
// actors after configuration drift (changed egress hosts, template, or atespace).
func (k *KitATERuntimeBackend) metadataForReap(r assistantRuntimeRecord) (kitATEMetadata, error) {
	var m kitATEMetadata
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return m, ErrKitRuntimeBinding
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return kitATEMetadata{
			RuntimeID:   r.ID,
			AssistantID: r.AssistantID,
			ProjectID:   r.ProjectID,
			ImageDigest: "",
			Atespace:    k.config.Atespace,
			Template:    "",
			EgressHosts: nil,
			Threads:     map[string]kitATEThread{},
		}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID {
		return m, ErrKitRuntimeBinding
	}
	return m, nil
}

func saveKitATEMetadata(ctx context.Context, m kitATEMetadata) error {
	write, ok := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter)
	if !ok {
		return errors.New("ATE actor mutation requires a durable lifecycle writer")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode ATE runtime metadata: %w", err)
	}
	return write(ctx, raw)
}

func (k *KitATERuntimeBackend) actorRef(name string) *ateapiv1.ObjectRef {
	return &ateapiv1.ObjectRef{Atespace: k.config.Atespace, Name: name}
}

func (k *KitATERuntimeBackend) getActor(ctx context.Context, t kitATEThread) (*ateapiv1.Actor, error) {
	actor, err := k.config.Client.GetActor(ctx, &ateapiv1.GetActorRequest{Actor: k.actorRef(t.ActorName)})
	if err != nil {
		return nil, fmt.Errorf("ATE GetActor: %w", err)
	}
	meta := actor.GetMetadata()
	tmpl := actor.GetActorTemplate()
	templateOK := kitATETemplateVersionMatches(k.config.Template, tmpl.GetName())
	if t.ActorUID == "" {
		// Adopting an unrecorded actor during initial creation must match the
		// active digest-versioned template verified by ensureTemplate.
		templateOK = tmpl.GetName() == k.templateVersionName()
	}
	if meta.GetAtespace() != k.config.Atespace || meta.GetName() != t.ActorName || meta.GetUid() == "" || (t.ActorUID != "" && meta.GetUid() != t.ActorUID) || tmpl.GetAtespace() != k.config.Atespace || !templateOK {
		return nil, ErrKitRuntimeBinding
	}
	return actor, nil
}

func (k *KitATERuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	m, err := k.metadata(r)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	if r.Retired {
		return RuntimeBackendEnsureResult{}, ErrKitRuntimeBinding
	}
	if err := k.ensureTemplate(ctx); err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	// Update runtime metadata to track the active image digest; existing actors
	// remain on their own digest-versioned template until recreated.
	m.ImageDigest = k.imageDigest
	raw, err := json.Marshal(m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("encode ATE ensure metadata: %w", err)
	}
	return RuntimeBackendEnsureResult{ColdStart: len(m.Threads) == 0, BackendMetadataJSON: raw}, nil
}

func (k *KitATERuntimeBackend) prepare(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (assistantRuntimeRecord, kitATEThread, string, error) {
	var prepared assistantRuntimeRecord
	var entry kitATEThread
	var token string

	err := k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if locked.Retired {
			return ErrKitRuntimeBinding
		}
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}

		var known bool
		entry, known = m.Threads[thread.String()]
		if !known {
			entry.ActorName = kitATEActorName(locked, thread)
			actor, err := k.getActor(ctx, entry)
			if status.Code(err) == codes.NotFound {
				actor, err = k.config.Client.CreateActor(ctx, &ateapiv1.CreateActorRequest{
					Actor: &ateapiv1.Actor{
						Metadata: &ateapiv1.ResourceMetadata{
							Atespace:   k.config.Atespace,
							Name:       entry.ActorName,
							Uid:        "",
							Version:    0,
							CreateTime: nil,
							UpdateTime: nil,
							DeleteTime: nil,
						},
						ActorTemplate:  k.actorRef(k.templateVersionName()),
						WorkerSelector: nil,
						SourceTag:      nil,
						Status:         nil,
					},
				})
				if err != nil {
					return fmt.Errorf("ATE CreateActor: %w", err)
				}
			} else if err != nil {
				return fmt.Errorf("lookup ATE actor: %w", err)
			}
			meta := actor.GetMetadata()
			if meta.GetUid() == "" || meta.GetName() != entry.ActorName || meta.GetAtespace() != k.config.Atespace {
				return ErrKitRuntimeBinding
			}
			entry.ActorUID = meta.GetUid()
			m.Threads[thread.String()] = entry
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
		} else if _, err := k.getActor(ctx, entry); err != nil {
			return err
		}

		token, err = k.config.Token(ctx, locked, thread)
		if err != nil {
			return err
		}

		helper, err := k.config.Client.ActorHTTPClient(entry.ActorName, ateInitPort)
		if err != nil {
			return fmt.Errorf("create ATE bootstrap transport: %w", err)
		}
		defer helper.CloseIdleConnections()

		base, err := ateclient.ActorEndpoint(entry.ActorName, ateInitPort)
		if err != nil {
			return fmt.Errorf("build ATE init endpoint: %w", err)
		}
		base = strings.TrimSuffix(base, "/acp")

		if err := waitKitATEHelper(ctx, helper, base+"/readyz", k.config.HelperPollInterval); err != nil {
			return err
		}

		if !entry.Configured {
			locked.BackendMetadataJSON, err = json.Marshal(m)
			if err != nil {
				return fmt.Errorf("encode ATE bootstrap binding: %w", err)
			}
			bootstrap, err := k.config.Bootstrap(ctx, locked, thread, entry.ActorName)
			if err != nil {
				return err
			}
			entry.Workload = bootstrap.Workload
			m.Threads[thread.String()] = entry
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
			if err := k.ensureEgress(ctx, entry.ActorName, bootstrap); err != nil {
				return err
			}
			archive, err := kitBootstrapArchive(bootstrap)
			if err != nil {
				return err
			}
			code, err := kitATEPost(ctx, helper, base+"/bootstrap", archive)
			if err != nil || (code != http.StatusNoContent && code != http.StatusConflict) {
				return errors.New("ATE Kit bootstrap failed")
			}
			entry.Configured = true
			m.Threads[thread.String()] = entry
			if err := saveKitATEMetadata(ctx, m); err != nil {
				return err
			}
		}

		if entry.Workload == nil || entry.Workload.Audience == "" {
			return ErrKitRuntimeBinding
		}

		if err := waitKitATEHelper(ctx, helper, base+"/kit-readyz", k.config.HelperPollInterval); err != nil {
			return err
		}

		raw, err := json.Marshal(struct {
			kitATEMetadata
			Workload *KitWorkloadBinding `json:"workload"`
		}{m, entry.Workload})
		if err != nil {
			return fmt.Errorf("encode prepared ATE metadata: %w", err)
		}
		prepared = locked
		prepared.LifecycleTx = nil
		prepared.BackendMetadataJSON = raw
		return nil
	})
	return prepared, entry, token, err
}

func waitKitATEHelper(ctx context.Context, client *guardian.HTTPClient, endpoint string, pollInterval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, ateHelperReadyTimeout)
	defer cancel()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("build ATE readiness request: %w", err)
		}

		res, err := client.Do(req)
		if err != nil {
			var connectErr *ateclient.ConnectError
			if !errors.As(err, &connectErr) || !connectErr.Retryable() {
				return errors.New("ATE initialization connection failed")
			}
		}
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
			if res.StatusCode != http.StatusServiceUnavailable {
				return errors.New("ATE initialization endpoint rejected")
			}
		}

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.New("ATE initialization readiness timed out")
		case <-timer.C:
		}
	}
}

func kitATEPost(ctx context.Context, c *guardian.HTTPClient, endpoint string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build ATE bootstrap request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	res, err := c.Do(req)
	if err != nil {
		return 0, errors.New("ATE initialization request failed")
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })

	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
	return res.StatusCode, nil
}

func (k *KitATERuntimeBackend) protocol(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID, t kitATEThread, token string) (runtimeProtocol, error) {
	client, err := k.config.Client.ActorHTTPClient(t.ActorName, ateACPPort)
	if err != nil {
		return nil, fmt.Errorf("create ATE ACP transport: %w", err)
	}
	endpoint, err := ateclient.ActorEndpoint(t.ActorName, ateACPPort)
	if err != nil {
		return nil, fmt.Errorf("build ATE ACP endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse ATE ACP endpoint: %w", err)
	}
	return k.config.Protocol(ctx, r, thread, t.ActorName, u, client, token)
}

func (k *KitATERuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	if turn.ThreadID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	prepared, t, token, err := k.prepare(ctx, r, turn.ThreadID)
	if err != nil {
		return err
	}
	p, err := k.protocol(ctx, prepared, turn.ThreadID, t, token)
	if err != nil {
		return err
	}
	if err := p.RunTurn(ctx, prepared, turn); err != nil {
		return fmt.Errorf("run ATE Kit turn: %w", err)
	}
	return nil
}

func (k *KitATERuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	// Interruption is persisted by the core and consumed by the active projector.
	m, err := k.metadata(r)
	if err != nil {
		return false, err
	}
	t, ok := m.Threads[thread.String()]
	if !ok {
		return false, nil
	}
	var interrupted bool
	err = k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		token, err := k.config.Token(ctx, locked, thread)
		if err != nil {
			return err
		}
		p, err := k.protocol(ctx, locked, thread, t, token)
		if err != nil {
			return err
		}
		interrupted, err = p.InterruptTurn(ctx, locked, thread)
		if err != nil {
			return fmt.Errorf("interrupt ATE Kit turn: %w", err)
		}
		return nil
	})
	return interrupted, err
}

func (k *KitATERuntimeBackend) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	_, err := k.metadata(r)
	return RuntimeBackendStatus{Configured: err == nil, IdleSeconds: nil}, err
}

func (k *KitATERuntimeBackend) RecycleImage(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}

func (k *KitATERuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	return k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}
		if err := k.config.CheckSettled(ctx, locked); err != nil {
			return err
		}

		for id, t := range m.Threads {
			actor, err := k.getActor(ctx, t)
			if err != nil {
				return err
			}
			if actor.GetStatus().GetState() != ateapiv1.ActorState_ACTOR_STATE_SUSPENDED {
				if actor.GetStatus().GetState() != ateapiv1.ActorState_ACTOR_STATE_RUNNING {
					return errors.New("ATE actor is not ready for suspension")
				}
				thread, err := uuid.Parse(id)
				if err != nil {
					return ErrKitRuntimeBinding
				}
				token, err := k.config.Token(ctx, locked, thread)
				if err != nil {
					return err
				}
				protocol, err := k.protocol(ctx, locked, thread, t, token)
				if err != nil {
					return err
				}
				settled, ok := protocol.(interface {
					CloseNativeThreadSession(context.Context, assistantRuntimeRecord, uuid.UUID) error
				})
				if !ok {
					return errors.New("ATE native checkpoint settlement is unavailable")
				}
				if err := settled.CloseNativeThreadSession(ctx, locked, thread); err != nil {
					return fmt.Errorf("settle ATE native thread: %w", err)
				}
				resp, err := k.config.Client.SuspendActor(ctx, &ateapiv1.SuspendActorRequest{
					Actor:   k.actorRef(t.ActorName),
					Options: &ateapiv1.SuspendOptions{Mode: ateapiv1.DurabilityMode_DURABILITY_MODE_SYNCHRONOUS},
				})
				if err != nil {
					return fmt.Errorf("ATE SuspendActor: %w", err)
				}
				actor = resp.GetActor()
			}

			if !hasCompletedDurableRootfsSnapshot(actor, t.ActorUID) {
				return errors.New("ATE actor has no completed durable rootfs snapshot")
			}
		}
		return nil
	})
}

func hasCompletedDurableRootfsSnapshot(actor *ateapiv1.Actor, expectedUID string) bool {
	if actor.GetMetadata().GetUid() != expectedUID || actor.GetStatus().GetState() != ateapiv1.ActorState_ACTOR_STATE_SUSPENDED {
		return false
	}
	generation := actor.GetStatus().GetLastAssignedGeneration()
	for _, snap := range actor.GetStatus().GetSnapshots() {
		if snap.GetGeneration() == generation &&
			snap.GetFidelity() == ateapiv1.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS &&
			snap.GetDurableSnapshot().GetState() == ateapiv1.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED {
			return true
		}
	}
	return false
}

func (k *KitATERuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	return k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		m, err := k.metadataForReap(locked)
		if err != nil {
			return err
		}
		atespace := m.Atespace
		if atespace == "" {
			atespace = k.config.Atespace
		}
		var reapErrs []error
		for id, t := range m.Threads {
			if t.Workload != nil {
				if err := k.config.Revoke(ctx, locked, t.Workload); err != nil {
					reapErrs = append(reapErrs, err)
					continue
				}
			}
			thread, err := uuid.Parse(id)
			if err != nil || thread == uuid.Nil || t.ActorName != kitATEActorName(locked, thread) || t.ActorUID == "" || atespace == "" {
				continue
			}
			ref := &ateapiv1.ObjectRef{Atespace: atespace, Name: t.ActorName}
			actor, err := k.config.Client.GetActor(ctx, &ateapiv1.GetActorRequest{Actor: ref})
			if status.Code(err) == codes.NotFound {
				continue
			}
			if err != nil {
				reapErrs = append(reapErrs, fmt.Errorf("ATE GetActor: %w", err))
				continue
			}
			meta := actor.GetMetadata()
			if meta.GetAtespace() != atespace || meta.GetName() != t.ActorName || meta.GetUid() != t.ActorUID {
				continue
			}
			_, err = k.config.Client.DeleteActor(ctx, &ateapiv1.DeleteActorRequest{
				Actor:   ref,
				Options: &ateapiv1.DeleteOptions{Version: 0, Uid: t.ActorUID},
			})
			if err != nil && status.Code(err) != codes.NotFound {
				reapErrs = append(reapErrs, fmt.Errorf("ATE DeleteActor: %w", err))
			}
		}
		return errors.Join(reapErrs...)
	})
}

func (k *KitATERuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	return k.config.Lifecycle(ctx, r, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if !locked.Retired {
			return errors.New("ATE reusable actor workspace must not be reaped by TTL")
		}
		return k.Reap(ctx, locked)
	})
}

func (k *KitATERuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	m, err := k.metadata(r)
	if err != nil {
		return false, err
	}
	for _, t := range m.Threads {
		_, err := k.getActor(ctx, t)
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func kitATETrustedDNSHost(host string) bool {
	u, err := url.Parse("https://" + host)
	return err == nil && host != "" && strings.Contains(host, ".") && !strings.ContainsAny(host, "/*:@?# \t\r\n") && !strings.HasSuffix(host, ".") && net.ParseIP(host) == nil && u.User == nil && u.Host == host && u.Hostname() == host
}
