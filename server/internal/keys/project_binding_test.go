package keys_test

import (
	"bytes"
	"encoding/json"
	httpkeys "github.com/speakeasy-api/gram/server/gen/http/keys/server"
	goahttp "goa.design/goa/v3/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
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
			ctx = authztest.WithAdminGrants(ctx)
			var selected *string
			if scoped {
				id := project.ID.String()
				selected = &id
			}
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
	for _, kind := range []string{"malformed", "unknown", "other-organization", "deleted", "no-project-grant", "no-admin-grant"} {
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
				_, err := ti.conn.Exec(ctx, "UPDATE projects SET deleted_at = now() WHERE id = $1", authCtx.ProjectID)
				require.NoError(t, err)
			case "no-project-grant":
				ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
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
			var count int
			require.NoError(t, ti.conn.QueryRow(ctx, "SELECT count(*) FROM api_keys").Scan(&count))
			require.Zero(t, count)
		})
	}
}
