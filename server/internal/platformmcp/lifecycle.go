//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const setupHandoffLifetime = 10 * time.Minute

type ReadinessState string

const (
	ReadinessReady                  ReadinessState = "ready"
	ReadinessNeedsProviderSetup     ReadinessState = "needs_provider_setup"
	ReadinessNeedsGramAuthorization ReadinessState = "needs_gram_authorization"
	ReadinessNeedsConfiguration     ReadinessState = "needs_configuration"
	ReadinessAuthFailed             ReadinessState = "auth_failed"
	ReadinessUnreachable            ReadinessState = "unreachable"
	ReadinessUnsupported            ReadinessState = "unsupported"
	ReadinessUnauthorized           ReadinessState = "unauthorized"
	ReadinessGuideUnavailable       ReadinessState = "guide_unavailable"
	ReadinessDegraded               ReadinessState = "degraded"
)

var (
	ErrSetupHandoffInvalid = errors.New("invalid platform mcp setup handoff")
	ErrReadinessInvalid    = errors.New("invalid platform mcp readiness")
)

type SetupHandoffBinding struct {
	ProjectID        uuid.UUID
	RegistrationID   uuid.UUID
	ProviderKey      string
	CatalogReference string
	Intent           string
}

type SetupHandoff struct {
	ID                   uuid.UUID
	ProjectID            uuid.UUID
	RegistrationID       uuid.UUID
	ProviderKey          string
	CatalogReference     string
	Intent               string
	ExpiresAt            time.Time
	ConnectionID         uuid.UUID
	ConnectionGeneration uuid.UUID
}

type IssuedSetupHandoff struct {
	SetupHandoff
	Value string
}

type ReadinessBinding struct {
	ProjectID                        uuid.UUID
	RegistrationID                   uuid.UUID
	ProviderAuthorizationFingerprint string
}

type Readiness struct {
	ID                   uuid.UUID
	ProjectID            uuid.UUID
	RegistrationID       uuid.UUID
	State                ReadinessState
	EvidenceCode         string
	CheckedAt            time.Time
	ExpiresAt            time.Time
	ConnectionID         uuid.UUID
	ConnectionGeneration uuid.UUID
	Fresh                bool
}

func (s *RegistrationStore) IssueSetupHandoff(ctx context.Context, principal Principal, binding SetupHandoffBinding, now time.Time) (IssuedSetupHandoff, error) {
	if s == nil || s.db == nil {
		return IssuedSetupHandoff{}, ErrUnavailable
	}
	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return IssuedSetupHandoff{}, err
	}
	if err := validateSetupHandoffBinding(principal.OrganizationID, binding); err != nil {
		return IssuedSetupHandoff{}, err
	}

	value, err := newSetupHandoffValue()
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("generate platform mcp setup handoff: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("begin platform mcp setup handoff: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	registration, err := lifecycleRegistration(ctx, q, principal, binding.ProjectID, binding.RegistrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedSetupHandoff{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("load platform mcp setup handoff registration: %w", err)
	}
	if registration.CatalogProvider != binding.ProviderKey || registration.CatalogReference != binding.CatalogReference || registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) {
		return IssuedSetupHandoff{}, ErrSetupHandoffInvalid
	}
	lock := platformrepo.LockPlatformMCPSetupHandoffParams{
		RegistrationID:       binding.RegistrationID.String(),
		ConnectionID:         connectionID.String(),
		ConnectionGeneration: generation.String(),
		Intent:               binding.Intent,
	}
	if err := q.LockPlatformMCPSetupHandoff(ctx, lock); err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("lock platform mcp setup handoff: %w", err)
	}
	if _, err := q.InvalidateActivePlatformMCPSetupHandoffs(ctx, platformrepo.InvalidateActivePlatformMCPSetupHandoffsParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            binding.ProjectID,
		RegistrationID:       binding.RegistrationID,
		ConnectionID:         presentConnection(connectionID),
		ConnectionGeneration: presentConnection(generation),
		Intent:               binding.Intent,
	}); err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("invalidate platform mcp setup handoffs: %w", err)
	}

	row, err := q.CreatePlatformMCPSetupHandoff(ctx, platformrepo.CreatePlatformMCPSetupHandoffParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            binding.ProjectID,
		RegistrationID:       binding.RegistrationID,
		ConnectionID:         presentConnection(connectionID),
		ConnectionGeneration: presentConnection(generation),
		ProviderKey:          binding.ProviderKey,
		Intent:               binding.Intent,
		HandoffHash:          setupHandoffHash(value),
		ExpiresAt:            timestamp(now.Add(setupHandoffLifetime)),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return IssuedSetupHandoff{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("create platform mcp setup handoff: %w", err)
	}
	if err := audit.NewLogger().LogPlatformMcpRegistrationHandoffIssue(ctx, tx, audit.LogPlatformMcpRegistrationHandoffEvent{
		OrganizationID:             principal.OrganizationID,
		ProjectID:                  binding.ProjectID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		PlatformMcpRegistrationURN: urn.NewPlatformMcpRegistration(registration.ID),
		CatalogProvider:            registration.CatalogProvider,
		CatalogReference:           registration.CatalogReference,
		HandoffID:                  row.ID,
		Intent:                     binding.Intent,
	}); err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("record platform mcp setup handoff issue audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("commit platform mcp setup handoff: %w", err)
	}
	issued := setupHandoffFromRow(row)
	issued.CatalogReference = registration.CatalogReference
	return IssuedSetupHandoff{SetupHandoff: issued, Value: value}, nil
}

func (s *RegistrationStore) ConsumeSetupHandoff(ctx context.Context, principal Principal, binding SetupHandoffBinding, value string) (SetupHandoff, error) {
	if s == nil || s.db == nil {
		return SetupHandoff{}, ErrUnavailable
	}
	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return SetupHandoff{}, err
	}
	if err := validateSetupHandoffBinding(principal.OrganizationID, binding); err != nil || value == "" {
		return SetupHandoff{}, ErrSetupHandoffInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SetupHandoff{}, fmt.Errorf("begin platform mcp setup handoff redemption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	registration, err := lifecycleRegistration(ctx, q, principal, binding.ProjectID, binding.RegistrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupHandoff{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return SetupHandoff{}, fmt.Errorf("load platform mcp setup handoff registration: %w", err)
	}
	if registration.CatalogProvider != binding.ProviderKey || registration.CatalogReference != binding.CatalogReference || registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) {
		return SetupHandoff{}, ErrSetupHandoffInvalid
	}
	row, err := q.ConsumePlatformMCPSetupHandoff(ctx, platformrepo.ConsumePlatformMCPSetupHandoffParams{
		HandoffHash:          setupHandoffHash(value),
		OrganizationID:       principal.OrganizationID,
		ProjectID:            binding.ProjectID,
		RegistrationID:       binding.RegistrationID,
		ConnectionID:         presentConnection(connectionID),
		ConnectionGeneration: presentConnection(generation),
		ProviderKey:          binding.ProviderKey,
		Intent:               binding.Intent,
		SubjectUrn:           userSubjectURN(principal.UserID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetupHandoff{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return SetupHandoff{}, fmt.Errorf("consume platform mcp setup handoff: %w", err)
	}
	if err := audit.NewLogger().LogPlatformMcpRegistrationHandoffRedeem(ctx, tx, audit.LogPlatformMcpRegistrationHandoffEvent{
		OrganizationID:             principal.OrganizationID,
		ProjectID:                  binding.ProjectID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		PlatformMcpRegistrationURN: urn.NewPlatformMcpRegistration(registration.ID),
		CatalogProvider:            registration.CatalogProvider,
		CatalogReference:           registration.CatalogReference,
		HandoffID:                  row.ID,
		Intent:                     binding.Intent,
	}); err != nil {
		return SetupHandoff{}, fmt.Errorf("record platform mcp setup handoff redemption audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SetupHandoff{}, fmt.Errorf("commit platform mcp setup handoff redemption: %w", err)
	}
	return setupHandoffFromRow(row), nil
}

// BeginProviderSetup redeems a single-use handoff on a trusted surface and
// dispatches only to the adapter bound to the registration's persisted provider.
func (s *RegistrationStore) BeginProviderSetup(ctx context.Context, principal Principal, binding SetupHandoffBinding, value string, adapters *ProviderAdapters) (ProviderSetupResult, error) {
	if s == nil || s.db == nil {
		return ProviderSetupResult{}, ErrUnavailable
	}
	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return ProviderSetupResult{}, err
	}
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, binding.ProjectID, binding.RegistrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderSetupResult{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return ProviderSetupResult{}, fmt.Errorf("load platform mcp provider registration: %w", err)
	}
	if registration.CatalogProvider != binding.ProviderKey || registration.CatalogReference != binding.CatalogReference || registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) {
		return ProviderSetupResult{}, ErrSetupHandoffInvalid
	}
	adapter, err := adapters.Get(registration.CatalogProvider)
	if err != nil {
		return ProviderSetupResult{}, err
	}
	endpoint, err := mcpendpointsrepo.New(s.db).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{
		ID:        registration.McpEndpointID.UUID,
		ProjectID: binding.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderSetupResult{}, ErrSetupHandoffInvalid
	}
	if err != nil {
		return ProviderSetupResult{}, fmt.Errorf("load platform mcp provider endpoint: %w", err)
	}
	if endpoint.McpServerID != registration.McpServerID.UUID || endpoint.Slug == "" || endpoint.CustomDomainID.Valid || endpoint.IsDomainRoot.Bool {
		return ProviderSetupResult{}, ErrSetupHandoffInvalid
	}
	preflight := ProviderSetupRequest{
		UserID:              principal.UserID,
		OrganizationID:      principal.OrganizationID,
		ProjectID:           binding.ProjectID,
		RegistrationID:      binding.RegistrationID,
		UserSessionIssuerID: registration.UserSessionIssuerID.UUID,
		MCPSlug:             endpoint.Slug,
		ConnectionID:        connectionID,
		Generation:          generation,
	}
	if err := adapter.PreflightSetup(ctx, preflight); err != nil {
		return ProviderSetupResult{}, fmt.Errorf("preflight platform mcp provider setup: %w", err)
	}
	handoff, err := s.ConsumeSetupHandoff(ctx, principal, binding, value)
	if err != nil {
		return ProviderSetupResult{}, err
	}
	if err := s.recordSetupMilestone(ctx, principal, registration, handoff.ID, "provider_setup_started"); err != nil {
		return ProviderSetupResult{}, fmt.Errorf("record platform mcp provider setup start: %w: %w", ErrSetupHandoffReissueRequired, err)
	}
	preflight.HandoffID = handoff.ID
	result, err := adapter.BeginSetup(ctx, preflight)
	if err != nil {
		s.recordSetupFailure(ctx, principal, registration, handoff.ID)
		return ProviderSetupResult{}, fmt.Errorf("begin platform mcp provider setup: %w: %w", ErrSetupHandoffReissueRequired, err)
	}
	if err := validateProviderSetupResult(result); err != nil {
		s.recordSetupFailure(ctx, principal, registration, handoff.ID)
		return ProviderSetupResult{}, fmt.Errorf("validate platform mcp provider setup: %w: %w", ErrSetupHandoffReissueRequired, err)
	}
	return result, nil
}

// ProbeProviderReadiness delegates fixture registrations to their reviewed
// adapter and browser-catalogue registrations to the persisted Remote MCP
// source path. Both paths persist only normalized, generation-bound evidence.
func (s *RegistrationStore) ProbeProviderReadiness(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID, adapters *ProviderAdapters, generic ...CatalogReadinessProber) (Readiness, error) {
	if s == nil || s.db == nil {
		return Readiness{}, ErrUnavailable
	}
	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return Readiness{}, err
	}
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, projectID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Readiness{}, ErrReadinessInvalid
	}
	if err != nil {
		return Readiness{}, fmt.Errorf("load platform mcp readiness registration: %w", err)
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) {
		return Readiness{}, ErrReadinessInvalid
	}
	request := ProviderReadinessProbeRequest{
		UserID:              principal.UserID,
		OrganizationID:      principal.OrganizationID,
		ProjectID:           projectID,
		RegistrationID:      registrationID,
		UserSessionIssuerID: registration.UserSessionIssuerID.UUID,
		ConnectionID:        connectionID,
		Generation:          generation,
	}
	var result ProviderReadinessProbeResult
	if isBrowserCatalogProviderKey(registration.CatalogProvider) {
		if len(generic) == 0 || generic[0] == nil {
			return Readiness{}, ErrProviderAdapterUnavailable
		}
		result, err = generic[0].ProbeCatalogReadiness(ctx, principal, projectID, registrationID, registration.RemoteMcpServerID.UUID, registration.UserSessionIssuerID.UUID, connectionID, generation)
	} else {
		adapter, adapterErr := adapters.Get(registration.CatalogProvider)
		if adapterErr != nil {
			return Readiness{}, adapterErr
		}
		result, err = adapter.ProbeReadiness(ctx, request)
	}
	if err != nil {
		return Readiness{}, fmt.Errorf("probe platform mcp provider readiness: %w", err)
	}
	if err := validateProviderReadinessProbeResult(result); err != nil {
		return Readiness{}, err
	}
	fingerprint, err := ProviderAuthorizationFingerprint(result.AuthorizationIdentity)
	if err != nil {
		return Readiness{}, ErrReadinessInvalid
	}
	return s.RecordReadiness(ctx, principal, ReadinessBinding{
		ProjectID:                        projectID,
		RegistrationID:                   registrationID,
		ProviderAuthorizationFingerprint: fingerprint,
	}, result.State, result.EvidenceCode, result.CheckedAt, result.ExpiresAt)
}

// GetProviderReadiness returns the most recent normalized evidence for the
// principal's active connection generation. It never returns the stored
// authorization fingerprint or attempts provider egress.
func (s *RegistrationStore) GetProviderReadiness(ctx context.Context, principal Principal, projectID, registrationID uuid.UUID) (Readiness, bool, error) {
	if s == nil || s.db == nil {
		return Readiness{}, false, ErrUnavailable
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return Readiness{}, false, err
	}
	q := platformrepo.New(s.db)
	if _, err := q.DeleteExpiredPlatformMCPReadiness(ctx, platformrepo.DeleteExpiredPlatformMCPReadinessParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            projectID,
		RegistrationID:       registrationID,
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
	}); err != nil {
		return Readiness{}, false, fmt.Errorf("delete expired platform mcp readiness: %w", err)
	}
	row, err := q.GetLatestPlatformMCPReadinessForLifecycle(ctx, platformrepo.GetLatestPlatformMCPReadinessForLifecycleParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            projectID,
		RegistrationID:       registrationID,
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
		UserID:               conv.ToPGText(principal.UserID),
		SubjectUrn:           userSubjectURN(principal.UserID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Readiness{}, false, nil
	}
	if err != nil {
		return Readiness{}, false, fmt.Errorf("load platform mcp readiness: %w", err)
	}
	if !isReadinessState(ReadinessState(row.State)) || !row.CheckedAt.Valid || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(row.CheckedAt.Time) {
		return Readiness{}, false, ErrReadinessInvalid
	}
	return readinessFromRow(row, time.Now()), true, nil
}

func (s *RegistrationStore) RecordReadiness(ctx context.Context, principal Principal, binding ReadinessBinding, state ReadinessState, evidenceCode string, checkedAt, expiresAt time.Time) (Readiness, error) {
	if s == nil || s.db == nil {
		return Readiness{}, ErrUnavailable
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return Readiness{}, err
	}
	if err := validateReadiness(principal.OrganizationID, binding, state, checkedAt, expiresAt); err != nil {
		return Readiness{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("begin platform mcp readiness: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	registration, err := lifecycleRegistration(ctx, q, principal, binding.ProjectID, binding.RegistrationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Readiness{}, ErrReadinessInvalid
		}
		return Readiness{}, fmt.Errorf("load platform mcp readiness registration lifecycle: %w", err)
	}

	row, err := q.UpsertPlatformMCPReadiness(ctx, platformrepo.UpsertPlatformMCPReadinessParams{
		OrganizationID:                   principal.OrganizationID,
		ProjectID:                        binding.ProjectID,
		RegistrationID:                   binding.RegistrationID,
		ConnectionID:                     connectionID,
		ConnectionGeneration:             generation,
		UserID:                           conv.ToPGText(principal.UserID),
		ActingSurface:                    conv.ToPGText(string(principal.surface())),
		ProviderAuthorizationFingerprint: binding.ProviderAuthorizationFingerprint,
		State:                            string(state),
		EvidenceCode:                     optionalText(evidenceCode),
		CheckedAt:                        timestamp(checkedAt),
		ExpiresAt:                        optionalLifecycleTimestamp(expiresAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current, loadErr := q.GetPlatformMCPReadiness(ctx, platformrepo.GetPlatformMCPReadinessParams{
			OrganizationID:                   principal.OrganizationID,
			ProjectID:                        binding.ProjectID,
			RegistrationID:                   binding.RegistrationID,
			ConnectionID:                     connectionID,
			ConnectionGeneration:             generation,
			ProviderAuthorizationFingerprint: binding.ProviderAuthorizationFingerprint,
		})
		if loadErr == nil {
			if err := tx.Commit(ctx); err != nil {
				return Readiness{}, fmt.Errorf("commit current platform mcp readiness: %w", err)
			}
			return readinessFromRow(current, time.Now()), nil
		}
		if errors.Is(loadErr, pgx.ErrNoRows) {
			return Readiness{}, ErrReadinessInvalid
		}
		return Readiness{}, fmt.Errorf("load current platform mcp readiness: %w", loadErr)
	}
	if err != nil {
		return Readiness{}, fmt.Errorf("upsert platform mcp readiness: %w", err)
	}
	if state == ReadinessReady && expiresAt.After(time.Now()) {
		handoff, err := q.GetLatestRedeemedPlatformMCPSetupHandoff(ctx, platformrepo.GetLatestRedeemedPlatformMCPSetupHandoffParams{
			OrganizationID:       principal.OrganizationID,
			ProjectID:            binding.ProjectID,
			RegistrationID:       binding.RegistrationID,
			ConnectionID:         connectionID,
			ConnectionGeneration: generation,
			SubjectUrn:           userSubjectURN(principal.UserID),
		})
		switch {
		case err == nil:
			for _, milestone := range []string{"provider_setup_succeeded", "platform_flow_ready"} {
				if err := recordSetupMilestone(ctx, q, principal, registration, handoff.ID, milestone); err != nil {
					return Readiness{}, fmt.Errorf("record platform mcp readiness milestone: %w", err)
				}
			}
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return Readiness{}, fmt.Errorf("load redeemed platform mcp setup handoff: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Readiness{}, fmt.Errorf("commit platform mcp readiness: %w", err)
	}
	return readinessFromRow(row, time.Now()), nil
}

func (s *RegistrationStore) recordSetupMilestone(ctx context.Context, principal Principal, registration platformrepo.PlatformMcpCatalogRegistration, handoffID uuid.UUID, milestone string) error {
	if handoffID == uuid.Nil {
		return ErrSetupHandoffInvalid
	}
	return recordSetupMilestone(ctx, platformrepo.New(s.db), principal, registration, handoffID, milestone)
}

func (s *RegistrationStore) recordSetupFailure(ctx context.Context, principal Principal, registration platformrepo.PlatformMcpCatalogRegistration, handoffID uuid.UUID) {
	_ = s.recordSetupMilestone(ctx, principal, registration, handoffID, "provider_setup_failed")
}

func recordSetupMilestone(ctx context.Context, q *platformrepo.Queries, principal Principal, registration platformrepo.PlatformMcpCatalogRegistration, handoffID uuid.UUID, milestone string) error {
	connectionID, generation, err := parseConnection(principal)
	if err != nil {
		return err
	}
	if registration.ID == uuid.Nil || registration.ProjectID == uuid.Nil || registration.CatalogProvider == "" || registration.CatalogReference == "" || handoffID == uuid.Nil || !isSetupMilestone(milestone) {
		return ErrReadinessInvalid
	}
	if err := q.RecordPlatformMCPSetupMilestone(ctx, platformrepo.RecordPlatformMCPSetupMilestoneParams{
		OrganizationID:       principal.OrganizationID,
		Milestone:            milestone,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		ProjectID:            uuid.NullUUID{UUID: registration.ProjectID, Valid: true},
		McpKey:               registration.CatalogProvider + ":" + registration.CatalogReference,
		AttemptID:            uuid.NullUUID{UUID: handoffID, Valid: true},
	}); err != nil {
		return fmt.Errorf("record platform mcp setup milestone: %w", err)
	}
	return nil
}

func isSetupMilestone(value string) bool {
	switch value {
	case "provider_setup_started", "provider_setup_failed", "provider_setup_succeeded", "platform_flow_ready":
		return true
	default:
		return false
	}
}

// lifecycleRegistration resolves a registration the caller is entitled to act
// on. Ownership matches the real user; a caller holding an OAuth connection
// additionally has its generation checked live, which a connectionless surface
// has no equivalent of and is instead authorized on every call upstream.
func lifecycleRegistration(ctx context.Context, q *platformrepo.Queries, principal Principal, projectID, registrationID uuid.UUID) (platformrepo.PlatformMcpCatalogRegistration, error) {
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, err
	}
	registration, err := q.GetPlatformMCPCatalogRegistrationForLifecycle(ctx, platformrepo.GetPlatformMCPCatalogRegistrationForLifecycleParams{
		RegistrationID:       registrationID,
		OrganizationID:       principal.OrganizationID,
		ProjectID:            projectID,
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
		UserID:               conv.ToPGText(principal.UserID),
		SubjectUrn:           userSubjectURN(principal.UserID),
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("load platform mcp lifecycle registration: %w", err)
	}
	return registration, nil
}

func validateSetupHandoffBinding(organizationID string, binding SetupHandoffBinding) error {
	if organizationID == "" || binding.ProjectID == uuid.Nil || binding.RegistrationID == uuid.Nil || binding.ProviderKey == "" || binding.CatalogReference == "" || binding.Intent == "" {
		return ErrSetupHandoffInvalid
	}
	return nil
}

func validateReadiness(organizationID string, binding ReadinessBinding, state ReadinessState, checkedAt, expiresAt time.Time) error {
	if organizationID == "" || binding.ProjectID == uuid.Nil || binding.RegistrationID == uuid.Nil || binding.ProviderAuthorizationFingerprint == "" || !isReadinessState(state) || checkedAt.IsZero() || expiresAt.IsZero() || !expiresAt.After(checkedAt) {
		return ErrReadinessInvalid
	}
	return nil
}

func isReadinessState(state ReadinessState) bool {
	switch state {
	case ReadinessReady, ReadinessNeedsProviderSetup, ReadinessNeedsGramAuthorization, ReadinessNeedsConfiguration, ReadinessAuthFailed, ReadinessUnreachable, ReadinessUnsupported, ReadinessUnauthorized, ReadinessGuideUnavailable, ReadinessDegraded:
		return true
	default:
		return false
	}
}

func newSetupHandoffValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read setup handoff entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func setupHandoffHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func setupHandoffFromRow(row platformrepo.PlatformMcpSetupHandoff) SetupHandoff {
	return SetupHandoff{
		ID:             row.ID,
		ProjectID:      row.ProjectID,
		RegistrationID: row.RegistrationID,
		ProviderKey:    row.ProviderKey,
		Intent:         row.Intent,
		ExpiresAt:      row.ExpiresAt.Time,
		// Only connection-bearing paths write handoffs today, so a null here
		// is unreachable rather than meaningfully zero.
		ConnectionID:         row.ConnectionID.UUID,
		ConnectionGeneration: row.ConnectionGeneration.UUID,
	}
}

func readinessFromRow(row platformrepo.PlatformMcpReadiness, now time.Time) Readiness {
	return Readiness{
		ID:                   row.ID,
		ProjectID:            row.ProjectID,
		RegistrationID:       row.RegistrationID,
		State:                ReadinessState(row.State),
		EvidenceCode:         row.EvidenceCode.String,
		CheckedAt:            row.CheckedAt.Time,
		ExpiresAt:            row.ExpiresAt.Time,
		ConnectionID:         row.ConnectionID.UUID,
		ConnectionGeneration: row.ConnectionGeneration.UUID,
		Fresh:                row.ExpiresAt.Valid && row.ExpiresAt.Time.After(now),
	}
}

func optionalText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func optionalLifecycleTimestamp(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return timestamp(value)
}
