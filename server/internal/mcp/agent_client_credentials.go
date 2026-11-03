package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Agent clients renew by proving possession again, never by retaining a refresh
// token. Their identity and ceiling come only from authenticated registration.
func (s *Service) handleAgentClientCredentialsGrant(ctx context.Context, w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, client *usersessions_repo.UserSessionClient, baseURL, method string, logger *slog.Logger) error {
	reject := func(code string) error {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, code, "agent client authorization refused")
	}
	admissionFailure := func(err error) error {
		if errors.Is(err, pgx.ErrNoRows) || isCredentialDenial(err) {
			return reject("unauthorized_client")
		}
		logger.ErrorContext(ctx, "agent client admission unavailable", attr.SlogError(err))
		return writeTokenError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "agent client authorization unavailable")
	}
	if !client.SubjectUrn.Valid || method != "private_key_jwt" || client.TokenEndpointAuthMethod.String != "private_key_jwt" || client.UserSessionIssuerID != endpoint.UserSessionIssuerID {
		return reject("unauthorized_client")
	}
	resource, err := endpoint.RootURL(baseURL)
	if err != nil || len(r.PostForm["resource"]) != 1 || r.PostForm.Get("resource") != resource {
		return reject("invalid_target")
	}
	if len(r.PostForm["scope"]) != 1 || r.PostForm.Get("scope") != "mcp:connect" {
		return reject("invalid_scope")
	}
	subject, err := urn.ParseSessionSubject(client.SubjectUrn.String)
	if err != nil {
		return reject("unauthorized_client")
	}
	credential, err := loadAgentSessionCredential(endpoint, subject, subject, client.OrganizationID, client.AuthorizerUserID, client.DelegatedGrants, client.DelegatedGrantsVersion)
	if err != nil {
		return reject("unauthorized_client")
	}
	ctx, err = s.contextForSessionSubject(ctx, endpoint, subject, "", client.ClientID)
	if err == nil {
		ctx, err = s.admitAgentSession(ctx, endpoint, subject, credential)
	}
	if err != nil {
		return admissionFailure(err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return admissionFailure(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := usersessions_repo.New(tx)
	if _, err = q.LockLiveUserSessionIssuerForChildWrite(ctx, usersessions_repo.LockLiveUserSessionIssuerForChildWriteParams{ID: endpoint.UserSessionIssuerID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID}); err != nil {
		return admissionFailure(err)
	}
	if _, err = q.LockUserSessionConsentClient(ctx, usersessions_repo.LockUserSessionConsentClientParams{UserSessionClientID: client.ID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID}); err != nil {
		return admissionFailure(err)
	}
	ctx, err = s.authz.AdmitPrincipalCredentialWithDBTX(ctx, tx)
	if err == nil {
		ctx, err = s.requireAgentSessionAuthorization(ctx, endpoint)
	}
	if err != nil {
		return admissionFailure(err)
	}
	minted, err := s.mintSession(ctx, endpoint, client, q, mintSessionParams{
		AuthorizationExpiresAt: nil, DesiredSessionDuration: nil, Replayable: false, SignInID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, CredentialID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ToolSelection: nil, UpstreamClientPicks: nil, AuthClientPicks: nil, Catalog: nil,
		Policy: sessionIssuancePolicyAgentClient, Audience: resource, BaseURL: baseURL, Subject: subject,
		AuthorizerUserID: pgtype.Text{String: credential.AuthorizerUserID, Valid: true}, DelegatedGrants: credential.DelegatedGrants, DelegatedGrantsVersion: pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true},
	}, logger)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnavailable, err, "commit agent token issuance")
	}
	return writeTokenSuccess(ctx, w, logger, minted.Body)
}
