package assistants

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// KitATEWorkloadAssertionPath stays outside container-managed secrets mounts.
const KitATEWorkloadAssertionPath = "/var/run/speakeasy/workload/assertion"

// KitATECompositionConfig is an explicit development-preview opt-in. It is
// separate from provider selection so legacy runtimes retain their backend.
type KitATECompositionConfig struct {
	KitLocalCompositionConfig
	Enabled      bool
	PreviewID    string
	ClientConfig ateclient.Config
	Atespace     string
	Template     string
	EgressHosts  []string
}

type kitATEWorkloadIdentity struct {
	Atespace, ActorName string
}

var kitATEPreviewIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ValidateKitATEPreview rejects production, local and unnamed preview startup.
// Neither an API URL nor a template implicitly enables the preview.
func ValidateKitATEPreview(environment string, enabled bool, previewID string) error {
	if !enabled {
		if previewID != "" {
			return errors.New("ATE preview identifier requires explicit assistant ATE enablement")
		}
		return nil
	}
	if environment != "dev" {
		return errors.New("assistant ATE preview requires the dev environment")
	}
	if len(previewID) > 63 || !kitATEPreviewIDPattern.MatchString(previewID) {
		return errors.New("assistant ATE preview requires an explicit DNS-label preview identifier")
	}
	return nil
}

func validateKitATEComposition(c KitATECompositionConfig) error {
	if err := ValidateKitATEPreview(c.Environment, c.Enabled, c.PreviewID); err != nil {
		return err
	}
	if !c.Enabled {
		return errors.New("assistant ATE preview is not enabled")
	}
	for _, name := range []string{c.Atespace, c.Template} {
		if len(name) > 63 || !kitATEPreviewIDPattern.MatchString(name) {
			return errors.New("assistant ATE requires explicit DNS-label atespace and template names")
		}
	}
	if c.nativeIdentity != nil || c.WorkloadSubject != "" || c.WorkloadAssertionFile != "" || c.Network != "" || c.ExtraCACertFile != "" || c.AllowInsecureInference {
		return errors.New("assistant ATE rejects GKE identity, operator assertions and local network or TLS overrides")
	}
	if !kitGKEExplicitImage(c.Image) {
		return errors.New("assistant ATE requires an explicit non-latest image tag or digest")
	}
	if !c.PersistedMCP {
		return errors.New("assistant ATE requires persisted assistant MCP attachments")
	}
	return validateKitComposition(c.KitLocalCompositionConfig)
}

// InstallKitATERuntime installs only the version-3 dispatch branch. The backend
// must create actors lazily when the thread is known, not during Ensure.
func (s *ServiceCore) InstallKitATERuntime(ctx context.Context, c KitATECompositionConfig) error {
	if err := validateKitATEComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("ATE requires guarded HTTP, transcript, inference and encrypted gateway credential services")
	}
	if err := s.validateKitATEBrokerKeys(ctx, c.WorkloadJWKSURL); err != nil {
		return err
	}
	c.ClientConfig.Atespace = c.Atespace
	client, err := ateclient.New(ctx, c.ClientConfig)
	if err != nil {
		return fmt.Errorf("create ATE control client: %w", err)
	}
	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Environment: c.Environment, PreviewID: c.PreviewID, Image: c.Image, Atespace: c.Atespace, Template: c.Template, ServerURL: s.serverURL, Client: client, EgressHosts: append(slices.Clone(c.EgressHosts), c.Origin.Hostname(), s.serverURL.Hostname()),
		Bootstrap: s.kitATECompositionBootstrap(c), Token: s.kitATEToken, Protocol: s.kitATEProtocolFactory(c), Lifecycle: s.kitATELifecycle, Revoke: s.kitATERevoke, CheckSettled: s.kitATECheckSettled,
	})
	if err != nil {
		_ = client.Close()
		return err
	}
	if err := s.installKitRuntime(backend); err != nil {
		_ = client.Close()
		return err
	}
	s.newAssistantRuntimeEligible = func(ctx context.Context, tx pgx.Tx, assistant assistantRecord) (bool, error) {
		return s.kitGKEAssistantEligible(ctx, tx, assistant, c.KitLocalCompositionConfig)
	}
	return nil
}

// kitATEActorToken derives an actor-only gateway credential from the encrypted
// runtime generation secret. Actor names must never be reused after deletion.
func kitATEActorToken(root, actorName string) string {
	mac := hmac.New(sha256.New, []byte(root))
	_, _ = mac.Write([]byte("speakeasy-kit-ate-actor-v1\x00" + actorName))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Use the broker's resolver without private-CA or internal-network overrides.
// A successful control-plane TLS connection does not establish broker trust.
func (s *ServiceCore) validateKitATEBrokerKeys(ctx context.Context, raw string) error {
	source, err := jwks.NewRemoteSource(raw)
	if err != nil {
		return fmt.Errorf("configure ATE broker JWKS: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resolver := jwks.NewResolver(s.guardianPolicy, noop.NewMeterProvider(), s.logger)
	var initialCache jwks.CacheState
	if _, err := resolver.Resolve(ctx, source, initialCache); err != nil {
		return fmt.Errorf("ATE JWKS must be reachable through the broker's public HTTPS trust path: %w", err)
	}
	return nil
}

func (s *ServiceCore) kitATERevoke(ctx context.Context, r assistantRuntimeRecord, binding *KitWorkloadBinding) error {
	if binding == nil || r.LifecycleTx == nil {
		return ErrKitRuntimeBinding
	}
	raw, err := json.Marshal(struct {
		Workload *KitWorkloadBinding `json:"workload"`
	}{binding})
	if err != nil {
		return fmt.Errorf("encode ATE workload revocation: %w", err)
	}
	r.BackendMetadataJSON = raw
	return s.revokeKitWorkload(ctx, r.LifecycleTx, r)
}

func (k *KitATERuntimeBackend) CheckpointIdleRuntime(ctx context.Context, r assistantRuntimeRecord) error {
	return k.Stop(ctx, r)
}

func (s *ServiceCore) kitATECheckSettled(ctx context.Context, r assistantRuntimeRecord) error {
	if r.LifecycleTx == nil || r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE {
		return ErrKitRuntimeBinding
	}
	busy, err := assistantrepo.New(r.LifecycleTx).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID})
	if err != nil {
		return fmt.Errorf("check ATE unsettled events and wakes: %w", err)
	}
	if !busy.Valid || busy.Bool {
		return errors.New("ATE suspension requires settled assistant events and wakes")
	}
	return nil
}

func (s *ServiceCore) validateKitATEBootstrapWorkload(ctx context.Context, r assistantRuntimeRecord, c KitLocalCompositionConfig, b *KitWorkloadBinding, identity assistantidentity.RuntimeIdentity, gateway string) error {
	if c.ateIdentity == nil || r.LifecycleTx == nil || !kitWorkloadIdentityMatches(r, b.Identity) || b.Identity != identity || b.IssuerID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	subject, _, err := kitWorkloadSubject(r, c, gateway)
	if err != nil || subject != b.Subject {
		return ErrKitRuntimeBinding
	}
	digest := sha256.Sum256([]byte(gateway))
	if b.Generation != fmt.Sprintf("%x", digest[:16]) {
		return ErrKitRuntimeBinding
	}
	project := uuid.NullUUID{UUID: r.ProjectID, Valid: true}
	q := workloadrepo.New(r.LifecycleTx)
	issuer, err := q.GetWorkloadIssuer(ctx, workloadrepo.GetWorkloadIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: project, ID: b.IssuerID})
	if err != nil {
		return fmt.Errorf("load recovered ATE workload issuer: %w", err)
	}
	canonicalIssuer, err := issuerurl.ParseHTTPSOnly(c.WorkloadIssuerURL)
	if err != nil {
		return fmt.Errorf("canonicalize recovered ATE workload issuer: %w", err)
	}
	if issuer.ProjectID != project || !kitOwnedWorkloadIssuer(issuer, canonicalIssuer.String(), c.WorkloadJWKSURL) {
		return ErrKitRuntimeBinding
	}
	admissions, err := q.ListWorkloadAdmissions(ctx, workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: project})
	if err != nil {
		return fmt.Errorf("load recovered ATE admissions: %w", err)
	}
	admitted := false
	for _, a := range admissions {
		if a.WorkloadIssuerID == b.IssuerID && a.Subject == b.Subject && a.MatchKind == "exact" && a.ProjectID == project && a.Name.String == "kit-runtime-"+r.ID.String()+"-"+b.Generation {
			admitted = true
		}
	}
	if !admitted {
		return ErrKitRuntimeBinding
	}
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: b.IssuerID, Subject: b.Subject, MatchKind: "exact"})
	if err != nil {
		return fmt.Errorf("load recovered ATE agent assignments: %w", err)
	}
	if len(assignments) == 0 {
		return ErrKitRuntimeBinding
	}
	for _, assignment := range assignments {
		if assignment.AgentID != identity.AgentID {
			return ErrKitRuntimeBinding
		}
	}
	shared, err := usersessionsrepo.New(r.LifecycleTx).GetSharedUserSessionIssuerByID(ctx, c.MCPUserSessionIssuerID)
	if err != nil {
		return fmt.Errorf("load recovered ATE broker issuer: %w", err)
	}
	if shared.ResolvedOrganizationID != identity.OrganizationID || (shared.UserSessionIssuer.ProjectID.Valid && shared.UserSessionIssuer.ProjectID.UUID != r.ProjectID) {
		return ErrKitRuntimeBinding
	}
	audience, err := (authserver.Hosts{ServerURL: s.serverURL.String(), AuthenticationHostBaseURL: c.WorkloadIssuerBaseURL, PlatformHosts: nil}).SharedIssuerURL(shared.UserSessionIssuer)
	if err != nil {
		return fmt.Errorf("resolve recovered ATE broker audience: %w", err)
	}
	if b.Audience != audience {
		return ErrKitRuntimeBinding
	}
	return nil
}

func (s *ServiceCore) kitATELifecycle(ctx context.Context, r assistantRuntimeRecord, operation func(context.Context, assistantRuntimeRecord) error) error {
	if r.LifecycleTx != nil {
		if _, ok := ctx.Value(kitAdmissionProgressKey{}).(kitAdmissionProgressWriter); !ok {
			return ErrKitRuntimeBinding
		}
		return operation(ctx, r)
	}
	if existing, ok := ctx.Value(kitLifecycleTxKey{}).(pgx.Tx); ok {
		r.LifecycleTx = existing
		return operation(ctx, r)
	}
	tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
	if err != nil {
		return fmt.Errorf("begin Kit ATE lifecycle: %w", err)
	}
	defer release()
	q := assistantrepo.New(tx)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: r.ProjectID, ID: r.ID})
	if err != nil {
		return fmt.Errorf("reload Kit ATE lifecycle: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 || row.Backend != "ate" {
		return ErrKitRuntimeBinding
	}
	r.RuntimeVersion, r.Backend, r.State = row.RuntimeVersion, row.Backend, row.State
	r.BackendMetadataJSON, r.WarmUntil, r.LifecycleTx = row.BackendMetadataJson, row.WarmUntil, tx
	assistant, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID})
	if err != nil {
		return fmt.Errorf("read Kit ATE lifecycle assistant: %w", err)
	}
	r.Retired = row.Ended || row.Deleted || assistant.DeletedAt.Valid
	ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, q)
	ctx = context.WithValue(ctx, kitLifecycleTxKey{}, r.LifecycleTx)
	ctx = context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(progressCtx context.Context, raw []byte) error {
		if err := q.UpdateAssistantRuntimeMetadata(progressCtx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: r.ProjectID, RuntimeID: r.ID, BackendMetadataJson: raw}); err != nil {
			return fmt.Errorf("update Kit ATE admission progress: %w", err)
		}
		return tx.Publish(progressCtx)
	}))
	operationErr := operation(ctx, r)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := tx.Commit(commitCtx); err != nil {
		return errors.Join(operationErr, fmt.Errorf("commit Kit ATE lifecycle: %w", err))
	}
	return operationErr
}

func (s *ServiceCore) kitATEToken(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (string, error) {
	if threadID == uuid.Nil || r.LifecycleTx == nil {
		return "", ErrKitRuntimeBinding
	}
	root, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
	if errors.Is(err, ErrKitGatewayTokenNotFound) {
		root, err = s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	}
	if err != nil {
		return "", err
	}
	return kitATEActorToken(root, threadID.String()), nil
}

func (s *ServiceCore) kitATECompositionBootstrap(c KitATECompositionConfig) func(context.Context, assistantRuntimeRecord, uuid.UUID, string) (KitLocalBootstrap, error) {
	return func(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, actorName string) (KitLocalBootstrap, error) {
		if threadID == uuid.Nil || len(actorName) > 63 || !kitATEPreviewIDPattern.MatchString(actorName) {
			return KitLocalBootstrap{}, ErrKitRuntimeBinding
		}
		if r.LifecycleTx == nil {
			return KitLocalBootstrap{}, errors.New("ATE bootstrap requires the runtime lifecycle lock")
		}
		token, err := s.kitATEToken(ctx, r, threadID)
		if err != nil {
			return KitLocalBootstrap{}, err
		}
		local := c.KitLocalCompositionConfig
		var metadata kitATEMetadata
		if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil || metadata.RuntimeID != r.ID || metadata.AssistantID != r.AssistantID || metadata.ProjectID != r.ProjectID || metadata.PreviewID != c.PreviewID || metadata.Atespace != c.Atespace || metadata.Template != c.Template {
			return KitLocalBootstrap{}, ErrKitRuntimeBinding
		}
		if prior, ok := metadata.Threads[threadID.String()]; ok && prior.Workload != nil {
			if prior.ActorName != actorName || prior.ActorUID == "" {
				return KitLocalBootstrap{}, ErrKitRuntimeBinding
			}
			local.bootstrapWorkload = prior.Workload
		}
		local.ateIdentity = &kitATEWorkloadIdentity{Atespace: c.Atespace, ActorName: actorName}
		local.bootstrapGatewayToken = token
		return s.kitCompositionBootstrap(ctx, r, local)
	}
}

func (s *ServiceCore) kitATEProtocolFactory(c KitATECompositionConfig) func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error) {
	projector := NewKitTranscriptProjector(s.db, s.chatWriter, c.TurnStream, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		bound, err := s.ValidateKitRuntimeSessionInTx(ctx, tx, kitRuntimeSessionKey{ProjectID: b.ProjectID, AssistantID: b.AssistantID, RuntimeID: b.RuntimeID, ThreadID: b.ThreadID}, b.SessionID)
		if err != nil {
			return err
		}
		if bound.ChatID != b.ChatID {
			return ErrKitRuntimeBinding
		}
		return nil
	})
	return func(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, actorName string, endpoint *url.URL, client *guardian.HTTPClient, token string) (runtimeProtocol, error) {
		if threadID == uuid.Nil || actorName == "" || endpoint == nil || endpoint.Path != "/acp" || client == nil || token == "" {
			return nil, ErrKitRuntimeBinding
		}
		p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{Core: s, Endpoint: endpoint, HTTPClient: client, Projector: projector, CWD: KitWorkspacePath, GatewayToken: token})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: p, core: s}, nil
	}
}
