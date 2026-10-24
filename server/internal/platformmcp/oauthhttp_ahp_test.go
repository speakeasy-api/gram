package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/stretchr/testify/require"
)

const ahpTestResource = "https://gram.example/ahp"
const ahpTestRedirect = "http://127.0.0.1:3000/callback"
const ahpTestVerifier = "abcdefghijklmnopqrstuvwxyz0123456789-ABCDEFG"

type ahpProtocolProjects struct {
	projects   []AHPProjectOption
	revoked    bool
	authorized []string
}

func (p *ahpProtocolProjects) EligibleProjects(context.Context, Principal) ([]AHPProjectOption, error) {
	return p.projects, nil
}
func (p *ahpProtocolProjects) AuthorizeProject(ctx context.Context, principal Principal, id string) (context.Context, error) {
	p.authorized = append(p.authorized, id)
	if !p.revoked && principal.OrganizationID == "org-1" && principal.UserID == "user-1" {
		for _, project := range p.projects {
			if id == project.ID {
				return ctx, nil
			}
		}
	}
	return ctx, ErrForbidden
}
func newAHPProtocolHTTP(t *testing.T, projects []AHPProjectOption) (*OAuthHTTP, *ahpProtocolProjects) {
	t.Helper()
	s := newTestOAuthHTTP(t)
	p := &ahpProtocolProjects{projects: projects}
	s.ahpProjects = p
	s.credentials.namespace = "ahp"
	require.NoError(t, testStore(t, s).RegisterClient(t.Context(), platformoauth.Client{ID: "client-1", Name: "Hook client", RedirectURIs: []string{ahpTestRedirect}}))
	return s, p
}
func ahpProtocolPost(handler http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func ahpProtocolConsent(t *testing.T, s *OAuthHTTP) (url.Values, string) {
	t.Helper()
	hash := sha256.Sum256([]byte(ahpTestVerifier))
	query := url.Values{"response_type": {"code"}, "client_id": {"client-1"}, "redirect_uri": {ahpTestRedirect}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}, "resource": {ahpTestResource}, "state": {"client-state"}}
	w := httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ahp/authorize?"+query.Encode(), nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	w = httptest.NewRecorder()
	s.IDPCallbackHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ahp/idp_callback?state="+url.QueryEscape(location.Query().Get("state"))+"&code=idp-code", nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	location, err = url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/ahp/select-organization", location.Path)
	w = httptest.NewRecorder()
	s.OrganizationSelectionHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, location.String(), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	require.Len(t, csrf, 2)
	form := url.Values{"state": {location.Query().Get("state")}, "csrf_token": {csrf[1]}, "organization_id": {"org-1"}}
	w = ahpProtocolPost(s.OrganizationSelectionHandler(), "/ahp/select-organization", form)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	location, err = url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/ahp/connect", location.Path)
	w = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, location.String(), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	form.Set("action", "approve")
	return form, w.Body.String()
}
func ahpProtocolCode(t *testing.T, s *OAuthHTTP, projectID string) string {
	t.Helper()
	form, _ := ahpProtocolConsent(t, s)
	form.Set("project_id", projectID)
	w := ahpProtocolPost(s.ConnectHandler(), "/ahp/connect", form)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	location, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "client-state", location.Query().Get("state"))
	code := location.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}
func ahpProtocolExchange(s *OAuthHTTP, code, resource string) *httptest.ResponseRecorder {
	return ahpProtocolPost(s.TokenHandler(), "/ahp/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {"client-1"}, "code": {code}, "code_verifier": {ahpTestVerifier}, "redirect_uri": {ahpTestRedirect}, "resource": {resource}})
}
func TestOAuthHTTPAHPConsentProjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		projects []AHPProjectOption
		selected string
	}{
		{"eligible default", []AHPProjectOption{{ID: "project-1", Name: "Default", Slug: "default"}, {ID: "project-2", Name: "Other", Slug: "other"}}, `value="project-1" selected`},
		{"no eligible default", []AHPProjectOption{{ID: "project-2", Name: "Other", Slug: "other"}}, `value="" disabled selected`},
		{"no eligible projects", nil, `value="" disabled selected`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := newAHPProtocolHTTP(t, tc.projects)
			_, body := ahpProtocolConsent(t, s)
			require.Contains(t, body, tc.selected)
			require.Regexp(t, `(?s)<select[^>]*name="project_id"[^>]*required`, body)
			for _, p := range tc.projects {
				require.Contains(t, body, `value="`+p.ID+`"`)
			}
			if len(tc.projects) == 0 {
				require.Regexp(t, `(?s)value="approve"\s+disabled`, body)
			}
		})
	}
	for _, id := range []string{"", "forged-project"} {
		t.Run("reject project "+id, func(t *testing.T) {
			t.Parallel()
			s, p := newAHPProtocolHTTP(t, []AHPProjectOption{{ID: "project-1", Slug: "default"}})
			form, _ := ahpProtocolConsent(t, s)
			form.Set("project_id", id)
			w := ahpProtocolPost(s.ConnectHandler(), "/ahp/connect", form)
			require.Equal(t, http.StatusFound, w.Code, w.Body.String())
			location, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "access_denied", location.Query().Get("error"))
			require.Empty(t, location.Query().Get("code"))
			require.Equal(t, []string{id}, p.authorized)
		})
	}
}
func TestOAuthHTTPAHPTokenAndLiveProjectRevocation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		projectID            string
		revokeBeforeExchange bool
	}{
		{"default refresh", "project-1", false},
		{"default exchange", "project-1", true},
		{"nondefault refresh", "project-2", false},
		{"nondefault exchange", "project-2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, p := newAHPProtocolHTTP(t, []AHPProjectOption{{ID: "project-1", Slug: "default"}, {ID: "project-2", Slug: "other"}})
			code := ahpProtocolCode(t, s, tc.projectID)
			if tc.revokeBeforeExchange {
				p.revoked = true
			}
			w := ahpProtocolExchange(s, code, ahpTestResource)
			if tc.revokeBeforeExchange {
				require.NotEqual(t, http.StatusOK, w.Code)
				require.Contains(t, w.Body.String(), "invalid_grant")
				return
			}
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var tokens struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokens))
			{
				stored, err := s.store.GetSessionByRefreshHash(t.Context(), "org-1", opaqueHash(tokens.RefreshToken))
				require.NoError(t, err)
				require.Equal(t, tc.projectID, stored.Connection.ProjectID)
				require.Equal(t, ahpTestResource, stored.Connection.ResourceURI)
			}
			claims, err := s.signer.ValidateExactAudience(tokens.AccessToken, ahpTestResource)
			require.NoError(t, err)
			require.Equal(t, []string{ahpTestResource}, []string(claims.Audience))
			require.Equal(t, ahpTestResource, claims.Issuer)
			_, err = s.signer.Validate(tokens.AccessToken, "https://gram.example/platform-mcp")
			require.Error(t, err)
			refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {"client-1"}, "refresh_token": {tokens.RefreshToken}, "resource": {ahpTestResource}}
			w = ahpProtocolPost(s.TokenHandler(), "/ahp/token", refresh)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tokens))
			{
				stored, err := s.store.GetSessionByRefreshHash(t.Context(), "org-1", opaqueHash(tokens.RefreshToken))
				require.NoError(t, err)
				require.Equal(t, tc.projectID, stored.Connection.ProjectID)
				require.Equal(t, ahpTestResource, stored.Connection.ResourceURI)
			}
			_, err = s.signer.ValidateExactAudience(tokens.AccessToken, ahpTestResource)
			require.NoError(t, err)
			p.revoked = true
			refresh.Set("refresh_token", tokens.RefreshToken)
			w = ahpProtocolPost(s.TokenHandler(), "/ahp/token", refresh)
			require.NotEqual(t, http.StatusOK, w.Code)
			require.Contains(t, w.Body.String(), "invalid_grant")
			require.Equal(t, []string{tc.projectID, tc.projectID, tc.projectID, tc.projectID}, p.authorized)
		})
	}
}
func TestOAuthHTTPAHPResourceAndNamespaceIsolation(t *testing.T) {
	t.Parallel()
	s, _ := newAHPProtocolHTTP(t, []AHPProjectOption{{ID: "project-1", Slug: "default"}})
	mcp := *s
	mcp.ahpProjects = nil
	codec := *s.credentials
	codec.namespace = ""
	mcp.credentials = &codec
	for _, kind := range []credentialKind{authorizationCodeCredential, refreshTokenCredential, accessJTICredential} {
		for _, pair := range [][2]*OAuthHTTP{{s, &mcp}, {&mcp, s}} {
			credential, err := pair[0].credentials.Issue(kind, "org-1", ahpTestResource)
			require.NoError(t, err)
			_, err = pair[1].credentials.decode(kind, credential)
			require.Error(t, err)
		}
	}
	form, _ := ahpProtocolConsent(t, s)
	w := httptest.NewRecorder()
	mcp.ConnectHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/platform-mcp/connect?state="+form.Get("state"), nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ahp/connect?state="+form.Get("state"), nil))
	require.Equal(t, http.StatusOK, w.Code)
	challenge, err := s.cache.Get(t.Context(), s.challengePrefix()+form.Get("state"))
	require.NoError(t, err)
	challenge.ID = "33333333-3333-4333-8333-333333333333"
	challenge.Namespace = ""
	require.NoError(t, mcp.cache.Store(t.Context(), challenge))
	w = httptest.NewRecorder()
	s.ConnectHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ahp/connect?state=33333333-3333-4333-8333-333333333333", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = httptest.NewRecorder()
	mcp.ConnectHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/platform-mcp/connect?state=33333333-3333-4333-8333-333333333333", nil))
	require.Equal(t, http.StatusOK, w.Code)

	code := ahpProtocolCode(t, s, "project-1")
	w = ahpProtocolExchange(s, code, "https://gram.example/platform-mcp")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")
	w = ahpProtocolExchange(&mcp, code, "https://gram.example/platform-mcp")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_grant")
	w = ahpProtocolExchange(s, code, ahpTestResource)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = httptest.NewRecorder()
	s.AuthorizeHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ahp/authorize?resource=https%3A%2F%2Fgram.example%2Fplatform-mcp", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "invalid_target")
}
