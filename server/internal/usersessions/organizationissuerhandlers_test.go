package usersessions_test

import (
	"context"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	cimdgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func seedTrustedRemoteSessionIssuerTarget(t *testing.T, ctx context.Context, ti *testInstance, slug string, projectID uuid.NullUUID, organizationID pgtype.Text) uuid.UUID {
	t.Helper()

	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         projectID,
		OrganizationID:                    organizationID,
		Slug:                              slug,
		Issuer:                            "https://" + slug + ".example.com",
		AuthorizationEndpoint:             pgtype.Text{String: "https://" + slug + ".example.com/authorize", Valid: true},
		TokenEndpoint:                     pgtype.Text{String: "https://" + slug + ".example.com/token", Valid: true},
		ScopesSupported:                   []string{"openid", "email", "offline_access"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

func seedTrustedRemoteSessionClientTarget(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID, clientID string, projectID uuid.NullUUID, organizationID pgtype.Text, scope []string) uuid.UUID {
	t.Helper()

	client, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:                       projectID,
		OrganizationID:                  organizationID,
		RemoteSessionIssuerID:           issuerID,
		ClientID:                        clientID,
		ClientSecretEncrypted:           pgtype.Text{String: "encrypted-test-secret", Valid: true},
		ClientIDIssuedAt:                pgtype.Timestamptz{},
		ClientSecretExpiresAt:           pgtype.Timestamptz{},
		TokenEndpointAuthMethod:         pgtype.Text{String: "client_secret_basic", Valid: true},
		TokenEndpointAuthAudienceFormat: pgtype.Text{},
		Scope:                           scope,
		Audience:                        pgtype.Text{},
		LegacyCallbackUrl:               false,
	})
	require.NoError(t, err)
	return client.ID
}

func TestOrganizationUserSessionIssuerTrustedRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	organizationID := pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true}
	organizationTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-org-target", uuid.NullUUID{}, organizationID)
	organizationTargetID := organizationTarget.String()
	organizationClient := seedTrustedRemoteSessionClientTarget(t, ctx, ti, organizationTarget, "trusted-org-client", uuid.NullUUID{}, organizationID, []string{"openid", "email", "profile", "offline_access"})
	organizationClientID := organizationClient.String()
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-org-user-issuer",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &organizationTargetID,
		TrustedRemoteSessionClientID: &organizationClientID,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *created.TrustedRemoteSessionIssuerID)
	require.Equal(t, organizationClientID, *created.TrustedRemoteSessionClientID)
	createAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	createSnapshot, err := audittest.DecodeAuditData(createAudit.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, createSnapshot["TrustedRemoteSessionIssuerID"])
	require.Equal(t, organizationClientID, createSnapshot["TrustedRemoteSessionClientID"])

	mode := "interactive"
	unchanged, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                 created.ID,
		AuthnChallengeMode: &mode,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *unchanged.TrustedRemoteSessionIssuerID, "omitting the trust field must retain the link")
	require.Equal(t, organizationClientID, *unchanged.TrustedRemoteSessionClientID, "omitting the trust field must retain the link")

	organizationTargetURN := "urn:uuid:" + organizationTargetID
	normalized, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &organizationTargetURN,
	})
	require.NoError(t, err)
	require.Equal(t, organizationTargetID, *normalized.TrustedRemoteSessionIssuerID, "accepted UUID forms are persisted canonically")

	globalTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-global-target", uuid.NullUUID{}, pgtype.Text{})
	globalTargetID := globalTarget.String()
	globalClient := seedTrustedRemoteSessionClientTarget(t, ctx, ti, globalTarget, "trusted-global-client", uuid.NullUUID{}, organizationID, []string{"openid", "email", "profile", "offline_access"})
	globalClientID := globalClient.String()
	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &globalTargetID,
		TrustedRemoteSessionClientID: &globalClientID,
	})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *updated.TrustedRemoteSessionIssuerID)
	require.Equal(t, globalClientID, *updated.TrustedRemoteSessionClientID)

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *loaded.TrustedRemoteSessionIssuerID)
	require.Equal(t, globalClientID, *loaded.TrustedRemoteSessionClientID)

	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &organizationTargetID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	stillLinked, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *stillLinked.TrustedRemoteSessionIssuerID)
	require.Equal(t, globalClientID, *stillLinked.TrustedRemoteSessionClientID)

	clearValue := ""
	issuerOnly, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionClientID: &clearValue,
	})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *issuerOnly.TrustedRemoteSessionIssuerID)
	require.Nil(t, issuerOnly.TrustedRemoteSessionClientID)

	relinked, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionClientID: &globalClientID,
	})
	require.NoError(t, err)
	require.Equal(t, globalTargetID, *relinked.TrustedRemoteSessionIssuerID)
	require.Equal(t, globalClientID, *relinked.TrustedRemoteSessionClientID)

	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &clearValue,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	whitespaceClearValue := "\t\n "
	clearedWithWhitespace, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &whitespaceClearValue,
		TrustedRemoteSessionClientID: &whitespaceClearValue,
	})
	require.NoError(t, err)
	require.Nil(t, clearedWithWhitespace.TrustedRemoteSessionIssuerID)
	require.Nil(t, clearedWithWhitespace.TrustedRemoteSessionClientID)

	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &globalTargetID,
		TrustedRemoteSessionClientID: &globalClientID,
	})
	require.NoError(t, err)

	cleared, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &clearValue,
		TrustedRemoteSessionClientID: &clearValue,
	})
	require.NoError(t, err)
	require.Nil(t, cleared.TrustedRemoteSessionIssuerID)
	require.Nil(t, cleared.TrustedRemoteSessionClientID)

	projectTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-project-target", uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, organizationID)
	projectTargetID := projectTarget.String()
	projectClient := seedTrustedRemoteSessionClientTarget(t, ctx, ti, projectTarget, "trusted-project-client", uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, organizationID, []string{"openid", "email"})
	projectClientID := projectClient.String()
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &projectTargetID,
		TrustedRemoteSessionClientID: &projectClientID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	otherOrganizationID := "org-" + uuid.NewString()
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrganizationID,
		Name:        "Other Organization",
		Slug:        "other-organization-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{String: otherOrganizationID, Valid: true},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	foreignTarget := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-foreign-target", uuid.NullUUID{}, pgtype.Text{String: otherOrganizationID, Valid: true})
	foreignTargetID := foreignTarget.String()
	foreignClient := seedTrustedRemoteSessionClientTarget(t, ctx, ti, foreignTarget, "trusted-foreign-client", uuid.NullUUID{}, pgtype.Text{String: otherOrganizationID, Valid: true}, []string{"openid", "email"})
	foreignClientID := foreignClient.String()
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &foreignTargetID,
		TrustedRemoteSessionClientID: &foreignClientID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	badID := "not-a-uuid"
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                           created.ID,
		TrustedRemoteSessionIssuerID: &badID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationUserSessionIssuerTrustedPairValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	organizationID := pgtype.Text{String: authCtx.ActiveOrganizationID, Valid: true}
	remoteIssuerID := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-validation-target", uuid.NullUUID{}, organizationID)
	remoteIssuerIDString := remoteIssuerID.String()

	issuerOnly, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-one-sided",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &remoteIssuerIDString,
		TrustedRemoteSessionClientID: nil,
	})
	require.NoError(t, err)
	require.Equal(t, remoteIssuerIDString, *issuerOnly.TrustedRemoteSessionIssuerID)
	require.Nil(t, issuerOnly.TrustedRemoteSessionClientID)

	eligibleClientID := seedTrustedRemoteSessionClientTarget(t, ctx, ti, remoteIssuerID, "trusted-validation-client-only", uuid.NullUUID{}, organizationID, []string{"openid", "email"}).String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-client-only",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: nil,
		TrustedRemoteSessionClientID: &eligibleClientID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	missingEmailClientID := seedTrustedRemoteSessionClientTarget(t, ctx, ti, remoteIssuerID, "trusted-validation-missing-email", uuid.NullUUID{}, organizationID, []string{"openid"}).String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-missing-email",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &remoteIssuerIDString,
		TrustedRemoteSessionClientID: &missingEmailClientID,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	wrongIssuerID := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-validation-wrong-issuer", uuid.NullUUID{}, organizationID).String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-wrong-pair",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &wrongIssuerID,
		TrustedRemoteSessionClientID: &eligibleClientID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	globalIssuerID := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-validation-global", uuid.NullUUID{}, pgtype.Text{})
	globalIssuerIDString := globalIssuerID.String()
	globalClientID := seedTrustedRemoteSessionClientTarget(t, ctx, ti, globalIssuerID, "trusted-validation-global", uuid.NullUUID{}, pgtype.Text{}, []string{"openid", "email"}).String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-global-client",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &globalIssuerIDString,
		TrustedRemoteSessionClientID: &globalClientID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	deletedClientID := seedTrustedRemoteSessionClientTarget(t, ctx, ti, remoteIssuerID, "trusted-validation-deleted-client", uuid.NullUUID{}, organizationID, []string{"openid", "email"})
	deletedClientRows, err := testrepo.New(ti.conn).SoftDeleteRemoteSessionClientFixture(ctx, deletedClientID)
	require.NoError(t, err)
	require.EqualValues(t, 1, deletedClientRows)
	deletedClientIDString := deletedClientID.String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-deleted-client",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &remoteIssuerIDString,
		TrustedRemoteSessionClientID: &deletedClientIDString,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	deletedIssuerID := seedTrustedRemoteSessionIssuerTarget(t, ctx, ti, "trusted-validation-deleted-issuer", uuid.NullUUID{}, organizationID)
	deletedIssuerRows, err := testrepo.New(ti.conn).SoftDeleteRemoteSessionIssuerFixture(ctx, deletedIssuerID)
	require.NoError(t, err)
	require.EqualValues(t, 1, deletedIssuerRows)
	deletedIssuerIDString := deletedIssuerID.String()
	_, err = ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:                 nil,
		Slug:                         "trusted-validation-deleted-issuer",
		AuthnChallengeMode:           "chain",
		SessionDurationHours:         24,
		TrustedRemoteSessionIssuerID: &deletedIssuerIDString,
		TrustedRemoteSessionClientID: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuersCRUDAndListIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "shared-slug",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	require.Empty(t, first.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, first.OrganizationID)

	second, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "shared-slug",
		AuthnChallengeMode:   "interactive",
		SessionDurationHours: 12,
	})
	require.NoError(t, err, "organization slugs are not unique identifiers")
	createAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, createAudit.OrganizationID)
	require.False(t, createAudit.ProjectID.Valid)

	_, err = ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "project-only",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListIssuers(ctx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Items, 2)

	mode := "interactive"
	duration := 48
	updated, err := ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            first.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          &duration,
		ClientIDMetadataAdmissionMode: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "interactive", updated.AuthnChallengeMode)
	require.Equal(t, 48, updated.SessionDurationHours)
	updateAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, updateAudit.OrganizationID)
	require.False(t, updateAudit.ProjectID.Valid)

	loaded, err := ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: first.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, updated, loaded)

	require.NoError(t, ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: second.ID, SessionToken: nil}))
	deleteAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, deleteAudit.OrganizationID)
	require.False(t, deleteAudit.ProjectID.Valid)
	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: second.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuerUpdateSerializesWithOwnerBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		Slug:                 "org-update-owner-binding-lock",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(created.ID)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, usersessionsrepo.New(tx).LockUserSessionIssuerForOwnerBinding(ctx, issuerID))

	mode := "interactive"
	blockedCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, err = ti.service.UpdateIssuer(blockedCtx, &orggen.UpdateIssuerPayload{
		ID:                 created.ID,
		AuthnChallengeMode: &mode,
	})
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded, "update must wait for the owner-binding lock")

	require.NoError(t, tx.Rollback(ctx))
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		ID:                 created.ID,
		AuthnChallengeMode: &mode,
	})
	require.NoError(t, err, "update must complete after the owner-binding lock is released")
}

func TestProjectIssuerMutationsRejectOrganizationOwnedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "organization-owned",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	mode := "interactive"
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &projectgen.UpdateUserSessionIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ProjectSlugInput:              nil,
		ID:                            created.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.DeleteUserSessionIssuer(ctx, &projectgen.DeleteUserSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.GetIssuer(ctx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
}

func TestOrganizationUserSessionIssuerRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-rbac",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err = ti.service.GetIssuer(readCtx, &orggen.GetIssuerPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	_, err = ti.service.ListIssuers(readCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	require.NoError(t, err)
	_, err = ti.service.GetIssuerDeletePreflight(readCtx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)

	_, err = ti.service.CreateIssuer(readCtx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "forbidden-create",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	mode := "interactive"
	_, err = ti.service.UpdateIssuer(readCtx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            created.ID,
		Slug:                          nil,
		AuthnChallengeMode:            &mode,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteIssuer(readCtx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestOrganizationUserSessionIssuerCimdClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-cimd",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	const clientURI = "https://org-cimd.example.com/client"
	_, err = ti.service.CreateUserSessionIssuerCimdClient(ctx, &cimdgen.CreateUserSessionIssuerCimdClientPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: clientURI,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	created, err := ti.service.CreateCimdClient(ctx, &orggen.CreateCimdClientPayload{
		SessionToken:        nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: clientURI,
	})
	require.NoError(t, err)
	require.Empty(t, created.Client.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, created.Client.OrganizationID)
	require.Equal(t, clientURI, created.Client.ClientIDMetadataURI)
	addAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCimdClientAdd)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, addAudit.OrganizationID)
	require.False(t, addAudit.ProjectID.Valid)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	loaded, err := ti.service.GetCimdClient(readCtx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, created.Client, loaded)
	listed, err := ti.service.ListCimdClients(readCtx, &orggen.ListCimdClientsPayload{
		UserSessionIssuerID: issuer.ID,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
	})
	require.NoError(t, err)
	require.Equal(t, []*types.UserSessionIssuerCimdClient{created.Client}, listed.Items)

	_, err = ti.service.CreateCimdClient(readCtx, &orggen.CreateCimdClientPayload{
		SessionToken:        nil,
		UserSessionIssuerID: issuer.ID,
		ClientIDMetadataURI: "https://forbidden.example.com/client",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.DeleteCimdClient(readCtx, &orggen.DeleteCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	err = ti.service.DeleteUserSessionIssuerCimdClient(ctx, &cimdgen.DeleteUserSessionIssuerCimdClientPayload{
		ID:               created.Client.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetCimdClient(ctx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteCimdClient(ctx, &orggen.DeleteCimdClientPayload{ID: created.Client.ID, SessionToken: nil}))
	removeAudit, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionUserSessionIssuerCimdClientRemove)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, removeAudit.OrganizationID)
	require.False(t, removeAudit.ProjectID.Valid)
	_, err = ti.service.GetCimdClient(ctx, &orggen.GetCimdClientPayload{ID: created.Client.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuersRejectAPIKeyContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	apiKeyAuthCtx := *authCtx
	apiKeyAuthCtx.APIKeyID = uuid.NewString()
	apiKeyCtx := contextvalues.SetAuthContext(ctx, &apiKeyAuthCtx)

	_, err := ti.service.ListIssuers(apiKeyCtx, &orggen.ListIssuersPayload{Cursor: nil, Limit: nil, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUserSessionIssuersRejectOverflowingDuration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "overflow-org",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 2562048,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	orgIssuer, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "overflow-org-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	overflowHours := 2562048
	_, err = ti.service.UpdateIssuer(ctx, &orggen.UpdateIssuerPayload{
		SessionToken:                  nil,
		ID:                            orgIssuer.ID,
		Slug:                          nil,
		AuthnChallengeMode:            nil,
		SessionDurationHours:          &overflowHours,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "overflow-project",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 2562048,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	projectIssuer, err := ti.service.CreateUserSessionIssuer(ctx, &projectgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "overflow-project-update",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &projectgen.UpdateUserSessionIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ProjectSlugInput:              nil,
		ID:                            projectIssuer.ID,
		Slug:                          nil,
		AuthnChallengeMode:            nil,
		SessionDurationHours:          &overflowHours,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOrganizationUserSessionIssuerDeletePreflight(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateIssuer(ctx, &orggen.CreateIssuerPayload{
		SessionToken:         nil,
		Slug:                 "org-preflight",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(created.ID)
	siblingID := createSiblingProject(t, ctx, ti.conn, "org-preflight-sibling")

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              siblingID,
		Name:                   "Preflight toolset",
		Slug:                   "preflight-toolset",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             false,
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           siblingID,
	})
	require.NoError(t, err)

	serverID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             siblingID,
		Name:                  pgtype.Text{String: "Preflight server", Valid: true},
		Slug:                  pgtype.Text{String: "preflight-server", Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuerID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "org-preflight-client")
	require.NoError(t, err)
	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, urn.NewUserSubject("org-preflight-user"))
	require.NoError(t, err)

	preflight, err := ti.service.GetIssuerDeletePreflight(ctx, &orggen.GetIssuerDeletePreflightPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, 1, preflight.ClientCount)
	require.Equal(t, 1, preflight.LiveSessionCount)
	require.False(t, preflight.CanDelete)
	require.Equal(t, serverID.String(), preflight.McpServers[0].ID)
	require.Equal(t, "Preflight server", preflight.McpServers[0].Name)
	require.Equal(t, siblingID.String(), preflight.McpServers[0].ProjectID)
	require.Equal(t, toolset.ID.String(), preflight.Toolsets[0].ID)
	require.Equal(t, "Preflight toolset", preflight.Toolsets[0].Name)

	err = ti.service.DeleteIssuer(ctx, &orggen.DeleteIssuerPayload{ID: created.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeConflict)
}
