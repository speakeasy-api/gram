package assistants

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

const (
	// maxATEDNSLabelLen is the RFC 1123 DNS-label length limit for ATE names.
	maxATEDNSLabelLen = 63

	// ateBrokerJWKSTimeout bounds startup verification that the ATE JWKS URL is reachable.
	ateBrokerJWKSTimeout = 10 * time.Second

	// ateLifecycleCommitTimeout bounds committing the lifecycle advisory transaction.
	ateLifecycleCommitTimeout = 30 * time.Second
)

// KitATECompositionConfig configures the ATE Kit v3 runtime backend.
type KitATECompositionConfig struct {
	KitLocalCompositionConfig

	// Enabled selects the ATE backend when true.
	Enabled bool

	// ClientConfig holds ATE control-plane and router connection settings.
	ClientConfig ateclient.Config

	// Atespace is the target ATE namespace.
	Atespace string

	// Template is the target ATE ActorTemplate base name (versioned by image digest).
	Template string

	// WorkerSelector holds the label selector applied to created ActorTemplates.
	WorkerSelector map[string]string
}

type kitATEWorkloadIdentity struct {
	Atespace, ActorName string
}

var kitATENamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func validateKitATEComposition(c KitATECompositionConfig) error {
	if !c.Enabled {
		return errors.New("assistant ATE runtime is not enabled")
	}
	if len(c.Atespace) > maxATEDNSLabelLen || !kitATENamePattern.MatchString(c.Atespace) {
		return errors.New("assistant ATE requires an explicit DNS-label atespace name")
	}
	if len(c.Template) > maxATETemplateBaseLen || !kitATENamePattern.MatchString(c.Template) {
		return errors.New("assistant ATE requires an explicit DNS-label template base name (max 50 chars)")
	}
	if err := validateKitATEWorkerSelector(c.WorkerSelector); err != nil {
		return err
	}
	if c.WorkloadSubject != "" || c.WorkloadAssertionFile != "" || c.Network != "" || c.ExtraCACertFile != "" || c.AllowInsecureInference {
		return errors.New("assistant ATE rejects operator assertions and local network or TLS overrides")
	}
	if _, err := kitATEImageDigest(c.Image); err != nil {
		return err
	}
	for _, raw := range []string{c.WorkloadIssuerURL, c.WorkloadJWKSURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("assistant ATE requires explicit HTTPS actor JWT issuer and JWKS URLs")
		}
	}
	if c.TurnStream == nil {
		return errors.New("assistant ATE requires a turn stream")
	}
	return nil
}

// InstallKitATERuntime installs only the version-3 dispatch branch. The backend
// creates actors lazily when the thread is known, not during Ensure.
func (s *ServiceCore) InstallKitATERuntime(ctx context.Context, c KitATECompositionConfig) error {
	if err := validateKitATEComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil || s.serverURL == nil {
		return errors.New("ATE requires guarded HTTP, transcript, inference, server URL, and encrypted gateway credential services")
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
		Image:              c.Image,
		Atespace:           c.Atespace,
		Template:           c.Template,
		WorkerSelector:     c.WorkerSelector,
		ServerURL:          s.serverURL,
		Client:             client,
		Bootstrap:          s.kitATECompositionBootstrap(c),
		Token:              s.kitATEToken,
		Protocol:           s.kitATEProtocolFactory(c),
		Lifecycle:          s.kitATELifecycle,
		ThreadLifecycle:    s.kitATEThreadLifecycle,
		Store:              s.kitATEActorStore(),
		Revoke:             s.kitATERevoke,
		CheckSettled:       s.kitATECheckSettled,
		CheckThreadSettled: s.kitATECheckThreadSettled,
		ClearThreadSession: s.kitATEClearThreadSession,
		Now:                nil,
		HelperPollInterval: 0,
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
		return s.kitAssistantEligible(ctx, tx, assistant, c.OrgHosts)
	}
	return nil
}

// kitATEActorToken derives a per-thread ACP credential from the encrypted
// runtime generation secret. Actor names are never reused after deletion.
func kitATEActorToken(root, threadID string) string {
	mac := hmac.New(sha256.New, []byte(root))
	_, _ = mac.Write([]byte("speakeasy-kit-ate-actor-v1\x00" + threadID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Use the broker's resolver without private-CA or internal-network overrides.
// A successful control-plane TLS connection does not establish broker trust.
func (s *ServiceCore) validateKitATEBrokerKeys(ctx context.Context, raw string) error {
	source, err := jwks.NewRemoteSource(raw)
	if err != nil {
		return fmt.Errorf("configure ATE broker JWKS: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, ateBrokerJWKSTimeout)
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
	busy, err := assistantrepo.New(r.LifecycleTx).AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID:   r.ProjectID,
		AssistantID: r.AssistantID,
	})
	if err != nil {
		return fmt.Errorf("check ATE unsettled events and wakes: %w", err)
	}
	if !busy.Valid || busy.Bool {
		return errors.New("ATE suspension requires settled assistant events and wakes")
	}
	return nil
}

func (s *ServiceCore) kitATECheckThreadSettled(ctx context.Context, r assistantRuntimeRecord, threadID, excludeEventID uuid.UUID) error {
	if r.LifecycleTx == nil || r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || threadID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	busy, err := assistantrepo.New(r.LifecycleTx).ThreadHasUnsettledEvents(ctx, assistantrepo.ThreadHasUnsettledEventsParams{
		ProjectID:      r.ProjectID,
		AssistantID:    r.AssistantID,
		ThreadID:       threadID,
		ExcludeEventID: excludeEventID,
	})
	if err != nil {
		return fmt.Errorf("check ATE thread unsettled events: %w", err)
	}
	if busy {
		return errors.New("ATE thread suspension requires settled thread events")
	}
	return nil
}

func (s *ServiceCore) kitATEClearThreadSession(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) error {
	if r.LifecycleTx == nil || r.RuntimeVersion != 3 || r.Backend != runtimeBackendATE || threadID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	if _, err := assistantrepo.New(r.LifecycleTx).DeleteKitRuntimeSessionForThread(ctx, assistantrepo.DeleteKitRuntimeSessionForThreadParams{
		ProjectID: uuid.NullUUID{UUID: r.ProjectID, Valid: true},
		RuntimeID: uuid.NullUUID{UUID: r.ID, Valid: true},
		ThreadID:  uuid.NullUUID{UUID: threadID, Valid: true},
	}); err != nil {
		return fmt.Errorf("delete replaced ATE thread session: %w", err)
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
	issuer, err := q.GetWorkloadIssuer(ctx, workloadrepo.GetWorkloadIssuerParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      project,
		ID:             b.IssuerID,
	})
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

	admissions, err := q.ListWorkloadAdmissions(ctx, workloadrepo.ListWorkloadAdmissionsParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      project,
	})
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

	assignments, err := q.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   identity.OrganizationID,
		WorkloadIssuerID: b.IssuerID,
		Subject:          b.Subject,
		MatchKind:        "exact",
	})
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
	return nil
}

func (s *ServiceCore) kitATELifecycle(ctx context.Context, r assistantRuntimeRecord, operation func(context.Context, assistantRuntimeRecord) error) error {
	if r.LifecycleTx != nil {
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
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 || row.Backend != runtimeBackendATE {
		return ErrKitRuntimeBinding
	}
	r.RuntimeVersion, r.Backend, r.State = row.RuntimeVersion, row.Backend, row.State
	r.BackendMetadataJSON, r.WarmUntil, r.LifecycleTx = row.BackendMetadataJson, row.WarmUntil, tx

	assistant, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{
		AssistantID: r.AssistantID,
		ProjectID:   r.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("read Kit ATE lifecycle assistant: %w", err)
	}
	r.Retired = row.Ended || row.Deleted || assistant.DeletedAt.Valid

	ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, q)
	ctx = context.WithValue(ctx, kitLifecycleTxKey{}, r.LifecycleTx)
	ctx = context.WithValue(ctx, kitATEPhasePublisherKey{}, kitATEPhasePublisher(tx.Publish))

	operationErr := operation(ctx, r)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ateLifecycleCommitTimeout)
	defer cancel()
	if err := tx.Commit(commitCtx); err != nil {
		return errors.Join(operationErr, fmt.Errorf("commit Kit ATE lifecycle: %w", err))
	}
	return operationErr
}

// kitATEThreadLifecycle serializes only one (runtime, thread) actor row via a
// per-thread session advisory lock and FOR UPDATE on assistant_ate_actors,
// leaving sibling threads of the same assistant unblocked.
func (s *ServiceCore) kitATEThreadLifecycle(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, operation func(context.Context, assistantRuntimeRecord) error) error {
	if threadID == uuid.Nil || r.ID == uuid.Nil || r.ProjectID == uuid.Nil || r.AssistantID == uuid.Nil {
		return ErrKitRuntimeBinding
	}
	if r.LifecycleTx != nil {
		return operation(ctx, r)
	}

	tx, release, err := beginKitATEThreadLifecycleTransaction(ctx, s.db, r.AssistantID, r.ID, threadID)
	if err != nil {
		return fmt.Errorf("begin Kit ATE thread lifecycle: %w", err)
	}
	defer release()

	q := assistantrepo.New(tx)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: r.ProjectID, ID: r.ID})
	if err != nil {
		return fmt.Errorf("reload Kit ATE thread lifecycle: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 || row.Backend != runtimeBackendATE {
		return ErrKitRuntimeBinding
	}
	r.RuntimeVersion, r.Backend, r.State = row.RuntimeVersion, row.Backend, row.State
	r.BackendMetadataJSON, r.WarmUntil, r.LifecycleTx = row.BackendMetadataJson, row.WarmUntil, tx

	assistant, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{
		AssistantID: r.AssistantID,
		ProjectID:   r.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("read Kit ATE thread lifecycle assistant: %w", err)
	}
	r.Retired = row.Ended || row.Deleted || assistant.DeletedAt.Valid

	ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, q)
	ctx = context.WithValue(ctx, kitATEPhasePublisherKey{}, kitATEPhasePublisher(tx.Publish))

	operationErr := operation(ctx, r)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ateLifecycleCommitTimeout)
	defer cancel()
	if err := tx.Commit(commitCtx); err != nil {
		return errors.Join(operationErr, fmt.Errorf("commit Kit ATE thread lifecycle: %w", err))
	}
	return operationErr
}

func (s *ServiceCore) kitATEToken(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (string, error) {
	if threadID == uuid.Nil || r.LifecycleTx == nil {
		return "", ErrKitRuntimeBinding
	}
	root, err := s.loadKitGatewayTokenAlreadyLocked(ctx, r)
	if errors.Is(err, ErrKitGatewayTokenNotFound) {
		root, err = s.ensureKitATEGatewayRootToken(ctx, r)
	}
	if err != nil {
		return "", err
	}
	return kitATEActorToken(root, threadID.String()), nil
}

// ensureKitATEGatewayRootToken initializes the runtime's encrypted root gateway
// secret once using ON CONFLICT DO NOTHING so concurrent sibling threads
// converge on the same root secret without overwriting each other or taking an
// assistant-wide exclusive lock.
func (s *ServiceCore) ensureKitATEGatewayRootToken(ctx context.Context, r assistantRuntimeRecord) (string, error) {
	if s.encryptionClient == nil {
		return "", errKitGatewayCredential
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate ATE root gateway secret: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	plain, err := json.Marshal(kitGatewayTokenEnvelope{
		ProjectID:   r.ProjectID,
		AssistantID: r.AssistantID,
		RuntimeID:   r.ID,
		Token:       token,
	})
	if err != nil {
		return "", fmt.Errorf("encode ATE root gateway secret: %w", err)
	}
	encrypted, err := s.encryptionClient.Encrypt(plain)
	if err != nil {
		return "", errKitGatewayCredential
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin ATE root gateway credential tx: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := assistantrepo.New(tx).StoreKitGatewayCredentialIfAbsent(ctx, assistantrepo.StoreKitGatewayCredentialIfAbsentParams{
		ProjectID:       r.ProjectID,
		AssistantID:     r.AssistantID,
		RuntimeID:       r.ID,
		TokenCiphertext: encrypted,
	}); err != nil {
		return "", fmt.Errorf("store ATE root gateway credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit ATE root gateway credential: %w", err)
	}
	return s.loadKitGatewayTokenAlreadyLocked(ctx, r)
}

func (s *ServiceCore) kitATECompositionBootstrap(c KitATECompositionConfig) func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *KitWorkloadBinding) (KitLocalBootstrap, error) {
	return func(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID, actorName string, priorWorkload *KitWorkloadBinding) (KitLocalBootstrap, error) {
		if threadID == uuid.Nil || len(actorName) > maxATEDNSLabelLen || !kitATENamePattern.MatchString(actorName) {
			return KitLocalBootstrap{}, ErrKitRuntimeBinding
		}
		if r.LifecycleTx == nil {
			return KitLocalBootstrap{}, errors.New("ATE bootstrap requires the thread lifecycle lock")
		}
		token, err := s.kitATEToken(ctx, r, threadID)
		if err != nil {
			return KitLocalBootstrap{}, err
		}

		local := c.KitLocalCompositionConfig
		var metadata kitATEMetadata
		if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil || metadata.RuntimeID != r.ID || metadata.AssistantID != r.AssistantID || metadata.ProjectID != r.ProjectID || metadata.Atespace != c.Atespace || metadata.Template != c.Template {
			return KitLocalBootstrap{}, ErrKitRuntimeBinding
		}
		if priorWorkload != nil {
			local.bootstrapWorkload = priorWorkload
		}
		local.ateIdentity = &kitATEWorkloadIdentity{Atespace: c.Atespace, ActorName: actorName}
		local.bootstrapGatewayToken = token
		return s.kitCompositionBootstrap(ctx, r, local)
	}
}

func (s *ServiceCore) kitATEProtocolFactory(c KitATECompositionConfig) func(context.Context, assistantRuntimeRecord, uuid.UUID, string, *url.URL, *guardian.HTTPClient, string) (runtimeProtocol, error) {
	projector := NewKitTranscriptProjector(s.db, s.chatWriter, c.TurnStream, func(ctx context.Context, tx pgx.Tx, b KitTranscriptBinding) error {
		bound, err := s.ValidateKitRuntimeSessionInTx(ctx, tx, kitRuntimeSessionKey{
			ProjectID:   b.ProjectID,
			AssistantID: b.AssistantID,
			RuntimeID:   b.RuntimeID,
			ThreadID:    b.ThreadID,
		}, b.SessionID)
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
		p, err := NewKitRuntimeProtocol(KitRuntimeProtocolConfig{
			Core:         s,
			Endpoint:     endpoint,
			HTTPClient:   client,
			Projector:    projector,
			CWD:          KitWorkspacePath,
			GatewayToken: token,
		})
		if err != nil {
			return nil, err
		}
		return &kitConfiguredRuntimeProtocol{KitRuntimeProtocol: p}, nil
	}
}
