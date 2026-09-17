package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type preparationDCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	Scope                   *string  `json:"scope"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
}

// Every failure after submission that does not prove rejection is indeterminate.
// No response body, client secret or management token becomes a diagnostic.
func (s *Service) submitPreparationDCR(ctx context.Context, in PreparationInput, endpoint, method string) (preparationDCRResponse, string) {
	var result preparationDCRResponse
	if (method != "client_secret_basic" && method != "client_secret_post") || !urls.IsAbsoluteHTTPSOrLoopback(endpoint) {
		return result, "manual_setup_required"
	}
	if s.policy == nil {
		return result, "indeterminate"
	}
	body, _ := json.Marshal(struct {
		GrantTypes []string `json:"grant_types"`
		AuthMethod string   `json:"token_endpoint_auth_method"`
		Scope      string   `json:"scope,omitempty"`
		ClientName string   `json:"client_name"`
	}{[]string{PreparationJWTBearerGrant}, method, strings.Join(in.Scopes, " "), "Gram identity chaining"})
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return result, "indeterminate"
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client := s.policy.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return result, "indeterminate"
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 400 && response.StatusCode < 500 && response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests {
		return result, "provider_rejection"
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return result, "indeterminate"
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return result, "indeterminate"
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return result, "indeterminate"
	}
	if err = json.Unmarshal(data, &result); err != nil {
		var empty preparationDCRResponse
		return empty, "indeterminate"
	}
	return result, validatePreparationDCR(result, in.Scopes, method)
}
func validatePreparationDCR(result preparationDCRResponse, requested []string, method string) string {
	if (method != "client_secret_basic" && method != "client_secret_post") || strings.TrimSpace(result.ClientID) == "" || result.ClientSecret == "" || result.TokenEndpointAuthMethod != method || result.ClientIDIssuedAt < 0 || result.ClientSecretExpiresAt < 0 {
		return "indeterminate"
	}
	// A provider response can narrow scope, never broaden the requested set.
	if result.Scope != nil {
		for scope := range strings.FieldsSeq(*result.Scope) {
			if !slices.Contains(requested, scope) {
				return "indeterminate"
			}
		}
	}
	for _, grant := range result.GrantTypes {
		if grant == "" || strings.ContainsAny(grant, " \t\r\n") {
			return "indeterminate"
		}
	}
	if !slices.Contains(result.GrantTypes, PreparationJWTBearerGrant) {
		return preparationMissingGrantsState(result.GrantTypes)
	}
	if result.ClientSecretExpiresAt > 0 && !time.Unix(result.ClientSecretExpiresAt, 0).After(time.Now()) {
		return "manual_setup_required"
	}
	return "ready"
}
func (s *Service) finishPreparationDCR(ctx context.Context, conn *pgxpool.Conn, in PreparationInput, claim repo.RemoteSessionEmaBinding, issuer repo.RemoteSessionIssuer, method string) (*PreparationResult, error) {
	var emptyClient repo.RemoteSessionClient
	// The caller holds the connection-scoped binding lock, not a transaction.
	response, state := s.submitPreparationDCR(ctx, in, issuer.RegistrationEndpoint.String, method)
	// Persist an outcome even if the requesting connection has gone away. If this
	// process dies before commit, the durable claim becomes indeterminate on read.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	tx, err := conn.Begin(saveCtx)
	if err != nil {
		return preparationResult(claim, issuer, emptyClient, "indeterminate"), err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	q := repo.New(tx)
	// Lock the project before parents, including wholly inherited configurations.
	if _, err = q.LockEMAProject(saveCtx, repo.LockEMAProjectParams{ProjectID: claim.ProjectID, OrganizationID: claim.OrganizationID}); err != nil {
		return nil, preparationLookupError(err, "project not found")
	}
	if err = lockUserSessionIssuersForClientBinding(saveCtx, s.logger, tx, q, claim.ProjectID, claim.OrganizationID, []uuid.UUID{claim.UserSessionIssuerID}); err != nil {
		return preparationResult(claim, issuer, emptyClient, "indeterminate"), err
	}
	if _, err = q.LockEMAUserIssuer(saveCtx, repo.LockEMAUserIssuerParams{ID: claim.UserSessionIssuerID, ProjectID: conv.ToNullUUID(claim.ProjectID), OrganizationID: conv.ToPGText(claim.OrganizationID)}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist preparation registration")
	}
	currentIssuer, err := q.LockEMAIssuer(saveCtx, repo.LockEMAIssuerParams{ID: issuer.ID, ProjectID: conv.ToNullUUID(claim.ProjectID), OrganizationID: conv.ToPGText(claim.OrganizationID)})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist preparation registration")
	}
	b, err := q.LockEMABinding(saveCtx, repo.LockEMABindingParams{ProjectID: claim.ProjectID, OrganizationID: claim.OrganizationID, UserSessionIssuerID: claim.UserSessionIssuerID, RemoteSessionIssuerID: claim.RemoteSessionIssuerID, Resource: claim.Resource})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist preparation registration")
	}
	if b.Generation != claim.Generation || b.ClaimID != claim.ClaimID || preparationBindingState(b.State) != "in_progress" {
		return preparationResult(b, currentIssuer, emptyClient, "configuration_required"), nil
	}
	if currentIssuer.Issuer != issuer.Issuer || currentIssuer.RegistrationEndpoint != issuer.RegistrationEndpoint || PreparationEligibility(currentIssuer.AuthorizationGrantProfilesSupported, currentIssuer.GrantTypesSupported) != "eligible" {
		state = "indeterminate"
	}
	var client repo.RemoteSessionClient
	if state == "ready" || state == "unknown_grants" || state == "manual_setup_required" {
		ciphertext, encErr := s.enc.Encrypt([]byte(response.ClientSecret))
		if encErr != nil {
			return preparationResult(b, currentIssuer, client, "indeterminate"), encErr
		}
		scopes := in.Scopes
		if response.Scope != nil {
			scopes = strings.Fields(*response.Scope)
		}
		issued, expires := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		if response.ClientIDIssuedAt > 0 {
			issued = conv.ToPGTimestamptz(time.Unix(response.ClientIDIssuedAt, 0))
		}
		if response.ClientSecretExpiresAt > 0 {
			expires = conv.ToPGTimestamptz(time.Unix(response.ClientSecretExpiresAt, 0))
		}
		client, err = q.CreateRemoteSessionClient(saveCtx, repo.CreateRemoteSessionClientParams{TokenEndpointAuthAudienceFormat: conv.ToPGTextEmpty(""), Audience: conv.ToPGTextEmpty(""), LegacyCallbackUrl: false, ProjectID: conv.ToNullUUID(b.ProjectID), OrganizationID: conv.ToPGText(b.OrganizationID), RemoteSessionIssuerID: issuer.ID, ClientID: response.ClientID, ClientSecretEncrypted: conv.ToPGText(ciphertext), TokenEndpointAuthMethod: conv.ToPGText(method), Scope: scopes, ClientIDIssuedAt: issued, ClientSecretExpiresAt: expires})
		if err != nil {
			return preparationResult(b, currentIssuer, client, "indeterminate"), err
		}
		client, err = q.SetEMAClientGrants(saveCtx, repo.SetEMAClientGrantsParams{ID: client.ID, ProjectID: conv.ToNullUUID(b.ProjectID), OrganizationID: conv.ToPGText(b.OrganizationID), GrantTypes: response.GrantTypes})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "persist preparation registration")
		}
		auth, ok := contextvalues.GetAuthContext(saveCtx)
		if !ok || auth == nil {
			return nil, oops.C(oops.CodeUnauthorized)
		}
		if err := s.auditLogger.LogRemoteSessionClientCreate(saveCtx, tx, audit.LogRemoteSessionClientCreateEvent{
			OrganizationID: b.OrganizationID, ProjectID: b.ProjectID,
			Actor: urn.NewPrincipal(urn.PrincipalTypeUser, auth.UserID), ActorDisplayName: auth.Email, ActorSlug: nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID), ClientID: client.ClientID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "audit preparation client creation")
		}
		b.RemoteSessionClientID = conv.ToNullUUID(client.ID)
		b.RequestedScopes = scopes
		b.GrantSource = conv.ToPGText("provider_returned")
		b.ClaimID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		b.ClaimedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		// A confirmed registration must be retained even when it is not usable.
		// Recheck after all lock waits and writes: metadata can change while the
		// POST is in flight, and a returned secret can expire before completion.
		state = preparationRegistrationReadiness(saveCtx, q, client, currentIssuer, b.OrganizationID)
	}
	b.State = conv.ToPGText(state)
	b, err = setPreparationBinding(saveCtx, q, b, b.Generation)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist preparation registration")
	}
	if err = tx.Commit(saveCtx); err != nil {
		return preparationResult(claim, issuer, emptyClient, "indeterminate"), err
	}
	return preparationResult(b, currentIssuer, client, state), nil
}
