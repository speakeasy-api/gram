package assets_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/assets"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// projectAuthzFixture holds two projects in one organization, owned by the
// same user, plus that user's dashboard session. Callers authenticate through
// the service's real security handlers rather than hand-built auth contexts.
type projectAuthzFixture struct {
	ti *testInstance

	orgID string

	orgSlug string

	userID string

	sessionID string

	ownProjectID uuid.UUID

	ownProjectSlug string

	otherProjectID uuid.UUID
}

func newProjectAuthzFixture(t *testing.T) *projectAuthzFixture {
	t.Helper()

	ctx, ti := newTestAssetsService(t)
	ownAuthCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, ownAuthCtx.ProjectID)
	require.NotNil(t, ownAuthCtx.ProjectSlug)
	require.NotNil(t, ownAuthCtx.SessionID)

	otherCtx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessionManager)
	otherAuthCtx, ok := contextvalues.GetAuthContext(otherCtx)
	require.True(t, ok)
	require.NotNil(t, otherAuthCtx.ProjectID)
	require.Equal(t, ownAuthCtx.ActiveOrganizationID, otherAuthCtx.ActiveOrganizationID)
	require.NotEqual(t, *ownAuthCtx.ProjectID, *otherAuthCtx.ProjectID)

	return &projectAuthzFixture{
		ti:             ti,
		orgID:          ownAuthCtx.ActiveOrganizationID,
		orgSlug:        ownAuthCtx.OrganizationSlug,
		userID:         ownAuthCtx.UserID,
		sessionID:      *ownAuthCtx.SessionID,
		ownProjectID:   *ownAuthCtx.ProjectID,
		ownProjectSlug: *ownAuthCtx.ProjectSlug,
		otherProjectID: *otherAuthCtx.ProjectID,
	}
}

func (f *projectAuthzFixture) createAsset(t *testing.T, organizationID string, projectID uuid.UUID, kind string) uuid.UUID {
	t.Helper()

	content := fmt.Sprintf("%s asset for project %s", kind, projectID)
	contentType := "application/octet-stream"
	filename := uuid.NewString()

	writer, uri, err := f.ti.storage.Write(t.Context(), filename, contentType, int64(len(content)))
	require.NoError(t, err)
	_, err = io.Copy(writer, strings.NewReader(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	asset, err := f.ti.repo.CreateAsset(t.Context(), repo.CreateAssetParams{
		Name:           filename,
		Url:            uri.String(),
		ProjectID:      projectID,
		OrganizationID: organizationID,
		Sha256:         uuid.NewString(),
		Kind:           kind,
		ContentType:    contentType,
		ContentLength:  int64(len(content)),
	})
	require.NoError(t, err)

	return asset.ID
}

func (f *projectAuthzFixture) grantProjectRead(t *testing.T, projectID string) {
	t.Helper()

	selectors, err := authz.NewSelector(authz.ScopeProjectRead, projectID).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(f.ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.orgID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, f.userID),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func (f *projectAuthzFixture) createLegacyAPIKey(t *testing.T, projectID uuid.UUID) string {
	t.Helper()

	key := "gram_local_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	hash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)

	_, err = keysrepo.New(f.ti.conn).CreateAPIKey(t.Context(), keysrepo.CreateAPIKeyParams{
		OrganizationID:  f.orgID,
		ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
		CreatedByUserID: f.userID,
		Name:            "assets-" + uuid.NewString(),
		KeyPrefix:       key[:16],
		KeyHash:         hash,
		Scopes:          []string{"consumer"},
	})
	require.NoError(t, err)

	return key
}

func (f *projectAuthzFixture) authenticateAPIKey(t *testing.T, key string) context.Context {
	t.Helper()

	ctx, err := f.ti.service.APIKeyAuth(t.Context(), key, &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         []string{"consumer", "producer", "chat", "hooks", "agent", "agent_user"},
		RequiredScopes: []string{},
	})
	require.NoError(t, err)

	return ctx
}

func (f *projectAuthzFixture) authenticateSession(t *testing.T) context.Context {
	t.Helper()

	ctx, err := f.ti.service.APIKeyAuth(t.Context(), f.sessionID, &security.APIKeyScheme{
		Name:           constants.SessionSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	require.NoError(t, err)

	return ctx
}

func (f *projectAuthzFixture) authenticateChatSession(t *testing.T, claims chatsessions.ChatSessionClaims) context.Context {
	t.Helper()

	token, _, err := f.ti.chatSessionsManager.GenerateToken(t.Context(), claims, "http://localhost", 3600)
	require.NoError(t, err)

	ctx, err := f.ti.service.JWTAuth(t.Context(), token, &security.JWTScheme{
		Name:           "chat_sessions_token",
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	require.NoError(t, err)

	return ctx
}

func (f *projectAuthzFixture) serve(t *testing.T, ctx context.Context, kind string, projectID, assetID uuid.UUID) error {
	t.Helper()

	var body io.ReadCloser
	var err error
	switch kind {
	case "openapiv3":
		_, body, err = f.ti.service.ServeOpenAPIv3(ctx, &assets.ServeOpenAPIv3Form{
			ApikeyToken:  nil,
			SessionToken: nil,
			ProjectID:    projectID.String(),
			ID:           assetID.String(),
		})
	case "functions":
		_, body, err = f.ti.service.ServeFunction(ctx, &assets.ServeFunctionForm{
			ApikeyToken:  nil,
			SessionToken: nil,
			ProjectID:    projectID.String(),
			ID:           assetID.String(),
		})
	case "chat_attachment":
		_, body, err = f.ti.service.ServeChatAttachment(ctx, &assets.ServeChatAttachmentForm{
			ApikeyToken:       nil,
			SessionToken:      nil,
			ChatSessionsToken: nil,
			ProjectID:         projectID.String(),
			ID:                assetID.String(),
		})
	default:
		return fmt.Errorf("unknown asset kind %q", kind)
	}
	if err != nil {
		return fmt.Errorf("serve %s asset: %w", kind, err)
	}
	require.NoError(t, body.Close())
	return nil
}

func requireForbidden(t *testing.T, err error) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

var downloadableAssetKinds = []string{"openapiv3", "functions", "chat_attachment"}

func TestAssetDownload_ProjectBoundAPIKeyCannotReadOtherProject(t *testing.T) {
	t.Parallel()

	for _, kind := range downloadableAssetKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			f := newProjectAuthzFixture(t)
			ownAssetID := f.createAsset(t, f.orgID, f.ownProjectID, kind)
			otherAssetID := f.createAsset(t, f.orgID, f.otherProjectID, kind)
			ctx := f.authenticateAPIKey(t, f.createLegacyAPIKey(t, f.ownProjectID))

			require.NoError(t, f.serve(t, ctx, kind, f.ownProjectID, ownAssetID))
			requireForbidden(t, f.serve(t, ctx, kind, f.otherProjectID, otherAssetID))
		})
	}
}

func TestAssetDownload_SessionRequiresProjectRead(t *testing.T) {
	t.Parallel()

	for _, kind := range downloadableAssetKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			f := newProjectAuthzFixture(t)
			ownAssetID := f.createAsset(t, f.orgID, f.ownProjectID, kind)
			otherAssetID := f.createAsset(t, f.orgID, f.otherProjectID, kind)
			f.grantProjectRead(t, f.ownProjectID.String())
			ctx := f.authenticateSession(t)

			require.NoError(t, f.serve(t, ctx, kind, f.ownProjectID, ownAssetID))
			requireForbidden(t, f.serve(t, ctx, kind, f.otherProjectID, otherAssetID))
		})
	}
}

func TestAssetDownload_RejectsProjectInOtherOrganization(t *testing.T) {
	t.Parallel()

	for _, kind := range downloadableAssetKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			f := newProjectAuthzFixture(t)
			foreignOrgID := "org_" + uuid.NewString()
			_, err := orgrepo.New(f.ti.conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
				ID:             foreignOrgID,
				Name:           "Foreign Org",
				Slug:           "foreign-" + uuid.NewString()[:8],
				WorkosID:       pgtype.Text{String: "", Valid: false},
				Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
				CreationSource: pgtype.Text{String: "", Valid: false},
				DefaultHost:    pgtype.Text{String: "", Valid: false},
			})
			require.NoError(t, err)
			foreignProject, err := projectsrepo.New(f.ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
				Name:           "foreign",
				Slug:           "foreign-" + uuid.NewString()[:8],
				OrganizationID: foreignOrgID,
			})
			require.NoError(t, err)
			foreignAssetID := f.createAsset(t, foreignOrgID, foreignProject.ID, kind)
			f.grantProjectRead(t, authz.WildcardResource)
			ctx := f.authenticateSession(t)

			requireForbidden(t, f.serve(t, ctx, kind, foreignProject.ID, foreignAssetID))
		})
	}
}

func TestServeChatAttachment_ChatSessionTokenBoundToProject(t *testing.T) {
	t.Parallel()

	f := newProjectAuthzFixture(t)
	ownAssetID := f.createAsset(t, f.orgID, f.ownProjectID, "chat_attachment")
	otherAssetID := f.createAsset(t, f.orgID, f.otherProjectID, "chat_attachment")
	ctx := f.authenticateChatSession(t, chatsessions.ChatSessionClaims{
		OrgID:            f.orgID,
		ProjectID:        f.ownProjectID.String(),
		OrganizationSlug: f.orgSlug,
		ProjectSlug:      f.ownProjectSlug,
	})

	require.NoError(t, f.serve(t, ctx, "chat_attachment", f.ownProjectID, ownAssetID))
	requireForbidden(t, f.serve(t, ctx, "chat_attachment", f.otherProjectID, otherAssetID))
}

func TestServeChatAttachment_UserChatSessionTokenRequiresProjectRead(t *testing.T) {
	t.Parallel()

	f := newProjectAuthzFixture(t)
	assetID := f.createAsset(t, f.orgID, f.ownProjectID, "chat_attachment")
	claims := chatsessions.ChatSessionClaims{
		OrgID:            f.orgID,
		ProjectID:        f.ownProjectID.String(),
		OrganizationSlug: f.orgSlug,
		ProjectSlug:      f.ownProjectSlug,
		UserID:           f.userID,
		SessionID:        &f.sessionID,
	}

	requireForbidden(t, f.serve(t, f.authenticateChatSession(t, claims), "chat_attachment", f.ownProjectID, assetID))

	f.grantProjectRead(t, f.ownProjectID.String())
	require.NoError(t, f.serve(t, f.authenticateChatSession(t, claims), "chat_attachment", f.ownProjectID, assetID))
}

func TestCreateSignedChatAttachmentURL_PayloadProjectMustMatchHeaderProject(t *testing.T) {
	t.Parallel()

	f := newProjectAuthzFixture(t)
	ownAssetID := f.createAsset(t, f.orgID, f.ownProjectID, "chat_attachment")
	otherAssetID := f.createAsset(t, f.orgID, f.otherProjectID, "chat_attachment")
	// The user may read both projects; only the Gram-Project header binding
	// stops the payload from naming the other one.
	f.grantProjectRead(t, f.ownProjectID.String())
	f.grantProjectRead(t, f.otherProjectID.String())

	ctx, err := f.ti.service.APIKeyAuth(f.authenticateSession(t), f.ownProjectSlug, &security.APIKeyScheme{
		Name:           constants.ProjectSlugSecuritySchema,
		Scopes:         []string{},
		RequiredScopes: []string{},
	})
	require.NoError(t, err)

	sign := func(projectID, assetID uuid.UUID) error {
		_, err := f.ti.service.CreateSignedChatAttachmentURL(ctx, &assets.CreateSignedChatAttachmentURLForm{
			ApikeyToken:       nil,
			SessionToken:      nil,
			ProjectSlugInput:  nil,
			ChatSessionsToken: nil,
			ID:                assetID.String(),
			ProjectID:         projectID.String(),
			TTLSeconds:        nil,
		})
		if err != nil {
			return fmt.Errorf("sign chat attachment url: %w", err)
		}
		return nil
	}

	require.NoError(t, sign(f.ownProjectID, ownAssetID))
	requireForbidden(t, sign(f.otherProjectID, otherAssetID))
}
