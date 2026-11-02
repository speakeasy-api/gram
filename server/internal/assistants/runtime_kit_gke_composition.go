package assistants

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitGKECompositionConfig opts new version-3 conversations into the production
// runtime. It does not convert or import existing conversations. MCP uses the
// same issuer-bound workload registration as local Kit, never a user's token.
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
	c.WorkloadNamespace = c.GKE.Namespace
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
		AuthorityStore: NewKitGKEPostgresAuthorityStore(s.db),
		Fence:          s.fenceKitRuntimeAlreadyLocked,
		Lifecycle:      s.kitGKELifecycle,
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
	// The deployment opts in only assistants whose persisted attachment can use
	// this machine-auth composition. Other assistants keep the legacy default.
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
	if len(r.BackendMetadataJSON) == 0 {
		r.BackendMetadataJSON = row.BackendMetadataJson
	}
	if err := s.revokeKitWorkload(ctx, db, r); err != nil {
		return err
	}
	// Withdraw before metadata can be cleared, even after identity tombstoning.
	// Retired rows must not receive fresh gateway authority.
	if row.Ended || row.Deleted {
		return nil
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

// kitGKEAssistantEligible reads the attachment written by creation, rather than
// trusting request flags or selecting Kit globally before attachments exist.
func (s *ServiceCore) kitGKEAssistantEligible(ctx context.Context, tx pgx.Tx, assistant assistantRecord, c KitLocalCompositionConfig) (bool, error) {
	resource, issuerID, err := s.kitPersistedMCP(ctx, tx, assistant)
	if errors.Is(err, errKitMCPIneligible) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !c.PersistedMCP && (c.MCPResource == nil || c.MCPResource.String() != resource.String() || c.MCPUserSessionIssuerID != issuerID) {
		return false, nil
	}
	return true, nil
}

var errKitMCPIneligible = errors.New("assistant has no supported persisted machine MCP attachment")

func (s *ServiceCore) kitPersistedMCP(ctx context.Context, db assistantrepo.DBTX, assistant assistantRecord) (*url.URL, uuid.UUID, error) {
	rows, err := loadAssistantMcpServers(ctx, db, assistantrepo.LoadAssistantMcpServersParams{ProjectID: assistant.ProjectID, AssistantIds: []uuid.UUID{assistant.ID}})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("load persisted assistant MCP servers: %w", err)
	}
	seen := map[uuid.UUID]bool{}
	var selected assistantMCPServerRow
	for _, row := range rows {
		if row.Visibility == "disabled" || seen[row.McpServerID] {
			continue
		}
		seen[row.McpServerID] = true
		selected = assistantMCPServerRow{ServerSlug: pgtype.Text{String: "", Valid: false}, ToolsetSlug: pgtype.Text{String: "", Valid: false}, DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false}, EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, EnvironmentSlug: pgtype.Text{String: "", Valid: false}, MCPServerID: row.McpServerID, EndpointSlug: row.EndpointSlug, EndpointDomain: row.EndpointDomain, EndpointIsDomainRoot: row.EndpointIsDomainRoot, Visibility: row.Visibility}
	}
	if len(seen) != 1 || selected.EndpointSlug == "" || s.serverURL == nil || s.serverURL.Scheme != "https" {
		return nil, uuid.Nil, errKitMCPIneligible
	}
	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: selected.MCPServerID, ProjectID: assistant.ProjectID})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("get persisted assistant MCP server: %w", err)
	}
	if !server.UserSessionIssuerID.Valid {
		return nil, uuid.Nil, errKitMCPIneligible
	}
	issuer, err := usersessionsrepo.New(db).GetSharedUserSessionIssuerByID(ctx, server.UserSessionIssuerID.UUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, uuid.Nil, errKitMCPIneligible
	}
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("get persisted assistant session issuer: %w", err)
	}
	if !authserver.IssuerInSharedMode(issuer.UserSessionIssuer) || issuer.ResolvedOrganizationID != assistant.OrganizationID || (issuer.UserSessionIssuer.ProjectID.Valid && issuer.UserSessionIssuer.ProjectID.UUID != assistant.ProjectID) {
		return nil, uuid.Nil, errKitMCPIneligible
	}
	return selected.endpointURL(s.serverURL), server.UserSessionIssuerID.UUID, nil
}
