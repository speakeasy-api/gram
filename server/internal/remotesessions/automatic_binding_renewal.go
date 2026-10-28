package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetAutomaticBindingRenewal authorizes renewal through the live OAuth parent.
// Configure before serving callbacks; absent wiring leaves assignments unchanged.
func (m *ChallengeManager) SetAutomaticBindingRenewal(service *Service, authorize func(context.Context, RemoteLoginState) (bool, error)) {
	m.bindingRenewalService = service
	m.authorizeBindingRenewal = authorize
}

type automaticBindingRenewal struct {
	source   repo.RemoteSession
	bindings []repo.PrincipalRemoteSessionBinding
}

// Lock agents and bindings before issuer/client/source locks, matching attachment
// writes (including foreign-key locks). Recheck the source after client serialization.
func (s *Service) prepareAutomaticBindingRenewal(ctx context.Context, tx pgx.Tx, state RemoteLoginState) (*automaticBindingRenewal, error) {
	if s.bindingAuthorizer == nil || state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser {
		return nil, nil
	}
	q := repo.New(tx)
	candidates, err := q.ListExpiredOwnBindingRenewalCandidates(ctx, repo.ListExpiredOwnBindingRenewalCandidatesParams{OrganizationID: state.OrganizationID, UserSessionIssuerID: state.UserSessionIssuerID, RemoteSessionClientID: state.RemoteSessionClientID, SubjectUrn: *state.Subject})
	if err != nil {
		return nil, fmt.Errorf("list expired agent connections: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	organization, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, state.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("load renewal organization: %w", err)
	}
	allowed := make([]repo.PrincipalRemoteSessionBinding, 0, len(candidates))
	for _, binding := range candidates {
		authCtx := contextvalues.WithConsentBindingAuthorization(ctx, state.Subject.ID, binding.OrganizationID, binding.ProjectID, binding.PrincipalID, binding.UserSessionIssuerID)
		human, _ := contextvalues.GetAuthContext(authCtx)
		human.OrganizationSlug = organization.Slug
		if err := s.bindingAuthorizer(authCtx, tx, binding.PrincipalID, true); err != nil {
			var denied *oops.ShareableError
			if errors.As(err, &denied) && (denied.Code == oops.CodeForbidden || denied.Code == oops.CodeNotFound || denied.Code == oops.CodeUnauthorized) {
				continue
			}
			return nil, fmt.Errorf("authorize agent connection renewal: %w", err)
		}
		agent, err := agentrepo.New(tx).GetAgentByID(ctx, agentrepo.GetAgentByIDParams{OrganizationID: binding.OrganizationID, ID: binding.PrincipalID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("recheck renewal agent: %w", err)
		}
		if !agent.ProjectID.Valid || agent.ProjectID.UUID != binding.ProjectID || agent.SuspendedAt.Valid || agent.RevokedAt.Valid || agent.OwnerReassignmentRequiredAt.Valid {
			continue
		}
		allowed = append(allowed, binding)
	}
	locked := make([]repo.PrincipalRemoteSessionBinding, 0, len(allowed))
	for _, binding := range allowed {
		current, err := q.LockPrincipalBindingForRefresh(ctx, repo.LockPrincipalBindingForRefreshParams{ID: binding.ID, PrincipalID: binding.PrincipalID, ProjectID: binding.ProjectID, OrganizationID: binding.OrganizationID, UserSessionIssuerID: binding.UserSessionIssuerID, SubjectUrn: state.Subject.String()})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock expired agent connection: %w", err)
		}
		if current.RemoteSessionID != binding.RemoteSessionID || current.GrantGeneration != binding.GrantGeneration {
			continue
		}
		locked = append(locked, current)
	}
	if len(locked) == 0 {
		return nil, nil
	}
	var source repo.RemoteSession
	return &automaticBindingRenewal{source: source, bindings: locked}, nil
}

func recheckAutomaticBindingRenewal(ctx context.Context, tx pgx.Tx, state RemoteLoginState, renewal *automaticBindingRenewal) (*automaticBindingRenewal, error) {
	if renewal == nil {
		return nil, nil
	}
	binding := renewal.bindings[0]
	source, err := repo.New(tx).LockRemoteSessionForAutomaticRenewal(ctx, repo.LockRemoteSessionForAutomaticRenewalParams{ID: binding.RemoteSessionID, SubjectUrn: *state.Subject, RemoteSessionClientID: state.RemoteSessionClientID, UserSessionIssuerID: state.UserSessionIssuerID, PreviousGeneration: binding.GrantGeneration})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("recheck expired connection: %w", err)
	}
	renewal.source = source
	return renewal, nil
}

func (s *Service) completeAutomaticBindingRenewal(ctx context.Context, tx pgx.Tx, renewal *automaticBindingRenewal, source repo.RemoteSession) error {
	if renewal == nil {
		return nil
	}
	if source.ID != renewal.source.ID || source.GrantGeneration != renewal.source.GrantGeneration+1 {
		return fmt.Errorf("reauthentication did not replace the locked grant")
	}
	q := repo.New(tx)
	for _, binding := range renewal.bindings {
		updated, err := q.UpdateExpiredBindingGrant(ctx, repo.UpdateExpiredBindingGrantParams{NewGeneration: source.GrantGeneration, ID: binding.ID, ProjectID: binding.ProjectID, OrganizationID: binding.OrganizationID, UserSessionIssuerID: binding.UserSessionIssuerID, SubjectUrn: source.SubjectUrn.String(), RemoteSessionID: source.ID, PreviousGeneration: renewal.source.GrantGeneration})
		if err != nil {
			return fmt.Errorf("renew expired agent connection: %w", err)
		}
		authCtx := contextvalues.WithConsentBindingAuthorization(ctx, source.SubjectUrn.ID, binding.OrganizationID, binding.ProjectID, binding.PrincipalID, binding.UserSessionIssuerID)
		op := &bindingOperation{tx: tx, queries: q, projectID: binding.ProjectID, organizationID: binding.OrganizationID, principalID: binding.PrincipalID, issuerID: binding.UserSessionIssuerID, subject: source.SubjectUrn}
		if err := s.logBinding(authCtx, op, audit.ActionRemoteSessionAttach, updated.ID, source.ID, source.RemoteSessionClientID, source.GrantGeneration); err != nil {
			return err
		}
	}
	return nil
}
