package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// SetBindingAuthorizer wires the human-only agent management authorizer without
// creating an import cycle with agentmanagement's session revocation support.
// Configure before serving requests. The callback must use the supplied transaction
// to RequireAgentOwnerForUpdate with OwnedAgentAuthorize, retaining its membership
// and agent locks through commit. An unconfigured service fails closed.
func (s *Service) SetBindingAuthorizer(authorize func(context.Context, pgx.Tx, uuid.UUID) error) {
	s.bindingAuthorizer = authorize
}

type bindingOperation struct {
	tx             pgx.Tx
	queries        *repo.Queries
	projectID      uuid.UUID
	organizationID string
	principalID    uuid.UUID
	issuerID       uuid.UUID
	subject        urn.SessionSubject
}

func bindingHuman(ctx context.Context) (*contextvalues.AuthContext, error) {
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ProjectID == nil || !contextvalues.HasValidatedGramSession(ctx) || auth.SessionID == nil || *auth.SessionID == "" || auth.UserID == "" || auth.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if auth.APIKeyID != "" || auth.APIKeyName != "" || len(auth.APIKeyScopes) > 0 || auth.OrgWidePluginHooksKey || contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetRBACScopeOverride(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	return auth, nil
}

func (s *Service) beginBindingOperation(ctx context.Context, principal, issuer string) (*bindingOperation, error) {
	auth, err := bindingHuman(ctx)
	if err != nil {
		return nil, err
	}
	if s.bindingAuthorizer == nil {
		return nil, oops.C(oops.CodeForbidden)
	}
	principalID, err := uuid.Parse(principal)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid principal_id")
	}
	issuerID, err := uuid.Parse(issuer)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin remote session attachment operation")
	}
	if err := s.bindingAuthorizer(ctx, tx, principalID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return &bindingOperation{tx: tx, queries: repo.New(tx), projectID: *auth.ProjectID, organizationID: auth.ActiveOrganizationID, principalID: principalID, issuerID: issuerID, subject: urn.NewUserSubject(auth.UserID)}, nil
}

func (op *bindingOperation) candidates(ctx context.Context) ([]repo.ListPrincipalRemoteSessionCandidatesRow, error) {
	rows, err := op.queries.ListPrincipalRemoteSessionCandidates(ctx, repo.ListPrincipalRemoteSessionCandidatesParams{
		ProjectID: op.projectID, OrganizationID: op.organizationID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject,
		Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ClientFilter: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: pgtype.Int4{Int32: 0, Valid: false},
	})
	if err != nil {
		return nil, fmt.Errorf("list attachment candidates: %w", err)
	}
	return rows, nil
}

func (s *Service) ListBindings(ctx context.Context, payload *gen.ListBindingsPayload) (*gen.ListBindingsResult, error) {
	op, err := s.beginBindingOperation(ctx, payload.PrincipalID, payload.UserSessionIssuerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.tx.Rollback(ctx) }()
	rows, err := op.queries.ListPrincipalRemoteSessionBindings(ctx, repo.ListPrincipalRemoteSessionBindingsParams{
		ProjectID: op.projectID, OrganizationID: op.organizationID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session attachments")
	}
	// Eligibility is shared with attachment candidates. Unavailable bindings
	// retain only their identifiers so the owner can detach without exposing
	// identity from a revoked, inaccessible, or replaced upstream grant.
	candidates, err := op.candidates(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve available attachment views")
	}
	available := make(map[uuid.UUID]repo.ListPrincipalRemoteSessionCandidatesRow, len(candidates))
	for _, candidate := range candidates {
		available[candidate.RemoteSession.ID] = candidate
	}
	items := make([]*gen.PrincipalRemoteSessionBinding, 0, len(rows))
	for _, row := range rows {
		view := bindingView(row.ID, row.PrincipalID, row.UserSessionIssuerID, row.RemoteSessionClientID, row.RemoteSessionID)
		if candidate, ok := available[row.RemoteSessionID]; ok && candidate.RemoteSession.GrantGeneration == row.GrantGeneration {
			view.RemoteSession = mv.BuildRemoteSessionView(candidate.RemoteSession, conv.FromPGText[string](candidate.SubjectDisplayName), conv.FromPGText[string](candidate.SubjectEmail))
		}
		items = append(items, view)
	}
	return &gen.ListBindingsResult{Items: items}, nil
}

func (s *Service) AttachBinding(ctx context.Context, payload *gen.AttachBindingPayload) (*gen.PrincipalRemoteSessionBinding, error) {
	op, err := s.beginBindingOperation(ctx, payload.PrincipalID, payload.UserSessionIssuerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = op.tx.Rollback(ctx) }()
	sessionID, err := uuid.Parse(payload.RemoteSessionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_id")
	}
	candidates, err := op.candidates(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate attachment candidate")
	}
	var generation int64
	var sessionView *types.RemoteSession
	for _, candidate := range candidates {
		if candidate.RemoteSession.ID == sessionID {
			generation = candidate.RemoteSession.GrantGeneration
			sessionView = mv.BuildRemoteSessionView(candidate.RemoteSession, conv.FromPGText[string](candidate.SubjectDisplayName), conv.FromPGText[string](candidate.SubjectEmail))
			break
		}
	}
	if sessionView == nil {
		return nil, oops.C(oops.CodeNotFound)
	}
	row, err := op.queries.AttachPrincipalRemoteSessionBinding(ctx, repo.AttachPrincipalRemoteSessionBindingParams{
		ProjectID: op.projectID, OrganizationID: op.organizationID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String(), RemoteSessionID: sessionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeConflict, err, "attachment unavailable or client already attached; detach before replacing")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "attach remote session")
	}
	if row.GrantGeneration != generation {
		return nil, oops.E(oops.CodeConflict, nil, "session changed while attaching; retry")
	}
	if err := s.logBinding(ctx, op, audit.ActionRemoteSessionAttach, row.ID, row.RemoteSessionID, row.RemoteSessionClientID, row.GrantGeneration); err != nil {
		return nil, err
	}
	if err := op.tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit remote session attachment")
	}
	view := bindingView(row.ID, row.PrincipalID, row.UserSessionIssuerID, row.RemoteSessionClientID, row.RemoteSessionID)
	view.RemoteSession = sessionView
	return view, nil
}

func (s *Service) DetachBinding(ctx context.Context, payload *gen.DetachBindingPayload) error {
	op, err := s.beginBindingOperation(ctx, payload.PrincipalID, payload.UserSessionIssuerID)
	if err != nil {
		return err
	}
	defer func() { _ = op.tx.Rollback(ctx) }()
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid binding id")
	}
	row, err := op.queries.DetachPrincipalRemoteSessionBinding(ctx, repo.DetachPrincipalRemoteSessionBindingParams{
		ProjectID: op.projectID, OrganizationID: op.organizationID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, SubjectUrn: op.subject.String(), ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach remote session")
	}
	if err := s.logBinding(ctx, op, audit.ActionRemoteSessionDetach, id, row.RemoteSessionID, row.RemoteSessionClientID, row.GrantGeneration); err != nil {
		return err
	}
	if err := op.tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit remote session detachment")
	}
	return nil
}

func bindingView(id, principalID, issuerID, clientID, sessionID uuid.UUID) *gen.PrincipalRemoteSessionBinding {
	return &gen.PrincipalRemoteSessionBinding{ID: id.String(), PrincipalID: principalID.String(), UserSessionIssuerID: issuerID.String(), RemoteSessionClientID: clientID.String(), RemoteSessionID: sessionID.String(), RemoteSession: nil}
}

func (s *Service) logBinding(ctx context.Context, op *bindingOperation, action audit.Action, bindingID, sessionID, clientID uuid.UUID, generation int64) error {
	auth, err := bindingHuman(ctx)
	if err != nil {
		return err
	}
	if err := s.auditLogger.LogRemoteSessionBinding(ctx, op.tx, action, audit.LogRemoteSessionBindingEvent{
		OrganizationID: op.organizationID, ProjectID: op.projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID),
		RemoteSessionURN: urn.NewRemoteSession(sessionID),
		Metadata:         audit.RemoteSessionBindingMetadata{BindingID: bindingID, PrincipalID: op.principalID, UserSessionIssuerID: op.issuerID, RemoteSessionClientID: clientID, GrantGeneration: generation},
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session binding")
	}
	return nil
}
