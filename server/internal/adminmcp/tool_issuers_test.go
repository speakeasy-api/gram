package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const testIssuerID = "a3fe1d35-4855-4dd8-a863-a0f45806e817"
const testSourceID = "b339d85d-6d0c-4b85-8a27-44b25998aaf4"

type recordingIssuerReader struct {
	recordingOrganizationReader
	pageInput      *gen.ListGlobalIssuersPayload
	detailInput    *gen.GetGlobalIssuerPayload
	duplicate      *gen.GetGlobalIssuerDuplicatePreflightPayload
	migration      *gen.GetGlobalIssuerMigratePreflightPayload
	page           *gen.ListGlobalRemoteSessionIssuersResult
	detail         *gen.GlobalRemoteSessionIssuer
	duplicates     *types.RemoteSessionIssuerDuplicatePreflight
	preflight      *gen.IssuerMigratePreflight
	candidates     *gen.ListIssuerConvergenceCandidatesResult
	candidateInput *gen.ListGlobalIssuerConvergenceCandidatesPayload
}

func (r *recordingIssuerReader) ListGlobalIssuers(_ context.Context, input *gen.ListGlobalIssuersPayload) (*gen.ListGlobalRemoteSessionIssuersResult, error) {
	r.pageInput = input
	return r.page, nil
}
func (r *recordingIssuerReader) GetGlobalIssuer(_ context.Context, input *gen.GetGlobalIssuerPayload) (*gen.GlobalRemoteSessionIssuer, error) {
	r.detailInput = input
	return r.detail, nil
}
func (r *recordingIssuerReader) GetGlobalIssuerDuplicatePreflight(_ context.Context, input *gen.GetGlobalIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	r.duplicate = input
	return r.duplicates, nil
}
func (r *recordingIssuerReader) GetGlobalIssuerMigratePreflight(_ context.Context, input *gen.GetGlobalIssuerMigratePreflightPayload) (*gen.IssuerMigratePreflight, error) {
	r.migration = input
	return r.preflight, nil
}

func (r *recordingIssuerReader) ListGlobalIssuerConvergenceCandidates(_ context.Context, input *gen.ListGlobalIssuerConvergenceCandidatesPayload) (*gen.ListIssuerConvergenceCandidatesResult, error) {
	r.candidateInput = input
	return r.candidates, nil
}

// issuerToolCall exercises the same runtime boundary as a real authenticated tool call.
func issuerToolCall(t *testing.T, reads OrganizationReader, name, args string, verified bool) (string, json.RawMessage) {
	t.Helper()
	principal := staffPrincipal()
	if verified {
		principal.staff = &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: principal.Email}
	}
	auth := &testAuthenticator{principal: principal}
	req := httptest.NewRequest(http.MethodPost, Path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp := httptest.NewRecorder()
	NewRuntime(auth, "", reads).Handler().ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	var message struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
			IsError           bool            `json:"isError"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &message))
	if message.Result.IsError {
		require.Contains(t, resp.Body.String(), `"isError":true`)
	}
	return resp.Body.String(), message.Result.StructuredContent
}

func TestIssuerReadsRequireVerifiedStaffAndBoundPage(t *testing.T) {
	t.Parallel()
	secret := "never-return-this-credential"
	cursor := "next-page"
	reads := &recordingIssuerReader{page: &gen.ListGlobalRemoteSessionIssuersResult{Items: []*gen.GlobalRemoteSessionIssuer{{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID, Slug: "issuer", Issuer: "https://issuer.example.test", TokenEndpoint: &secret}, GlobalClientCount: 2}}, NextCursor: &cursor}}
	body, data := issuerToolCall(t, reads, "list_global_issuers", `{"limit":2,"cursor":"previous"}`, true)
	require.NotContains(t, body, secret)
	require.Equal(t, 2, *reads.pageInput.Limit)
	require.Equal(t, "previous", *reads.pageInput.Cursor)
	var page IssuerPage
	require.NoError(t, json.Unmarshal(data, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, testIssuerID, page.Items[0].ID)
	require.Equal(t, &cursor, page.NextCursor)

	reads.pageInput = nil
	body, _ = issuerToolCall(t, reads, "list_global_issuers", `{}`, false)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.pageInput)
	for _, args := range []string{`{"limit":51}`, `{"limit":-1}`} {
		body, _ = issuerToolCall(t, reads, "list_global_issuers", args, true)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.pageInput)
	}

	oversized := strings.Repeat("c", maxIssuerCursor+1)
	reads.page.NextCursor = &oversized
	body, _ = issuerToolCall(t, reads, "list_global_issuers", `{}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, oversized)
}

func TestIssuerConvergenceCandidatesRedactAndBound(t *testing.T) {
	t.Parallel()
	secret := "private-endpoint-value"
	cursor := "next-page"
	reads := &recordingIssuerReader{candidates: &gen.ListIssuerConvergenceCandidatesResult{
		Items:      []*gen.IssuerConvergenceCandidate{{Issuer: &types.RemoteSessionIssuer{ID: testSourceID, TokenEndpoint: &secret}, OrganizationID: "org-a", ClientCount: 2, EndpointMismatches: []*types.IssuerFieldMismatch{{Field: "token_endpoint", SourceValue: &secret}}}},
		NextCursor: &cursor,
	}}
	body, data := issuerToolCall(t, reads, "list_global_issuer_convergence_candidates", `{"target_id":"`+testIssuerID+`","limit":1}`, true)
	require.NotContains(t, body, secret)
	require.Equal(t, testIssuerID, reads.candidateInput.TargetID)
	var page IssuerConvergencePage
	require.NoError(t, json.Unmarshal(data, &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, testSourceID, page.Items[0].SourceID)
	require.Equal(t, []string{"token_endpoint"}, page.Items[0].EndpointMismatchFields)
	require.True(t, page.PossiblyIncomplete)
	oversized := strings.Repeat("c", maxIssuerCursor+1)
	for _, args := range []string{
		`{"target_id":"not-a-uuid"}`,
		`{"target_id":"` + testIssuerID + `","limit":51}`,
		`{"target_id":"` + testIssuerID + `","limit":-1}`,
		`{"target_id":"` + testIssuerID + `","cursor":"` + oversized + `"}`,
	} {
		reads.candidateInput = nil
		body, _ = issuerToolCall(t, reads, "list_global_issuer_convergence_candidates", args, true)
		require.Contains(t, body, `"isError":true`, args)
		require.Nil(t, reads.candidateInput, args)
	}

	body, _ = issuerToolCall(t, reads, "list_global_issuer_convergence_candidates", `{"target_id":"`+testIssuerID+`"}`, true)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, 20, *reads.candidateInput.Limit)

	reads.candidates.NextCursor = &oversized
	body, _ = issuerToolCall(t, reads, "list_global_issuer_convergence_candidates", `{"target_id":"`+testIssuerID+`"}`, true)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, oversized)

	reads.candidates.NextCursor = &cursor
	reads.candidates.Items[0].ClientCount = -1
	body, _ = issuerToolCall(t, reads, "list_global_issuer_convergence_candidates", `{"target_id":"`+testIssuerID+`"}`, true)
	require.Contains(t, body, `"isError":true`)
}

func TestIssuerDetailAndPreflightsValidateTargetsAndRedact(t *testing.T) {
	t.Parallel()
	secret := "never-return-this-value"
	reads := &recordingIssuerReader{
		detail:     &gen.GlobalRemoteSessionIssuer{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID, Issuer: "https://issuer.example.test", TokenEndpoint: &secret}},
		duplicates: &types.RemoteSessionIssuerDuplicatePreflight{Matches: []*types.RemoteSessionIssuerDuplicateMatch{{ID: testIssuerID, Issuer: "https://issuer.example.test"}}},
		preflight:  &gen.IssuerMigratePreflight{CanMigrate: false, ClientCount: 2, EndpointMismatches: []*types.IssuerFieldMismatch{{Field: "token_endpoint", SourceValue: &secret}}, McpServerNames: []string{secret}},
	}
	body, _ := issuerToolCall(t, reads, "get_global_issuer", `{"id":"`+testIssuerID+`"}`, true)
	require.NotContains(t, body, secret)
	require.Equal(t, testIssuerID, reads.detailInput.ID)
	body, data := issuerToolCall(t, reads, "check_global_issuer_duplicates", `{"issuer":"https://issuer.example.test"}`, true)
	require.NotContains(t, body, secret)
	require.Equal(t, "https://issuer.example.test", *reads.duplicate.Issuer)
	var duplicate DuplicateIssuerResult
	require.NoError(t, json.Unmarshal(data, &duplicate))
	require.Len(t, duplicate.Matches, 1)
	body, data = issuerToolCall(t, reads, "check_global_issuer_migration", `{"source_id":"`+testSourceID+`","target_id":"`+testIssuerID+`"}`, true)
	require.NotContains(t, body, secret)
	var preflight IssuerMigrationResult
	require.NoError(t, json.Unmarshal(data, &preflight))
	require.Equal(t, []string{"token_endpoint"}, preflight.EndpointMismatches)
	require.Equal(t, 2, preflight.ClientCount)
	reads.detailInput, reads.duplicate, reads.migration = nil, nil, nil
	for _, tc := range []struct{ name, args string }{{"get_global_issuer", `{"id":"slug"}`}, {"check_global_issuer_duplicates", `{"issuer":"bad"}`}, {"check_global_issuer_migration", `{"source_id":"` + testIssuerID + `","target_id":"` + testIssuerID + `"}`}} {
		body, _ = issuerToolCall(t, reads, tc.name, tc.args, true)
		require.Contains(t, body, `"isError":true`)
	}
	require.Nil(t, reads.detailInput)
	require.Nil(t, reads.duplicate)
	require.Nil(t, reads.migration)

	longName := strings.Repeat("n", 257)
	negative := -1
	for _, malformed := range []*gen.GlobalRemoteSessionIssuer{
		{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID, Slug: strings.Repeat("s", 257)}},
		{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID, Name: &longName}},
		{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID, Issuer: "https://" + strings.Repeat("i", 2048)}},
		{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID}, TenantClientCount: -1},
		{Issuer: &types.RemoteSessionIssuer{ID: testIssuerID}, EmaBindingCount: &negative},
	} {
		reads.detail = malformed
		body, _ = issuerToolCall(t, reads, "get_global_issuer", `{"id":"`+testIssuerID+`"}`, true)
		require.Contains(t, body, `"isError":true`)
	}
}
