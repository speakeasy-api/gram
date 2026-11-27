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

	// ateActorWarmWindow keeps a thread actor warm after activity before idle sweep suspends it.
	ateActorWarmWindow = 10 * time.Minute
)

type kitATEControl interface {
	GetAtespace(context.Context, *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error)
	CreateAtespace(context.Context, *ateapiv1.CreateAtespaceRequest) (*ateapiv1.Atespace, error)
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

	// Revoke withdraws a thread actor's workload admission during reap.
	Revoke func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error

	// CheckSettled verifies the assistant has no unsettled events or wakes before suspend.
	CheckSettled func(context.Context, assistantRuntimeRecord) error

	// Image is the digest-pinned OCI image ref for the ATE Kit runtime.
	Image string

	// Atespace is the ATE namespace owning templates and thread actors.
	Atespace string

	// Template is the ATE ActorTemplate name used to create thread actors.
	Template string

	// ServerURL is the deployment's base URL.
	ServerURL *url.URL

	// Client talks to the ATE control API and actor ingress router.
	Client kitATEControl

	// Bootstrap provisions workload admission and produces the non-secret bootstrap config + ACP token.
	Bootstrap func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *KitWorkloadBinding) (KitLocalBootstrap, error)

	// Token derives the per-thread ACP bearer token for authenticating calls to kit serve.
	Token func(context.Context, assistantRuntimeRecord, uuid.UUID) (string, error)

	// Protocol constructs the native Kit ACP protocol driver for a thread actor.
	Protocol func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error)

	// Lifecycle acquires the assistant-wide runtime lock for runtime-scoped Stop and Reap operations.
	Lifecycle func(context.Context, assistantRuntimeRecord, func(context.Context, assistantRuntimeRecord) error) error

	// ThreadLifecycle acquires only the per-thread lock and actor row lock so sibling threads do not serialize.
	ThreadLifecycle func(context.Context, assistantRuntimeRecord, uuid.UUID, func(context.Context, assistantRuntimeRecord) error) error

	// Store persists durable per-thread ATE actor rows in assistant_ate_actors.
	Store kitATEActorStore

	// Now returns the current time when stamping per-thread warm_until deadlines (defaults to time.Now).
	Now func() time.Time

	// HelperPollInterval overrides the readiness polling interval when positive (for tests).
	HelperPollInterval time.Duration
}

// KitATERuntimeBackend runs one ATE actor per assistant thread over native Kit ACP.
type KitATERuntimeBackend struct{ config KitATERuntimeConfig }

type kitATEMetadata struct {
	RuntimeID   uuid.UUID `json:"runtime_id"`
	AssistantID uuid.UUID `json:"assistant_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	Image       string    `json:"image"`
	Atespace    string    `json:"atespace"`
	Template    string    `json:"template"`
	EgressHosts []string  `json:"egress_hosts"`
}

var _ RuntimeBackend = (*KitATERuntimeBackend)(nil)

// NewKitATERuntimeBackend validates configuration and builds a KitATERuntimeBackend.
func NewKitATERuntimeBackend(c KitATERuntimeConfig) (*KitATERuntimeBackend, error) {
	if !kitGKEExplicitImage(c.Image) || !strings.Contains(c.Image, "@sha256:") || !kitATENamePattern.MatchString(c.Atespace) || !kitATENamePattern.MatchString(c.Template) || c.ServerURL == nil || c.Client == nil || c.Bootstrap == nil || c.Token == nil || c.Protocol == nil || c.Lifecycle == nil || c.ThreadLifecycle == nil || c.Store == nil || c.Revoke == nil || c.CheckSettled == nil {
		return nil, errors.New("ATE runtime requires a digest-pinned image, actor scope, authenticated client, actor store, and lifecycle callbacks")
	}

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
	if c.Now == nil {
		c.Now = time.Now
	}

	return &KitATERuntimeBackend{config: c}, nil
}

func (k *KitATERuntimeBackend) Backend() string               { return runtimeBackendATE }
func (k *KitATERuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendATE }
func (k *KitATERuntimeBackend) ImageRef() string              { return k.config.Image }
func (k *KitATERuntimeBackend) ServerURL() *url.URL           { u := *k.config.ServerURL; return &u }

func kitATEActorNamePrefix(runtimeID uuid.UUID) string {
	return "spk-kit-" + strings.ReplaceAll(runtimeID.String(), "-", "") + "-"
}

func kitATEActorName(r assistantRuntimeRecord, thread uuid.UUID) string {
	sum := sha256.Sum256([]byte(thread.String()))
	return kitATEActorNamePrefix(r.ID) + hex.EncodeToString(sum[:8])
}

func kitATEActorNameValid(r assistantRuntimeRecord, name string) bool {
	return len(name) <= maxATEDNSLabelLen && kitATENamePattern.MatchString(name) && strings.HasPrefix(name, kitATEActorNamePrefix(r.ID))
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
			Image:       k.config.Image,
			Atespace:    k.config.Atespace,
			Template:    k.config.Template,
			EgressHosts: append([]string(nil), k.config.EgressHosts...),
		}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.Atespace != k.config.Atespace || m.Template != k.config.Template || m.Image != k.config.Image || !slices.Equal(m.EgressHosts, k.config.EgressHosts) {
		return m, ErrKitRuntimeBinding
	}
	return m, nil
}

func (k *KitATERuntimeBackend) validateActorRow(r assistantRuntimeRecord, actor kitATEThreadActor) error {
	if actor.ThreadID == uuid.Nil || actor.ActorUID == "" || !kitATEActorNameValid(r, actor.ActorName) {
		return ErrKitRuntimeBinding
	}
	return nil
}

func (k *KitATERuntimeBackend) actorRef(name string) *ateapiv1.ObjectRef {
	return &ateapiv1.ObjectRef{Atespace: k.config.Atespace, Name: name}
}

func (k *KitATERuntimeBackend) getActor(ctx context.Context, t kitATEThreadActor) (*ateapiv1.Actor, error) {
	actor, err := k.config.Client.GetActor(ctx, &ateapiv1.GetActorRequest{Actor: k.actorRef(t.ActorName)})
	if err != nil {
		return nil, fmt.Errorf("ATE GetActor: %w", err)
	}
	meta := actor.GetMetadata()
	tmpl := actor.GetActorTemplate()
	if meta.GetAtespace() != k.config.Atespace || meta.GetName() != t.ActorName || meta.GetUid() == "" || (t.ActorUID != "" && meta.GetUid() != t.ActorUID) || tmpl.GetAtespace() != k.config.Atespace || tmpl.GetName() != k.config.Template {
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
	count, err := k.config.Store.CountForRuntime(ctx, r)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("count ATE actors during ensure: %w", err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("encode ATE ensure metadata: %w", err)
	}
	return RuntimeBackendEnsureResult{ColdStart: count == 0, BackendMetadataJSON: raw}, nil
}

func (k *KitATERuntimeBackend) prepare(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (assistantRuntimeRecord, kitATEThreadActor, string, error) {
	var prepared assistantRuntimeRecord
	var entry kitATEThreadActor
	var token string

	err := k.config.ThreadLifecycle(ctx, r, thread, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if locked.Retired {
			return ErrKitRuntimeBinding
		}
		m, err := k.metadata(locked)
		if err != nil {
			return err
		}

		var known bool
		entry, known, err = k.config.Store.GetForThread(ctx, locked, thread)
		if err != nil {
			return fmt.Errorf("load ATE thread actor: %w", err)
		}
		warmUntil := k.config.Now().Add(ateActorWarmWindow)
		if !known {
			candidate := kitATEThreadActor{
				ID:         uuid.Nil,
				RuntimeID:  locked.ID,
				ThreadID:   thread,
				ActorName:  kitATEActorName(locked, thread),
				ActorUID:   "",
				State:      ateActorStateRunning,
				Configured: false,
				Workload:   nil,
				WarmUntil:  &warmUntil,
			}
			actor, err := k.getActor(ctx, candidate)
			if status.Code(err) == codes.NotFound {
				actor, err = k.config.Client.CreateActor(ctx, &ateapiv1.CreateActorRequest{
					Actor: &ateapiv1.Actor{
						Metadata: &ateapiv1.ResourceMetadata{
							Atespace:   k.config.Atespace,
							Name:       candidate.ActorName,
							Uid:        "",
							Version:    0,
							CreateTime: nil,
							UpdateTime: nil,
							DeleteTime: nil,
						},
						ActorTemplate:  k.actorRef(k.config.Template),
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
			if meta.GetUid() == "" || meta.GetName() != candidate.ActorName || meta.GetAtespace() != k.config.Atespace {
				return ErrKitRuntimeBinding
			}
			candidate.ActorUID = meta.GetUid()
			entry, err = k.config.Store.Insert(ctx, locked, candidate)
			if err != nil {
				return fmt.Errorf("persist ATE thread actor: %w", err)
			}
		} else {
			if err := k.validateActorRow(locked, entry); err != nil {
				return err
			}
			if _, err := k.getActor(ctx, entry); err != nil {
				return err
			}
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
			bootstrap, err := k.config.Bootstrap(ctx, locked, thread, entry.ActorName, entry.Workload)
			if err != nil {
				return err
			}
			entry.Workload = bootstrap.Workload
			entry.State = ateActorStateRunning
			entry.WarmUntil = &warmUntil
			entry, err = k.config.Store.UpdateBootstrap(ctx, locked, entry)
			if err != nil {
				return fmt.Errorf("save ATE actor workload binding: %w", err)
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
			entry.State = ateActorStateRunning
			entry.WarmUntil = &warmUntil
			entry, err = k.config.Store.UpdateBootstrap(ctx, locked, entry)
			if err != nil {
				return fmt.Errorf("mark ATE actor configured: %w", err)
			}
		} else {
			entry, err = k.config.Store.UpdateState(ctx, locked, entry.ID, ateActorStateRunning, &warmUntil)
			if err != nil {
				return fmt.Errorf("refresh ATE actor state: %w", err)
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

func (k *KitATERuntimeBackend) protocol(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID, t kitATEThreadActor, token string) (runtimeProtocol, error) {
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
	if _, err := k.metadata(r); err != nil {
		return false, err
	}
	var interrupted bool
	err := k.config.ThreadLifecycle(ctx, r, thread, func(ctx context.Context, locked assistantRuntimeRecord) error {
		t, ok, err := k.config.Store.GetForThread(ctx, locked, thread)
		if err != nil {
			return fmt.Errorf("load ATE thread actor for interrupt: %w", err)
		}
		if !ok {
			return nil
		}
		if err := k.validateActorRow(locked, t); err != nil {
			return err
		}
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
		if _, err := k.metadata(locked); err != nil {
			return err
		}
		if err := k.config.CheckSettled(ctx, locked); err != nil {
			return err
		}
		actors, err := k.config.Store.ListForRuntime(ctx, locked)
		if err != nil {
			return fmt.Errorf("list ATE actors for stop: %w", err)
		}

		for _, t := range actors {
			if err := k.validateActorRow(locked, t); err != nil {
				return err
			}
			actor, err := k.getActor(ctx, t)
			if err != nil {
				return err
			}
			if actor.GetStatus().GetState() != ateapiv1.ActorState_ACTOR_STATE_SUSPENDED {
				if actor.GetStatus().GetState() != ateapiv1.ActorState_ACTOR_STATE_RUNNING {
					return errors.New("ATE actor is not ready for suspension")
				}
				token, err := k.config.Token(ctx, locked, t.ThreadID)
				if err != nil {
					return err
				}
				protocol, err := k.protocol(ctx, locked, t.ThreadID, t, token)
				if err != nil {
					return err
				}
				settled, ok := protocol.(interface {
					CloseNativeThreadSession(context.Context, assistantRuntimeRecord, uuid.UUID) error
				})
				if !ok {
					return errors.New("ATE native checkpoint settlement is unavailable")
				}
				if err := settled.CloseNativeThreadSession(ctx, locked, t.ThreadID); err != nil {
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
			if _, err := k.config.Store.UpdateState(ctx, locked, t.ID, ateActorStateSuspended, nil); err != nil {
				return fmt.Errorf("record ATE actor suspended: %w", err)
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
		if _, err := k.metadata(locked); err != nil {
			return err
		}
		actors, err := k.config.Store.ListForRuntime(ctx, locked)
		if err != nil {
			return fmt.Errorf("list ATE actors for reap: %w", err)
		}
		for _, t := range actors {
			if err := k.validateActorRow(locked, t); err != nil {
				return err
			}
			if t.Workload != nil {
				if err := k.config.Revoke(ctx, locked, t.Workload); err != nil {
					return err
				}
			}
			_, err := k.getActor(ctx, t)
			if status.Code(err) != codes.NotFound {
				if err != nil {
					return err
				}
				_, err = k.config.Client.DeleteActor(ctx, &ateapiv1.DeleteActorRequest{
					Actor:   k.actorRef(t.ActorName),
					Options: &ateapiv1.DeleteOptions{Version: 0, Uid: t.ActorUID},
				})
				if err != nil && status.Code(err) != codes.NotFound {
					return fmt.Errorf("ATE DeleteActor: %w", err)
				}
			}
			if err := k.config.Store.SoftDelete(ctx, locked, t.ID); err != nil {
				return fmt.Errorf("retire ATE thread actor: %w", err)
			}
		}
		return nil
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
	if _, err := k.metadata(r); err != nil {
		return false, err
	}
	actors, err := k.config.Store.ListForRuntime(ctx, r)
	if err != nil {
		return false, fmt.Errorf("list ATE actors for existence check: %w", err)
	}
	for _, t := range actors {
		if err := k.validateActorRow(r, t); err != nil {
			return false, err
		}
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
