package assistants

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// KitWorkloadBinding records an exact, runtime-owned admission, not a credential.
type KitWorkloadBinding struct {
	IssuerID       uuid.UUID                         `json:"issuer_id"`
	Subject        string                            `json:"subject"`
	Identity       assistantidentity.RuntimeIdentity `json:"identity"`
	ServiceAccount string                            `json:"service_account"`
	Audience       string                            `json:"audience"`
	Generation     string                            `json:"generation"`
}

func kitWorkloadSubject(r assistantRuntimeRecord, c KitLocalCompositionConfig, gateway string) (string, string, error) {
	if r.ID == uuid.Nil || gateway == "" {
		return "", "", ErrKitRuntimeBinding
	}
	if c.Environment == "local" {
		if strings.TrimSpace(c.WorkloadSubject) == "" {
			return "", "", errors.New("kit workload subject is required")
		}
		return c.WorkloadSubject, "", nil
	}
	if c.WorkloadNamespace == "" || strings.ContainsAny(c.WorkloadNamespace, ":/ ") {
		return "", "", errors.New("kit workload namespace is required")
	}
	digest := sha256.Sum256([]byte(gateway))
	sa := fmt.Sprintf("kit-%s-%x", strings.ReplaceAll(r.ID.String(), "-", ""), digest[:8])
	return "system:serviceaccount:" + c.WorkloadNamespace + ":" + sa, sa, nil
}

func kitWorkloadIssuerName(issuer string) string {
	sum := sha256.Sum256([]byte(issuer))
	return fmt.Sprintf("kit-runtime-%x", sum[:16])
}

func kitWorkloadIdentityMatches(r assistantRuntimeRecord, identity assistantidentity.RuntimeIdentity) bool {
	return r.RuntimeVersion == 3 && r.ID != uuid.Nil && r.ProjectID != uuid.Nil && r.AssistantID != uuid.Nil && identity.ProjectID == r.ProjectID && identity.AssistantID == r.AssistantID && identity.OrganizationID != "" && identity.AgentID != uuid.Nil && identity.BindingID != uuid.Nil && identity.BindingGeneration > 0
}

// The caller owns the runtime lifecycle fence. Local callers get an atomic
// transaction here; GKE callers keep writes in their existing lifecycle transaction.
func (s *ServiceCore) provisionKitWorkload(ctx context.Context, r assistantRuntimeRecord, c KitLocalCompositionConfig, identity assistantidentity.RuntimeIdentity, gateway string) (*KitWorkloadBinding, error) {
	if !kitWorkloadIdentityMatches(r, identity) {
		return nil, ErrKitRuntimeBinding
	}
	subject, sa, err := kitWorkloadSubject(r, c, gateway)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	canonicalIssuer, err := issuerurl.ParseHTTPSOnly(c.WorkloadIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("kit workload issuer must be an HTTPS URL: %w", err)
	}
	c.WorkloadIssuerURL = canonicalIssuer.String()
	jwksURL := c.WorkloadJWKSURL
	jwks, err := url.Parse(jwksURL)
	if err != nil || jwks.Scheme != "https" || jwks.Host == "" || jwks.User != nil || jwks.RawQuery != "" || jwks.Fragment != "" {
		return nil, errors.New("kit workload JWKS must be an explicit HTTPS URL")
	}
	if s.audit == nil {
		return nil, errors.New("kit workload provisioning requires audit logging")
	}
	if r.LifecycleTx == nil {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		r.LifecycleTx = tx
		binding, err := s.provisionKitWorkload(ctx, r, c, identity, gateway)
		if err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		return binding, nil
	}
	db := r.LifecycleTx
	resolved, err := s.identities.ResolveRuntimeLocked(ctx, db, identity.OrganizationID, r.ProjectID, r.AssistantID)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if resolved != identity {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	// Check the agent's own effective grants, never the initiating user's grants.
	// This path intentionally cannot add roles or inference permissions.
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String())
	agent, err := agents.ResolvePrincipal(ctx, db, identity.OrganizationID, agentPrincipal)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if agents.DeriveLifecycle(agent) != agents.LifecycleActive || agent.OwnerReassignmentRequiredAt.Valid {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	ownerPrincipals, eligible, err := runtimepolicy.ResolveEligibleUser(ctx, db, identity.OrganizationID, agent.OwnerUserID)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if !eligible {
		return nil, assistantidentity.ErrInvalidIdentity
	}
	agentPolicy, err := runtimepolicy.LoadAgentPolicy(ctx, db, identity.OrganizationID, agentPrincipal)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	ownerPolicy, err := authz.LoadGrants(ctx, db, identity.OrganizationID, ownerPrincipals)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if err := kitWorkloadInferenceAllowed(r.ProjectID, agentPolicy, ownerPolicy); err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	shared, err := usersessionsrepo.New(db).GetSharedUserSessionIssuerByID(ctx, c.MCPUserSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if shared.ResolvedOrganizationID != identity.OrganizationID || (shared.UserSessionIssuer.ProjectID.Valid && shared.UserSessionIssuer.ProjectID.UUID != r.ProjectID) {
		return nil, ErrKitRuntimeBinding
	}
	audience, err := (authserver.Hosts{ServerURL: s.serverURL.String(), AuthenticationHostBaseURL: c.WorkloadIssuerBaseURL, PlatformHosts: nil}).SharedIssuerURL(shared.UserSessionIssuer)
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	generationHash := sha256.Sum256([]byte(gateway))
	generation := fmt.Sprintf("%x", generationHash[:16])
	admissionName := "kit-runtime-" + r.ID.String() + "-" + generation
	project := uuid.NullUUID{UUID: r.ProjectID, Valid: true}
	q := workloadrepo.New(db)
	if c.Environment != "local" && c.WorkloadIssuerURL == "https://accounts.google.com" {
		subject, err = kitGKEWorkloadSubject(ctx, sa)
		if err != nil {
			return nil, fmt.Errorf("provision Google workload identity: %w", err)
		}
	}
	if c.Environment == "local" || c.WorkloadIssuerURL == "https://accounts.google.com" {
		// Serialize the external subject across projects before taking a project
		// lock. A project-only lock cannot protect the organization-wide history.
		key, err := json.Marshal([]string{"kit-local-subject", identity.OrganizationID, c.WorkloadIssuerURL, subject})
		if err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		if _, err := db.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", string(key)); err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
	}
	// The project row serializes creation across assistants sharing a cluster.
	// Issuer child writes additionally use the ordinary workload-policy row lock.
	var locked uuid.UUID
	if err := db.QueryRow(ctx, "SELECT id FROM projects WHERE id = $1 AND organization_id = $2 FOR UPDATE", r.ProjectID, identity.OrganizationID).Scan(&locked); err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	issuers, err := q.FindWorkloadIssuersByIssuer(ctx, workloadrepo.FindWorkloadIssuersByIssuerParams{OrganizationID: identity.OrganizationID, ProjectID: project, Issuers: canonicalIssuer.MatchCandidates()})
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	var issuer workloadrepo.WorkloadIssuer
	for _, candidate := range issuers {
		if candidate.ProjectID != project {
			continue
		}
		if issuer.ID != uuid.Nil || !kitOwnedWorkloadIssuer(candidate, c.WorkloadIssuerURL, jwksURL) {
			return nil, errors.New("kit workload issuer conflicts with existing policy")
		}
		issuer = candidate
	}
	if issuer.ID == uuid.Nil {
		issuer, err = q.CreateWorkloadIssuer(ctx, workloadrepo.CreateWorkloadIssuerParams{Description: pgtype.Text{String: "", Valid: false}, AllowWildcardAdmission: false, OrganizationID: identity.OrganizationID, ProjectID: project, Name: kitWorkloadIssuerName(c.WorkloadIssuerURL), Tags: []string{"kit-runtime"}, Issuer: c.WorkloadIssuerURL, JwksUri: jwksURL, AllowAssertionReuse: pgtype.Bool{Bool: true, Valid: true}})
		if err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		if err := s.audit.LogWorkloadIssuerCreate(ctx, db, audit.LogWorkloadIssuerCreateEvent{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: identity.OrganizationID, ProjectID: project, Actor: urn.NewSystemPrincipal("kit-runtime"), IssuerURN: urn.NewWorkloadIssuer(issuer.ID), IssuerName: issuer.Name, IssuerSnapshotAfter: kitWorkloadIssuerSnapshot(issuer)}); err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
	}
	if _, err := q.LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: identity.OrganizationID, ID: issuer.ID}); err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	if c.Environment == "local" || c.WorkloadIssuerURL == "https://accounts.google.com" {
		// Never rebind an externally verified subject, even after withdrawal or
		// issuer recreation. Old assertions and sessions can still be valid;
		// changing only metadata.Generation cannot distinguish them. Query all
		// historical issuer rows for this external issuer URL, not just its ID.
		var used bool
		if err := db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM workload_identity_admissions a JOIN workload_issuers i ON i.id = a.workload_issuer_id WHERE a.organization_id = $1 AND i.organization_id = $1 AND i.issuer = ANY($2::text[]) AND a.subject = $3 AND a.match_kind = 'exact')", identity.OrganizationID, canonicalIssuer.MatchCandidates(), subject).Scan(&used); err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		if used {
			return nil, errors.New("Kit workload subjects cannot be reused; configure a fresh generation subject")
		}
	}
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: issuer.ID, Subject: subject, MatchKind: "exact"})
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	for _, assignment := range assignments {
		if assignment.AgentID != identity.AgentID {
			return nil, errors.New("kit workload subject is assigned to another agent")
		}
	}
	admissions, err := q.ListWorkloadAdmissions(ctx, workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: identity.OrganizationID, ProjectID: project})
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	found := false
	for _, admission := range admissions {
		if admission.WorkloadIssuerID != issuer.ID || admission.Subject != subject || admission.MatchKind != "exact" {
			continue
		}
		if admission.ProjectID != project || admission.Name.String != admissionName {
			return nil, errors.New("kit workload subject belongs to another runtime")
		}
		found = true
	}
	if !found {
		admission, err := q.CreateWorkloadAdmission(ctx, workloadrepo.CreateWorkloadAdmissionParams{OrganizationID: identity.OrganizationID, ProjectID: project, WorkloadIssuerID: issuer.ID, Subject: subject, MatchKind: "exact", Name: pgtype.Text{String: admissionName, Valid: true}, Tags: []string{"kit-runtime"}})
		if err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
		if err := s.audit.LogWorkloadAdmissionAdmit(ctx, db, audit.LogWorkloadAdmissionAdmitEvent{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: identity.OrganizationID, ProjectID: project, Actor: urn.NewSystemPrincipal("kit-runtime"), AdmissionURN: urn.NewWorkloadAdmission(admission.ID), AdmissionDisplayName: subject, AdmissionSnapshotAfter: kitWorkloadAdmissionSnapshot(admission, issuer, identity.AgentID.String())}); err != nil {
			return nil, fmt.Errorf("provision kit workload: %w", err)
		}
	}

	_, err = q.UpsertWorkloadAgentAssignment(ctx, workloadrepo.UpsertWorkloadAgentAssignmentParams{OrganizationID: identity.OrganizationID, WorkloadIssuerID: issuer.ID, Subject: subject, MatchKind: "exact", AgentID: identity.AgentID})
	if err != nil {
		return nil, fmt.Errorf("provision kit workload: %w", err)
	}
	return &KitWorkloadBinding{IssuerID: issuer.ID, Subject: subject, Identity: identity, ServiceAccount: sa, Audience: audience, Generation: generation}, nil
}

// revokeKitWorkload withdraws only this runtime's exact admission and assignment.
// It never deletes the shared issuer, the assistant's root binding or its agent.
func (s *ServiceCore) revokeKitWorkload(ctx context.Context, db assistantrepo.DBTX, r assistantRuntimeRecord) error {
	var metadata struct {
		Workload *KitWorkloadBinding `json:"workload"`
	}
	if len(r.BackendMetadataJSON) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.BackendMetadataJSON, &metadata); err != nil {
		return fmt.Errorf("decode Kit workload binding: %w", err)
	}
	b := metadata.Workload
	if b == nil {
		return nil
	}
	if !kitWorkloadIdentityMatches(r, b.Identity) || b.IssuerID == uuid.Nil || b.Subject == "" || len(b.Generation) != 32 {
		return ErrKitRuntimeBinding
	}
	if _, ok := db.(pgx.Tx); !ok {
		tx, err := s.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("revoke kit workload: %w", err)
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		if err := s.revokeKitWorkload(ctx, tx, r); err != nil {
			return fmt.Errorf("revoke kit workload: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit kit workload withdrawal: %w", err)
		}
		return nil
	}
	// Withdrawal must survive identity tombstoning. Authorize against durable
	// assistant tenancy, then the exact runtime-generation admission and agent
	// assignment below; never resolve or revive an active assistant binding.
	bound, err := assistantrepo.New(db).GetAssistantIgnoringDeleted(ctx, assistantrepo.GetAssistantIgnoringDeletedParams{ProjectID: r.ProjectID, AssistantID: r.AssistantID})
	if err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	if bound.OrganizationID != b.Identity.OrganizationID {
		return ErrKitRuntimeBinding
	}
	project := uuid.NullUUID{UUID: r.ProjectID, Valid: true}
	q := workloadrepo.New(db)
	issuer, err := q.GetWorkloadIssuer(ctx, workloadrepo.GetWorkloadIssuerParams{OrganizationID: b.Identity.OrganizationID, ProjectID: project, ID: b.IssuerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	if issuer.ProjectID != project || issuer.Name != kitWorkloadIssuerName(issuer.Issuer) {
		return ErrKitRuntimeBinding
	}
	if _, err := q.LockWorkloadIssuerForWrite(ctx, workloadrepo.LockWorkloadIssuerForWriteParams{OrganizationID: b.Identity.OrganizationID, ID: b.IssuerID}); err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	assignments, err := q.GetWorkloadAgentAssignmentForSubject(ctx, workloadrepo.GetWorkloadAgentAssignmentForSubjectParams{OrganizationID: b.Identity.OrganizationID, WorkloadIssuerID: b.IssuerID, Subject: b.Subject, MatchKind: "exact"})
	if err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	for _, a := range assignments {
		if a.AgentID != b.Identity.AgentID {
			return ErrKitRuntimeBinding
		}
	}
	admissions, err := q.ListWorkloadAdmissions(ctx, workloadrepo.ListWorkloadAdmissionsParams{OrganizationID: b.Identity.OrganizationID, ProjectID: project})
	if err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	owned := false
	for _, a := range admissions {
		if a.WorkloadIssuerID != b.IssuerID || a.Subject != b.Subject || a.MatchKind != "exact" {
			continue
		}
		if a.ProjectID != project || a.Name.String != "kit-runtime-"+r.ID.String()+"-"+b.Generation {
			return ErrKitRuntimeBinding
		}
		owned = true
		if s.audit == nil {
			return errors.New("kit workload withdrawal requires audit logging")
		}
		admission, err := q.GetWorkloadAdmission(ctx, workloadrepo.GetWorkloadAdmissionParams{OrganizationID: b.Identity.OrganizationID, ProjectID: project, ID: a.ID})
		if err != nil {
			return fmt.Errorf("revoke kit workload: %w", err)
		}
		if err := s.audit.LogWorkloadAdmissionWithdraw(ctx, db, audit.LogWorkloadAdmissionWithdrawEvent{ActorDisplayName: nil, ActorSlug: nil, OrganizationID: b.Identity.OrganizationID, ProjectID: project, Actor: urn.NewSystemPrincipal("kit-runtime"), AdmissionURN: urn.NewWorkloadAdmission(a.ID), AdmissionDisplayName: a.Subject, AdmissionSnapshotBefore: kitWorkloadAdmissionSnapshot(admission, issuer, b.Identity.AgentID.String())}); err != nil {
			return fmt.Errorf("revoke kit workload: %w", err)
		}
		if _, err := q.SoftDeleteWorkloadAdmission(ctx, workloadrepo.SoftDeleteWorkloadAdmissionParams{OrganizationID: b.Identity.OrganizationID, ProjectID: project, ID: a.ID}); err != nil {
			return fmt.Errorf("revoke kit workload: %w", err)
		}
	}
	if !owned {
		return nil
	}
	remaining, err := q.CountLiveAdmissionsForSubject(ctx, workloadrepo.CountLiveAdmissionsForSubjectParams{OrganizationID: b.Identity.OrganizationID, WorkloadIssuerID: b.IssuerID, Subject: b.Subject, MatchKind: "exact"})
	if err != nil {
		return fmt.Errorf("revoke kit workload: %w", err)
	}
	if remaining != 0 {
		return nil
	}
	_, err = q.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{OrganizationID: b.Identity.OrganizationID, WorkloadIssuerID: b.IssuerID, Subject: b.Subject, MatchKind: "exact"})
	if err != nil {
		return fmt.Errorf("withdraw kit workload assignment: %w", err)
	}
	return nil
}

func kitWorkloadInferenceAllowed(project uuid.UUID, policies ...[]authz.Grant) error {
	if project == uuid.Nil || len(policies) != 2 {
		return ErrKitRuntimeBinding
	}
	for _, policy := range policies {
		allowed, err := authz.GrantsAuthorize(policy, authz.InferenceExecuteCheck(project.String()))
		if err != nil {
			return fmt.Errorf("check kit workload inference policy: %w", err)
		}
		if !allowed {
			return errors.New("kit assistant agent and owner both require inference:execute permission")
		}
	}
	return nil
}

func kitWorkloadIssuerSnapshot(row workloadrepo.WorkloadIssuer) *audit.WorkloadIssuerSnapshot {
	return &audit.WorkloadIssuerSnapshot{Name: row.Name, Issuer: row.Issuer, JwksURI: row.JwksUri, Description: row.Description.String, Tags: row.Tags, AllowWildcardAdmission: row.AllowWildcardAdmission, AllowAssertionReuse: row.AllowAssertionReuse.Valid && row.AllowAssertionReuse.Bool, Tier: "project"}
}
func kitWorkloadAdmissionSnapshot(row workloadrepo.WorkloadIdentityAdmission, issuer workloadrepo.WorkloadIssuer, agentID string) *audit.WorkloadAdmissionSnapshot {
	return &audit.WorkloadAdmissionSnapshot{Issuer: issuer.Issuer, IssuerName: issuer.Name, Subject: row.Subject, MatchKind: row.MatchKind, Name: row.Name.String, Tags: row.Tags, AssignedAgentID: agentID, Tier: "project"}
}

// Existing policy must retain the dedicated runtime ownership markers and exact
// operator trust settings. Never turn on assertion reuse on user-owned policy.
func kitOwnedWorkloadIssuer(issuer workloadrepo.WorkloadIssuer, externalIssuer, jwksURL string) bool {
	return issuer.Name == kitWorkloadIssuerName(externalIssuer) && slices.Equal(issuer.Tags, []string{"kit-runtime"}) && issuer.Issuer == externalIssuer && issuer.JwksUri == jwksURL && !issuer.AllowWildcardAdmission && issuer.AllowAssertionReuse.Valid && issuer.AllowAssertionReuse.Bool
}
