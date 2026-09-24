package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestSupportMatrixPersistence(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot.Methods, 13)
	require.Len(t, snapshot.Products, 25)
	require.Len(t, snapshot.Capabilities, 12)
	require.Empty(t, snapshot.Draft.Mappings)
	require.Equal(t, "supported", snapshot.Draft.References["device"]["org"].Status)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	same, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, snapshot.Revision, same.Revision)

	snapshot.Draft.Mappings["device/claude-code-web"] = &gen.SupportMapping{Applicability: "applicable", Conditions: "Team plan", Accounts: map[string]string{"personal": "unsupported"}, Facts: map[string]*gen.SupportFact{"org": {Status: "partial", Note: "Requires setup", Verify: true}}}
	snapshot.Draft.References["device"]["org"] = &gen.SupportFact{Status: "partial", Note: "Reference caveat", Verify: true}
	saved, err := svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.NoError(t, err)
	require.NotEqual(t, snapshot.Revision, saved.Revision)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	loaded, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	require.Equal(t, "Requires setup", loaded.Draft.Mappings["device/claude-code-web"].Facts["org"].Note)
	require.Equal(t, "Reference caveat", loaded.Methods[0].Facts["org"].Note)
	require.Equal(t, map[string]string{"personal": "unsupported"}, loaded.Draft.Mappings["device/claude-code-web"].Accounts)

	_, err = svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.ErrorContains(t, err, "Support matrix changed")
	saved.Draft.Mappings["device/claude-code-web"].Facts["org"].Note = ""
	_, err = svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: saved.Revision, Draft: saved.Draft})
	require.ErrorContains(t, err, "partial coverage requires notes")
	unchanged, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, loaded, unchanged)
}

func TestSupportMatrixAccountEligibility(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)

	// The two rows the prose plan notes could not describe now answer for every
	// account type, which is what kept them blank in the matrix.
	for _, method := range []string{"hooks", "litellm"} {
		require.Equal(t, map[string]string{"personal": "supported", "team": "supported", "enterprise": "supported"}, snapshot.Draft.Accounts[method], method)
	}
	require.Equal(t, map[string]string{"personal": "unsupported", "team": "unsupported", "enterprise": "supported"}, snapshot.Draft.Accounts["openai-api"])
	for _, method := range snapshot.Methods {
		require.Equal(t, snapshot.Draft.Accounts[method.ID], method.Accounts, method.ID)
		require.Len(t, method.Accounts, 3, method.ID)
	}

	snapshot.Draft.Accounts["litellm"] = map[string]string{"personal": "unknown", "team": "supported", "enterprise": "supported"}
	saved, err := svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.NoError(t, err)
	require.Equal(t, "unknown", saved.Draft.Accounts["litellm"]["personal"])

	// Seeding must not undo that edit, but it must still correct the catalog's
	// own presentation fields.
	require.NoError(t, SeedSupportMatrix(ctx, db))
	reseeded, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, saved.Draft.Accounts, reseeded.Draft.Accounts)
	require.Equal(t, saved.Revision, reseeded.Revision)
}

func TestSupportMatrixSeedRefreshesCatalogPresentation(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	stale := []byte(`{"products":[{"id":"claude-chat-web","name":"Stale name","vendor":"Stale","family":"Stale","surface":"Stale"}]}`)
	require.NoError(t, repo.New(db).SeedSupportPlatforms(ctx, stale))
	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	for _, product := range snapshot.Products {
		if product.ID == "claude-chat-web" {
			require.Equal(t, "Claude Chat · Web", product.Name)
			require.Equal(t, "Anthropic", product.Vendor)
			require.Equal(t, "Claude Chat", product.Family)
			return
		}
	}
	t.Fatal("claude-chat-web missing from the catalog")
}

func TestSupportMatrixLiftsLabelledAccountConditions(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	snapshot.Draft.Mappings["device/claude-code-cli"] = &gen.SupportMapping{
		Applicability: "applicable",
		Conditions:    "macOS only; Personal accounts: unsupported; Team plans: supported; Enterprise plans: unknown",
		Accounts:      map[string]string{},
		Facts:         map[string]*gen.SupportFact{},
	}
	_, err = svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.NoError(t, err)

	require.NoError(t, SeedSupportMatrix(ctx, db))
	lifted, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	mapping := lifted.Draft.Mappings["device/claude-code-cli"]
	require.Equal(t, "macOS only", mapping.Conditions)
	require.Equal(t, map[string]string{"personal": "unsupported", "team": "supported", "enterprise": "unknown"}, mapping.Accounts)

	// The lift is a one-off: a later edit back to the empty map stands.
	mapping.Accounts = map[string]string{}
	saved, err := svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: lifted.Revision, Draft: lifted.Draft})
	require.NoError(t, err)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	after, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, saved.Revision, after.Revision)
	require.Empty(t, after.Draft.Mappings["device/claude-code-cli"].Accounts)
}

func TestSplitLabelledAccountConditions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		conditions string
		accounts   map[string]string
		remaining  string
	}{
		{
			name:       "written by the editor",
			conditions: "macOS only; Personal accounts: unsupported; Team plans: supported; Enterprise plans: unknown",
			accounts:   map[string]string{"personal": "unsupported", "team": "supported", "enterprise": "unknown"},
			remaining:  "macOS only",
		},
		{
			name:       "imported symbols",
			conditions: "Personal accounts: ☠️; Team plans: ✅; Enterprise accounts: ✅",
			accounts:   map[string]string{"personal": "unsupported", "team": "supported", "enterprise": "supported"},
			remaining:  "",
		},
		{
			name:       "enterprise only narrows every other account",
			conditions: "Personal accounts: enterprise only; Team plans: enterprise only; Enterprise plans: enterprise only",
			accounts:   map[string]string{"personal": "unsupported", "team": "unsupported", "enterprise": "supported"},
			remaining:  "",
		},
		{
			name:       "unreadable claims stay visible as unknown",
			conditions: "WIP; Team plans: ask legal",
			accounts:   map[string]string{"team": "unknown"},
			remaining:  "WIP",
		},
		{
			name:       "no claims at all",
			conditions: "cost only; no hooks",
			accounts:   map[string]string{},
			remaining:  "cost only; no hooks",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			accounts, remaining := splitLabelledAccountConditions(test.conditions)
			require.Equal(t, test.accounts, accounts)
			require.Equal(t, test.remaining, remaining)
		})
	}
}

func TestSupportMatrixHTTP(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/admin/supportMatrix.get"
		if method == http.MethodPost {
			path = "/admin/supportMatrix.update"
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("{}")))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	rec := request(http.MethodGet, "/admin/supportMatrix.get", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var snapshot gen.SupportMatrix
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
	for index, body := range []string{
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{},"accounts":{}},"extra":true}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{"bad/platform":{"applicability":"applicable","conditions":"","accounts":{},"facts":{}}},"references":{},"accounts":{}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{"device":{"org":{"status":"wrong","note":"","verify":false}}},"accounts":{}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{"device":{"org":{"status":"supported","note":"\u0000","verify":false}}},"accounts":{}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{"device/claude-code-cli":{"applicability":"applicable","conditions":"\u0000","accounts":{},"facts":{}}},"references":{},"accounts":{}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{},"accounts":{"device":{"personal":"maybe"}}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{},"accounts":{"nope":{"personal":"supported"}}}}`,
	} {
		rec = request(http.MethodPost, "/admin/supportMatrix.update", body)
		expected := http.StatusBadRequest
		if index == 1 || (index >= 3 && index != 5) {
			expected = http.StatusUnprocessableEntity
		}
		require.Equal(t, expected, rec.Code, rec.Body.String())
	}
	body := []byte(`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{"device/claude-code-cli":{"applicability":"applicable","conditions":"","accounts":{},"facts":{}}},"references":{},"accounts":{}}}`)
	rec = request(http.MethodPost, "/admin/supportMatrix.update", string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, bytes.Contains(rec.Body.Bytes(), []byte("device/claude-code-cli")))
	rec = request(http.MethodPost, "/admin/supportMatrix.update", string(body))
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestValidateSupportDraftUnicodeText(t *testing.T) {
	t.Parallel()
	var catalog gen.SupportMatrix
	require.NoError(t, json.Unmarshal(supportCatalog, &catalog))
	fact := &gen.SupportFact{Status: "supported", Note: strings.Repeat("🙂", 10000), Verify: false}
	mapping := &gen.SupportMapping{Applicability: "applicable", Conditions: strings.Repeat("é", 10000), Accounts: map[string]string{}, Facts: map[string]*gen.SupportFact{"org": fact}}
	draft := &gen.SupportDraft{Mappings: map[string]*gen.SupportMapping{"device/claude-code-cli": mapping}, References: map[string]map[string]*gen.SupportFact{"device": {"org": fact}}, Accounts: map[string]map[string]string{}}
	require.NoError(t, validateSupportDraft(draft, &catalog))
	fact.Note += "🙂"
	require.ErrorContains(t, validateSupportDraft(draft, &catalog), "10000 characters")
	fact.Note = "valid"
	mapping.Conditions += "é"
	require.ErrorContains(t, validateSupportDraft(draft, &catalog), "10000 characters")
	mapping.Conditions = "valid"
	fact.Note = "before\x00after"
	require.ErrorContains(t, validateSupportDraft(draft, &catalog), "NUL")
	fact.Note = "valid"
	mapping.Conditions = "before\x00after"
	require.ErrorContains(t, validateSupportDraft(draft, &catalog), "NUL")
}
