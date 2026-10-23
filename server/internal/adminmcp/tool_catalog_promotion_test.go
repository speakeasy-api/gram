package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type recordingCatalogPromotionReader struct {
	listCalls  int
	getCalls   int
	cursor     string
	limit      int
	target     catalogpromotion.Target
	candidates []catalogpromotion.Candidate
	next       string
	candidate  catalogpromotion.Candidate
	err        error
}

func (r *recordingCatalogPromotionReader) ListCandidates(_ context.Context, cursor string, limit int) ([]catalogpromotion.Candidate, string, error) {
	r.listCalls++
	r.cursor, r.limit = cursor, limit
	return r.candidates, r.next, r.err
}

func (r *recordingCatalogPromotionReader) GetCandidate(_ context.Context, target catalogpromotion.Target) (catalogpromotion.Candidate, error) {
	r.getCalls++
	r.target = target
	return r.candidate, r.err
}

func catalogPromotionToolCall(t *testing.T, reader catalogPromotionReader, name, args string, verified bool) (string, json.RawMessage) {
	t.Helper()
	principal := staffPrincipal()
	if verified {
		principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	}
	runtime := NewRuntime(&testAuthenticator{principal: principal}, "")
	registerCatalogPromotionTools(runtime.server, reader)
	request := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &message))
	return response.Body.String(), message.Result.StructuredContent
}

func TestCatalogPromotionReadsRequireLiveStaffVerification(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{}
	for _, name := range []string{"list_registry_promotion_candidates", "get_registry_promotion_candidate"} {
		body, _ := catalogPromotionToolCall(t, reader, name, `{"organization_id":"org-example","project_id":"`+testRegistryID+`","mcp_server_id":"`+testRegistryID+`"}`, false)
		require.Contains(t, body, `"isError":true`)
	}
	require.Zero(t, reader.listCalls)
	require.Zero(t, reader.getCalls)
}

func TestCatalogPromotionListBoundsAndCursor(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{candidates: []catalogpromotion.Candidate{{ID: testRegistryID, OrganizationID: "org-example", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "aabbccdd", Status: "candidate", Eligible: true, Reason: "eligible", ObservedSuccesses: 12, ObservedDays: 2}}, next: "next-page"}
	body, data := catalogPromotionToolCall(t, reader, "list_registry_promotion_candidates", `{"cursor":"previous-page","limit":3}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "previous-page", reader.cursor)
	require.Equal(t, 3, reader.limit)
	var page CatalogPromotionPage
	require.NoError(t, json.Unmarshal(data, &page))
	require.Len(t, page.Candidates, 1)
	require.Equal(t, "next-page", page.NextCursor)
	require.Equal(t, int64(12), page.Candidates[0].ObservedSuccesses)
}

func TestCatalogPromotionListRejectsInvalidBoundsBeforeReading(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`{"limit":51}`, `{"limit":-1}`, `{"cursor":"` + strings.Repeat("c", maxRegistryCursor+1) + `"}`} {
		reader := &recordingCatalogPromotionReader{}
		body, _ := catalogPromotionToolCall(t, reader, "list_registry_promotion_candidates", args, true)
		require.Contains(t, body, `"isError":true`)
		require.Zero(t, reader.listCalls)
	}
}

func TestCatalogPromotionListRejectsOversizedReaderOutput(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{candidates: make([]catalogpromotion.Candidate, 2)}
	body, _ := catalogPromotionToolCall(t, reader, "list_registry_promotion_candidates", `{"limit":1}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, `"observed_successes"`)
}

func TestCatalogPromotionDetailUsesExactTargetAndMasksErrors(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{candidate: catalogpromotion.Candidate{ID: testRegistryID, OrganizationID: "org-example", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "aabbccdd", Status: "candidate", Reason: "eligible", Eligible: true}}
	args := `{"organization_id":"org-example","project_id":"` + testRegistryID + `","mcp_server_id":"` + testRegistryID + `"}`
	body, _ := catalogPromotionToolCall(t, reader, "get_registry_promotion_candidate", args, true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, catalogpromotion.Target{OrganizationID: "org-example", ProjectID: uuid.MustParse(testRegistryID), MCPServerID: uuid.MustParse(testRegistryID)}, reader.target)
	reader.err = errors.New("sensitive upstream credential")
	body, _ = catalogPromotionToolCall(t, reader, "get_registry_promotion_candidate", args, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "sensitive upstream credential")
}

func TestCatalogPromotionDetailRejectsNonCanonicalUUID(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{}
	args := `{"organization_id":"org-example","project_id":"` + strings.ToUpper(testRegistryID) + `","mcp_server_id":"` + testRegistryID + `"}`
	body, _ := catalogPromotionToolCall(t, reader, "get_registry_promotion_candidate", args, true)
	require.Contains(t, body, `"isError":true`)
	require.Zero(t, reader.getCalls)
}

func TestCatalogPromotionDetailRejectsDifferentTenantResult(t *testing.T) {
	t.Parallel()
	reader := &recordingCatalogPromotionReader{candidate: catalogpromotion.Candidate{ID: testRegistryID, OrganizationID: "another-organization", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "aabbccdd", Status: "candidate", Reason: "eligible", Eligible: true}}
	args := `{"organization_id":"org-example","project_id":"` + testRegistryID + `","mcp_server_id":"` + testRegistryID + `"}`
	body, _ := catalogPromotionToolCall(t, reader, "get_registry_promotion_candidate", args, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "another-organization")
}

func TestCatalogPromotionProjectionRejectsUnsafeOutput(t *testing.T) {
	t.Parallel()
	for _, candidate := range []catalogpromotion.Candidate{
		{ID: testRegistryID, OrganizationID: "org-example", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "aabbccdd", Status: "candidate", Reason: "eligible", DocsURL: "https://private.example.com?credential=secret"},
		{ID: testRegistryID, OrganizationID: "org-example", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "aabbccdd", Status: "candidate", Reason: "eligible", DeliveryError: "private upstream failure"},
	} {
		_, ok := projectCatalogPromotion(candidate)
		require.False(t, ok)
	}
}

func TestCatalogPromotionProjectionPreservesDeletedSourceHistory(t *testing.T) {
	t.Parallel()
	source := catalogpromotion.Candidate{ID: testRegistryID, OrganizationID: "org-example", SourceRevision: "aabbccdd", Status: "promoted", Reason: "already_promoted", RegistryEntryID: testRegistryID, DocsStatus: "queued"}
	safe, ok := projectCatalogPromotion(source)
	require.True(t, ok)
	require.Equal(t, testRegistryID, safe.RegistryEntryID)
	require.Empty(t, safe.ProjectID)
	source.Status = "candidate"
	_, ok = projectCatalogPromotion(source)
	require.False(t, ok)
}

func TestCatalogPromotionProjectsUnobservedCandidate(t *testing.T) {
	t.Parallel()
	candidate := catalogpromotion.Candidate{ID: testRegistryID, OrganizationID: "org-example", ProjectID: testRegistryID, MCPServerID: testRegistryID, RemoteMCPServerID: testRegistryID, SourceRevision: "unobserved", Status: "candidate", Reason: "telemetry_unavailable"}
	_, ok := projectCatalogPromotion(candidate)
	require.True(t, ok)
	candidate.Eligible = true
	_, ok = projectCatalogPromotion(candidate)
	require.False(t, ok)
}
