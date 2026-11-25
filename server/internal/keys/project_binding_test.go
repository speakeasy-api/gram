package keys_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	httpkeys "github.com/speakeasy-api/gram/server/gen/http/keys/server"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestKeysService_CreateKey_ProjectBinding(t *testing.T) {
	t.Parallel()
	for _, scoped := range []bool{false, true} {
		name := "organization-wide"
		if scoped {
			name = "project-bound"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestKeysService(t)
			authCtx := testAuthContext(t, ctx)
			require.NotNil(t, authCtx.ProjectID)
			// Choose a different project from the ambient context.
			project, err := projectrepo.New(ti.conn).CreateProject(ctx, projectrepo.CreateProjectParams{
				OrganizationID: authCtx.ActiveOrganizationID, Name: "Selected project", Slug: "selected-project",
			})
			require.NoError(t, err)
			grants := []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)}
			var selected *string
			if scoped {
				id := project.ID.String()
				selected = &id
				grants = append(grants, authz.NewGrant(authz.ScopeProjectRead, id))
			}
			ctx = authztest.WithExactGrants(t, ctx, grants...)
			body := map[string]any{"name": name, "scopes": []string{"producer"}}
			if selected != nil {
				body["project_id"] = *selected
			}
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			request := httptest.NewRequest("POST", "/rpc/keys.create", bytes.NewReader(encoded))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Gram-Project", *authCtx.ProjectSlug)
			payload, err := httpkeys.DecodeCreateKeyRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)(request)
			require.NoError(t, err)
			key, err := ti.service.CreateKey(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, selected, key.ProjectID)
			require.Equal(t, []string{"producer"}, key.Scopes)
			listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
			require.NoError(t, err)
			require.Len(t, listed.Keys, 1)
			require.Equal(t, selected, listed.Keys[0].ProjectID)
			require.Nil(t, listed.Keys[0].Key)
		})
	}
}

func TestKeysService_CreateKey_RejectsInvalidProjectBinding(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"malformed", "unknown", "other-organization", "deleted", "no-project-grant", "wrong-project-grant", "no-admin-grant", "wrong-organization-grant"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestKeysService(t)
			authCtx := testAuthContext(t, ctx)
			projectID := authCtx.ProjectID.String()
			ctx = authztest.WithAdminGrants(ctx)
			want := oops.CodeNotFound
			switch kind {
			case "malformed":
				projectID = "not-a-uuid"
				want = oops.CodeBadRequest
			case "unknown":
				projectID = uuid.NewString()
			case "other-organization":
				authCtx.ActiveOrganizationID = "org_other_test"
			case "deleted":
				_, err := projectrepo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
				require.NoError(t, err)
			case "no-project-grant":
				ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
				want = oops.CodeForbidden
			case "wrong-project-grant":
				project, err := projectrepo.New(ti.conn).CreateProject(ctx, projectrepo.CreateProjectParams{
					OrganizationID: authCtx.ActiveOrganizationID, Name: "Selected project", Slug: "selected-project",
				})
				require.NoError(t, err)
				ctx = authztest.WithExactGrants(t, ctx,
					authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
					authz.NewGrant(authz.ScopeProjectRead, projectID),
				)
				projectID = project.ID.String()
				want = oops.CodeForbidden
			case "wrong-organization-grant":
				ctx = authztest.WithExactGrants(t, ctx,
					authz.NewGrant(authz.ScopeOrgAdmin, "org_other_test"),
					authz.NewGrant(authz.ScopeProjectRead, projectID),
				)
				want = oops.CodeForbidden
			case "no-admin-grant":
				ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, projectID)})
				want = oops.CodeForbidden
			}
			key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "rejected-key", Scopes: []string{"consumer"}, ProjectID: &projectID})
			require.Nil(t, key)
			var oe *oops.ShareableError
			require.ErrorAs(t, err, &oe)
			require.Equal(t, want, oe.Code)
			stored, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
			require.NoError(t, err)
			require.Empty(t, stored)
		})
	}
}
