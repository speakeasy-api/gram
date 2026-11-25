package admin

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func (s *Service) ListUpstreamOAuthMigrationOrganizations(ctx context.Context, _ *gen.ListUpstreamOAuthMigrationOrganizationsPayload) (*gen.AdminUpstreamOAuthMigrationOrganizations, error) {
	rows, err := repo.New(s.db).AdminListUpstreamOAuthMigrationOrganizations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list upstream oauth migration organizations").LogError(ctx, s.logger)
	}
	organizations := make([]*gen.AdminUpstreamOAuthMigrationOrganization, 0, len(rows))
	for _, row := range rows {
		organizations = append(organizations, &gen.AdminUpstreamOAuthMigrationOrganization{
			ID:      row.ID,
			Name:    row.Name,
			Slug:    row.Slug,
			Servers: row.Servers,
		})
	}
	return &gen.AdminUpstreamOAuthMigrationOrganizations{Organizations: organizations}, nil
}

func (s *Service) GetUpstreamOAuthMigrationStatus(ctx context.Context, p *gen.GetUpstreamOAuthMigrationStatusPayload) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.upstreamOAuthMigrationStatus(ctx, organizationID)
}

// RunUpstreamOAuthMigration keeps the old staff contract non-mutating.
func (s *Service) RunUpstreamOAuthMigration(ctx context.Context, _ *gen.RunUpstreamOAuthMigrationPayload) (*gen.AdminUpstreamOAuthMigrationRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return nil, oops.E(oops.CodeNotImplemented, nil, "the upstream OAuth migration has been retired").LogWarn(ctx, s.logger)
}

// upstreamOAuthMigrationStatus retains the old response shape for staff
// clients, but derives readiness from typed offers, choices, and profiles.
func (s *Service) upstreamOAuthMigrationStatus(ctx context.Context, organizationID string) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	status := &gen.AdminUpstreamOAuthMigrationStatus{
		AuthClients: nil, ExecutionProfiles: nil, Enforcement: nil,
		OrganizationID: organizationID, Migrated: false, Servers: 0,
		PendingAssociations: 0, UnresolvedServers: []*gen.AdminUpstreamOAuthMigrationServer{},
		Credentials: 0, PendingSelections: 0, ConsentScopedSelections: nil, ReconnectCredentials: 0,
	}
	enforced, err := s.productFeatures.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureUpstreamAuthEnforced)
	if err != nil {
		s.logger.ErrorContext(ctx, "read upstream auth enforcement", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
	} else {
		status.AuthClients, status.ReconnectCredentials = s.authClientMigrationStatus(ctx, organizationID)
		status.ExecutionProfiles = s.executionProfileMigrationStatus(ctx, organizationID, enforced)
		if clients := status.AuthClients; clients != nil {
			status.Migrated = clients.Complete && clients.PendingSelectionMappings != nil && *clients.PendingSelectionMappings == 0
			if clients.PendingSelectionMappings != nil {
				status.PendingSelections = *clients.PendingSelectionMappings
			}
		}
	}
	status.Enforcement = s.upstreamAuthEnforcement(ctx, status)
	return status, nil
}

// authClientMigrationStatus reports typed readiness without applying changes.
// Failed phases are named alongside the counts from successful phases.
func (s *Service) authClientMigrationStatus(ctx context.Context, organizationID string) (*gen.AdminUpstreamOAuthMigrationAuthClients, int64) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	report, err := authclientbackfill.Run(ctx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0, TypedOnly: true})
	switch {
	case errors.Is(err, authclientbackfill.ErrRunInProgress):
		return &gen.AdminUpstreamOAuthMigrationAuthClients{
			Complete:                 false,
			InProgress:               true,
			Associations:             0,
			PendingClients:           0,
			PendingLinks:             0,
			PendingHeaders:           0,
			PendingRetirements:       0,
			PendingMarkedServers:     0,
			Unresolved:               0,
			UnresolvedAssociations:   []*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{},
			Warnings:                 []*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{},
			Kinds:                    []*gen.AdminUpstreamOAuthMigrationAuthClientKind{},
			FailedPhases:             []string{},
			SelectionsComplete:       new(false),
			PendingSelectionMappings: new(int64(0)),
			PendingGrants:            new(int64(0)),
			AttachmentsWaiting:       new(int64(0)),
			SelectionClasses:         []*gen.AdminUpstreamOAuthMigrationSelectionClass{},
			SelectionGaps:            []*gen.AdminUpstreamOAuthMigrationSelectionGap{},
			SelectionGapCount:        new(int64(0)),
		}, 0
	case err != nil:
		logger.ErrorContext(ctx, "check typed auth clients", attr.SlogError(err))
		return nil, 0
	}
	phases := failedPhases(report)
	for _, phase := range phases {
		logger.ErrorContext(ctx, "check typed auth clients", attr.SlogError(errors.New(report.PhaseErrors[phase])), attr.SlogAuthClientBackfillPhase(phase))
	}
	unresolved := []*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{}
	warnings := []*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{}
	for _, result := range report.Results {
		var list *[]*gen.AdminUpstreamOAuthMigrationUnresolvedAssociation
		switch result.Outcome {
		case authclientbackfill.OutcomeAmbiguous:
			list = &unresolved
		case authclientbackfill.OutcomeWarning:
			list = &warnings
		default:
			continue
		}
		var authClientID *string
		if result.AuthClientID.Valid {
			authClientID = new(result.AuthClientID.UUID.String())
		}
		bindingIDs, sourceIDs, serverIDs := idStrings(result.BindingIDs), idStrings(result.SourceIDs), idStrings(result.ServerIDs)
		*list = append(*list, &gen.AdminUpstreamOAuthMigrationUnresolvedAssociation{
			Kind:                  string(result.Kind),
			Reason:                string(result.Reason),
			SubjectID:             result.SubjectID.String(),
			AssociationID:         result.AssociationID.String(),
			McpServerID:           result.MCPServerID.String(),
			ProjectID:             result.ProjectID.String(),
			RemoteSessionClientID: result.RemoteSessionClientID.String(),
			BindingIds:            bindingIDs,
			SourceIds:             sourceIDs,
			ServerIds:             serverIDs,
			AuthClientID:          authClientID,
		})
	}
	kinds := make([]*gen.AdminUpstreamOAuthMigrationAuthClientKind, 0, len(authclientbackfill.Kinds))
	for _, kind := range authclientbackfill.Kinds {
		counts := report.ByKind[kind]
		if counts == nil {
			continue
		}
		kinds = append(kinds, &gen.AdminUpstreamOAuthMigrationAuthClientKind{
			Kind:               string(kind),
			Decided:            int64(counts.Decided),
			PendingClients:     int64(counts.PendingClients),
			PendingLinks:       int64(counts.PendingLinks),
			PendingHeaders:     int64(counts.PendingHeaders),
			PendingRetirements: int64(counts.PendingRetirements),
			Unresolved:         int64(counts.Unresolved),
		})
	}
	pendingRetirements := int64(report.PendingRetiredClients + report.PendingRetiredLinks)
	classes, gaps, gapCount := selectionCoverage(report)
	_, selectionsFailed := report.PhaseErrors[authclientbackfill.PhaseSelections]
	return &gen.AdminUpstreamOAuthMigrationAuthClients{
		Complete:                 report.PendingClients == 0 && report.PendingLinks == 0 && report.PendingHeaders == 0 && report.Gaps == 0 && pendingRetirements == 0 && report.AttachmentsWaiting == 0 && len(phases) == 0,
		InProgress:               false,
		Associations:             int64(report.Associations),
		PendingClients:           int64(report.PendingClients),
		PendingLinks:             int64(report.PendingLinks),
		PendingHeaders:           int64(report.PendingHeaders),
		PendingRetirements:       pendingRetirements,
		PendingMarkedServers:     0,
		Unresolved:               int64(len(unresolved)),
		UnresolvedAssociations:   unresolved,
		Warnings:                 warnings,
		Kinds:                    kinds,
		FailedPhases:             phases,
		SelectionsComplete:       new(gapCount == 0 && report.PendingSelectionMappings == 0 && !selectionsFailed),
		PendingSelectionMappings: new(int64(report.PendingSelectionMappings)),
		PendingGrants:            new(int64(report.PendingGrants)),
		AttachmentsWaiting:       new(int64(report.AttachmentsWaiting)),
		SelectionClasses:         classes,
		SelectionGaps:            gaps,
		SelectionGapCount:        new(int64(gapCount)),
	}, countReconnectCredentials(report.SelectionGaps)
}

func countReconnectCredentials(gaps []authclientbackfill.SelectionGap) int64 {
	credentials := make(map[uuid.UUID]struct{})
	for _, gap := range gaps {
		switch gap.Outcome {
		case authclientbackfill.SelectionAmbiguous, authclientbackfill.SelectionStaleNoAuth, authclientbackfill.SelectionUnusable:
			credentials[gap.CredentialID] = struct{}{}
		case authclientbackfill.SelectionMapped, authclientbackfill.SelectionMap, authclientbackfill.SelectionNoAuth, authclientbackfill.SelectionNoClient, authclientbackfill.SelectionSkippedRace:
			// These choices are covered, need configuration, or can be rechecked.
		}
	}
	return int64(len(credentials))
}

// selectionGapLimit bounds the choices a status lists; the count covers the
// rest.
const selectionGapLimit = 100

// selectionCoverage reports a run's choice coverage by credential class, in a
// stable order, with the first gaps.
func selectionCoverage(report authclientbackfill.Report) ([]*gen.AdminUpstreamOAuthMigrationSelectionClass, []*gen.AdminUpstreamOAuthMigrationSelectionGap, int) {
	names := make([]string, 0, len(report.Selections))
	for class := range report.Selections {
		names = append(names, string(class))
	}
	slices.Sort(names)
	classes := make([]*gen.AdminUpstreamOAuthMigrationSelectionClass, 0, len(names))
	gapCount := 0
	for _, name := range names {
		counts := report.Selections[authclientbackfill.CredentialClass(name)]
		gapCount += counts.Gaps()
		classes = append(classes, &gen.AdminUpstreamOAuthMigrationSelectionClass{
			CredentialClass: name,
			Selections:      int64(counts.Selections),
			Mapped:          int64(counts.Outcomes[authclientbackfill.SelectionMapped] + counts.Written),
			Pending:         int64(counts.Outcomes[authclientbackfill.SelectionMap] - counts.Written),
			NoAuth:          int64(counts.Outcomes[authclientbackfill.SelectionNoAuth]),
			Ambiguous:       int64(counts.Outcomes[authclientbackfill.SelectionAmbiguous]),
			NoClient:        int64(counts.Outcomes[authclientbackfill.SelectionNoClient]),
			StaleNoAuth:     int64(counts.Outcomes[authclientbackfill.SelectionStaleNoAuth]),
			Unusable:        int64(counts.Outcomes[authclientbackfill.SelectionUnusable]),
		})
	}
	gaps := make([]*gen.AdminUpstreamOAuthMigrationSelectionGap, 0, min(len(report.SelectionGaps), selectionGapLimit))
	for _, gap := range report.SelectionGaps[:min(len(report.SelectionGaps), selectionGapLimit)] {
		gaps = append(gaps, &gen.AdminUpstreamOAuthMigrationSelectionGap{
			SelectionID:     gap.SelectionID.String(),
			CredentialClass: string(gap.CredentialClass),
			CredentialID:    gap.CredentialID.String(),
			McpServerID:     gap.MCPServerID.String(),
			ProjectID:       gap.ProjectID.String(),
			Outcome:         string(gap.Outcome),
		})
	}
	return classes, gaps, gapCount
}

// executionProfileMigrationStatus dry-runs the execution-selection profile
// backfill. A failure only leaves it out.
func (s *Service) executionProfileMigrationStatus(ctx context.Context, organizationID string, enforced bool) *gen.AdminUpstreamOAuthMigrationExecutionProfiles {
	report, err := executionprofiles.Backfill(ctx, s.db, organizationID, false)
	if err != nil {
		s.logger.ErrorContext(ctx, "check execution selection profiles", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return nil
	}
	gaps := []*gen.AdminUpstreamOAuthMigrationExecutionProfileGap{}
	for _, execCtx := range executionprofiles.Contexts {
		reasons := make([]string, 0, len(report.Gaps[execCtx]))
		for reason := range report.Gaps[execCtx] {
			reasons = append(reasons, string(reason))
		}
		slices.Sort(reasons)
		for _, reason := range reasons {
			gaps = append(gaps, &gen.AdminUpstreamOAuthMigrationExecutionProfileGap{
				ExecutionContext: string(execCtx),
				Reason:           reason,
				Servers:          int64(report.Gaps[execCtx][executionprofiles.GapReason(reason)]),
			})
		}
	}
	// Before enforcement a run records every profile from the configuration
	// that serves turns today, so none is left to be decided differently
	// later. Under enforcement a turn's own bind decides exactly what a run
	// would, so a profile no turn has run under yet is not pending.
	unexercised := 0
	if enforced {
		unexercised = report.FirstVersions
	}
	pending := report.VersionsWritten - unexercised
	return &gen.AdminUpstreamOAuthMigrationExecutionProfiles{
		Complete:            pending == 0 && len(gaps) == 0,
		Targets:             int64(report.Targets),
		PendingVersions:     int64(pending),
		UnexercisedVersions: new(int64(unexercised)),
		Gaps:                gaps,
	}
}

// failedPhases names the phases a run reports failed, in run order.
func failedPhases(report authclientbackfill.Report) []string {
	phases := []string{}
	for _, phase := range []string{authclientbackfill.PhaseOAuth, authclientbackfill.PhaseHeaders, authclientbackfill.PhaseEMA, authclientbackfill.PhaseEMAWarnings, authclientbackfill.PhaseReconcile, authclientbackfill.PhaseSelections, authclientbackfill.PhaseGrants} {
		if _, failed := report.PhaseErrors[phase]; failed {
			phases = append(phases, phase)
		}
	}
	return phases
}

// idStrings renders ids, never nil.
func idStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
