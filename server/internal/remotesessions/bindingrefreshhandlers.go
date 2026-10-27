package remotesessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func (s *Service) SetBindingRefreshAttempts(attempts *BindingRefreshAttempts) {
	s.bindingRefresh = attempts
}

func (s *Service) beginRefreshOperation(ctx context.Context, principal, issuer string) (*bindingOperation, error) {
	if !contextvalues.HasValidatedGramSession(ctx) || s.bindingRefresh == nil {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, consent := contextvalues.GetConsentBindingAuthorization(ctx); consent {
		return nil, oops.C(oops.CodeForbidden)
	}
	return s.beginBindingOperation(ctx, principal, issuer, true)
}

func (s *Service) BeginBindingRefresh(ctx context.Context, payload *gen.BeginBindingRefreshPayload) (*gen.BindingRefreshAttempt, error) {
	op, err := s.beginRefreshOperation(ctx, payload.PrincipalID, payload.UserSessionIssuerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.tx.Rollback(ctx) }()
	bindingID, err := uuid.Parse(payload.BindingID)
	if err != nil {
		return nil, oops.C(oops.CodeBadRequest)
	}
	binding, err := op.queries.LockPrincipalBindingForRefresh(ctx, repo.LockPrincipalBindingForRefreshParams{ID: bindingID, PrincipalID: op.principalID, ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load attachment for refresh")
	}
	attempt := bindingRefreshAttempt{ID: uuid.NewString(), OrganizationID: op.organizationID, ProjectID: op.projectID, PrincipalID: op.principalID, IssuerID: op.issuerID, BindingID: binding.ID, ClientID: binding.RemoteSessionClientID, Subject: op.subject, CreatedAt: time.Now(), Started: false, RemoteSessionID: uuid.Nil, GrantGeneration: 0}
	if err := s.bindingRefresh.attempts.Store(ctx, attempt); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create connection refresh attempt")
	}
	return &gen.BindingRefreshAttempt{AttemptID: attempt.ID}, nil
}

func (s *Service) CompleteBindingRefresh(ctx context.Context, payload *gen.CompleteBindingRefreshPayload) (*gen.BindingRefreshResult, error) {
	op, err := s.beginRefreshOperation(ctx, payload.PrincipalID, payload.UserSessionIssuerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.tx.Rollback(ctx) }()
	key := "bindingRefresh:" + payload.AttemptID
	attempt, err := s.bindingRefresh.attempts.Get(ctx, key)
	if err != nil || attempt.TTL() <= 0 || attempt.OrganizationID != op.organizationID || attempt.ProjectID != op.projectID || attempt.PrincipalID != op.principalID || attempt.IssuerID != op.issuerID || attempt.Subject != op.subject {
		return nil, oops.E(oops.CodeNotFound, nil, "connection refresh attempt is unavailable")
	}
	// Authenticate before consumption; an unauthorized caller cannot cancel another person's popup.
	attempt, err = s.bindingRefresh.attempts.GetAndDelete(ctx, key)
	if err != nil || attempt.TTL() <= 0 {
		return nil, oops.E(oops.CodeConflict, nil, "connection refresh attempt was already completed or expired")
	}
	if !attempt.Started || attempt.GrantGeneration == 0 {
		return &gen.BindingRefreshResult{Completed: false, Binding: nil}, nil
	}
	binding, err := op.queries.LockPrincipalBindingForRefresh(ctx, repo.LockPrincipalBindingForRefreshParams{ID: attempt.BindingID, PrincipalID: op.principalID, ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "attachment changed during sign-in; review the current connection")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock attachment for refresh")
	}
	if binding.RemoteSessionClientID != attempt.ClientID {
		return nil, oops.C(oops.CodeConflict)
	}
	source, err := op.queries.LockRemoteSessionGrantForBindingRefresh(ctx, repo.LockRemoteSessionGrantForBindingRefreshParams{ID: attempt.RemoteSessionID, RemoteSessionClientID: attempt.ClientID, SubjectUrn: op.subject, GrantGeneration: attempt.GrantGeneration})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "connection changed after this sign-in; reconnect to choose the account again")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock exact refreshed grant")
	}
	if _, err := op.queries.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{ID: binding.ID, PrincipalID: op.principalID, ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "replace refreshed attachment")
	}
	attached, err := op.queries.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{ProjectID: op.projectID, OrganizationID: op.organizationID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String(), RemoteSessionID: source.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "refreshed connection is no longer eligible")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach refreshed connection")
	}
	if attached.GrantGeneration != attempt.GrantGeneration {
		return nil, oops.C(oops.CodeConflict)
	}
	if err := s.logBinding(ctx, op, audit.ActionRemoteSessionDetach, binding.ID, binding.RemoteSessionID, binding.RemoteSessionClientID, binding.GrantGeneration); err != nil {
		return nil, err
	}
	if err := s.logBinding(ctx, op, audit.ActionRemoteSessionAttach, attached.ID, attached.RemoteSessionID, attached.RemoteSessionClientID, attached.GrantGeneration); err != nil {
		return nil, err
	}
	if err := op.tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit refreshed attachment")
	}
	view := bindingView(attached.ID, attached.PrincipalID, attached.UserSessionIssuerID, attached.RemoteSessionClientID, attached.RemoteSessionID)
	view.RemoteSession = mv.BuildRemoteSessionView(source, nil, nil)
	return &gen.BindingRefreshResult{Completed: true, Binding: view}, nil
}
