package codemode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/sandbox"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	// codeClaimTTL lets the cluster reap orphaned pods even after a Speakeasy
	// crash. Live runners renew it, so a busy pod is not replaced every TTL.
	codeClaimTTL = 5 * time.Minute
	// codeClaimRenewMargin renews a claim this long before it expires, which
	// leaves several sweeps to retry a failed renewal before the drain margin.
	codeClaimRenewMargin = 2 * time.Minute
	// codeIdleTTL releases an organization's pod after ten minutes without
	// executions, so an active organization does not cold-start between bursts.
	codeIdleTTL = 10 * time.Minute
	// codeRunnerMaxAge retires a pod this long after its claim, even while it
	// is busy: a replacement warms beside it and in-flight executions finish
	// first. A busy organization may never go idle, so this is what rolls a
	// template change (image, capacity, resources) out to its pods.
	codeRunnerMaxAge = 30 * time.Minute
	// codeMismatchedRunnerAge retires sooner a pod whose runner reports a
	// capacity other than RunnerStreams, such as a warm pod from a previous
	// template. It still serves at its own capacity until then, and a
	// misconfigured template costs one replacement claim per pod per period.
	codeMismatchedRunnerAge = 2 * time.Minute
	// codeTemplateRefresh is how often the sandbox template's presence is
	// re-read, so a deploy that installs it late enables Code Mode promptly.
	codeTemplateRefresh = 15 * time.Second
	// codeTemplateCheckTimeout bounds one template read.
	codeTemplateCheckTimeout = 3 * time.Second
	// codeCapacityWait bounds how long an execution waits for a stream when
	// its organization's pods are all busy and at their cap.
	codeCapacityWait = DefaultAdmissionWait
	// codeTemplateNotFoundReason is the claim condition reason the sandbox
	// controller reports when the referenced template does not exist.
	codeTemplateNotFoundReason = "TemplateNotFound"
	// codeDrainMargin reserves enough lifetime for execution and stream cleanup.
	codeDrainMargin = MaxWallTime + 5*time.Second
	// codePoolSweep bounds idle resource retention without durable background jobs.
	codePoolSweep = 10 * time.Second
	// codeClaimPollStart is the first readiness poll after creating a claim. Polls
	// double from here so a warm pod, which the controller binds within a few
	// hundred milliseconds, is seen promptly.
	codeClaimPollStart = 25 * time.Millisecond
	// codeClaimPoll caps the poll interval for cold starts.
	codeClaimPoll = time.Second
	// codeColdStartTimeout allows a cold pod to become ready independently of one request.
	codeColdStartTimeout = 2 * time.Minute
	// defaultMaxOrganizationRunners caps one organization's shared pods per
	// replica: four pods at twelve streams each serve 48 concurrent
	// executions, and keep one organization from owning every replica slot.
	defaultMaxOrganizationRunners = 4
	// codeCleanupTimeout bounds a best-effort Kubernetes deletion; expiry is the fallback.
	codeCleanupTimeout = 3 * time.Second
	// codeShutdownTimeout allows claim cleanup while reserving the shared
	// service shutdown deadline for member sessions and verification drains.
	codeShutdownTimeout = 5 * time.Second
	// defaultMaxRunners bounds pods and bookkeeping per Speakeasy replica.
	defaultMaxRunners = 64
	// codePurposeLabel separates the code pool from assistant workloads.
	codePurposeLabel = "gram.ai/runtime-purpose"
	// codeOrganizationLabel records a digest of the organization whose
	// executions share a claimed runtime. Organization IDs are not always
	// valid label values, so the label holds a fixed-length digest.
	codeOrganizationLabel = "gram.ai/organization-digest"
	// codeOwnerLabel prevents one Speakeasy replica adopting another replica's claim.
	codeOwnerLabel = "gram.ai/runtime-owner"
)

// GKEConfig points at a dedicated code template on the assistants cluster.
// Credentials, image, namespace and network policy are operator-owned.
type GKEConfig struct {
	// Logger records failed cleanup without exposing credentials or program contents.
	Logger *slog.Logger

	// Dynamic authenticates to the remote cluster using Speakeasy's workload identity.
	Dynamic dynamic.Interface
	// Namespace contains only code-purpose claims, templates and warm-pool pods.
	Namespace string
	// Template identifies the dedicated code runner SandboxTemplate.
	Template string
	// Image is the SHA-256 reference the template runs; the running container
	// digest must match it. Previews may use a tag instead (AllowTaggedImage).
	Image string
	// AllowTaggedImage admits a tag reference for PR previews, whose server runs
	// the same pull request's code, so a digest would not constrain its author.
	// Released environments leave it false and must pin a digest.
	AllowTaggedImage bool
	// Port is the runner HTTP upgrade port in the pod.
	Port int
	// Token authenticates Speakeasy to the code runner and never reaches Monty.
	Token string
	// Dialer applies Speakeasy's outbound network policy.
	Dialer *net.Dialer
	// CIDRs bounds admissible pod IPs as well as the dialer's network exception.
	CIDRs []*net.IPNet
	// MaxRunners caps claimed pods per replica; zero uses the default.
	MaxRunners int
	// MaxOrganizationRunners caps one organization's pods per replica; zero uses the default.
	MaxOrganizationRunners int
}

var errCodeSandboxMismatch = errors.New("code sandbox identity mismatch")

// errCodeSandboxPending distinguishes cold-start polling from API failures.
var errCodeSandboxPending = errors.New("code sandbox is not ready")

// runnerScope identifies one of an organization's shared runner pods. Pods
// are shared by every caller in the organization, including the organization
// gateway: Python state never outlives an execution, each execution runs in
// its own Monty worker process, and the runner holds no credentials or tool
// authority. Speakeasy answers every tool callback with the execution's own
// scope, so sharing a pod shares CPU, never authority.
type runnerScope struct {
	organization string
	slot         int
}

// runnerOrganization validates an execution scope and returns the
// organization whose runner pool serves it.
func runnerOrganization(scope Scope) (string, error) {
	if scope.Organization == "" || (scope.Project == uuid.Nil) != scope.OrganizationWide || len(scope.Caller) == 0 {
		return "", fmt.Errorf("code runtime requires project ownership and a caller")
	}
	return scope.Organization, nil
}

// organizationLabel is the label value that binds a claim to its organization.
func organizationLabel(organization string) string {
	digest := sha256.Sum256([]byte(organization))
	return hex.EncodeToString(digest[:16])
}

type projectRunner struct {
	key  runnerScope
	name string
	uid  types.UID
	// podUID and ip pin the pod admitted at creation.
	podUID types.UID
	ip     string
	// expires is the claim's confirmed shutdown time.
	expires time.Time
	// claimed is when this replica created the claim; it bounds the pod's age.
	claimed time.Time
	// renewal is a shutdown time being written to the claim; until the write
	// is confirmed, either time is accepted.
	renewal time.Time
	idle    time.Time
	refs    int
	// waiting counts executions waiting for this pod to finish warming.
	waiting  int
	creating bool
	retired  bool
	cleaning bool
	ready    chan struct{}
	client   *RunnerClient
	err      error
}

// GKEProvider keeps bounded pools of organization-scoped runner pods. Python
// state never lives in this pool: every stream starts a fresh worker process
// inside its pod.
type GKEProvider struct {
	config  GKEConfig
	owner   string
	now     func() time.Time
	mu      sync.Mutex
	entries map[runnerScope]*projectRunner
	// changed is closed and replaced whenever a stream or pod slot frees, to
	// wake executions waiting for capacity.
	changed chan struct{}
	// available is false once the sandbox template is known to be missing.
	available atomic.Bool
	// templateUnreadable records that a forbidden read was already logged.
	templateUnreadable atomic.Bool
	// capacityWait bounds the wait for a stream at the organization's cap.
	capacityWait time.Duration
	closed       bool
	ctx          context.Context //nolint:containedctx // Provider-owned lifetime for cold starts; never a request context.
	cancel       context.CancelFunc
	draining     map[*projectRunner]struct{}
	workers      sync.WaitGroup
	leases       sync.WaitGroup
	done         chan struct{}
}

// NewGKEProvider starts only local resource housekeeping; execution is never queued.
func NewGKEProvider(ctx context.Context, config GKEConfig) (*GKEProvider, error) {
	imageOK := validImageReference(config.Image) || (config.AllowTaggedImage && validTaggedImageReference(config.Image))
	if config.Logger == nil || config.Dynamic == nil || config.Namespace == "" || config.Template == "" || !imageOK || config.Port < 1 || config.Port > 65535 || len(config.CIDRs) == 0 {
		return nil, fmt.Errorf("code GKE runtime requires a dedicated namespace, template, SHA-256-pinned image (previews may use a tag), port and pod CIDRs")
	}
	for _, cidr := range config.CIDRs {
		if cidr == nil {
			return nil, fmt.Errorf("code GKE pod CIDRs must be non-nil")
		}
	}
	// Validate authentication and transport configuration before claiming any pod.
	check, err := NewRunnerClient("http://127.0.0.1", config.Token, config.Dialer)
	if err != nil {
		return nil, err
	}
	_ = check.Close()
	if config.MaxRunners <= 0 {
		config.MaxRunners = defaultMaxRunners
	}
	if config.MaxOrganizationRunners <= 0 {
		config.MaxOrganizationRunners = defaultMaxOrganizationRunners
	}
	runCtx, cancel := context.WithCancel(ctx)
	p := &GKEProvider{config: config, owner: uuid.NewString(), now: time.Now, mu: sync.Mutex{}, entries: make(map[runnerScope]*projectRunner), changed: make(chan struct{}), available: atomic.Bool{}, templateUnreadable: atomic.Bool{}, capacityWait: codeCapacityWait, closed: false, ctx: runCtx, cancel: cancel, draining: make(map[*projectRunner]struct{}), workers: sync.WaitGroup{}, leases: sync.WaitGroup{}, done: make(chan struct{})}
	// Until the template is read, assume it exists: an API brownout at startup
	// must not disable Code Mode, and a missing template is caught here or by
	// the first claim's TemplateNotFound condition.
	p.available.Store(true)
	p.checkTemplate(runCtx)
	go p.housekeep(runCtx)
	return p, nil
}

// Available reports whether the sandbox template is installed. While it is
// missing, as during a deploy that has not synced it yet, serving falls back
// to Progressive discovery instead of refusing every execution.
func (p *GKEProvider) Available() bool { return p.available.Load() }

func (p *GKEProvider) checkTemplate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, codeTemplateCheckTimeout)
	defer cancel()
	_, err := p.config.Dynamic.Resource(sandbox.Templates).Namespace(p.config.Namespace).Get(ctx, p.config.Template, metav1.GetOptions{})
	switch {
	case err == nil:
		p.available.Store(true)
	case k8serrors.IsNotFound(err):
		if p.available.Swap(false) {
			p.config.Logger.WarnContext(ctx, "code sandbox template is missing; serving Code Mode gateways progressively", attr.SlogError(err))
		}
	case k8serrors.IsForbidden(err):
		// Without read access to templates the claim's condition is the only
		// signal, so assume the template exists.
		p.available.Store(true)
		if !p.templateUnreadable.Swap(true) {
			p.config.Logger.WarnContext(ctx, "code sandbox template is unreadable; relying on claim conditions", attr.SlogError(err))
		}
	default:
		p.config.Logger.WarnContext(ctx, "read code sandbox template", attr.SlogError(err))
	}
}

// notifyLocked wakes executions waiting for a free stream or pod slot.
func (p *GKEProvider) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// Acquire places an execution on the least busy of its organization's pods,
// warming another pod when every pod is full and the organization is under
// its cap, and otherwise waiting briefly for a free stream. Admission
// refreshes cluster ownership, image and pod identity before returning a lease.
func (p *GKEProvider) Acquire(ctx context.Context, scope Scope) (Lease, error) {
	organization, err := runnerOrganization(scope)
	if err != nil {
		return nil, err
	}
	evicted := false
	var capacityTimer *time.Timer
	defer func() {
		if capacityTimer != nil {
			capacityTimer.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("acquire code sandbox: %w", err)
		}
		if !p.Available() {
			return nil, fmt.Errorf("%w: code sandbox template is not installed", ErrRuntimeUnavailable)
		}
		p.mu.Lock()
		if p.closed || p.ctx.Err() != nil {
			p.mu.Unlock()
			return nil, fmt.Errorf("%w: code runtime is stopping", ErrRuntimeUnavailable)
		}
		var best, warming, expiring *projectRunner
		organizationRunners, warmingRunners, warmingDemand := 0, 0, 0
		for _, entry := range p.entries {
			if entry.key.organization != organization {
				continue
			}
			organizationRunners++
			switch {
			case entry.creating:
				warmingRunners++
				warmingDemand += entry.waiting
				if warming == nil || entry.waiting < warming.waiting {
					warming = entry
				}
			case entry.retired || !p.now().Add(codeDrainMargin).Before(entry.expires) || p.agedLocked(ctx, entry):
				expiring = entry
			case entry.refs < p.streamsLocked(entry) && (best == nil || entry.refs < best.refs):
				best = entry
			}
		}
		if expiring != nil {
			idle := p.retireLocked(expiring)
			p.mu.Unlock()
			if idle {
				p.destroy(ctx, expiring)
			}
			continue
		}
		for entry := range p.draining {
			if entry.key.organization == organization {
				organizationRunners++
			}
		}
		// Warm another pod alongside those already warming once their
		// streams are spoken for, so a burst does not warm pods one by one.
		if best == nil && warming != nil && warmingDemand >= warmingRunners*RunnerStreams && organizationRunners < p.config.MaxOrganizationRunners && len(p.entries)+len(p.draining) < p.config.MaxRunners {
			warming = nil
		}
		if best == nil && warming == nil {
			if len(p.entries)+len(p.draining) >= p.config.MaxRunners || organizationRunners >= p.config.MaxOrganizationRunners {
				var oldest *projectRunner
				// Evict at most once per execution, so failing deletions
				// cannot let one execution clear every idle pod.
				if organizationRunners < p.config.MaxOrganizationRunners && !evicted {
					for _, candidate := range p.entries {
						if candidate.creating || candidate.refs != 0 {
							continue
						}
						if oldest == nil || candidate.idle.Before(oldest.idle) {
							oldest = candidate
						}
					}
				}
				if oldest != nil {
					evicted = true
					p.retireLocked(oldest)
					p.mu.Unlock()
					p.destroy(ctx, oldest)
					continue
				}
				changed := p.changed
				p.mu.Unlock()
				if capacityTimer == nil {
					capacityTimer = time.NewTimer(p.capacityWait)
				}
				select {
				case <-changed:
					continue
				case <-capacityTimer.C:
					return nil, fmt.Errorf("%w: code runtime pool is at capacity", ErrCapacity)
				case <-ctx.Done():
					return nil, fmt.Errorf("wait for code runtime capacity: %w", ctx.Err())
				}
			}
			slot := 0
			for p.entries[runnerScope{organization: organization, slot: slot}] != nil {
				slot++
			}
			warming = &projectRunner{key: runnerScope{organization: organization, slot: slot}, name: "gram-code-" + uuid.NewString(), uid: "", podUID: "", ip: "", expires: p.now().UTC().Add(codeClaimTTL).Truncate(time.Second), claimed: p.now(), renewal: time.Time{}, idle: time.Time{}, refs: 0, waiting: 0, creating: true, retired: false, cleaning: false, ready: make(chan struct{}), client: nil, err: nil}
			p.entries[warming.key] = warming
			p.workers.Add(1)
			go p.initialize(warming)
		}
		if best == nil {
			ready := warming.ready
			warming.waiting++
			p.mu.Unlock()
			var waitErr error
			select {
			case <-ready:
				waitErr = warming.err
			case <-ctx.Done():
				waitErr = fmt.Errorf("wait for code sandbox: %w", ctx.Err())
			}
			p.mu.Lock()
			warming.waiting--
			p.mu.Unlock()
			if waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		entry := best
		entry.refs++
		p.leases.Add(1)
		p.mu.Unlock()
		// A live connection can outlast its pod's readiness, so every admission
		// checks the claim, sandbox and pod (deletion, readiness, UID, IP and
		// image), whether it reuses that connection or dials a new one.
		if err := p.inspect(ctx, entry, false); err != nil {
			mismatch := errors.Is(err, errCodeSandboxMismatch)
			if mismatch {
				p.mu.Lock()
				p.retireLocked(entry)
				p.mu.Unlock()
			}
			_ = p.release(ctx, entry, false)
			if mismatch {
				// The pod is retired; place the execution on another or a new one.
				continue
			}
			return nil, fmt.Errorf("validate code sandbox admission: %w", err)
		}
		p.mu.Lock()
		retired := entry.retired || p.closed
		p.mu.Unlock()
		if retired {
			_ = p.release(ctx, entry, false)
			continue
		}
		return &gkeLease{provider: p, entry: entry, once: sync.Once{}}, nil
	}
}

// streamsLocked is how many executions an admitted pod takes at once: the
// capacity its runner reported, or RunnerStreams before it has been reached.
func (p *GKEProvider) streamsLocked(entry *projectRunner) int {
	if entry.client != nil {
		if capacity := entry.client.knownCapacity(); capacity > 0 {
			return capacity
		}
	}
	return RunnerStreams
}

// agedLocked reports whether a pod should be replaced for its age, sooner
// when its runner's capacity does not match the server's.
func (p *GKEProvider) agedLocked(ctx context.Context, entry *projectRunner) bool {
	age := p.now().Sub(entry.claimed)
	if age >= codeRunnerMaxAge {
		return true
	}
	streams := p.streamsLocked(entry)
	if streams == RunnerStreams || age < codeMismatchedRunnerAge {
		return false
	}
	p.config.Logger.WarnContext(ctx, "code runner capacity differs from the server's streams; replacing its pod", attr.SlogValueInt(streams), attr.SlogOrganizationID(entry.key.organization))
	return true
}

// retireLocked removes an admission target while retaining active leases in the
// bounded draining set. A replacement may warm while those executions finish.
func (p *GKEProvider) retireLocked(entry *projectRunner) bool {
	entry.retired = true
	if p.entries[entry.key] == entry {
		delete(p.entries, entry.key)
	}
	p.draining[entry] = struct{}{}
	return entry.refs == 0 && !entry.creating
}

func (p *GKEProvider) initialize(entry *projectRunner) {
	defer p.workers.Done()
	ctx, cancel := context.WithTimeout(p.ctx, codeColdStartTimeout)
	defer cancel()
	err := p.create(ctx, entry)
	p.mu.Lock()
	entry.creating = false
	entry.err = err
	entry.idle = p.now()
	destroy := false
	if err != nil || p.closed {
		destroy = p.retireLocked(entry)
	}
	p.notifyLocked()
	p.mu.Unlock()
	if destroy {
		p.destroy(ctx, entry)
	}
	close(entry.ready)
}

func (p *GKEProvider) create(ctx context.Context, entry *projectRunner) error {
	claim := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": sandbox.Claims.Group + "/" + sandbox.Claims.Version, "kind": "SandboxClaim",
		"metadata": map[string]any{"name": entry.name, "namespace": p.config.Namespace, "labels": map[string]any{codePurposeLabel: "code", codeOrganizationLabel: organizationLabel(entry.key.organization), codeOwnerLabel: p.owner}},
		"spec":     map[string]any{"sandboxTemplateRef": map[string]any{"name": p.config.Template}, "lifecycle": map[string]any{"shutdownPolicy": "Delete", "shutdownTime": entry.expires.Format(time.RFC3339)}},
	}}
	created, err := p.config.Dynamic.Resource(sandbox.Claims).Namespace(p.config.Namespace).Create(ctx, claim, metav1.CreateOptions{})
	if err != nil {
		// A timeout can hide a successful Create. Recover only our random-name claim
		// and record its UID so cleanup cannot delete a replacement.
		recovery, stop := context.WithTimeout(context.WithoutCancel(ctx), codeCleanupTimeout)
		existing, readErr := p.config.Dynamic.Resource(sandbox.Claims).Namespace(p.config.Namespace).Get(recovery, entry.name, metav1.GetOptions{})
		stop()
		if readErr == nil && existing.GetLabels()[codeOwnerLabel] == p.owner {
			entry.uid = existing.GetUID()
		}
		return fmt.Errorf("create code sandbox claim: %w", err)
	}
	entry.uid = created.GetUID()
	if entry.uid == "" {
		return fmt.Errorf("code sandbox claim has no UID")
	}
	if err := p.checkClaim(created, entry); err != nil {
		return err
	}
	poll := time.NewTimer(codeClaimPollStart)
	defer poll.Stop()
	for delay := codeClaimPollStart; ; delay = min(2*delay, codeClaimPoll) {
		err := p.inspect(ctx, entry, true)
		if err == nil {
			break
		}
		// API brownouts must not discard a warming pod. Only a confirmed identity
		// mismatch or a missing template aborts before the provider-owned
		// cold-start deadline.
		if errors.Is(err, errCodeSandboxMismatch) || errors.Is(err, ErrRuntimeUnavailable) {
			return err
		}
		poll.Reset(delay)
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for code sandbox: %w", ctx.Err())
		case <-poll.C:
		}
	}
	dialer := *p.config.Dialer
	control := dialer.ControlContext
	dialer.ControlContext = func(ctx context.Context, network, address string, conn syscall.RawConn) error {
		// Reconnection must not dial a stale IP that has been reassigned to another pod.
		if err := p.inspect(ctx, entry, false); err != nil {
			return err
		}
		if control != nil {
			return control(ctx, network, address, conn)
		}
		return nil
	}
	client, err := NewRunnerClient("http://"+net.JoinHostPort(entry.ip, strconv.Itoa(p.config.Port)), p.config.Token, &dialer)
	if err != nil {
		return err
	}
	entry.client = client
	return nil
}

func (p *GKEProvider) checkClaim(claim *unstructured.Unstructured, entry *projectRunner) error {
	labels := claim.GetLabels()
	template, _, _ := unstructured.NestedString(claim.Object, "spec", "sandboxTemplateRef", "name")
	expires, _, _ := unstructured.NestedString(claim.Object, "spec", "lifecycle", "shutdownTime")
	policy, _, _ := unstructured.NestedString(claim.Object, "spec", "lifecycle", "shutdownPolicy")
	p.mu.Lock()
	confirmed, renewal := entry.expires, entry.renewal
	p.mu.Unlock()
	expiryOK := expiryMatches(expires, confirmed) || (!renewal.IsZero() && expiryMatches(expires, renewal))
	if claim.GetUID() != entry.uid || claim.GetDeletionTimestamp() != nil || labels[codePurposeLabel] != "code" || labels[codeOrganizationLabel] != organizationLabel(entry.key.organization) || labels[codeOwnerLabel] != p.owner || template != p.config.Template || !expiryOK || policy != "Delete" {
		return fmt.Errorf("%w: code sandbox claim ownership, purpose, template or expiry mismatch", errCodeSandboxMismatch)
	}
	if !p.now().Add(codeDrainMargin).Before(confirmed) {
		return fmt.Errorf("%w: code sandbox claim is expiring", errCodeSandboxMismatch)
	}
	return nil
}

func (p *GKEProvider) inspect(ctx context.Context, entry *projectRunner, initial bool) error {
	claim, err := p.config.Dynamic.Resource(sandbox.Claims).Namespace(p.config.Namespace).Get(ctx, entry.name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return fmt.Errorf("%w: claim no longer exists", errCodeSandboxMismatch)
	}
	if err != nil {
		return fmt.Errorf("read code sandbox claim: %w", err)
	}
	if err := p.checkClaim(claim, entry); err != nil {
		return err
	}
	if initial && templateNotFound(claim) {
		if p.available.Swap(false) {
			p.config.Logger.WarnContext(ctx, "code sandbox claim reports a missing template; serving Code Mode gateways progressively")
		}
		return fmt.Errorf("%w: code sandbox template is not installed", ErrRuntimeUnavailable)
	}
	name := sandbox.AssignedName(claim)
	if name == "" {
		return missingCodeSandbox(initial)
	}
	resource, err := p.config.Dynamic.Resource(sandbox.Sandboxes).Namespace(p.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return missingCodeSandbox(initial)
	}
	if err != nil {
		return fmt.Errorf("read code sandbox: %w", err)
	}
	if initial && !sandbox.Ready(resource) {
		return errCodeSandboxPending
	}
	pods, err := sandbox.RunningPods(ctx, p.config.Dynamic, p.config.Namespace, string(entry.uid))
	if err != nil {
		return fmt.Errorf("discover code pod: %w", err)
	}
	if len(pods) == 0 {
		return missingCodeSandbox(initial)
	}
	if len(pods) != 1 {
		return fmt.Errorf("%w: code claim must own exactly one running pod", errCodeSandboxMismatch)
	}
	pod := &pods[0]
	if !initial && entry.podUID != pod.GetUID() {
		return fmt.Errorf("%w: code pod identity changed", errCodeSandboxMismatch)
	}
	if !sandbox.Ready(pod) || !sandbox.Ready(resource) {
		return errCodeSandboxPending
	}
	runtimeClass, _, _ := unstructured.NestedString(pod.Object, "spec", "runtimeClassName")
	automount, hasAutomount, _ := unstructured.NestedBool(pod.Object, "spec", "automountServiceAccountToken")
	hostNetwork, _, _ := unstructured.NestedBool(pod.Object, "spec", "hostNetwork")
	if runtimeClass != "gvisor" || !hasAutomount || automount || hostNetwork {
		return fmt.Errorf("%w: code pod isolation policy mismatch", errCodeSandboxMismatch)
	}
	ip, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
	parsed := net.ParseIP(ip)
	allowed := false
	for _, cidr := range p.config.CIDRs {
		if cidr.Contains(parsed) {
			allowed = true
			break
		}
	}
	if parsed == nil || !allowed || pod.GetUID() == "" {
		return fmt.Errorf("%w: code pod has no admissible network identity", errCodeSandboxMismatch)
	}
	containers, _, _ := unstructured.NestedSlice(pod.Object, "spec", "containers")
	if len(containers) != 1 {
		return fmt.Errorf("%w: code pod must have exactly one container", errCodeSandboxMismatch)
	}
	container, ok := containers[0].(map[string]any)
	if !ok || container["name"] != "code-runner" || container["image"] != p.config.Image {
		return fmt.Errorf("%w: code pod image or runtime purpose mismatch", errCodeSandboxMismatch)
	}
	statuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	if len(statuses) != 1 {
		return fmt.Errorf("%w: code pod image status is unavailable", errCodeSandboxMismatch)
	}
	status, ok := statuses[0].(map[string]any)
	imageID, _ := status["imageID"].(string)
	if !ok || status["name"] != "code-runner" || status["ready"] != true || imageID == "" {
		return fmt.Errorf("%w: code pod is not running the configured image", errCodeSandboxMismatch)
	}
	// A preview's tagged image has no digest to compare; a pinned one must match.
	if _, digest, pinned := strings.Cut(p.config.Image, "@"); pinned && !strings.HasSuffix(imageID, digest) {
		return fmt.Errorf("%w: code pod is not running the pinned image", errCodeSandboxMismatch)
	}
	if initial {
		entry.podUID, entry.ip = pod.GetUID(), ip
	} else if entry.podUID != pod.GetUID() || entry.ip != ip {
		return fmt.Errorf("%w: code pod identity changed", errCodeSandboxMismatch)
	}
	return nil
}

func (p *GKEProvider) release(ctx context.Context, entry *projectRunner, admitted bool) error {
	defer p.leases.Done()
	p.mu.Lock()
	entry.refs--
	if admitted {
		entry.idle = p.now()
	}
	destroy := entry.refs == 0 && (entry.retired || p.closed)
	if destroy {
		p.retireLocked(entry)
	}
	p.notifyLocked()
	p.mu.Unlock()
	if destroy {
		p.destroy(ctx, entry)
	}
	return nil
}

// destroy never deletes a replacement resource: the claim UID is a precondition.
// Native claim expiry is the fallback when Kubernetes or Speakeasy is unavailable.
func (p *GKEProvider) destroy(ctx context.Context, entry *projectRunner) {
	p.mu.Lock()
	if entry.cleaning {
		p.mu.Unlock()
		return
	}
	entry.cleaning = true
	p.mu.Unlock()
	removed := false
	defer func() {
		p.mu.Lock()
		entry.cleaning = false
		if removed {
			delete(p.draining, entry)
			p.notifyLocked()
		}
		p.mu.Unlock()
	}()
	if entry.client != nil {
		_ = entry.client.Close()
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), codeCleanupTimeout)
	defer stop()
	claims := p.config.Dynamic.Resource(sandbox.Claims).Namespace(p.config.Namespace)
	if entry.uid == "" {
		existing, err := claims.Get(cleanup, entry.name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			removed = true
			return
		}
		if err != nil {
			p.config.Logger.WarnContext(cleanup, "recover code sandbox claim ownership", attr.SlogError(err))
			return
		}
		if existing.GetLabels()[codeOwnerLabel] != p.owner {
			removed = true
			return
		}
		entry.uid = existing.GetUID()
		if entry.uid == "" {
			return
		}
	}
	if err := claims.Delete(cleanup, entry.name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &entry.uid}}); err != nil && !k8serrors.IsNotFound(err) && !k8serrors.IsConflict(err) {
		p.config.Logger.WarnContext(cleanup, "delete code sandbox claim", attr.SlogError(err), attr.SlogOrganizationID(entry.key.organization))
		return
	}
	removed = true
}

func (p *GKEProvider) housekeep(ctx context.Context) {
	defer close(p.done)
	ticker := time.NewTicker(codePoolSweep)
	defer ticker.Stop()
	templates := time.NewTicker(codeTemplateRefresh)
	defer templates.Stop()
	for {
		select {
		case <-ctx.Done():
			p.stop(ctx)
			p.workers.Wait()
			p.leases.Wait()
			return
		case <-ticker.C:
			p.reap(ctx)
			p.renew(ctx)
		case <-templates.C:
			p.checkTemplate(ctx)
		}
	}
}

// renew extends the claims of pods still in use before they expire, so an
// active organization keeps its warm pods instead of replacing them every
// claim TTL. A failed renewal is retried on the next sweep; if every attempt
// fails, the pod drains and is replaced as before.
func (p *GKEProvider) renew(ctx context.Context) {
	type renewal struct {
		entry *projectRunner
		uid   types.UID
		until time.Time
	}
	now := p.now()
	var due []renewal
	p.mu.Lock()
	for _, entry := range p.entries {
		if entry.creating || entry.retired || entry.uid == "" || entry.expires.Sub(now) > codeClaimRenewMargin {
			continue
		}
		if entry.refs == 0 && now.Sub(entry.idle) >= codeIdleTTL {
			continue
		}
		if entry.renewal.IsZero() {
			entry.renewal = now.UTC().Add(codeClaimTTL).Truncate(time.Second)
		}
		due = append(due, renewal{entry: entry, uid: entry.uid, until: entry.renewal})
	}
	p.mu.Unlock()
	claims := p.config.Dynamic.Resource(sandbox.Claims).Namespace(p.config.Namespace)
	for _, item := range due {
		patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(item.uid)}, "spec": map[string]any{"lifecycle": map[string]any{"shutdownTime": item.until.Format(time.RFC3339)}}})
		if err != nil {
			continue
		}
		write, stop := context.WithTimeout(ctx, codeCleanupTimeout)
		_, err = claims.Patch(write, item.entry.name, types.MergePatchType, patch, metav1.PatchOptions{})
		stop()
		if err != nil {
			p.config.Logger.WarnContext(ctx, "renew code sandbox claim", attr.SlogError(err), attr.SlogOrganizationID(item.entry.key.organization))
			continue
		}
		p.mu.Lock()
		if item.entry.renewal.Equal(item.until) {
			item.entry.expires, item.entry.renewal = item.until, time.Time{}
		}
		p.mu.Unlock()
	}
}

func (p *GKEProvider) reap(ctx context.Context) {
	p.mu.Lock()
	for _, entry := range p.entries {
		if entry.creating {
			continue
		}
		if !p.now().Add(codeDrainMargin).Before(entry.expires) || p.agedLocked(ctx, entry) {
			entry.retired = true
		}
		if entry.refs == 0 && (entry.retired || p.now().Sub(entry.idle) >= codeIdleTTL) {
			p.retireLocked(entry)
		}
	}
	expired := make([]*projectRunner, 0, len(p.draining))
	for entry := range p.draining {
		if entry.refs != 0 || entry.creating || entry.cleaning {
			continue
		}
		// Failed/ambiguous API operations keep consuming capacity until cleanup or
		// the controller-enforced expiry plus drain slack, preventing orphan churn.
		if p.now().After(entry.expires.Add(codeDrainMargin)) {
			delete(p.draining, entry)
			p.notifyLocked()
			continue
		}
		expired = append(expired, entry)
	}
	p.mu.Unlock()
	p.destroyAll(ctx, expired)
}

// Shutdown runs after the HTTP listener has drained. Cancelling warm-up work
// and closing residual transports cannot replay an interrupted execution.
func (p *GKEProvider) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, codeShutdownTimeout)
	defer cancel()
	p.cancel()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop code sandbox provider: %w", ctx.Err())
	}
}

func (p *GKEProvider) stop(ctx context.Context) {
	p.mu.Lock()
	p.closed = true
	var idle []*projectRunner
	var clients []*RunnerClient
	all := make(map[*projectRunner]struct{}, len(p.entries)+len(p.draining))
	for _, entry := range p.entries {
		all[entry] = struct{}{}
	}
	for entry := range p.draining {
		all[entry] = struct{}{}
	}
	for entry := range all {
		if p.retireLocked(entry) {
			idle = append(idle, entry)
		}
		if !entry.creating && entry.client != nil {
			clients = append(clients, entry.client)
		}
	}
	p.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
	p.destroyAll(ctx, idle)
}

func (p *GKEProvider) destroyAll(ctx context.Context, entries []*projectRunner) {
	// Deletions run concurrently within the fixed pool bound, so a wedged API
	// cannot multiply the shutdown deadline by the number of claimed pods.
	var wg sync.WaitGroup
	for _, entry := range entries {
		wg.Go(func() { p.destroy(ctx, entry) })
	}
	wg.Wait()
}

func expiryMatches(encoded string, expected time.Time) bool {
	actual, err := time.Parse(time.RFC3339, encoded)
	return err == nil && actual.Equal(expected)
}

type gkeLease struct {
	provider *GKEProvider
	entry    *projectRunner
	once     sync.Once
}

func (l *gkeLease) Client() *RunnerClient { return l.entry.client }
func (l *gkeLease) Release(ctx context.Context) error {
	l.once.Do(func() { _ = l.provider.release(ctx, l.entry, true) })
	return nil
}

// templateNotFound reports the controller's condition for a claim whose
// template does not exist, which never resolves for that claim.
func templateNotFound(claim *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(claim.Object, "status", "conditions")
	for _, raw := range conditions {
		if condition, ok := raw.(map[string]any); ok && condition["reason"] == codeTemplateNotFoundReason {
			return true
		}
	}
	return false
}

// A missing pod can be expected only before the first immutable identity is pinned.
func missingCodeSandbox(initial bool) error {
	if initial {
		return errCodeSandboxPending
	}
	return fmt.Errorf("%w: admitted sandbox or pod disappeared", errCodeSandboxMismatch)
}

// validImageReference requires repository@sha256:digest so the node cannot
// resolve a mutable image reference.
func validImageReference(image string) bool {
	if image == "" || strings.ContainsAny(image, " \t\n") {
		return false
	}
	repository, digest, pinned := strings.Cut(image, "@sha256:")
	return pinned && repository != "" && len(digest) == 64 && strings.Trim(digest, "0123456789abcdef") == ""
}

// validTaggedImageReference accepts repository:tag, admitted only for previews.
// It rejects latest, which names whatever was pushed last rather than one build.
func validTaggedImageReference(image string) bool {
	if image == "" || strings.ContainsAny(image, " \t\n@") {
		return false
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash+1 || colon == len(image)-1 {
		return false
	}
	tag := image[colon+1:]
	return tag != "latest" && len(tag) <= 128 && strings.Trim(tag, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-") == "" && tag[0] != '.' && tag[0] != '-'
}
