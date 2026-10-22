package assistants

import (
	"context"
	"errors"
	"fmt"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitGKECompositionConfig opts new version-3 conversations into the production
// runtime. It does not convert or import existing conversations. MCP uses the
// same per-turn credential and server assembly as the default runtime.
type KitGKECompositionConfig struct {
	KitLocalCompositionConfig
	GKE       GKERuntimeConfig
	Provision KitGKEProvisionInput
	// Snapshots optionally overrides the managed GKE driver for tests. Nil uses
	// the concrete managed API driver when snapshot provisioning is enabled.
	Snapshots KitGKECheckpointDriver
}

func validateKitGKEComposition(c KitGKECompositionConfig) error {
	if c.Environment == "" || c.Environment == "local" {
		return errors.New("kit GKE composition requires an explicit non-local environment")
	}
	if c.Network != "" || c.ExtraCACertFile != "" || c.AllowInsecureInference {
		return errors.New("kit GKE composition rejects local network and TLS overrides")
	}
	if err := validateKitComposition(c.KitLocalCompositionConfig); err != nil {
		return err
	}
	if c.GKE.Dynamic == nil || c.GKE.Namespace == "" || len(c.GKE.RunnerCIDRBlocks) == 0 {
		return errors.New("kit GKE composition requires Kubernetes, namespace and explicit gateway CIDRs")
	}
	for _, cidr := range c.GKE.RunnerCIDRBlocks {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid Kit GKE gateway CIDR %q: %w", cidr, err)
		}
	}
	return nil
}

// InstallKitGKERuntime installs only the v3 dispatch branch. All startup
// credentials are minted under the existing runtime admission lock. The
// production gateway uses the same canonical transcript reducer and session
// binding checks as local Kit; no legacy protocol fallback is installed.
// Platform MCP parity: provisioning/checkpoint operations are internal lifecycle
// machinery, not member operations. Existing assistant tools remain unchanged.
func (s *ServiceCore) InstallKitGKERuntime(c KitGKECompositionConfig) error {
	if err := validateKitGKEComposition(c); err != nil {
		return err
	}
	if s.guardianPolicy == nil || s.chatWriter == nil || s.assistantTokens == nil || s.encryptionClient == nil {
		return errors.New("kit requires guarded HTTP, transcript, inference and encrypted gateway credential services")
	}
	if c.GKE.GuestPort == 0 {
		c.GKE.GuestPort = 8081
	}
	if c.GKE.GuestPort < 1 || c.GKE.GuestPort > 65535 {
		return errors.New("invalid kit gateway port")
	}
	c.Provision.Image = c.Image
	c.Provision.GuestPort = int32(c.GKE.GuestPort)
	backend, err := NewKitGKERuntimeBackend(KitGKERuntimeConfig{
		GKE: c.GKE, Provision: c.Provision, Snapshots: c.Snapshots,
		BootstrapGeneration: "per-turn-mcp-v1",
		AuthorityStore:      NewKitGKEPostgresAuthorityStore(s.db),
		Fence:               s.fenceKitRuntimeAlreadyLocked,
		Lifecycle:           s.kitGKELifecycle,
		Bootstrap: func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return s.kitCompositionBootstrap(ctx, r, c.KitLocalCompositionConfig)
		},
		Protocol: s.kitProtocolFactory(c.KitLocalCompositionConfig, c.GKE.RunnerCIDRBlocks),
	})
	if err != nil {
		return err
	}
	if err := s.installKitRuntime(backend); err != nil {
		return err
	}
	// Runtime selection does not depend on attachment count or issuer mode.
	s.newAssistantRuntimeEligible = func(ctx context.Context, tx pgx.Tx, assistant assistantRecord) (bool, error) {
		return s.kitGKEAssistantEligible(ctx, tx, assistant, c.KitLocalCompositionConfig)
	}
	return nil
}

// fenceKitRuntimeAlreadyLocked revokes runtime-scoped external authority before
// capturing or deleting a settled process. Failure never authorizes teardown.
// It must run under the same admission/recycling lock as bootstrap.
func (s *ServiceCore) fenceKitRuntimeAlreadyLocked(ctx context.Context, r assistantRuntimeRecord) error {
	if r.RuntimeVersion != 3 {
		return ErrKitRuntimeBinding
	}
	var db assistantrepo.DBTX = s.db
	if r.LifecycleTx != nil {
		db = r.LifecycleTx
	}
	q := assistantrepo.New(db)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: r.ProjectID, ID: r.ID})
	if err != nil {
		return fmt.Errorf("read kit runtime before credential fence: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 {
		return ErrKitRuntimeBinding
	}
	if err := tokenrepo.New(db).RevokeRuntimeInferenceCredentials(ctx, tokenrepo.RevokeRuntimeInferenceCredentialsParams{ProjectID: r.ProjectID, RuntimeID: r.ID}); err != nil {
		return fmt.Errorf("revoke kit runtime inference authority: %w", err)
	}
	// Retirement is already a durable admission fence for workload OAuth and
	// gateway binding reads. Do not try to issue a new gateway key for a row
	// which the canonical credential query correctly refuses to resurrect.
	if row.Ended || row.Deleted {
		return nil
	}
	a, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID})
	if err != nil {
		return fmt.Errorf("read kit assistant before workload fence: %w", err)
	}
	_, err = usersessionsrepo.New(db).RevokeAssistantRuntimeWorkloadClients(ctx, usersessionsrepo.RevokeAssistantRuntimeWorkloadClientsParams{
		RuntimeID: uuid.NullUUID{UUID: r.ID, Valid: true}, OrganizationID: conv.ToPGText(a.OrganizationID), ProjectID: r.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("fence Kit workload credentials: %w", err)
	}
	_, err = s.rotateKitGatewayTokenAlreadyLocked(ctx, r)
	return err
}

// kitGKELifecycle serializes Kubernetes effects and durable recovery evidence
// under the same assistant fence as admission, retirement and selection.
func (s *ServiceCore) kitGKELifecycle(ctx context.Context, r assistantRuntimeRecord, operation func(context.Context, assistantRuntimeRecord) error) error {
	tx, release, err := beginKitLifecycleTransaction(ctx, s.db, r.AssistantID.String())
	if err != nil {
		return fmt.Errorf("begin Kit GKE lifecycle: %w", err)
	}
	defer release()
	q := assistantrepo.New(tx)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ProjectID: r.ProjectID, ID: r.ID})
	if err != nil {
		return fmt.Errorf("reload Kit GKE lifecycle: %w", err)
	}
	if row.AssistantID != r.AssistantID || row.RuntimeVersion != 3 || row.Backend != runtimeBackendGKE {
		return ErrKitRuntimeBinding
	}
	r.RuntimeVersion, r.Backend, r.State = row.RuntimeVersion, row.Backend, row.State
	r.BackendMetadataJSON, r.WarmUntil, r.LifecycleTx = row.BackendMetadataJson, row.WarmUntil, tx
	assistant, err := q.GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{AssistantID: r.AssistantID, ProjectID: r.ProjectID})
	if err != nil {
		return fmt.Errorf("read Kit lifecycle assistant: %w", err)
	}
	r.Retired = row.Ended || row.Deleted || assistant.DeletedAt.Valid
	ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, q)
	ctx = context.WithValue(ctx, kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(progressCtx context.Context, raw []byte) error {
		if err := q.UpdateAssistantRuntimeMetadata(progressCtx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: r.ProjectID, RuntimeID: r.ID, BackendMetadataJson: raw}); err != nil {
			return fmt.Errorf("update Kit GKE admission progress: %w", err)
		}
		return tx.Publish(progressCtx)
	}))
	operationErr := operation(ctx, r)
	// Failed effects can still leave durable cleanup work. Preserve its evidence,
	// even if the initiating request timed out after creating a Kubernetes object.
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := tx.Commit(commitCtx); err != nil {
		return errors.Join(operationErr, fmt.Errorf("commit Kit GKE lifecycle: %w", err))
	}
	return operationErr
}

// kitGKEAssistantEligible verifies the assistant exists in the selected project
// and organization without imposing tool-availability requirements.
// Tool availability is not runtime eligibility. Per-turn credentials and the
// existing MCP admission path authorize each connection independently.
func (s *ServiceCore) kitGKEAssistantEligible(ctx context.Context, tx pgx.Tx, assistant assistantRecord, _ KitLocalCompositionConfig) (bool, error) {
	row, err := assistantrepo.New(tx).GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: assistant.ProjectID, AssistantID: assistant.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Kit assistant eligibility: %w", err)
	}
	return row.OrganizationID == assistant.OrganizationID, nil
}
