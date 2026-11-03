package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// platformWorkloadResource accepts only exact, registered builtin URLs.
func (s *Service) platformWorkloadResource(resource string) (string, bool) {
	for slug := range s.platformToolsets {
		if resource == platformtools.PlatformToolsetURL(s.serverURL, slug) {
			return slug, true
		}
	}
	return "", false
}

// The project-bound envelope binds persisted tenancy. Existing workload
// admission derives authority from live agent policy; this marker grants nothing.
func encodePlatformWorkloadPolicy(projectID uuid.UUID, version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
	policy, err := runtimepolicy.NewDelegatedPolicy(version, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, projectID.String())})
	if err != nil {
		return nil, fmt.Errorf("create platform workload policy: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return nil, fmt.Errorf("encode platform workload policy: %w", err)
	}
	return encoded, nil
}

func (s *Service) handlePlatformWorkloadGrant(ctx context.Context, w http.ResponseWriter, r *http.Request, issuer *sharedIssuer, resource string) error {
	logger := issuer.logger
	if refused, err := s.refuseUnservedWorkloadGrant(ctx, w, r, extractClientCredentials(r), logger); refused {
		return err
	}
	assertion := r.PostForm.Get(oauthwire.ParamAssertion)
	if assertion == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "assertion is required")
	}
	projectID, err := uuid.Parse(r.PostForm.Get("project_id"))
	shared := issuer.authorizationServer
	if err != nil || len(r.PostForm["project_id"]) != 1 || projectID == uuid.Nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, "a project in the authorization issuer's tenancy is required")
	}
	urls, err := shared.urls()
	if err != nil {
		return fmt.Errorf("exchange workload platform: %w", err)
	}
	// Authenticate before looking up project existence or tenancy. The requested
	// project still selects project-tier workload admission; tenancy is checked
	// below before any credential can be issued.
	presented, err := admitWorkloadAssertion(ctx, s.workloadGrant, workloadTenancy{OrganizationID: issuer.organizationID, ProjectID: projectID, UserSessionIssuerID: shared.issuerID}, []string{urls.Issuer, urls.Token}, assertion)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}
	if shared.projectID != uuid.Nil && shared.projectID != projectID {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, "a project in the authorization issuer's tenancy is required")
	}
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && project.OrganizationID != issuer.organizationID) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidTarget, "a project in the authorization issuer's tenancy is required")
	}
	if err != nil {
		return fmt.Errorf("exchange workload platform: %w", err)
	}
	version := runtimepolicy.IssuedDelegatedPolicyVersion
	grants, err := encodePlatformWorkloadPolicy(projectID, version)
	if err != nil {
		return fmt.Errorf("exchange workload platform: %w", err)
	}
	credential := workloadSessionCredential{DerivesPolicyAtAdmission: true, DelegatedGrants: grants, DelegatedGrantsVersion: int32(version)}
	subject := urn.NewWorkloadSubject(presented.issuerID, presented.subject)
	session := workloadSessionIssuance(workloadSessionTarget{userSessionIssuerID: shared.issuerID, projectID: projectID, organizationID: issuer.organizationID, issuer: shared.issuer, audience: resource}, subject, credential)
	// The session row inherits its issuer tenancy (organization issuers have
	// no project_id). Persist the exact target in the immutable resource and
	// marker so authentication never takes tenancy from caller headers.
	session.Resource = resource + ":" + projectID.String()
	return s.issueWorkloadGrantSession(ctx, w, logger, workloadGrantIssuance{client: nil, beforeCommit: nil, presented: presented, credential: credential, projectID: &projectID, endpoint: nil, session: session, choosesUpstream: false, requestedAuthClients: nil})
}

// authorizeWorkloadPlatform implements the builtin-only authentication
// boundary. Unverified claims are used solely to select this validator; every
// identity, tenant, ceiling and expiry is subsequently verified and read live.
func (s *Service) authorizeWorkloadPlatform(ctx context.Context, token, resource string) (context.Context, bool, error) {
	claims := &jwt.RegisteredClaims{} //nolint:exhaustruct // ParseUnverified populates routing hints; validation below establishes authority
	if _, _, err := jwt.NewParser().ParseUnverified(token, claims); err != nil {
		return ctx, false, nil
	}
	platform := false
	for _, audience := range claims.Audience {
		if audience == resource {
			platform = true
		}
	}
	if !platform {
		return ctx, false, nil
	}
	denied := func() (context.Context, bool, error) { return ctx, true, oops.C(oops.CodeUnauthorized) }
	session, err := s.userSessionSigner.ValidateExactAudienceBearer(ctx, token, resource, s.chatSessionsManager)
	if err != nil {
		if errors.Is(err, sessiontokens.ErrRevocationUnavailable) {
			return ctx, true, oops.E(oops.CodeUnexpected, err, "platform credential validation unavailable")
		}
		return ctx, true, oops.E(oops.CodeUnauthorized, err, "invalid platform credential")
	}
	if session.Subject().Kind != urn.SessionSubjectKindWorkload {
		return denied()
	}
	issuerURL, err := url.Parse(session.Issuer())
	if err != nil {
		return denied()
	}
	issuerID, err := uuid.Parse(path.Base(issuerURL.Path))
	if err != nil {
		return denied()
	}
	queries := usersessionsrepo.New(s.db)
	issuer, err := queries.GetSharedUserSessionIssuerByID(ctx, issuerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	if !authserver.IssuerInSharedMode(issuer.UserSessionIssuer) {
		return denied()
	}
	shared, err := s.sharedAuthorizationServerFor(issuer.UserSessionIssuer)
	if err != nil || shared.issuer != session.Issuer() {
		return denied()
	}
	row, err := queries.GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: session.JTI()})
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	if !row.OrganizationID.Valid || row.OrganizationID.String != issuer.ResolvedOrganizationID || !row.Resource.Valid || !strings.HasPrefix(row.Resource.String, resource+":") || row.UserSessionClientID.Valid || !row.ExpiresAt.Valid || !time.Now().Before(row.ExpiresAt.Time) || !row.RefreshExpiresAt.Valid || !time.Now().Before(row.RefreshExpiresAt.Time) {
		return denied()
	}
	projectID, err := uuid.Parse(strings.TrimPrefix(row.Resource.String, resource+":"))
	if err != nil || projectID == uuid.Nil || (row.ProjectID.Valid && row.ProjectID.UUID != projectID) {
		return denied()
	}
	if shared.projectID != uuid.Nil && shared.projectID != projectID {
		return denied()
	}
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return denied()
	}
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	if project.OrganizationID != issuer.ResolvedOrganizationID {
		return denied()
	}
	credential, err := checkWorkloadSessionCredential(issuer.ResolvedOrganizationID, session.Subject(), row.SubjectUrn, row.OrganizationID, row.DelegatedGrants, row.DelegatedGrantsVersion, func(version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
		return encodePlatformWorkloadPolicy(projectID, version)
	})
	if err != nil {
		return denied()
	}
	authCtx, err := s.sessionAuthContext(ctx, issuer.ResolvedOrganizationID, &projectID, session.JTI())
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	authCtx.ProjectSlug = &project.Slug
	workloadIssuerID, externalSubject, err := session.Subject().Workload()
	if err != nil {
		return denied()
	}
	admitted, err := workloadidentity.IsAdmitted(ctx, s.db, workloadidentity.AdmissionParams{OrganizationID: issuer.ResolvedOrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, WorkloadIssuerID: workloadIssuerID, Subject: externalSubject})
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	if !admitted {
		return denied()
	}
	ctx = contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewWorkloadPrincipal(workloadIssuerID, externalSubject))
	credential.DerivesPolicyAtAdmission = true
	ctx, err = s.admitWorkloadSession(ctx, issuer.ResolvedOrganizationID, session.Subject(), credential)
	if err != nil {
		return ctx, true, fmt.Errorf("authorize workload platform: %w", err)
	}
	return ctx, true, nil
}
