package skills_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

//nolint:paralleltest,tparallel // Subtests mutate the same distribution edge and verify its state.
func TestSkillsAPIKeyAuthDelegatesProjectAccessToScopedHandlers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "auth-boundary-skill", "Authentication boundary.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "auth-boundary-target")
	for _, tc := range []struct {
		name     string
		scope    authz.Scope
		resource string
		write    bool
	}{
		{"project read", authz.ScopeSkillRead, ti.projectID.String(), false},
		{"project write", authz.ScopeSkillWrite, ti.projectID.String(), false},
		{"resource read", authz.ScopeSkillRead, created.Skill.ID, false},
		{"resource write", authz.ScopeSkillWrite, created.Skill.ID, false},
		{"plugin write and resource read", authz.ScopeSkillRead, created.Skill.ID, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grants := []authz.Grant{authz.NewGrant(tc.scope, tc.resource)}
			if tc.write {
				grants = append(grants, authz.NewGrant(authz.ScopePluginWrite, ti.projectID.String()))
			}
			scoped := authztest.WithExactGrants(t, ctx, grants...)
			authorize := func(method string) context.Context {
				t.Helper()
				authenticated, err := ti.service.APIKeyAuth(context.WithValue(scoped, goa.MethodKey, method), *ti.authContext.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
				require.NoError(t, err)
				return authenticated
			}
			got, err := ti.service.Get(authorize("get"), &gen.GetPayload{ID: created.Skill.ID})
			require.NoError(t, err)
			require.Equal(t, created.Skill.ID, got.Skill.ID)
			_, err = ti.service.ListDistributions(authorize("listDistributions"), &gen.ListDistributionsPayload{SkillID: &created.Skill.ID, Limit: 50})
			require.NoError(t, err)
			_, err = ti.service.Distribute(authorize("distribute"), &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String())})
			if tc.write {
				require.NoError(t, err)
			} else {
				requireOopsCode(t, err, oops.CodeForbidden)
			}
			err = ti.service.Undistribute(authorize("undistribute"), &gen.UndistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String())})
			if tc.write {
				require.NoError(t, err)
			} else {
				requireOopsCode(t, err, oops.CodeForbidden)
			}
		})
	}
}

func TestSkillsAPIKeyAuthRetainsTenantScoping(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	cloned := *ti.authContext
	cloned.ActiveOrganizationID = "other-organization"
	mismatched := contextvalues.SetAuthContext(ctx, &cloned)
	_, err := ti.service.APIKeyAuth(context.WithValue(mismatched, goa.MethodKey, "get"), *ti.authContext.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSkillsAPIKeyAuthCollectionProjectResources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	_, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	for _, method := range []string{"list", "listDistributions", "create"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, allowProject, blockedProject string
				allowed                            bool
			}{
				{"other project grant", otherProjectID.String(), "", false},
				{"matching project grant", ti.projectID.String(), "", true},
				{"matching project exclusion", ti.projectID.String(), ti.projectID.String(), false},
				{"unrelated project exclusion", ti.projectID.String(), otherProjectID.String(), true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					// Project authentication mutates request-local auth state.
					authContext := *ti.authContext
					ctx := contextvalues.SetAuthContext(ctx, &authContext)
					grant := authz.NewGrant(authz.ScopeSkillWrite, tc.allowProject)
					grants := []authz.Grant{grant}
					if method == "create" {
						grants = append(grants, authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String()))
					}
					if tc.blockedProject != "" {
						scope := authz.ScopeSkillBlockedRead
						if method == "create" {
							scope = authz.ScopeSkillBlockedWrite
						}
						blocked := authz.NewGrant(scope, tc.blockedProject)
						grants = append(grants, blocked)
					}
					scoped := authztest.WithExactGrants(t, ctx, grants...)
					authenticated, err := ti.service.APIKeyAuth(context.WithValue(scoped, goa.MethodKey, method), *ti.authContext.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
					require.NoError(t, err, "authentication must reach the scoped handler")
					switch method {
					case "list":
						_, err = ti.service.List(authenticated, &gen.ListPayload{Limit: 10})
					case "listDistributions":
						_, err = ti.service.ListDistributions(authenticated, &gen.ListDistributionsPayload{Limit: 50})
					case "create":
						name := "dimension-created"
						if tc.name == "unrelated project exclusion" {
							name = "dimension-unrelated"
						}
						_, err = ti.service.Create(authenticated, &gen.CreatePayload{Content: skillManifest(name, "Dimension scope.", "body")})
					}
					if tc.allowed {
						require.NoError(t, err)
					} else {
						requireOopsCode(t, err, oops.CodeForbidden)
					}
				})
			}
		})
	}
}

func TestPluginWriteCannotAuthorSkills(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "plugin-write-authoring", "Authoring boundary.")
	// Project read passes the transport gate, but cannot replace skill write.
	scoped := authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String()),
		authz.NewGrant(authz.ScopePluginWrite, ti.projectID.String()),
	)
	_, err := ti.service.Create(scoped, &gen.CreatePayload{Content: capturedManifest("unauthorized-skill", "Authoring boundary.", "body")})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Update(scoped, &gen.UpdatePayload{ID: created.Skill.ID, Name: "unauthorized-rename", DisplayName: "Unauthorized rename"})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSkillsAuthoringAPIKeyAuthRetainsProjectRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	for _, method := range []string{"create", "addVersion", "restoreVersion", "update", "triggerSuggestion", "approveSuggestion", "dismissSuggestion", "approveAllSuggestions", "archive", "share", "unshare"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			// Project authentication mutates request-local auth state.
			authContext := *ti.authContext
			ctx := contextvalues.SetAuthContext(ctx, &authContext)
			scoped := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()))
			_, err := ti.service.APIKeyAuth(context.WithValue(scoped, goa.MethodKey, method), *ti.authContext.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
			requireOopsCode(t, err, oops.CodeForbidden)
			scoped = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()), authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String()))
			_, err = ti.service.APIKeyAuth(context.WithValue(scoped, goa.MethodKey, method), *ti.authContext.ProjectSlug, &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema})
			require.NoError(t, err)
		})
	}
}
