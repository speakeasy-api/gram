package adminmcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// allowOperation renders the stored feature preview but skips state
// revalidation, so these tests exercise only the browser approval checks.
type allowOperation struct{ *featureWriter }

func (allowOperation) revalidate(context.Context, pgx.Tx, Proposal) error { return nil }

func allowFeatureApprovals() map[WriteOperation]approvableOperation {
	return map[WriteOperation]approvableOperation{OperationSetOrganizationFeature: allowOperation{&featureWriter{}}} //nolint:exhaustive // Only the implemented operation is approvable.
}

func approvalRequest(method string, id uuid.UUID, form url.Values, session string) *http.Request {
	path := Path + "/proposals/" + id.String()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, "https://staff.example.test"+path, body)
	r.SetPathValue("id", id.String())
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://staff.example.test")
	}
	if session != "" {
		r.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: session})
	}
	return r
}

func approvalForm(t *testing.T, html string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, name := range []string{"challenge", "csrf_token", "digest"} {
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(html)
		require.Len(t, match, 2, name)
		form.Set(name, match[1])
	}
	form.Set("action", "approve")
	return form
}

func TestStaffProposalApprovalBrowserFlow(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_approval_browser")
	p, _, err := f.store.Create(t.Context(), f.owner, featureProposal(f.orgA, "browser", true), time.Now())
	require.NoError(t, err)
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}}
	memory := testenv.NewMemoryCache()
	authorization := NewStaffOAuthAuthorization(nil, nil, memory, verifier, f.cipher, staffAudience)
	approval := newStaffProposalApproval(f.store, authorization, memory, WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}) //nolint:exhaustive // Only selected write operations are enabled by this test.
	approval.operations = allowFeatureApprovals()
	handler := middleware.AdminOriginCheck(nil)(approval.Handler())

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, approvalRequest(http.MethodGet, p.ID, nil, ""))
	require.Equal(t, http.StatusFound, login.Code)
	require.Contains(t, login.Header().Get("Location"), "/admin/auth.login?return_to=")

	get := func(session string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, approvalRequest(http.MethodGet, p.ID, nil, session))
		return rec
	}
	page := get("browser-session")
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	require.Contains(t, page.Body.String(), p.ProposalDigest)
	require.Contains(t, page.Body.String(), p.Target.OrganizationID)
	require.Contains(t, page.Body.String(), "Turn the logs feature on")
	require.Contains(t, page.Body.String(), "<td>logs feature</td><td>Off</td><td>On</td>")
	require.NotContains(t, page.Body.String(), "browser-session")
	form := approvalForm(t, page.Body.String())
	post := func(values url.Values, session string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, approvalRequest(http.MethodPost, p.ID, values, session))
		return rec
	}

	wrongSession := post(form, "different-session")
	require.Equal(t, http.StatusForbidden, wrongSession.Code)
	current, err := f.store.GetForSubject(t.Context(), p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalPendingApproval, current.Status)

	page = get("browser-session")
	require.Equal(t, http.StatusOK, page.Code)
	form = approvalForm(t, page.Body.String())
	forged := approvalRequest(http.MethodPost, p.ID, form, "browser-session")
	forged.Header.Set("Origin", "https://other.example.test")
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, forged)
	require.Equal(t, http.StatusForbidden, blocked.Code)

	form.Set("csrf_token", "wrong")
	require.Equal(t, http.StatusForbidden, post(form, "browser-session").Code)
	page = get("browser-session")
	require.Equal(t, http.StatusOK, page.Code)
	form = approvalForm(t, page.Body.String())
	require.Equal(t, http.StatusOK, post(form, "browser-session").Code)
	require.Equal(t, http.StatusForbidden, post(form, "browser-session").Code, "the CSRF challenge is one use")
	approved, err := f.store.GetForSubject(t.Context(), p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, approved.Status)
}

func TestStaffProposalApprovalRequiresLinkedWritableConnection(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_approval_scope")
	p, _, err := f.store.Create(t.Context(), f.owner, featureProposal(f.orgA, "scope", true), time.Now())
	require.NoError(t, err)
	verifier := &fakeAdminVerifier{result: &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}}
	memory := testenv.NewMemoryCache()
	authorization := NewStaffOAuthAuthorization(nil, nil, memory, verifier, f.cipher, staffAudience)
	approval := newStaffProposalApproval(f.store, authorization, memory, WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationFeature: true}}) //nolint:exhaustive // Only selected write operations are enabled by this test.
	approval.operations = allowFeatureApprovals()
	handler := approval.Handler()

	get := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, approvalRequest(http.MethodGet, p.ID, nil, "browser-session"))
		return rec
	}
	page := get()
	require.Equal(t, http.StatusOK, page.Code)
	form := approvalForm(t, page.Body.String())

	err = repo.New(f.db).SetConnectionScopesFixture(t.Context(), repo.SetConnectionScopesFixtureParams{Scopes: []string{ScopeRead}, ID: f.owner.ConnectionID})
	require.NoError(t, err)
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, approvalRequest(http.MethodPost, p.ID, form, "browser-session"))
	require.Equal(t, http.StatusConflict, post.Code)
	require.Equal(t, http.StatusNotFound, get().Code, "a downgraded connection invalidates the proposal")

	// A separate proposal is needed after the scope downgrade invalidates the first.
	err = repo.New(f.db).SetConnectionScopesFixture(t.Context(), repo.SetConnectionScopesFixtureParams{Scopes: []string{ScopeRead, ScopeWrite}, ID: f.owner.ConnectionID})
	require.NoError(t, err)
	p, _, err = f.store.Create(t.Context(), f.owner, featureProposal(f.orgA, "reauth", true), time.Now())
	require.NoError(t, err)
	page = get()
	require.Equal(t, http.StatusOK, page.Code)
	form = approvalForm(t, page.Body.String())
	f.reauth()
	post = httptest.NewRecorder()
	handler.ServeHTTP(post, approvalRequest(http.MethodPost, p.ID, form, "browser-session"))
	require.Equal(t, http.StatusConflict, post.Code)
	current, err := f.store.GetForSubject(t.Context(), p.ID, proposalTestSubject)
	require.NoError(t, err)
	require.NotEqual(t, ProposalApproved, current.Status)

	// Check isolation on a fresh pending proposal: the prior generation was invalidated.
	generation, err := repo.New(f.db).GetConnectionGenerationFixture(t.Context(), f.owner.ConnectionID)
	require.NoError(t, err)
	f.owner.Generation = generation
	p, _, err = f.store.Create(t.Context(), f.owner, featureProposal(f.orgA, "other-staff", true), time.Now())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, get().Code)
	verifier.result.OIDCSubject = "other-staff"
	require.Equal(t, http.StatusNotFound, get().Code)
}
