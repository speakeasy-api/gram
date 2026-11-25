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
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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
	// Projects without a served gateway keep the legacy default.
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

// kitGKEAssistantEligible retains the legacy default where the project gateway
// is not served. Tool assignments do not affect runtime selection.
func (s *ServiceCore) kitGKEAssistantEligible(ctx context.Context, tx pgx.Tx, assistant assistantRecord, c KitLocalCompositionConfig) (bool, error) {
	if s.featureFlags == nil {
		return false, nil
	}
	enabled, err := s.featureFlags.IsFlagEnabledLocal(ctx, feature.FlagProjectGatewayMachineAccess, assistant.OrganizationID, nil, nil)
	if err != nil {
		return false, fmt.Errorf("evaluate project gateway machine access: %w", err)
	}
	if !enabled {
		return false, nil
	}
	_, _, err = s.kitProjectGateway(ctx, tx, assistant, c.OrgHosts)
	if errors.Is(err, errKitMCPIneligible) {
		return false, nil
	}
	return err == nil, err
}

var errKitMCPIneligible = errors.New("assistant project gateway is unavailable")

func (s *ServiceCore) kitProjectGateway(ctx context.Context, db assistantrepo.DBTX, assistant assistantRecord, hosts *orghost.Resolver) (kitMCPConnection, uuid.UUID, error) {
	var zero kitMCPConnection
	org, err := orgrepo.New(db).GetOrganizationMetadata(ctx, assistant.OrganizationID)
	if err != nil {
		return zero, uuid.Nil, fmt.Errorf("load Kit gateway organization: %w", err)
	}
	issuerID, unavailable, err := orggateway.Availability(ctx, db, org)
	if err != nil {
		return zero, uuid.Nil, fmt.Errorf("resolve Kit project gateway: %w", err)
	}
	if unavailable != orggateway.UnavailabilityNone {
		return zero, uuid.Nil, fmt.Errorf("%w: %s", errKitMCPIneligible, unavailable)
	}
	issuer, err := usersessionsrepo.New(db).GetSharedUserSessionIssuerByID(ctx, issuerID)
	if err != nil {
		return zero, uuid.Nil, fmt.Errorf("load Kit project gateway issuer: %w", err)
	}
	if !authserver.IssuerInSharedMode(issuer.UserSessionIssuer) {
		return zero, uuid.Nil, fmt.Errorf("%w: gateway issuer must use shared mode", errKitMCPIneligible)
	}
	project, err := projectsrepo.New(db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: assistant.ProjectID, OrganizationID: org.ID})
	if err != nil {
		return zero, uuid.Nil, fmt.Errorf("load Kit gateway project: %w", err)
	}
	baseURL := s.serverURL
	if hosts != nil {
		baseURL = hosts.ServerURL(org.DefaultHost)
	}
	if baseURL == nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return zero, uuid.Nil, fmt.Errorf("%w: gateway requires HTTPS", errKitMCPIneligible)
	}
	resource, err := url.Parse(orggateway.ProjectURL(baseURL, org.Slug, project.Slug))
	if err != nil {
		return zero, uuid.Nil, fmt.Errorf("parse Kit project gateway resource: %w", err)
	}
	return kitMCPConnection{Name: "project-gateway", Resource: resource}, issuerID, nil
}
