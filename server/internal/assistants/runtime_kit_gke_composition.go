package assistants

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

// KitGKECompositionConfig opts new version-3 conversations into the production
// runtime. It does not convert or import existing conversations. MCP uses the
// same issuer-bound workload registration as local Kit, never a user's token.
type KitGKECompositionConfig struct {
	KitLocalCompositionConfig
	KitGKEIdentityConfig
	GKE       GKERuntimeConfig
	Provision KitGKEProvisionInput
	// Snapshots optionally overrides the managed GKE driver for tests. Nil uses
	// the concrete managed API driver when snapshot provisioning is enabled.
	Snapshots KitGKECheckpointDriver
}

// KitGKEIdentityConfig independently identifies the cluster's native Agent Identity.
// OrganizationID is required: standalone project pools are not inferred.
type KitGKEIdentityConfig struct {
	ProjectNumber, OrganizationID, ClusterLocation, ClusterName string
}

func (c KitGKEIdentityConfig) Validate() error {
	for _, id := range []string{c.ProjectNumber, c.OrganizationID} {
		if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(id) {
			return errors.New("native Agent Identity requires numeric project number and organization ID")
		}
	}
	for _, part := range []string{c.ClusterLocation, c.ClusterName} {
		if len(part) > 63 || !regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`).MatchString(part) {
			return errors.New("native Agent Identity requires valid cluster location and name")
		}
	}
	return nil
}
func (c KitGKEIdentityConfig) validate() error { return c.Validate() }
func (c KitGKEIdentityConfig) trustDomain() string {
	return "agents.global.org-" + c.OrganizationID + ".system.id.goog"
}
func (c KitGKEIdentityConfig) issuer() string {
	return "https://sts.googleapis.com/v1/organizations/" + c.OrganizationID + "/locations/global/workloadIdentityPools/" + c.trustDomain()
}
func (c KitGKEIdentityConfig) subject(namespace, sa string) string {
	return "spiffe://" + c.trustDomain() + "/resources/container/projects/" + c.ProjectNumber + "/locations/" + c.ClusterLocation + "/clusters/" + c.ClusterName + "/ns/" + namespace + "/sa/" + sa
}

func kitGKENativeIssuerConfig(c KitGKECompositionConfig) KitGKECompositionConfig {
	c.WorkloadIssuerURL = c.issuer()
	c.WorkloadJWKSURL = c.issuer() + "/openid/jwks"
	c.nativeIdentity = &c.KitGKEIdentityConfig
	return c
}

func validateKitGKEComposition(c KitGKECompositionConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.WorkloadIssuerURL != c.issuer() || c.WorkloadJWKSURL != c.issuer()+"/openid/jwks" {
		return errors.New("kit GKE requires derived native Agent Identity issuer and JWKS")
	}
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
	c = kitGKENativeIssuerConfig(c)
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
		KitGKEIdentityConfig: c.KitGKEIdentityConfig, GKE: c.GKE, Provision: c.Provision, Snapshots: c.Snapshots,
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
	// Projects without a served Project Gateway use the Default runtime.
	s.newAssistantRuntimeEligible = func(ctx context.Context, tx pgx.Tx, assistant assistantRecord) (bool, error) {
		return s.kitAssistantEligible(ctx, tx, assistant, c.OrgHosts)
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
	ctx = context.WithValue(ctx, kitLifecycleTxKey{}, r.LifecycleTx)
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

// kitAssistantEligible reports whether the project's Project Gateway is served
// for machine access, keeping the Default runtime otherwise. This predicate
// itself does not read tool assignments or backing-agent grants (CreateAssistant
// keeps creates with explicit MCP attachments on Default before calling it).
func (s *ServiceCore) kitAssistantEligible(ctx context.Context, tx pgx.Tx, assistant assistantRecord, hosts *orghost.Resolver) (bool, error) {
	_, _, err := s.kitProjectGateway(ctx, tx, assistant, hosts)
	if errors.Is(err, errKitMCPIneligible) {
		return false, nil
	}
	return err == nil, err
}

var errKitMCPIneligible = errors.New("assistant project gateway is unavailable")

// kitProjectGateway resolves the Project Gateway URL and shared organization
// user-session issuer for a Kit assistant through the shared orggateway
// availability predicate.
func (s *ServiceCore) kitProjectGateway(ctx context.Context, db assistantrepo.DBTX, assistant assistantRecord, hosts *orghost.Resolver) (*url.URL, uuid.UUID, error) {
	availability, err := orggateway.ResolveProjectGateway(ctx, db, orggateway.ResolveProjectGatewayParams{
		OrganizationID:          assistant.OrganizationID,
		ProjectID:               assistant.ProjectID,
		ServerURL:               s.serverURL,
		OrgHosts:                hosts,
		Features:                s.featureFlags,
		RequireSharedIssuerMode: true,
	})
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("resolve Kit project gateway: %w", err)
	}
	if !availability.Available {
		return nil, uuid.Nil, fmt.Errorf("%w: %s", errKitMCPIneligible, availability.Reason)
	}
	return availability.GatewayURL, availability.IssuerID, nil
}
