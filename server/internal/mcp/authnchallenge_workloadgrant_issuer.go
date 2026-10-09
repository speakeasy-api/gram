// The workload assertion grant at a shared authorization server, for a token
// request naming no RFC 8707 resource: the session it mints is valid at every
// MCP server of the issuer rather than one.

package mcp

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// handleIssuerWorkloadAssertionGrant exchanges a workload's platform-issued
// identity token, presented to a shared authorization server without a
// resource, for a session valid at every MCP server of the issuer.
//
// RFC 7523 makes no resource part of the grant, and RFC 8707 §2 lets an
// authorization server serve a request naming none with a default resource.
// The default here is all of the issuer's MCP servers, which the access token
// names as its audience (urn.UserSessionIssuerMCPServers), as RFC 9068 §3
// requires. Platforms such as Claude Tag exchange one identity token per
// authorization server and attach the access token to every MCP server they
// call, so a token bound to one server would strand the rest.
//
// The assertion is verified and its subject admitted as for a grant naming a
// resource, but against the issuer's own tenancy: its project and the
// organization above for a project issuer, the organization alone for an
// organization issuer. The workload must have a live assigned agent. Which of
// the issuer's MCP servers the session reaches is not decided here: each
// checks the agent's live policy on every request and answers 403 where it may
// not connect.
func (s *Service) handleIssuerWorkloadAssertionGrant(ctx context.Context, w http.ResponseWriter, r *http.Request, issuer *sharedIssuer) error {
	logger := issuer.logger
	if refused, err := s.refuseUnservedWorkloadGrant(ctx, w, r, extractClientCredentials(r), issuer.organizationID, logger); refused {
		return err
	}

	assertion := r.PostForm.Get(oauthwire.ParamAssertion)
	if assertion == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "assertion is required")
	}

	shared := issuer.authorizationServer
	urls, err := shared.urls()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build workload assertion audiences").LogError(ctx, logger)
	}
	tenancy := workloadTenancy{
		OrganizationID:      issuer.organizationID,
		ProjectID:           shared.projectID,
		UserSessionIssuerID: shared.issuerID,
	}
	presented, err := admitWorkloadAssertion(ctx, s.workloadGrant, tenancy, []string{urls.Issuer, urls.Token}, assertion)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	subject := urn.NewWorkloadSubject(presented.issuerID, presented.subject)
	if _, _, err := subject.Workload(); err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("assertion_subject_invalid", err))
	}
	version := runtimepolicy.CurrentDelegatedPolicyVersion
	delegatedGrants, err := encodeIssuerWorkloadSessionPolicy(shared.projectID, version)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode issuer workload session policy").LogError(ctx, logger)
	}
	// The same check the MCP side applies to the stored row, so a session it
	// would refuse is never written.
	credential, err := loadIssuerWorkloadSessionCredential(
		issuer.organizationID, shared.projectID, subject, subject,
		pgtype.Text{String: issuer.organizationID, Valid: true},
		delegatedGrants, pgtype.Int4{Int32: int32(version), Valid: true},
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "validate issuer workload session policy").LogError(ctx, logger)
	}

	var projectID *uuid.UUID
	if shared.projectID != uuid.Nil {
		projectID = &shared.projectID
	}
	return s.issueWorkloadGrantSession(ctx, w, logger, workloadGrantIssuance{
		presented:  presented,
		credential: credential,
		projectID:  projectID,
		endpoint:   nil,
		session: workloadSessionIssuance(workloadSessionTarget{
			userSessionIssuerID: shared.issuerID,
			projectID:           shared.projectID,
			organizationID:      issuer.organizationID,
			issuer:              shared.issuer,
			audience:            urn.NewUserSessionIssuerMCPServers(shared.issuerID).String(),
		}, subject, credential),
	})
}
