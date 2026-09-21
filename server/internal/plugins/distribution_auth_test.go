package plugins_test

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDistributionPluginsHTTPSkillOnlyAuthorization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	skill, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *ac.ProjectID, Name: "http-skill", DisplayName: "HTTP skill", Summary: pgtype.Text{}})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "HTTP target", Description: new("HTTP projection description")})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	plugins.Attach(mux, ti.service)
	cases := []struct {
		path   string
		status int
	}{
		{"/rpc/plugins.listDistributionPlugins?skill_id=" + skill.ID.String(), http.StatusOK},
		{"/rpc/plugins.getDistributionPlugin?skill_id=" + skill.ID.String() + "&id=" + plugin.ID, http.StatusOK},
		{"/rpc/plugins.listPlugins", http.StatusForbidden},
		{"/rpc/plugins.getPlugin?id=" + plugin.ID, http.StatusForbidden},
	}
	for _, denied := range []struct {
		name, session string
		status        int
	}{
		{"no skill grants", *ac.SessionID, http.StatusForbidden},
		{"invalid session", "invalid-session", http.StatusUnauthorized},
	} {
		t.Run(denied.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/rpc/plugins.listDistributionPlugins?skill_id="+skill.ID.String(), nil).WithContext(authztest.WithExactGrants(t, ctx))
			req.Header.Set("Gram-Session", denied.session)
			req.Header.Set("Gram-Project", *ac.ProjectSlug)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			require.Equal(t, denied.status, rec.Code, rec.Body.String())
		})
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			// Exercise real session authentication, generated HTTP method routing, and
			// Gram-Project resolution with only an exact skill grant (no project grant).
			requestCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, skill.ID.String()))
			req := httptest.NewRequest(http.MethodGet, tc.path, nil).WithContext(requestCtx)
			req.Header.Set("Gram-Session", *ac.SessionID)
			req.Header.Set("Gram-Project", *ac.ProjectSlug)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status != http.StatusOK {
				return
			}
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			projections := []map[string]json.RawMessage{body}
			if strings.Contains(tc.path, "listDistributionPlugins") {
				require.Len(t, body, 1)
				projections = nil // Decode into fresh maps rather than retaining envelope keys.
				require.NoError(t, json.Unmarshal(body["plugins"], &projections))
			}
			found := false
			for _, projection := range projections {
				// Check raw transport keys: typed decoding alone ignores leaked fields.
				for key := range projection {
					require.Contains(t, []string{"id", "name", "description", "is_default"}, key)
				}
				var id string
				require.NoError(t, json.Unmarshal(projection["id"], &id))
				if id == plugin.ID {
					found = true
					encoded, err := json.Marshal(projection)
					require.NoError(t, err)
					require.JSONEq(t, `{"id":"`+plugin.ID+`","name":"HTTP target","description":"HTTP projection description","is_default":false}`, string(encoded))
				}
			}
			require.True(t, found, "response must include the created plugin")
		})
	}
}
