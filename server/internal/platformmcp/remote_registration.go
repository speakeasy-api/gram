package platformmcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	// remoteURLSourceKind marks a registration provisioned from a
	// user-supplied remote MCP URL rather than a reviewed catalogue entry.
	// inventory.go maps it to the "user_supplied_url" display source.
	remoteURLSourceKind = "remote_url"

	// remoteURLCatalogProvider is the sentinel provider recorded for remote
	// URL registrations. The registration row requires a provider identity,
	// and the sentinel keeps the existing audit event and McpKey derivation
	// working unchanged while making rows unmistakably non-catalogue.
	remoteURLCatalogProvider = "remote-url"

	// maxRemoteDisplayNameLength bounds the caller-supplied display name, the
	// same bound idempotency keys carry.
	maxRemoteDisplayNameLength = 128
)

// RegisterRemoteMCPInput registers a probed remote MCP URL as a project
// source. There is deliberately no URL field: the URL travels only inside the
// signed probe receipt, preserving the invariant that mutations accept
// server-issued identities alone.
type RegisterRemoteMCPInput struct {
	// ProjectSlug names the registration target project.
	ProjectSlug string

	// ProbeReceipt is the signed receipt a successful probe_remote_mcp call
	// issued to this caller. It carries the normalized URL.
	ProbeReceipt string

	// IdempotencyKey scopes replay: repeating a call with the same key returns
	// the original registration.
	IdempotencyKey string

	// DisplayName optionally names the provisioned components. When empty the
	// remote server's host is used.
	DisplayName string
}

// RegisterRemoteMCPResult reports one remote URL registration.
type RegisterRemoteMCPResult struct {
	// Project is the resolved registration target.
	Project ResolvedProject

	// RemoteURL is the normalized URL the registration persisted, recovered
	// from the validated probe receipt.
	RemoteURL string

	// Receipt is the operation receipt behind the registration.
	Receipt OperationReceipt

	// Registration is the registration row id.
	Registration string

	// BlockedPendingApproval reports that organization MCP approval
	// enforcement is active for the project and this server is not approved
	// under it. The registration stands, but the server stays blocked until an
	// admin approves it in the dashboard.
	BlockedPendingApproval bool

	// DashboardApprovalsURL is the dashboard surface where the pending
	// approval is requested and decided. Set only when BlockedPendingApproval.
	DashboardApprovalsURL string
}

// RemoteMCPApprovalState is what org MCP approval enforcement says about one
// server URL.
type RemoteMCPApprovalState struct {
	// EnforcementActive reports whether the project has at least one enabled
	// blocking shadow-MCP policy.
	EnforcementActive bool

	// Approved reports whether the server URL is approved under every enabled
	// blocking policy. Meaningful only when EnforcementActive.
	Approved bool
}

// RemoteMCPApprovalChecker consults org MCP approval enforcement for one
// server URL. Registration never bypasses enforcement: when the checker
// reports an active-but-unapproved server the result carries the
// blocked_pending_approval signal instead of quietly admitting the server.
type RemoteMCPApprovalChecker interface {
	CheckRemoteMCPApproval(ctx context.Context, organizationID string, projectID uuid.UUID, remoteURL string) (RemoteMCPApprovalState, error)
}

// WithRemoteRegistration enables RegisterRemoteMCP. receiptKeyMaterial must be
// the same key material the probe service signs receipts with; approvals is
// the post-registration org MCP approval enforcement consult. Both are
// required — while either is missing the remote URL path stays unavailable
// rather than becoming a loophole around org policy.
func (s *RegistrationService) WithRemoteRegistration(receiptKeyMaterial string, approvals RemoteMCPApprovalChecker) *RegistrationService {
	if s == nil || approvals == nil {
		return s
	}
	codec, err := newProbeReceiptCodec(receiptKeyMaterial)
	if err != nil {
		return s
	}
	s.probeReceipts = codec
	s.remoteApprovals = approvals
	return s
}

// RegisterRemoteMCP registers a probed remote MCP URL as a project source. It
// mirrors RegisterCatalogMCP's spine — budget, gate, identity validation,
// project eligibility, then the receipt-driven convergence — with the probe
// receipt standing in for the reviewed catalogue identity. The URL is trusted
// from the receipt: Encode only ever signs a normalized, shape-valid URL.
func (s *RegistrationService) RegisterRemoteMCP(ctx context.Context, principal Principal, input RegisterRemoteMCPInput) (RegisterRemoteMCPResult, error) {
	if s == nil || s.gate == nil || s.store == nil || s.probeReceipts == nil || s.remoteApprovals == nil || !s.budgets.Registration.valid() || input.ProjectSlug == "" || input.ProbeReceipt == "" || input.IdempotencyKey == "" {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	if err := s.budgets.Registration.Allow(ctx, principal); err != nil {
		return RegisterRemoteMCPResult{}, err
	}

	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("check remote mcp registration gate: %w", err)
	}
	if !enabled {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}

	probeReceipt, err := s.probeReceipts.Decode(input.ProbeReceipt, principal, s.now())
	if err != nil {
		return RegisterRemoteMCPResult{}, err
	}

	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("resolve remote mcp registration project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return RegisterRemoteMCPResult{}, err
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if len(displayName) > maxRemoteDisplayNameLength {
		return RegisterRemoteMCPResult{}, ErrRegistrationInvalid
	}
	if displayName == "" {
		displayName = remoteMCPDisplayName(probeReceipt.NormalizedURL)
	}

	request := CatalogRegistrationRequest{
		ProjectSlug:       project.Slug,
		SourceKind:        remoteURLSourceKind,
		CatalogProvider:   remoteURLCatalogProvider,
		CatalogReference:  probeReceipt.NormalizedURL,
		ConfigurationHash: "",
		IdempotencyKey:    input.IdempotencyKey,
		InputHash:         catalogRegistrationInputHash(project.Slug, remoteURLSourceKind, remoteURLCatalogProvider, probeReceipt.NormalizedURL),
	}
	receipt, err := s.store.BeginReceipt(ctx, principal, project, request, s.now())
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("begin remote mcp registration receipt: %w", err)
	}
	if !receipt.Replayed || receipt.Status == receiptStatusPending {
		receipt, err = s.store.ConvergeRegistration(ctx, principal, project, request, receipt)
		if err != nil {
			return RegisterRemoteMCPResult{}, fmt.Errorf("converge remote mcp registration: %w", err)
		}
	}
	if receipt.ResultCode == receiptResultActiveCap {
		return RegisterRemoteMCPResult{}, ErrRegistrationCap
	}
	if !receipt.RegistrationID.Valid {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	if receipt.Status == receiptStatusPending {
		receipt, err = s.store.CompleteRegistrationWithRemoteURL(ctx, principal, project, request, receipt, probeReceipt.NormalizedURL, displayName)
		if err != nil {
			return RegisterRemoteMCPResult{}, fmt.Errorf("complete remote mcp registration: %w", err)
		}
	}
	if !receipt.RegistrationID.Valid {
		s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "unavailable", State: ""})
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "succeeded", State: ""})

	result := RegisterRemoteMCPResult{
		Project:                project,
		RemoteURL:              probeReceipt.NormalizedURL,
		Receipt:                receipt,
		Registration:           receipt.RegistrationID.UUID.String(),
		BlockedPendingApproval: false,
		DashboardApprovalsURL:  "",
	}

	// Post-registration enforcement consult. A failure here fails the call
	// rather than reporting an unknown state as unblocked; the registration
	// stands, and an idempotent replay re-runs the consult.
	approval, err := s.remoteApprovals.CheckRemoteMCPApproval(ctx, principal.OrganizationID, project.ID, probeReceipt.NormalizedURL)
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("consult remote mcp approval enforcement: %w", err)
	}
	if approval.EnforcementActive && !approval.Approved {
		approvalsURL, err := s.remoteApprovalsDashboardURL(ctx, principal, project, receipt.RegistrationID.UUID)
		if err != nil {
			return RegisterRemoteMCPResult{}, err
		}
		result.BlockedPendingApproval = true
		result.DashboardApprovalsURL = approvalsURL
	}

	return result, nil
}

// RemoteRegistrationEnforcement reports what organization MCP approval
// enforcement currently says about one lifecycle-bound registration.
type RemoteRegistrationEnforcement struct {
	// BlockedPendingApproval reports that the registration is a remote URL
	// source, approval enforcement is active for its project, and the server is
	// not approved under it. The registration stands, but the server stays
	// blocked until an admin decides the approval in the dashboard.
	BlockedPendingApproval bool

	// DashboardApprovalsURL is the dashboard surface where the pending approval
	// is requested and decided. Set only when BlockedPendingApproval.
	DashboardApprovalsURL string
}

// RemoteRegistrationEnforcementStatus consults current organization MCP
// approval enforcement for a lifecycle-bound registration, so status surfaces
// report a blocked remote server honestly instead of steering the user through
// setup steps that cannot succeed. Catalogue registrations are never
// enforcement-blocked here and report the zero value without a consult. For a
// remote URL registration the consult fails closed: an unknown enforcement
// state is an error, never "unblocked".
func (s *RegistrationService) RemoteRegistrationEnforcementStatus(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (RemoteRegistrationEnforcement, error) {
	unblocked := RemoteRegistrationEnforcement{BlockedPendingApproval: false, DashboardApprovalsURL: ""}
	if s == nil || s.store == nil || registrationID == uuid.Nil {
		return unblocked, ErrRegistrationUnavailable
	}
	candidate, err := s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, registrationID)
	if err != nil {
		return unblocked, fmt.Errorf("resolve remote mcp registration identity: %w", err)
	}
	if candidate.ProviderKey != remoteURLCatalogProvider {
		return unblocked, nil
	}
	if s.remoteApprovals == nil {
		return unblocked, ErrRegistrationUnavailable
	}
	// The consult uses the persisted registration reference — the normalized
	// URL the registration was completed with — never a caller-supplied value.
	approval, err := s.remoteApprovals.CheckRemoteMCPApproval(ctx, principal.OrganizationID, project.ID, candidate.CatalogRef)
	if err != nil {
		return unblocked, fmt.Errorf("consult remote mcp approval enforcement: %w", err)
	}
	if !approval.EnforcementActive || approval.Approved {
		return unblocked, nil
	}
	approvalsURL, err := s.remoteApprovalsDashboardURL(ctx, principal, project, registrationID)
	if err != nil {
		return unblocked, err
	}
	return RemoteRegistrationEnforcement{BlockedPendingApproval: true, DashboardApprovalsURL: approvalsURL}, nil
}

// remoteApprovalsDashboardURL is the dashboard surface where pending shadow-MCP
// approvals for the project are requested and decided: absolute when a
// dashboard origin is configured, a root-relative path otherwise. It is the
// shadow-MCP list page, not a per-server detail page, because a just-registered
// server may have no inventory row behind a detail route yet.
func (s *RegistrationService) remoteApprovalsDashboardURL(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (string, error) {
	setup, err := s.store.ResolveRegistrationDashboardSetup(ctx, principal, project, registrationID)
	if err != nil {
		return "", fmt.Errorf("resolve remote mcp approvals dashboard organization: %w", err)
	}
	approvalsBase := &url.URL{Path: "/"}
	if s.dashboardURL != nil {
		approvalsBase = s.dashboardURL
	}
	return approvalsBase.JoinPath(setup.OrganizationSlug, "projects", project.Slug, "shadow-mcp").String(), nil
}

// remoteMCPDisplayName derives the default component display name from a
// normalized remote URL: its host. The URL was validated at probe time, so a
// parse failure cannot practically occur; the static fallback keeps the
// components named even then.
func remoteMCPDisplayName(normalizedURL string) string {
	parsed, err := url.Parse(normalizedURL)
	if err != nil || parsed.Host == "" {
		return "Remote MCP server"
	}
	return parsed.Host
}

// PostgresRemoteMCPApprovals answers the registration-time approval consult
// from the same state the shadow-MCP hook layer enforces at runtime: the
// project's enabled blocking policies and their per-URL grants, which recorded
// approval decisions reconcile into. This is deliberately the grant read, not
// the review-row read — a server can be allowed with no review at all under an
// allow-by-default policy, and blocked with no review under block-by-default.
type PostgresRemoteMCPApprovals struct {
	db *pgxpool.Pool
}

func NewPostgresRemoteMCPApprovals(db *pgxpool.Pool) *PostgresRemoteMCPApprovals {
	return &PostgresRemoteMCPApprovals{db: db}
}

var _ RemoteMCPApprovalChecker = (*PostgresRemoteMCPApprovals)(nil)

// CheckRemoteMCPApproval reports whether the project's blocking shadow-MCP
// policies admit the server URL. Mirroring the enforcement writes: a
// block-by-default policy admits a URL only through a standing
// risk_policy:bypass grant, an allow-by-default policy blocks one only through
// a standing risk_policy:block grant, and every blocking policy must admit the
// URL for it to count as approved. Grant audience is not evaluated — any
// standing bypass grant is a recorded approval decision, whatever its blast
// radius.
func (c *PostgresRemoteMCPApprovals) CheckRemoteMCPApproval(ctx context.Context, organizationID string, projectID uuid.UUID, remoteURL string) (RemoteMCPApprovalState, error) {
	if c == nil || c.db == nil || organizationID == "" || projectID == uuid.Nil {
		return RemoteMCPApprovalState{EnforcementActive: false, Approved: false}, ErrRegistrationUnavailable
	}
	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(remoteURL)
	if !ok {
		return RemoteMCPApprovalState{EnforcementActive: false, Approved: false}, fmt.Errorf("canonicalize remote mcp server url for approval consult: %w", ErrRemoteURLInvalid)
	}

	policies, err := riskrepo.New(c.db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return RemoteMCPApprovalState{EnforcementActive: false, Approved: false}, fmt.Errorf("list shadow mcp policies for remote mcp approval consult: %w", err)
	}

	state := RemoteMCPApprovalState{EnforcementActive: false, Approved: true}
	for _, policy := range policies {
		if policy.Action != "block" {
			continue
		}
		state.EnforcementActive = true

		// Legacy rows without a disposition default to block-by-default,
		// exactly as the enforcement writes treat them.
		allowAll := policy.ShadowMcpDisposition.Valid && policy.ShadowMcpDisposition.String == shadowmcp.DispositionAllowAll
		scope := authz.ScopeRiskPolicyBypass
		if allowAll {
			scope = authz.ScopeRiskPolicyBlock
		}
		grants, err := authz.ListGrantsForResource(ctx, c.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          scope,
			ResourceID:     policy.ID.String(),
		})
		if err != nil {
			return RemoteMCPApprovalState{EnforcementActive: true, Approved: false}, fmt.Errorf("list shadow mcp policy grants for remote mcp approval consult: %w", err)
		}
		granted := false
		for _, grant := range grants {
			// Legacy selectors may carry extra keys alongside server_url; any
			// grant naming the canonical URL covers it, matching the
			// inventory read and the runtime evaluator.
			if grant.Selector[authz.SelectorKeyServerURL] == inventoryURL.CanonicalURL {
				granted = true
				break
			}
		}
		// Block-by-default blocks unless a bypass grant stands;
		// allow-by-default blocks exactly when a block rule stands.
		policyBlocks := granted == allowAll
		if policyBlocks {
			state.Approved = false
		}
	}
	return state, nil
}
