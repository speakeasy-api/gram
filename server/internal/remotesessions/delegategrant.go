package remotesessions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// DelegateGrant attaches only the exact callback grant. An existing assignment
// can be renewed only by its delegator, with agent-write permission. Both
// the displayed binding and the exact newly authorized source are rechecked.
func (s *Service) DelegateGrant(ctx context.Context, principal uuid.UUID, grant RemoteGrant, expectedBinding uuid.UUID) error {
	op, err := s.beginBindingOperation(ctx, principal.String(), grant.UserSessionIssuerID.String(), true)
	if err != nil {
		return err
	}
	defer o11y.NoLogDefer(func() error { return op.tx.Rollback(ctx) })
	if grant.Subject != op.subject || grant.GrantGeneration <= 0 || expectedBinding == uuid.Nil {
		return oops.C(oops.CodeForbidden)
	}
	var old *repo.PrincipalRemoteSessionBinding
	if expectedBinding != uuid.Nil {
		binding, err := op.queries.LockPrincipalBindingForRefresh(ctx, repo.LockPrincipalBindingForRefreshParams{ID: expectedBinding, PrincipalID: op.principalID, ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String()})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeConflict, err, "assignment changed; review the agent's connection")
		}
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock existing delegation")
		}
		if binding.RemoteSessionClientID != grant.RemoteSessionClientID {
			return oops.C(oops.CodeConflict)
		}
		old = &binding
	}
	source, err := op.queries.LockRemoteSessionGrantForBindingRefresh(ctx, repo.LockRemoteSessionGrantForBindingRefreshParams{ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, ID: grant.RemoteSessionID, RemoteSessionClientID: grant.RemoteSessionClientID, SubjectUrn: op.subject, GrantGeneration: grant.GrantGeneration})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeConflict, err, "connection changed after sign-in; reconnect to choose the account again")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock newly authorized connection")
	}
	if old != nil {
		if old.RemoteSessionID == grant.RemoteSessionID && old.GrantGeneration == grant.GrantGeneration {
			return nil
		}
		if _, err := op.queries.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ID: old.ID, PrincipalID: op.principalID, ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "replace previous delegation")
		}
		if err := s.logBinding(ctx, op, audit.ActionRemoteSessionDetach, old.ID, old.RemoteSessionID, old.RemoteSessionClientID, old.GrantGeneration); err != nil {
			return err
		}
	}
	attached, err := op.queries.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: op.projectID, OrganizationID: op.organizationID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String(), RemoteSessionID: source.RemoteSession.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeConflict, err, "an assignment already exists; review the agent's connection")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delegate authorized connection")
	}
	if attached.GrantGeneration != grant.GrantGeneration {
		return oops.C(oops.CodeConflict)
	}
	if err := s.logBinding(ctx, op, audit.ActionRemoteSessionAttach, attached.ID, attached.RemoteSessionID, attached.RemoteSessionClientID, attached.GrantGeneration); err != nil {
		return err
	}
	if err := op.tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit connection delegation")
	}
	return nil
}
