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
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/attr"
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

	// ateSuspendingWaitTimeout bounds waiting for an in-flight ACTOR_STATE_SUSPENDING transition to finish.
	ateSuspendingWaitTimeout = 30 * time.Second

	// ateDrainCheckTimeout bounds polling GET /status on kit-ate-init for eviction drain detection.
	ateDrainCheckTimeout = 5 * time.Second

	// ateDrainSuspendTimeout bounds synchronous durable suspension triggered by worker eviction drain.
	ateDrainSuspendTimeout = 3 * time.Minute

	// ateInitWireVersion is the expected X-ATE-Init-Version header and bootstrap payload version.
	ateInitWireVersion = 2

	// ateInitVersionHeader carries the init helper wire contract version on every response.
	ateInitVersionHeader = "X-ATE-Init-Version"

	// meterATEActorReadyDuration records actor create->ready and resume->ready durations.
	meterATEActorReadyDuration = "assistant.ate.actor.ready_duration_ms"

	ateStartupModeColdStart = "cold_start"
	ateStartupModeResume    = "resume"
	ateStartupModeWarm      = "warm"
)

type kitATEControl interface {
	GetAtespace(context.Context, *ateapiv1.GetAtespaceRequest) (*ateapiv1.Atespace, error)
	GetActorTemplate(context.Context, *ateapiv1.GetActorTemplateRequest) (*ateapiv1.ActorTemplate, error)
	CreateActorTemplate(context.Context, *ateapiv1.CreateActorTemplateRequest) (*ateapiv1.ActorTemplate, error)
	GetActor(context.Context, *ateapiv1.GetActorRequest) (*ateapiv1.Actor, error)
	CreateActor(context.Context, *ateapiv1.CreateActorRequest) (*ateapiv1.Actor, error)
	ResumeActor(context.Context, *ateapiv1.ResumeActorRequest) (*ateapiv1.ResumeActorResponse, error)
	RevertActor(context.Context, *ateapiv1.RevertActorRequest) (*ateapiv1.RevertActorResponse, error)
	SuspendActor(context.Context, *ateapiv1.SuspendActorRequest) (*ateapiv1.SuspendActorResponse, error)
	DeleteActor(context.Context, *ateapiv1.DeleteActorRequest) (*ateapiv1.Actor, error)
	GetActorEgressPolicy(context.Context, *ateapiv1.GetActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error)
	CreateActorEgressPolicy(context.Context, *ateapiv1.CreateActorEgressPolicyRequest) (*ateapiv1.EgressPolicy, error)
	ActorHTTPClient(string, uint16) (*guardian.HTTPClient, error)
}

var _ kitATEControl = (*ateclient.Client)(nil)

// KitATERuntimeConfig configures the ATE-backed Kit runtime.
type KitATERuntimeConfig struct {
	// WorkerSelector holds the label selector applied to created ActorTemplates.
	WorkerSelector map[string]string

	// Revoke withdraws a thread actor's workload admission during reap.
	Revoke func(context.Context, assistantRuntimeRecord, *KitWorkloadBinding) error

	// CheckSettled verifies the assistant has no unsettled events or wakes before runtime-wide stop.
	CheckSettled func(context.Context, assistantRuntimeRecord) error

	// CheckThreadSettled verifies one thread has no unsettled events (excluding
	// an optional completing event ID) before per-thread idle or drain suspend.
	CheckThreadSettled func(context.Context, assistantRuntimeRecord, uuid.UUID, uuid.UUID) error

	// ClearThreadSession removes a thread's persisted ACP session mapping when a crashed actor is replaced.
	ClearThreadSession func(context.Context, assistantRuntimeRecord, uuid.UUID) error

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

	// Logger receives structured ATE actor lifecycle logs (create->ready and resume->ready durations).
	Logger *slog.Logger

	// Meter optionally records OpenTelemetry histograms for ATE actor startup durations.
	Meter metric.Meter
}

// KitATERuntimeBackend runs one ATE actor per assistant thread over native Kit ACP.
type KitATERuntimeBackend struct {
	config        KitATERuntimeConfig
	imageDigest   string
	logger        *slog.Logger
	readyDuration metric.Float64Histogram
}

type kitATEMetadata struct {
	RuntimeID         uuid.UUID `json:"runtime_id"`
	AssistantID       uuid.UUID `json:"assistant_id"`
	ProjectID         uuid.UUID `json:"project_id"`
	ImageDigest       string    `json:"image_digest"`
	Atespace          string    `json:"atespace"`
	Template          string    `json:"template"`
	LegacyEgressHosts []string  `json:"egress_hosts,omitempty"`
}

var _ RuntimeBackend = (*KitATERuntimeBackend)(nil)

// NewKitATERuntimeBackend validates configuration and builds a KitATERuntimeBackend.
func NewKitATERuntimeBackend(c KitATERuntimeConfig) (*KitATERuntimeBackend, error) {
	imageDigest, err := kitATEImageDigest(c.Image)
	if err != nil || !kitATENamePattern.MatchString(c.Atespace) || len(c.Atespace) > maxATEDNSLabelLen || !kitATENamePattern.MatchString(c.Template) || len(c.Template) > maxATETemplateBaseLen || c.ServerURL == nil || c.Client == nil || c.Bootstrap == nil || c.Token == nil || c.Protocol == nil || c.Lifecycle == nil || c.ThreadLifecycle == nil || c.Store == nil || c.Revoke == nil || c.CheckSettled == nil {
		return nil, errors.New("ATE runtime requires a digest-pinned image, actor scope, worker selector, authenticated client, actor store, and lifecycle callbacks")
	}
	if err := validateKitATEWorkerSelector(c.WorkerSelector); err != nil {
		return nil, err
	}
	c.WorkerSelector = maps.Clone(c.WorkerSelector)

	if c.HelperPollInterval <= 0 {
		c.HelperPollInterval = ateHelperPollInterval
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	logger := c.Logger
	var readyDuration metric.Float64Histogram
	if c.Meter != nil {
		hist, histErr := c.Meter.Float64Histogram(
			meterATEActorReadyDuration,
			metric.WithDescription("Duration from ATE actor create or resume until Kit ACP app-readyz succeeds"),
			metric.WithUnit("ms"),
		)
		if histErr != nil {
			if logger != nil {
				logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterATEActorReadyDuration), attr.SlogError(histErr))
			}
		} else {
			readyDuration = hist
		}
	}

	return &KitATERuntimeBackend{
		config:        c,
		imageDigest:   imageDigest,
		logger:        logger,
		readyDuration: readyDuration,
	}, nil
}

func (k *KitATERuntimeBackend) Backend() string               { return runtimeBackendATE }
func (k *KitATERuntimeBackend) SupportsBackend(b string) bool { return b == runtimeBackendATE }
func (k *KitATERuntimeBackend) ImageRef() string              { return k.config.Image }
func (k *KitATERuntimeBackend) ServerURL() *url.URL           { u := *k.config.ServerURL; return &u }

func (k *KitATERuntimeBackend) templateVersionName() string {
	return kitATETemplateVersionName(k.config.Template, k.imageDigest)
}

func kitATEActorNamePrefix(runtimeID uuid.UUID) string {
	return "spk-kit-" + strings.ReplaceAll(runtimeID.String(), "-", "") + "-"
}

func kitATEActorName(r assistantRuntimeRecord, thread uuid.UUID) string {
	sum := sha256.Sum256([]byte(thread.String()))
	return kitATEActorNamePrefix(r.ID) + hex.EncodeToString(sum[:8])
}

func kitATEActorThreadPrefix(runtimeID, thread uuid.UUID) string {
	sum := sha256.Sum256([]byte(thread.String()))
	return kitATEActorNamePrefix(runtimeID) + hex.EncodeToString(sum[:4])
}

func kitATEActorReplacementName(r assistantRuntimeRecord, thread, nonce uuid.UUID) string {
	nonceSum := sha256.Sum256([]byte(nonce.String()))
	return kitATEActorThreadPrefix(r.ID, thread) + "-" + hex.EncodeToString(nonceSum[:3])
}

func kitATEActorNameValid(r assistantRuntimeRecord, thread uuid.UUID, name string) bool {
	if thread == uuid.Nil || len(name) > maxATEDNSLabelLen || !kitATENamePattern.MatchString(name) {
		return false
	}
	return name == kitATEActorName(r, thread) || strings.HasPrefix(name, kitATEActorThreadPrefix(r.ID, thread)+"-")
}

func (k *KitATERuntimeBackend) metadata(r assistantRuntimeRecord) (kitATEMetadata, error) {
	var m kitATEMetadata
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || r.ID == uuid.Nil || r.AssistantID == uuid.Nil || r.ProjectID == uuid.Nil {
		return m, ErrKitRuntimeBinding
	}
	if kitLocalUnprovisionedMetadata(r.BackendMetadataJSON) {
		return kitATEMetadata{
			RuntimeID:         r.ID,
			AssistantID:       r.AssistantID,
			ProjectID:         r.ProjectID,
			ImageDigest:       k.imageDigest,
			Atespace:          k.config.Atespace,
			Template:          k.config.Template,
			LegacyEgressHosts: nil,
		}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID || m.Atespace != k.config.Atespace || m.Template != k.config.Template || !ateImageDigestPattern.MatchString(m.ImageDigest) || len(m.LegacyEgressHosts) > 0 {
		return m, ErrKitRuntimeBinding
	}
	return m, nil
}

func (k *KitATERuntimeBackend) validateActorRow(r assistantRuntimeRecord, actor kitATEThreadActor) error {
	if actor.ThreadID == uuid.Nil || actor.ActorUID == "" || !kitATEActorNameValid(r, actor.ThreadID, actor.ActorName) {
		return ErrKitRuntimeBinding
	}
	return nil
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
			RuntimeID:         r.ID,
			AssistantID:       r.AssistantID,
			ProjectID:         r.ProjectID,
			ImageDigest:       "",
			Atespace:          k.config.Atespace,
			Template:          "",
			LegacyEgressHosts: nil,
		}, nil
	}
	if json.Unmarshal(r.BackendMetadataJSON, &m) != nil || m.RuntimeID != r.ID || m.AssistantID != r.AssistantID || m.ProjectID != r.ProjectID {
		return m, ErrKitRuntimeBinding
	}
	return m, nil
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
	count, err := k.config.Store.CountForRuntime(ctx, r)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("count ATE actors during ensure: %w", err)
	}
	// Update runtime metadata to track the active image digest; existing actors
	// remain on their own digest-versioned template until recreated.
	m.ImageDigest = k.imageDigest
	raw, err := json.Marshal(m)
	if err != nil {
		return RuntimeBackendEnsureResult{}, fmt.Errorf("encode ATE ensure metadata: %w", err)
	}
	return RuntimeBackendEnsureResult{ColdStart: count == 0, BackendMetadataJSON: raw}, nil
}

func (k *KitATERuntimeBackend) prepare(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID, initiator ateThreadInitiator) (assistantRuntimeRecord, kitATEThreadActor, string, error) {
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
		prepareStart := time.Now()
		startupMode := ateStartupModeWarm
		warmUntil := k.config.Now().Add(ateActorWarmWindow)
		needBootstrap := false
		if !known {
			startupMode = ateStartupModeColdStart
			entry, err = k.createThreadActor(ctx, locked, thread, kitATEActorName(locked, thread), warmUntil, initiator)
			if err != nil {
				return err
			}
			needBootstrap = true
		} else {
			if err := k.validateActorRow(locked, entry); err != nil {
				return err
			}
			var resumed bool
			entry, resumed, err = k.ensureActorRunning(ctx, locked, thread, entry, warmUntil, initiator)
			if err != nil {
				return err
			}
			switch {
			case !entry.Configured:
				startupMode = ateStartupModeColdStart
				needBootstrap = true
			case resumed:
				startupMode = ateStartupModeResume
				needBootstrap = true
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
		defer func() { helper.CloseIdleConnections() }()

		base, err := ateclient.ActorEndpoint(entry.ActorName, ateInitPort)
		if err != nil {
			return fmt.Errorf("build ATE init endpoint: %w", err)
		}
		base = strings.TrimSuffix(base, "/acp")

		if err := waitKitATEHelper(ctx, helper, base+"/readyz", k.config.HelperPollInterval); err != nil {
			return err
		}

		if !needBootstrap {
			// On a rootfs-only resume/revert (for example if the router auto-woke
			// the actor or a prior server crashed after ResumeActor), kit-ate-init
			// restarts fresh with started=false and waits for POST /bootstrap.
			st, statusErr := kitATEInitStatus(ctx, helper, base+"/status")
			if statusErr != nil {
				return statusErr
			}
			if !st.Started {
				needBootstrap = true
			}
		}

		if needBootstrap {
			for attempt := range 2 {
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
				if err := k.ensureEgress(ctx, entry.ActorName, entry.ActorUID, bootstrap); err != nil {
					return err
				}
				payload, err := kitATEBootstrapPayload(bootstrap)
				if err != nil {
					return err
				}
				code, err := kitATEPost(ctx, helper, base+"/bootstrap", payload)
				if err != nil {
					return fmt.Errorf("ATE Kit bootstrap failed: %w", err)
				}
				if code == http.StatusNoContent {
					entry.Configured = true
					entry.State = ateActorStateRunning
					entry.WarmUntil = &warmUntil
					entry, err = k.config.Store.UpdateBootstrap(ctx, locked, entry)
					if err != nil {
						return fmt.Errorf("mark ATE actor configured: %w", err)
					}
					break
				}
				if code == http.StatusConflict {
					if attempt == 1 {
						if retireErr := k.retireActor(ctx, locked, thread, entry); retireErr != nil {
							return retireErr
						}
						return errors.New("ATE Kit bootstrap conflicted after actor recreation")
					}
					entry, err = k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
					if err != nil {
						return err
					}
					helper.CloseIdleConnections()
					helper, err = k.config.Client.ActorHTTPClient(entry.ActorName, ateInitPort)
					if err != nil {
						return fmt.Errorf("create ATE bootstrap transport: %w", err)
					}
					base, err = ateclient.ActorEndpoint(entry.ActorName, ateInitPort)
					if err != nil {
						return fmt.Errorf("build ATE init endpoint: %w", err)
					}
					base = strings.TrimSuffix(base, "/acp")
					if err := waitKitATEHelper(ctx, helper, base+"/readyz", k.config.HelperPollInterval); err != nil {
						return err
					}
					continue
				}
				return errors.New("ATE Kit bootstrap failed")
			}
		} else {
			entry, err = k.config.Store.UpdateState(ctx, locked, entry.ID, ateActorStateRunning, &warmUntil)
			if err != nil {
				return fmt.Errorf("refresh ATE actor state: %w", err)
			}
		}

		if entry.Workload == nil || entry.Workload.Subject == "" || !entry.Workload.MCPProjectGateway {
			return ErrKitRuntimeBinding
		}

		if err := waitKitATEHelper(ctx, helper, base+"/app-readyz", k.config.HelperPollInterval); err != nil {
			return err
		}
		k.recordActorReadyDuration(ctx, locked, entry, startupMode, time.Since(prepareStart))

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

func (k *KitATERuntimeBackend) createThreadActor(ctx context.Context, locked assistantRuntimeRecord, thread uuid.UUID, actorName string, warmUntil time.Time, turnInitiator ateThreadInitiator) (kitATEThreadActor, error) {
	var zero kitATEThreadActor
	// Inherit the thread's original recorded initiating user across crash
	// recovery, image changes, and runtime replacements so a replacement actor
	// can never adopt a later message sender's identity.
	resolvedInitiator, foundPrior, err := k.config.Store.GetThreadInitiator(ctx, locked, thread)
	if err != nil {
		return zero, fmt.Errorf("resolve durable ATE thread initiator: %w", err)
	}
	if !foundPrior {
		resolvedInitiator = turnInitiator
	}
	if err := validateATEThreadInitiator(resolvedInitiator); err != nil {
		return zero, fmt.Errorf("validate ATE thread initiator: %w", err)
	}
	candidate := kitATEThreadActor{
		ID:         uuid.Nil,
		RuntimeID:  locked.ID,
		ThreadID:   thread,
		ActorName:  actorName,
		ActorUID:   "",
		State:      ateActorStateRunning,
		Configured: false,
		Workload:   nil,
		WarmUntil:  &warmUntil,
		Initiator:  resolvedInitiator,
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
				ActorTemplate:  k.actorRef(k.templateVersionName()),
				WorkerSelector: nil,
				SourceTag:      nil,
				Status:         nil,
			},
		})
		if err != nil {
			return zero, fmt.Errorf("ATE CreateActor: %w", err)
		}
	} else if err != nil {
		return zero, fmt.Errorf("lookup ATE actor: %w", err)
	}
	meta := actor.GetMetadata()
	if meta.GetUid() == "" || meta.GetName() != candidate.ActorName || meta.GetAtespace() != k.config.Atespace {
		return zero, ErrKitRuntimeBinding
	}
	candidate.ActorUID = meta.GetUid()
	inserted, err := k.config.Store.Insert(ctx, locked, candidate)
	if err != nil {
		return zero, fmt.Errorf("persist ATE thread actor: %w", err)
	}
	return inserted, nil
}

func (k *KitATERuntimeBackend) recordActorReadyDuration(ctx context.Context, r assistantRuntimeRecord, entry kitATEThreadActor, startupMode string, d time.Duration) {
	ms := float64(d) / float64(time.Millisecond)
	if k.readyDuration != nil {
		k.readyDuration.Record(ctx, ms, metric.WithAttributes(attr.AssistantATEStartupMode(startupMode)))
	}
	if k.logger == nil {
		return
	}
	args := []any{
		attr.SlogAssistantID(r.AssistantID.String()),
		attr.SlogProjectID(r.ProjectID.String()),
		attr.SlogAssistantRuntimeID(r.ID.String()),
		attr.SlogAssistantThreadID(entry.ThreadID.String()),
		attr.SlogAssistantATEActorName(entry.ActorName),
		attr.SlogAssistantATEActorUID(entry.ActorUID),
		attr.SlogAssistantATEStartupMode(startupMode),
		attr.SlogAssistantATEReadyDurationMS(d.Milliseconds()),
	}
	switch startupMode {
	case ateStartupModeColdStart:
		args = append(args, attr.SlogAssistantATECreateToReadyMS(d.Milliseconds()))
	case ateStartupModeResume:
		args = append(args, attr.SlogAssistantATEResumeToReadyMS(d.Milliseconds()))
	}
	k.logger.InfoContext(ctx, "ATE thread actor ready", args...)
}

// ensureActorRunning brings an existing thread actor into ACTOR_STATE_RUNNING:
//   - SUSPENDED: calls ResumeActor explicitly before dialing the router (the
//     router only parks requests briefly while an actor wakes) and requires
//     re-bootstrap because SNAPSHOT_FIDELITY_ROOTFS restarts kit-ate-init fresh.
//   - CRASHED / REVERTING: calls RevertActor + ResumeActor when a completed
//     durable external rootfs snapshot exists (requiring re-bootstrap);
//     otherwise (or if revert fails) retires the broken actor and provisions a
//     fresh replacement automatically.
//   - DELETING / NotFound: retires the stale row and provisions a replacement.
//
// The boolean return reports whether the actor resumed from a non-running state
// (SUSPENDED, CRASHED, REVERTING, or in-flight transition), which also requires
// re-bootstrapping provisioned files on SNAPSHOT_FIDELITY_ROOTFS restore.
func (k *KitATERuntimeBackend) ensureActorRunning(ctx context.Context, locked assistantRuntimeRecord, thread uuid.UUID, entry kitATEThreadActor, warmUntil time.Time, initiator ateThreadInitiator) (kitATEThreadActor, bool, error) {
	var zero kitATEThreadActor
	actor, err := k.getActor(ctx, entry)
	if status.Code(err) == codes.NotFound {
		replaced, recErr := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
		return replaced, false, recErr
	}
	if err != nil {
		return zero, false, err
	}

	resumed := false
	if state := actor.GetStatus().GetState(); state == ateapiv1.ActorState_ACTOR_STATE_SUSPENDING || state == ateapiv1.ActorState_ACTOR_STATE_RESUMING {
		resumed = true
		actor, err = k.waitForStableActorState(ctx, entry)
		if status.Code(err) == codes.NotFound {
			replaced, recErr := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
			return replaced, false, recErr
		}
		if err != nil {
			return zero, false, err
		}
	}

	switch actor.GetStatus().GetState() {
	case ateapiv1.ActorState_ACTOR_STATE_RUNNING:
		return entry, resumed, nil
	case ateapiv1.ActorState_ACTOR_STATE_SUSPENDED:
		_, err := k.config.Client.ResumeActor(ctx, &ateapiv1.ResumeActorRequest{
			Actor: k.actorRef(entry.ActorName),
		})
		if err == nil {
			return entry, true, nil
		}
		if status.Code(err) == codes.FailedPrecondition {
			// Per ATE v1.0.1 ResumeActor semantics, a missing local snapshot upload
			// transitions the actor to ACTOR_STATE_CRASHED and returns FailedPrecondition.
			refreshed, getErr := k.getActor(ctx, entry)
			if status.Code(getErr) == codes.NotFound {
				replaced, recErr := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
				return replaced, false, recErr
			}
			if getErr == nil && (refreshed.GetStatus().GetState() == ateapiv1.ActorState_ACTOR_STATE_CRASHED || refreshed.GetStatus().GetState() == ateapiv1.ActorState_ACTOR_STATE_REVERTING) {
				return k.recoverCrashedActor(ctx, locked, thread, entry, refreshed, warmUntil, initiator)
			}
		}
		if status.Code(err) == codes.NotFound {
			replaced, recErr := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
			return replaced, false, recErr
		}
		return zero, false, fmt.Errorf("ATE ResumeActor: %w", err)
	case ateapiv1.ActorState_ACTOR_STATE_CRASHED, ateapiv1.ActorState_ACTOR_STATE_REVERTING:
		return k.recoverCrashedActor(ctx, locked, thread, entry, actor, warmUntil, initiator)
	case ateapiv1.ActorState_ACTOR_STATE_DELETING:
		replaced, recErr := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
		return replaced, false, recErr
	default:
		return zero, false, fmt.Errorf("unexpected ATE actor state %s", actor.GetStatus().GetState().String())
	}
}

func (k *KitATERuntimeBackend) waitForStableActorState(ctx context.Context, entry kitATEThreadActor) (*ateapiv1.Actor, error) {
	waitCtx, cancel := context.WithTimeout(ctx, ateSuspendingWaitTimeout)
	defer cancel()
	for {
		actor, err := k.getActor(waitCtx, entry)
		if err != nil {
			return nil, err
		}
		state := actor.GetStatus().GetState()
		if state != ateapiv1.ActorState_ACTOR_STATE_SUSPENDING && state != ateapiv1.ActorState_ACTOR_STATE_RESUMING {
			return actor, nil
		}
		timer := time.NewTimer(k.config.HelperPollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return nil, errors.New("ATE actor state transition did not complete before resume")
		case <-timer.C:
		}
	}
}

func (k *KitATERuntimeBackend) recoverCrashedActor(ctx context.Context, locked assistantRuntimeRecord, thread uuid.UUID, entry kitATEThreadActor, actor *ateapiv1.Actor, warmUntil time.Time, initiator ateThreadInitiator) (kitATEThreadActor, bool, error) {
	if entry.Configured && hasCompletedExternalRootfsSnapshot(actor) {
		if _, err := k.config.Client.RevertActor(ctx, &ateapiv1.RevertActorRequest{
			Actor: k.actorRef(entry.ActorName),
		}); err == nil {
			if _, err := k.config.Client.ResumeActor(ctx, &ateapiv1.ResumeActorRequest{
				Actor: k.actorRef(entry.ActorName),
			}); err == nil {
				return entry, true, nil
			}
		}
	}
	recreated, err := k.retireAndRecreateActor(ctx, locked, thread, entry, warmUntil, initiator)
	return recreated, false, err
}

func (k *KitATERuntimeBackend) retireActor(ctx context.Context, locked assistantRuntimeRecord, thread uuid.UUID, entry kitATEThreadActor) error {
	if entry.Workload != nil {
		if err := k.config.Revoke(ctx, locked, entry.Workload); err != nil {
			return fmt.Errorf("revoke ATE actor workload: %w", err)
		}
	}
	if _, err := k.config.Client.DeleteActor(ctx, &ateapiv1.DeleteActorRequest{
		Actor:   k.actorRef(entry.ActorName),
		Options: &ateapiv1.DeleteOptions{Version: 0, Uid: entry.ActorUID},
	}); err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("delete ATE actor: %w", err)
	}
	if err := k.config.Store.SoftDelete(ctx, locked, entry.ID); err != nil {
		return fmt.Errorf("soft-delete ATE actor row: %w", err)
	}
	if k.config.ClearThreadSession != nil {
		if err := k.config.ClearThreadSession(ctx, locked, thread); err != nil {
			return fmt.Errorf("clear replaced ATE thread session: %w", err)
		}
	}
	return nil
}

func (k *KitATERuntimeBackend) retireAndRecreateActor(ctx context.Context, locked assistantRuntimeRecord, thread uuid.UUID, entry kitATEThreadActor, warmUntil time.Time, initiator ateThreadInitiator) (kitATEThreadActor, error) {
	if err := k.retireActor(ctx, locked, thread, entry); err != nil {
		return kitATEThreadActor{}, err
	}
	replacementName := kitATEActorReplacementName(locked, thread, uuid.New())
	return k.createThreadActor(ctx, locked, thread, replacementName, warmUntil, initiator)
}

func hasCompletedExternalRootfsSnapshot(actor *ateapiv1.Actor) bool {
	for _, snap := range actor.GetStatus().GetSnapshots() {
		if snap.GetFidelity() == ateapiv1.SnapshotFidelity_SNAPSHOT_FIDELITY_ROOTFS &&
			snap.GetDurableSnapshot().GetState() == ateapiv1.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED {
			return true
		}
	}
	return false
}

type kitATEInitStatusResponse struct {
	Version  int  `json:"version"`
	Draining bool `json:"draining"`
	Started  bool `json:"started"`
}

func kitATEInitStatus(ctx context.Context, helper *guardian.HTTPClient, endpoint string) (kitATEInitStatusResponse, error) {
	var payload kitATEInitStatusResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return payload, fmt.Errorf("build ATE status request: %w", err)
	}
	res, err := helper.Do(req)
	if err != nil {
		return payload, fmt.Errorf("query ATE init status: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })

	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
		return payload, fmt.Errorf("ATE init status returned %d", res.StatusCode)
	}
	if err := validateATEInitVersionHeader(res); err != nil {
		return payload, err
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxATEHelperErrorBodyBytes)).Decode(&payload); err != nil {
		return payload, fmt.Errorf("decode ATE init status: %w", err)
	}
	return payload, nil
}

func isKitATEActorDraining(ctx context.Context, client kitATEControl, actorName string) (bool, error) {
	checkCtx, cancel := context.WithTimeout(ctx, ateDrainCheckTimeout)
	defer cancel()

	helper, err := client.ActorHTTPClient(actorName, ateInitPort)
	if err != nil {
		return false, fmt.Errorf("create ATE drain status transport: %w", err)
	}
	defer helper.CloseIdleConnections()

	base, err := ateclient.ActorEndpoint(actorName, ateInitPort)
	if err != nil {
		return false, fmt.Errorf("build ATE drain status endpoint: %w", err)
	}
	base = strings.TrimSuffix(base, "/acp")

	payload, err := kitATEInitStatus(checkCtx, helper, base+"/status")
	if err != nil {
		return false, err
	}
	return payload.Draining, nil
}

type kitATEBootstrapDir struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"`
}

type kitATEBootstrapFile struct {
	Path    string `json:"path"`
	Mode    uint32 `json:"mode"`
	Content []byte `json:"content"`
}

type kitATEBootstrapRequest struct {
	Version int                   `json:"version"`
	Dirs    []kitATEBootstrapDir  `json:"dirs"`
	Files   []kitATEBootstrapFile `json:"files"`
	Env     map[string]string     `json:"env,omitempty"`
}

// kitATEBootstrapPayload builds the versioned generic bootstrap payload for
// kit-ate-init so all Kit-specific paths, contents, and process env stay in server code.
func kitATEBootstrapPayload(b KitLocalBootstrap) ([]byte, error) {
	if err := validateKitBootstrap(b); err != nil {
		return nil, err
	}
	if len(b.GatewayToken) < 32 || bytes.ContainsAny(b.GatewayToken, " \r\n\x00") {
		return nil, errors.New("invalid ATE Kit gateway token")
	}
	payload := kitATEBootstrapRequest{
		Version: ateInitWireVersion,
		Dirs: []kitATEBootstrapDir{
			{Path: ".kit", Mode: 0o700},
			{Path: ".kit/credentials", Mode: 0o700},
			{Path: "workspace", Mode: 0o700},
		},
		Files: []kitATEBootstrapFile{
			{Path: ".kit/AGENTS.md", Mode: 0o600, Content: b.AgentsMD},
			{Path: ".kit/config.toml", Mode: 0o600, Content: []byte(kitBootstrapConfig(b))},
			{Path: ".kit/credentials/mcp.json", Mode: 0o600, Content: b.MCPJSON},
			{Path: ".kit/credentials/gateway-token", Mode: 0o600, Content: b.GatewayToken},
		},
		Env: map[string]string{
			"OPENROUTER_BASE_URL": b.InferenceEndpoint,
			"OPENROUTER_API_KEY":  kitATEEgressAuthPlaceholder,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode ATE bootstrap payload: %w", err)
	}
	return raw, nil
}

func validateATEInitVersionHeader(res *http.Response) error {
	got := res.Header.Get(ateInitVersionHeader)
	want := fmt.Sprintf("%d", ateInitWireVersion)
	if got != want {
		return fmt.Errorf("ATE init helper version mismatch: got %q, want %q", got, want)
	}
	return nil
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
				return validateATEInitVersionHeader(res)
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
	req.Header.Set("Content-Type", "application/json")

	res, err := c.Do(req)
	if err != nil {
		return 0, errors.New("ATE initialization request failed")
	}
	defer o11y.NoLogDefer(func() error { return res.Body.Close() })

	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxATEHelperErrorBodyBytes))
	if err := validateATEInitVersionHeader(res); err != nil {
		return res.StatusCode, err
	}
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
	prepared, t, token, err := k.prepare(ctx, r, turn.ThreadID, turn.Initiator)
	if err != nil {
		return err
	}
	p, err := k.protocol(ctx, prepared, turn.ThreadID, t, token)
	if err != nil {
		return err
	}
	runErr := p.RunTurn(ctx, prepared, turn)

	// If the host worker received a drain SIGTERM while this turn was in flight,
	// kit-ate-init kept the actor alive so the turn could finish cleanly.
	// Synchronously suspend the actor now with a durable rootfs snapshot so it
	// resumes on a healthy worker before the drain budget elapses.
	drainCtx, drainCancel := context.WithTimeout(context.WithoutCancel(ctx), ateDrainSuspendTimeout)
	defer drainCancel()
	if draining, drainErr := isKitATEActorDraining(drainCtx, k.config.Client, t.ActorName); drainErr == nil && draining {
		if suspendErr := k.suspendDrainingThreadActor(drainCtx, r, turn.ThreadID, turn.EventID); suspendErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("suspend draining ATE actor: %w", suspendErr))
		}
	}

	if runErr != nil {
		return fmt.Errorf("run ATE Kit turn: %w", runErr)
	}
	return nil
}

func (k *KitATERuntimeBackend) suspendDrainingThreadActor(ctx context.Context, r assistantRuntimeRecord, threadID, excludeEventID uuid.UUID) error {
	return k.config.ThreadLifecycle(ctx, r, threadID, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if _, err := k.metadata(locked); err != nil {
			return err
		}
		t, ok, err := k.config.Store.GetForThread(ctx, locked, threadID)
		if err != nil {
			return fmt.Errorf("load draining ATE thread actor: %w", err)
		}
		if !ok || t.State != ateActorStateRunning {
			return nil
		}
		if err := k.validateActorRow(locked, t); err != nil {
			return err
		}
		// Skip only if another event on this same thread is already unsettled;
		// the completing turn's own event_id is excluded because service.go
		// marks it completed after RunTurn returns.
		if k.config.CheckThreadSettled != nil {
			if err := k.config.CheckThreadSettled(ctx, locked, threadID, excludeEventID); err != nil {
				return nil
			}
		} else if err := k.config.CheckSettled(ctx, locked); err != nil {
			return nil
		}
		_, err = k.suspendThreadActorLocked(ctx, locked, t)
		return err
	})
}

// SuspendThreadActor suspends a single thread actor synchronously when either
// its warm window has elapsed past runtimeIdleSweepGrace or its init helper
// reports a worker eviction drain (SIGTERM). It acquires only the per-thread
// lock so active sibling threads of the same assistant are never blocked.
func (k *KitATERuntimeBackend) SuspendThreadActor(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, now time.Time) (bool, error) {
	if threadID == uuid.Nil {
		return false, ErrKitRuntimeBinding
	}
	suspended := false
	err := k.config.ThreadLifecycle(ctx, r, threadID, func(ctx context.Context, locked assistantRuntimeRecord) error {
		if locked.Retired {
			return nil
		}
		if _, err := k.metadata(locked); err != nil {
			return err
		}
		t, ok, err := k.config.Store.GetForThread(ctx, locked, threadID)
		if err != nil {
			return fmt.Errorf("load ATE thread actor for sweep: %w", err)
		}
		if !ok || t.State != ateActorStateRunning {
			return nil
		}
		if err := k.validateActorRow(locked, t); err != nil {
			return err
		}

		idle := t.WarmUntil != nil && !t.WarmUntil.After(now.Add(-runtimeIdleSweepGrace))
		draining := false
		if !idle {
			var drainErr error
			draining, drainErr = isKitATEActorDraining(ctx, k.config.Client, t.ActorName)
			if drainErr != nil || !draining {
				return nil
			}
		}

		// Never interrupt an active turn during periodic sweep: if draining,
		// RunTurn suspends the actor synchronously as soon as the turn settles.
		if k.config.CheckThreadSettled != nil {
			if err := k.config.CheckThreadSettled(ctx, locked, threadID, uuid.Nil); err != nil {
				return nil
			}
		} else if err := k.config.CheckSettled(ctx, locked); err != nil {
			return nil
		}

		didSuspend, err := k.suspendThreadActorLocked(ctx, locked, t)
		if err != nil {
			return err
		}
		suspended = didSuspend
		return nil
	})
	return suspended, err
}

func (k *KitATERuntimeBackend) suspendThreadActorLocked(ctx context.Context, locked assistantRuntimeRecord, t kitATEThreadActor) (bool, error) {
	actor, err := k.getActor(ctx, t)
	if status.Code(err) == codes.NotFound {
		if delErr := k.config.Store.SoftDelete(ctx, locked, t.ID); delErr != nil {
			return false, fmt.Errorf("retire missing ATE thread actor: %w", delErr)
		}
		if k.config.ClearThreadSession != nil {
			if clearErr := k.config.ClearThreadSession(ctx, locked, t.ThreadID); clearErr != nil {
				return false, fmt.Errorf("clear missing ATE thread session: %w", clearErr)
			}
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}

	switch actor.GetStatus().GetState() {
	case ateapiv1.ActorState_ACTOR_STATE_CRASHED:
		if _, err := k.config.Store.UpdateState(ctx, locked, t.ID, ateActorStateCrashed, nil); err != nil {
			return false, fmt.Errorf("record crashed ATE actor state: %w", err)
		}
		return false, nil
	case ateapiv1.ActorState_ACTOR_STATE_SUSPENDED:
		if !hasCompletedDurableRootfsSnapshot(actor, t.ActorUID) {
			return false, errors.New("ATE actor has no completed durable rootfs snapshot")
		}
		if _, err := k.config.Store.UpdateState(ctx, locked, t.ID, ateActorStateSuspended, nil); err != nil {
			return false, fmt.Errorf("record ATE actor suspended: %w", err)
		}
		return false, nil
	case ateapiv1.ActorState_ACTOR_STATE_RUNNING:
		token, err := k.config.Token(ctx, locked, t.ThreadID)
		if err != nil {
			return false, err
		}
		protocol, err := k.protocol(ctx, locked, t.ThreadID, t, token)
		if err != nil {
			return false, err
		}
		settled, ok := protocol.(interface {
			CloseNativeThreadSession(context.Context, assistantRuntimeRecord, uuid.UUID) error
		})
		if !ok {
			return false, errors.New("ATE native checkpoint settlement is unavailable")
		}
		if err := settled.CloseNativeThreadSession(ctx, locked, t.ThreadID); err != nil {
			return false, fmt.Errorf("settle ATE native thread: %w", err)
		}
		resp, err := k.config.Client.SuspendActor(ctx, &ateapiv1.SuspendActorRequest{
			Actor:   k.actorRef(t.ActorName),
			Options: &ateapiv1.SuspendOptions{Mode: ateapiv1.DurabilityMode_DURABILITY_MODE_SYNCHRONOUS},
		})
		if err != nil {
			return false, fmt.Errorf("ATE SuspendActor: %w", err)
		}
		actor = resp.GetActor()
		if !hasCompletedDurableRootfsSnapshot(actor, t.ActorUID) {
			return false, errors.New("ATE actor has no completed durable rootfs snapshot")
		}
		if _, err := k.config.Store.UpdateState(ctx, locked, t.ID, ateActorStateSuspended, nil); err != nil {
			return false, fmt.Errorf("record ATE actor suspended: %w", err)
		}
		return true, nil
	default:
		return false, errors.New("ATE actor is not ready for suspension")
	}
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
			if _, err := k.suspendThreadActorLocked(ctx, locked, t); err != nil {
				return err
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
		actors, err := k.config.Store.ListForRuntime(ctx, locked)
		if err != nil {
			return fmt.Errorf("list ATE actors for reap: %w", err)
		}
		var reapErrs []error
		for _, t := range actors {
			if t.Workload != nil {
				if err := k.config.Revoke(ctx, locked, t.Workload); err != nil {
					reapErrs = append(reapErrs, err)
					continue
				}
			}
			if err := k.validateActorRow(locked, t); err != nil || atespace == "" {
				continue
			}
			ref := &ateapiv1.ObjectRef{Atespace: atespace, Name: t.ActorName}
			actor, err := k.config.Client.GetActor(ctx, &ateapiv1.GetActorRequest{Actor: ref})
			if status.Code(err) != codes.NotFound {
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
					continue
				}
			}
			if err := k.config.Store.SoftDelete(ctx, locked, t.ID); err != nil {
				reapErrs = append(reapErrs, fmt.Errorf("retire ATE thread actor: %w", err))
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
	if _, err := k.metadata(r); err != nil {
		return false, err
	}
	actors, err := k.config.Store.ListForRuntime(ctx, r)
	if err != nil {
		return false, fmt.Errorf("list ATE actors for existence check: %w", err)
	}
	if len(actors) == 0 {
		return !r.Retired, nil
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

var _ runtimeSwitchCoordinator = (*KitATERuntimeBackend)(nil)

// RuntimeReadyForAdmission checks that the pre-created ATE atespace is
// reachable before committing a runtime-version switch to Kit on ATE.
func (k *KitATERuntimeBackend) RuntimeReadyForAdmission(ctx context.Context) error {
	if _, err := k.config.Client.GetAtespace(ctx, &ateapiv1.GetAtespaceRequest{
		Atespace: &ateapiv1.ObjectRef{Atespace: "", Name: k.config.Atespace},
	}); err != nil {
		return fmt.Errorf("%w: inspect ATE atespace %q: %w", errRuntimeSwitchUnavailable, k.config.Atespace, err)
	}
	return nil
}

type admitBackendSwitchContextKey struct{}

func withAdmitBackendSwitch(ctx context.Context) context.Context {
	return context.WithValue(ctx, admitBackendSwitchContextKey{}, true)
}

func isAdmitBackendSwitch(ctx context.Context) bool {
	allowed, _ := ctx.Value(admitBackendSwitchContextKey{}).(bool)
	return allowed
}

// PrepareRuntimeSwitch verifies destination readiness (when switching to v3)
// and that every active ATE runtime row and its running thread actors are
// settled under the caller's exclusive assistant advisory lock (`LifecycleTx`,
// which fences all `kitATEThreadLifecycle` shared locks and admissions until
// the caller's transaction commits or rolls back).
func (k *KitATERuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	result := runtimeSwitchReadiness{
		DestinationReady: false,
		Settled:          false,
		Release:          sync.OnceFunc(func() {}),
	}
	if destination != 2 && destination != 3 {
		return result, errRuntimeSwitchUnavailable
	}
	if destination == 3 {
		if err := k.RuntimeReadyForAdmission(ctx); err != nil {
			return result, err
		}
		result.DestinationReady = true
	}
	for _, row := range rows {
		if row.AssistantID != assistant.ID || row.ProjectID != assistant.ProjectID || row.LifecycleTx == nil {
			return result, ErrKitRuntimeBinding
		}
		if _, err := k.metadata(row); err != nil {
			return result, err
		}
		if !isAdmitBackendSwitch(ctx) {
			if err := k.config.CheckSettled(ctx, row); err != nil {
				if errors.Is(err, ErrKitRuntimeBinding) {
					return result, err
				}
				return result, errRuntimeSwitchBusy
			}
		}
		if err := k.verifyActorsSettledForSwitch(ctx, row); err != nil {
			return result, err
		}
	}
	result.Settled = true
	return result, nil
}

func (k *KitATERuntimeBackend) verifyActorsSettledForSwitch(ctx context.Context, row assistantRuntimeRecord) error {
	actors, err := k.config.Store.ListForRuntime(ctx, row)
	if err != nil {
		return fmt.Errorf("%w: list ATE actors for switch: %w", errRuntimeSwitchUnavailable, err)
	}
	for _, t := range actors {
		if err := k.validateActorRow(row, t); err != nil {
			return err
		}
		actor, err := k.getActor(ctx, t)
		if status.Code(err) == codes.NotFound {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: inspect ATE actor for switch: %w", errRuntimeSwitchUnavailable, err)
		}
		switch actor.GetStatus().GetState() {
		case ateapiv1.ActorState_ACTOR_STATE_SUSPENDED, ateapiv1.ActorState_ACTOR_STATE_CRASHED:
			continue
		case ateapiv1.ActorState_ACTOR_STATE_RUNNING:
			token, err := k.config.Token(ctx, row, t.ThreadID)
			if err != nil {
				return fmt.Errorf("%w: load ATE actor token for switch: %w", errRuntimeSwitchUnavailable, err)
			}
			protocol, err := k.protocol(ctx, row, t.ThreadID, t, token)
			if err != nil {
				return fmt.Errorf("%w: build ATE actor protocol for switch: %w", errRuntimeSwitchUnavailable, err)
			}
			checker, ok := protocol.(interface {
				VerifyNativeThreadSettled(context.Context, assistantRuntimeRecord, uuid.UUID) error
			})
			if !ok {
				return errRuntimeSwitchUnavailable
			}
			if err := checker.VerifyNativeThreadSettled(ctx, row, t.ThreadID); err != nil {
				if errors.Is(err, ErrKitRuntimeBinding) {
					return fmt.Errorf("verify ATE actor switch settlement: %w", err)
				}
				return errRuntimeSwitchBusy
			}
		default:
			return errRuntimeSwitchBusy
		}
	}
	return nil
}

func kitATETrustedDNSHost(host string) bool {
	u, err := url.Parse("https://" + host)
	return err == nil && host != "" && strings.Contains(host, ".") && !strings.ContainsAny(host, "/*:@?# \t\r\n") && !strings.HasSuffix(host, ".") && net.ParseIP(host) == nil && u.User == nil && u.Host == host && u.Hostname() == host
}
