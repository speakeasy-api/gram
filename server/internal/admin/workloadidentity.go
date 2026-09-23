package admin

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// GetWorkloadIdentity reads one organization's whole workload trust policy.
func (s *Service) GetWorkloadIdentity(ctx context.Context, payload *gen.GetWorkloadIdentityPayload) (*gen.AdminWorkloadIdentityState, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.workloadIdentityState(ctx, organizationID)
}

// CreateWorkloadIssuer trusts one assertion issuer for an organization.
func (s *Service) CreateWorkloadIssuer(ctx context.Context, payload *gen.CreateWorkloadIssuerPayload) (*gen.AdminWorkloadIdentityState, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	projectID, err := conv.PtrToNullUUID(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "project_id must be a uuid")
	}

	issuerID, err := repo.New(s.db).AdminCreateWorkloadIssuer(ctx, repo.AdminCreateWorkloadIssuerParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           payload.Name,
		Issuer:         payload.Issuer,
		JwksUri:        payload.JwksURI,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create workload issuer").LogError(ctx, s.logger)
	}

	_, _, operatorEmail := adminActor(ctx)
	s.logger.InfoContext(ctx, "workload issuer trusted",
		attr.SlogOrganizationID(organizationID),
		attr.SlogWorkloadIssuerID(issuerID.String()),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	return s.workloadIdentityState(ctx, organizationID)
}

// AdmitWorkloadSubject admits one exact subject and assigns the agent whose
// policy it inherits. Both rows are written together because a subject admitted
// without an assigned agent is refused at the token endpoint, and splitting them
// across two calls leaves that half-configured state reachable.
func (s *Service) AdmitWorkloadSubject(ctx context.Context, payload *gen.AdmitWorkloadSubjectPayload) (*gen.AdminWorkloadIdentityState, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	issuerID, err := uuid.Parse(payload.WorkloadIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "workload_issuer_id must be a uuid")
	}
	agentID, err := uuid.Parse(payload.AgentID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "agent_id must be a uuid")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin workload subject admission").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)

	// Both writes select the issuer row, so an issuer in another organization
	// or a withdrawn one inserts nothing rather than admitting across tenants.
	if _, err := queries.AdminAdmitWorkloadSubject(ctx, repo.AdminAdmitWorkloadSubjectParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
		Subject:          payload.Subject,
		Name:             conv.PtrToPGText(payload.Name),
	}); err != nil {
		return nil, s.workloadIssuerWriteError(ctx, err, "admit workload subject")
	}
	if _, err := queries.AdminAssignWorkloadAgent(ctx, repo.AdminAssignWorkloadAgentParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
		Subject:          payload.Subject,
		AgentID:          agentID,
	}); err != nil {
		return nil, s.workloadIssuerWriteError(ctx, err, "assign workload agent")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit workload subject admission").LogError(ctx, s.logger)
	}

	_, _, operatorEmail := adminActor(ctx)
	s.logger.InfoContext(ctx, "workload subject admitted",
		attr.SlogOrganizationID(organizationID),
		attr.SlogWorkloadIssuerID(issuerID.String()),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	return s.workloadIdentityState(ctx, organizationID)
}

// SetWorkloadAuthenticationHost moves one issuer's announced OAuth issuer and
// endpoint origin to the deployment's authentication host.
func (s *Service) SetWorkloadAuthenticationHost(ctx context.Context, payload *gen.SetWorkloadAuthenticationHostPayload) (*gen.AdminWorkloadIdentityState, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	sessionIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "user_session_issuer_id must be a uuid")
	}

	rows, err := repo.New(s.db).AdminSetWorkloadAuthenticationHost(ctx, repo.AdminSetWorkloadAuthenticationHostParams{
		OrganizationID:      organizationID,
		UserSessionIssuerID: sessionIssuerID,
		Enabled:             payload.Enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set workload authentication host").LogError(ctx, s.logger)
	}
	if rows == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "user session issuer not found in this organization")
	}

	_, _, operatorEmail := adminActor(ctx)
	s.logger.InfoContext(ctx, "workload authentication host set",
		attr.SlogOrganizationID(organizationID),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	return s.workloadIdentityState(ctx, organizationID)
}

// TeardownWorkloadIssuer withdraws an issuer together with everything admitted
// under it, in one transaction, so teardown cannot leave a subject admitted
// against an issuer that no longer resolves.
func (s *Service) TeardownWorkloadIssuer(ctx context.Context, payload *gen.TeardownWorkloadIssuerPayload) (*gen.AdminWorkloadIdentityState, error) {
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	issuerID, err := uuid.Parse(payload.WorkloadIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "workload_issuer_id must be a uuid")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin workload issuer teardown").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)

	// Children first: a reader that sees the issuer gone must not still find a
	// live admission under it.
	if err := queries.AdminWithdrawWorkloadIssuerAssignments(ctx, repo.AdminWithdrawWorkloadIssuerAssignmentsParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "withdraw workload agent assignments").LogError(ctx, s.logger)
	}
	if err := queries.AdminWithdrawWorkloadIssuerAdmissions(ctx, repo.AdminWithdrawWorkloadIssuerAdmissionsParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "withdraw workload admissions").LogError(ctx, s.logger)
	}
	rows, err := queries.AdminWithdrawWorkloadIssuer(ctx, repo.AdminWithdrawWorkloadIssuerParams{
		OrganizationID:   organizationID,
		WorkloadIssuerID: issuerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "withdraw workload issuer").LogError(ctx, s.logger)
	}
	if rows == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "workload issuer not found in this organization")
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit workload issuer teardown").LogError(ctx, s.logger)
	}

	_, _, operatorEmail := adminActor(ctx)
	s.logger.InfoContext(ctx, "workload issuer withdrawn",
		attr.SlogOrganizationID(organizationID),
		attr.SlogWorkloadIssuerID(issuerID.String()),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	return s.workloadIdentityState(ctx, organizationID)
}

// workloadIssuerWriteError maps an insert that matched no issuer row. The
// queries select the issuer, so a missing one yields no rows rather than a
// constraint violation, and reporting it as not-found keeps a wrong issuer id
// from reading as a server fault.
func (s *Service) workloadIssuerWriteError(ctx context.Context, err error, action string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "workload issuer not found in this organization")
	}
	return oops.E(oops.CodeUnexpected, err, "%s", action).LogError(ctx, s.logger)
}

func (s *Service) workloadIdentityState(ctx context.Context, organizationID string) (*gen.AdminWorkloadIdentityState, error) {
	queries := repo.New(s.db)

	issuerRows, err := queries.AdminListWorkloadIssuers(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list workload issuers").LogError(ctx, s.logger)
	}
	subjectRows, err := queries.AdminListWorkloadSubjects(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list workload subjects").LogError(ctx, s.logger)
	}
	hostRows, err := queries.AdminListWorkloadAuthenticationHosts(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list workload authentication hosts").LogError(ctx, s.logger)
	}

	issuers := make([]*gen.AdminWorkloadIssuer, 0, len(issuerRows))
	for _, row := range issuerRows {
		issuers = append(issuers, &gen.AdminWorkloadIssuer{
			ID:        row.ID.String(),
			Name:      row.Name,
			Issuer:    row.Issuer,
			JwksURI:   row.JwksUri,
			ProjectID: nullUUIDString(row.ProjectID),
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	subjects := make([]*gen.AdminWorkloadSubject, 0, len(subjectRows))
	for _, row := range subjectRows {
		subjects = append(subjects, &gen.AdminWorkloadSubject{
			WorkloadIssuerID: row.WorkloadIssuerID.String(),
			Subject:          row.Subject,
			Name:             conv.FromPGText[string](row.Name),
			AgentID:          nullUUIDString(row.AgentID),
			AgentName:        conv.FromPGText[string](row.AgentName),
		})
	}

	hosts := make([]*gen.AdminWorkloadAuthenticationHost, 0, len(hostRows))
	for _, row := range hostRows {
		hosts = append(hosts, &gen.AdminWorkloadAuthenticationHost{
			UserSessionIssuerID:   row.ID.String(),
			ProjectID:             nullUUIDString(row.ProjectID),
			UseAuthenticationHost: row.UseAuthenticationHost,
		})
	}

	return &gen.AdminWorkloadIdentityState{
		OrganizationID:      organizationID,
		Issuers:             issuers,
		Subjects:            subjects,
		AuthenticationHosts: hosts,
	}, nil
}

func nullUUIDString(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}
	return conv.PtrEmpty(value.UUID.String())
}
