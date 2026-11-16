package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
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

// authClientRunTimeout bounds a migration run, which outlives the request so
// a closed tab does not cut a phase short.
const authClientRunTimeout = 15 * time.Minute

// authClientStatusTimeout bounds the status read that follows a run.
const authClientStatusTimeout = 2 * time.Minute

func (s *Service) RunUpstreamOAuthMigration(ctx context.Context, p *gen.RunUpstreamOAuthMigrationPayload) (*gen.AdminUpstreamOAuthMigrationRun, error) {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationID(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))

	// Every phase outlives the request, so a closed tab does not cut one
	// short; each is idempotent and safe to run again.
	detached := context.WithoutCancel(ctx)
	migrationCtx, cancelMigration := context.WithTimeout(detached, authClientRunTimeout)
	defer cancelMigration()

	// Associations first: a credential's choice is only recorded for a client
	// associated with the server.
	associations, err := associationbackfill.Run(migrationCtx, s.db, associationbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate upstream oauth client associations").LogError(ctx, logger)
	}
	// Under enforcement a choice is recorded from the auth clients a server
	// offers, so those are recorded first, and choices only after a run that
	// recorded every auth client phase: a choice recorded while a server's
	// OAuth-backed client is missing reads as no-auth, and the auth client
	// backfill leaves it stale once that client exists. Otherwise choices are
	// recorded from the associations just written, before the auth clients
	// that map them.
	enforced, err := s.productFeatures.IsFeatureEnabledUncached(migrationCtx, organizationID, productfeatures.FeatureUpstreamAuthEnforced)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read upstream auth enforcement").LogError(ctx, logger)
	}
	var authClients authclientbackfill.Report
	var authClientsError *string
	if enforced {
		authClients, authClientsError = s.recordAuthClients(ctx, migrationCtx, organizationID, true)
	}
	selections, err := selectionbackfill.Run(migrationCtx, s.db, selectionbackfill.Options{OrganizationID: organizationID, Apply: !enforced || authClientsError == nil, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "migrate upstream oauth client selections").LogError(ctx, logger)
	}
	if !enforced {
		authClients, authClientsError = s.recordAuthClients(ctx, migrationCtx, organizationID, false)
	}
	// Profiles decide from the typed auth clients, so they are recorded only
	// after a run that recorded every auth client phase.
	var profileVersionsWritten *int64
	if authClientsError == nil {
		profiles, err := executionprofiles.Backfill(migrationCtx, s.db, organizationID, true)
		switch {
		case err != nil:
			logger.ErrorContext(ctx, "record execution selection profiles", attr.SlogError(err))
		default:
			profileVersionsWritten = new(int64(profiles.VersionsWritten))
		}
	}
	// The writes above are committed even if the caller has gone, so the
	// audit line and the status read do not depend on the request.
	logger.InfoContext(detached, "upstream oauth migration ran",
		attr.SlogAuditAction("upstream_oauth_migration:run"),
		attr.SlogAuditSubject("organization"),
		attr.SlogAuditSubjectID(organizationID),
		attr.SlogAdminOIDCSubject(auth.OIDCSubject),
	)
	statusCtx, cancelStatus := context.WithTimeout(detached, authClientStatusTimeout)
	defer cancelStatus()
	status, err := s.upstreamOAuthMigrationStatus(statusCtx, organizationID)
	if err != nil {
		return nil, err
	}
	return &gen.AdminUpstreamOAuthMigrationRun{
		AssociationsWritten:                int64(associations.Written),
		AssociationsSkipped:                int64(associations.Skipped),
		SelectionsWritten:                  selections.Written,
		AuthClientsWritten:                 new(int64(authClients.ClientsWritten)),
		AuthClientLinksWritten:             new(int64(authClients.LinksWritten)),
		ServersMarkedRequiringUpstreamAuth: nil,
		SelectionsMapped:                   new(int64(authClients.SelectionsMapped)),
		ExecutionProfileVersionsWritten:    profileVersionsWritten,
		AuthPermissionGrantsWritten:        new(int64(authClients.GrantsWritten)),
		AuthClientsError:                   authClientsError,
		Status:                             status,
	}, nil
}

// recordAuthClients runs the typed auth client backfill for one migration
// run. A failure is returned as the message reported beside the run, never as
// an error, so it does not fail the migration.
func (s *Service) recordAuthClients(ctx, migrationCtx context.Context, organizationID string, enforced bool) (authclientbackfill.Report, *string) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	var authClientsError *string
	// Under enforcement the run records no choices after a failure here.
	rerun := "Run again; the upstream OAuth migration is unaffected."
	if enforced {
		rerun = "Run again; credential choices are recorded once every auth client phase succeeds."
	}
	authClients, err := authclientbackfill.Run(migrationCtx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: true, PageSize: 0, TypedOnly: enforced})
	switch {
	case errors.Is(err, authclientbackfill.ErrRunInProgress):
		logger.WarnContext(ctx, "record typed auth clients", attr.SlogError(err))
		message := "A typed auth client run is already in progress for this organization; try again when it finishes."
		authClientsError = &message
	case err != nil:
		logger.ErrorContext(ctx, "record typed auth clients", attr.SlogError(err))
		message := "Recording typed auth clients failed. " + rerun
		authClientsError = &message
	case len(authClients.PhaseErrors) > 0:
		phases := failedPhases(authClients)
		for _, phase := range phases {
			logger.ErrorContext(ctx, "record typed auth clients", attr.SlogError(errors.New(authClients.PhaseErrors[phase])), attr.SlogAuthClientBackfillPhase(phase))
		}
		message := fmt.Sprintf("Recording typed auth clients failed in %s; the other phases were recorded. %s", strings.Join(phases, ", "), rerun)
		authClientsError = &message
	}
	if err == nil {
		for _, kind := range authclientbackfill.Kinds {
			counts := authClients.ByKind[kind]
			logger.InfoContext(ctx, "typed auth clients recorded",
				attr.SlogAuthClientBackfillKind(string(kind)),
				attr.SlogAuthClientBackfillDecided(counts.Decided),
				attr.SlogAuthClientBackfillWritten(counts.Written),
				attr.SlogAuthClientBackfillRetired(counts.Retired),
				attr.SlogAuthClientBackfillUnresolved(counts.Unresolved),
			)
		}
		if _, failed := authClients.PhaseErrors[authclientbackfill.PhaseGrants]; !failed {
			logger.InfoContext(context.WithoutCancel(ctx), "default auth permissions granted",
				attr.SlogAuthClientBackfillWritten(authClients.GrantsWritten),
				attr.SlogAuthClientBackfillAPIKeys(authClients.APIKeysGranted),
			)
		}
	}
	return authClients, authClientsError
}

// upstreamOAuthMigrationStatus dry-runs both backfills for one organization.
func (s *Service) upstreamOAuthMigrationStatus(ctx context.Context, organizationID string) (*gen.AdminUpstreamOAuthMigrationStatus, error) {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	associations, err := associationbackfill.Run(ctx, s.db, associationbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check upstream oauth client associations").LogError(ctx, logger)
	}
	selections, err := selectionbackfill.Run(ctx, s.db, selectionbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check upstream oauth client selections").LogError(ctx, logger)
	}

	unresolved := make([]*gen.AdminUpstreamOAuthMigrationServer, 0, associations.Gaps)
	listed := map[uuid.UUID]bool{}
	addUnresolved := func(serverID, projectID uuid.UUID) {
		if !listed[serverID] {
			listed[serverID] = true
			unresolved = append(unresolved, &gen.AdminUpstreamOAuthMigrationServer{McpServerID: serverID.String(), ProjectID: projectID.String()})
		}
	}
	// An enforced organization's servers offer only what was configured for
	// them, so what the backfill would infer from an issuer is neither pending
	// nor unresolved there.
	for _, result := range associations.Results {
		if !result.Outcome.Covered() && !result.Enforced {
			addUnresolved(result.MCPServerID, result.ProjectID)
		}
	}
	for _, result := range associations.GatewayResults {
		if !result.Outcome.Covered() && !result.Enforced {
			addUnresolved(result.MCPServerID, result.ProjectID)
		}
	}
	pendingAssociations := int64(associations.Decided - associations.Enforced)
	reconnect := map[uuid.UUID]bool{}
	for _, gap := range selections.Gaps {
		if gap.Outcome == selectionbackfill.OutcomeAmbiguous || gap.Outcome == selectionbackfill.OutcomeUnreachableClient || gap.Outcome == selectionbackfill.OutcomeUnusableClient {
			reconnect[gap.CredentialID] = true
		}
	}
	// A credential issued for the servers approved at consent has choices for
	// those servers only and is refused anywhere else, so the rest of its
	// issuer's reach is not pending.
	consentScopedSelections := int64(selections.ConsentScoped)
	pendingSelections := int64(selections.Pairs[selectionbackfill.OutcomeClient]+selections.Pairs[selectionbackfill.OutcomeNoAuth]) - consentScopedSelections

	// Both sections read enforcement, so they are decided from one read. A
	// failed read leaves both out, as a failed check of either does.
	var authClients *gen.AdminUpstreamOAuthMigrationAuthClients
	var executionProfiles *gen.AdminUpstreamOAuthMigrationExecutionProfiles
	enforced, err := s.productFeatures.IsFeatureEnabledUncached(ctx, organizationID, productfeatures.FeatureUpstreamAuthEnforced)
	if err != nil {
		logger.ErrorContext(ctx, "read upstream auth enforcement", attr.SlogError(err))
	} else {
		authClients = s.authClientMigrationStatus(ctx, organizationID, enforced)
		executionProfiles = s.executionProfileMigrationStatus(ctx, organizationID, enforced)
	}

	status := &gen.AdminUpstreamOAuthMigrationStatus{
		AuthClients:             authClients,
		ExecutionProfiles:       executionProfiles,
		Enforcement:             nil,
		OrganizationID:          organizationID,
		Migrated:                pendingAssociations == 0 && len(unresolved) == 0 && pendingSelections == 0,
		Servers:                 int64(associations.Servers),
		PendingAssociations:     pendingAssociations,
		UnresolvedServers:       unresolved,
		Credentials:             int64(selections.Credentials),
		PendingSelections:       pendingSelections,
		ConsentScopedSelections: new(consentScopedSelections),
		ReconnectCredentials:    int64(len(reconnect)),
	}
	status.Enforcement = s.upstreamAuthEnforcement(ctx, status)
	return status, nil
}

// authClientMigrationStatus dry-runs the typed auth client backfill. It is
// reported apart from migrated and never gates the cutover, so a failure only
// leaves it out, and a failed phase is named beside the others' counts.
func (s *Service) authClientMigrationStatus(ctx context.Context, organizationID string, enforced bool) *gen.AdminUpstreamOAuthMigrationAuthClients {
	logger := s.logger.With(attr.SlogOrganizationID(organizationID))
	report, err := authclientbackfill.Run(ctx, s.db, authclientbackfill.Options{OrganizationID: organizationID, Apply: false, PageSize: 0, TypedOnly: enforced})
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
		}
	case err != nil:
		logger.ErrorContext(ctx, "check typed auth clients", attr.SlogError(err))
		return nil
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
	}
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
