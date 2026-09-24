package mcpapproval_test

import (
	"encoding/json"

	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestReadPlatformReview_ClosedSummaryDoesNotLeakStoredText(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	const (
		target       = "https://sensitive-target.invalid/mcp?token=target-secret"
		requester    = "sensitive-requester"
		note         = "sensitive requester note"
		secretName   = "SENSITIVE_API_TOKEN"
		scope        = "sensitive:scope"
		toolName     = "sensitive_tool_name"
		claimText    = "sensitive research claim"
		citationURL  = "https://citation.invalid/sensitive"
		citationName = "Sensitive Citation Title"
		traceQuery   = "sensitive trace query"
	)
	evidenceDocument := `{
		"identity":{"kind":"package","artifact_ref":"npm:sensitive-package@1.2.3","version_pinned":true,"package_name":"sensitive-package","package_version":"1.2.3"},
		"package":{"registry":"npm","name":"sensitive-package"},
		"repository":{"url":"https://repository.invalid/sensitive","host":"repository.invalid","owner":"secret-owner","name":"secret-repository","stars":4,"forks":1,"open_issues":2},
		"advisories":{"ecosystem":"npm","package":"sensitive-package","known_count":3},
		"authority":{"mode":"oauth","scopes":["` + scope + `"],"demanded_secrets":[{"name":"` + secretName + `","required":true,"description":"secret description"}]},
		"capabilities_source":"server",
		"capabilities":[{"tool":"` + toolName + `","declared":["destructive"],"acts_on_behalf":true},{"tool":"safe_but_private"}],
		"gaps":["domain_lookup_failed"]
	}`
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: target, status: "requested", evidence: evidenceDocument, version: 1})
	seedRequester(t, ctx, ti, ti.projectID, requestID, requester, note)

	_, err := ti.repo.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, McpApprovalRequestID: requestID,
		Decision: "approved", DecidedBy: "sensitive-decision-actor", Rationale: conv.ToPGText("sensitive rationale"),
		EvidenceSnapshot: []byte(evidenceDocument), EvidenceVersion: 1, GrantedPrincipalUrns: []string{"urn:gram:user:sensitive-principal"},
	})
	require.NoError(t, err)

	report := `{
		"summary":"sensitive report summary",
		"coverage":{"level":"moderate","note":"sensitive coverage note"},
		"claims":[{"tier":"independently_reported","text":"` + claimText + `","citations":[
			{"url":"` + citationURL + `","title":"` + citationName + `","trusted_source":"security research"},
			{"url":"https://other.invalid/private","title":"Other Private Title"}
		]}],
		"injections":[{"url":"https://injection.invalid/private","rationale":"sensitive injection rationale"}],
		"run":{"model":"sensitive-model","prompt_version":"sensitive-prompt"}
	}`
	reportRow, err := ti.repo.CreateResearchReport(ctx, repo.CreateResearchReportParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, McpApprovalRequestID: requestID,
		Status: "running", Report: []byte(`{}`), ReportVersion: 1, Model: conv.ToPGText("sensitive-model"),
		PromptVersion: conv.ToPGText("sensitive-prompt"), RequestedBy: conv.ToPGText("sensitive-researcher"),
		StartedAt: pgtype.Timestamptz{}, CompletedAt: pgtype.Timestamptz{}, Error: pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = ti.repo.CompleteResearchReport(ctx, repo.CompleteResearchReportParams{
		Report: []byte(report), ReportVersion: 1,
		ToolCalls: []byte(`[{"tool":"web_search","search":{"query":"` + traceQuery + `"}},{"tool":"fetch_page","fetch":{"url":"https://trace.invalid/private","content_preview":"sensitive preview"}}]`),
		Model:     conv.ToPGText("sensitive-model"), ID: reportRow.ID, ProjectID: ti.projectID,
	})
	require.NoError(t, err)

	summary, err := ti.service.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, TargetKind: "server_url", TargetKey: target,
	})
	require.NoError(t, err)
	require.Equal(t, "requested", summary.Status)
	require.Equal(t, "approved", summary.StandingDecision)
	require.Equal(t, int64(1), summary.RequesterCount)
	require.NotEmpty(t, summary.CreatedAt)
	require.NotEmpty(t, summary.UpdatedAt)
	require.True(t, summary.EvidenceCollected)
	require.False(t, summary.EvidenceChanged)
	require.Equal(t, []string{"domain_lookup_failed"}, summary.EvidenceGaps)
	require.Equal(t, "package", summary.IdentityKind)
	require.True(t, summary.VersionPinned)
	require.Equal(t, "published", summary.PackagePublication)
	require.Equal(t, "found", summary.RepositoryState)
	require.Equal(t, "complete", summary.AdvisoryLookup)
	require.Equal(t, 3, summary.KnownAdvisories)
	require.Equal(t, "declared", summary.AuthorityState)
	require.Equal(t, "server", summary.CapabilitySource)
	require.Equal(t, 2, summary.DeclaredToolCount)
	require.Equal(t, 1, summary.RiskyDeclarationCount)
	require.Equal(t, "completed", summary.ResearchStatus)
	require.Equal(t, "moderate", summary.ResearchCoverage)
	require.Equal(t, 2, summary.CitationCount)
	require.Equal(t, 1, summary.TrustedCitationCount)
	require.NotNil(t, summary.EvidenceGaps)

	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	output := string(encoded)
	for _, private := range []string{
		target, requester, requester + "@example.test", note, secretName, scope, toolName,
		"sensitive-package", "secret-owner", "secret-repository", "sensitive-decision-actor",
		"sensitive rationale", "sensitive-principal", claimText, citationURL, citationName,
		"Other Private Title", "injection.invalid", "sensitive injection rationale", "sensitive-model",
		"sensitive-prompt", "sensitive-researcher", traceQuery, "trace.invalid", "sensitive preview",
	} {
		require.NotContains(t, output, private)
	}
}

func TestReadPlatformReview_ProjectAndOrganizationBoundariesAreNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	target := "npx sensitive-command --token command-secret"
	requestID := seedUnresolvedRequest(t, ctx, ti, ti.projectID, target)
	seedRequester(t, ctx, ti, ti.projectID, requestID, "private-user", "private note")

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	_, err := ti.service.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{
		OrganizationID: ti.organizationID, ProjectID: otherProject, TargetKind: "stdio_command", TargetKey: target,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{
		OrganizationID: "other-organization", ProjectID: ti.projectID, TargetKind: "stdio_command", TargetKey: target,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestReadPlatformReview_UnreadableEvidenceAndSupersededDecisionStayClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	target := "https://malformed.invalid/private"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: target, status: "superseded", evidence: `{"private":"valid json, unsupported evidence version"}`, version: 99})
	_, err := ti.repo.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, McpApprovalRequestID: requestID,
		Decision: "denied", DecidedBy: "private actor", Rationale: conv.ToPGText("private rationale"),
		EvidenceSnapshot: []byte(`{"private":"snapshot"}`), EvidenceVersion: 1, GrantedPrincipalUrns: []string{},
	})
	require.NoError(t, err)

	summary, err := ti.service.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, TargetKind: "server_url", TargetKey: target,
	})
	require.NoError(t, err)
	require.Equal(t, "superseded", summary.Status)
	require.Empty(t, summary.StandingDecision)
	require.Equal(t, []string{"unreadable_evidence"}, summary.EvidenceGaps)
	require.Equal(t, "unknown", summary.IdentityKind)
	require.Equal(t, "unknown", summary.PackagePublication)
	require.Equal(t, "unknown", summary.RepositoryState)
	require.Equal(t, "unknown", summary.AdvisoryLookup)
	require.Equal(t, "unknown", summary.AuthorityState)
	require.Equal(t, "unknown", summary.CapabilitySource)
	require.Equal(t, "none", summary.ResearchStatus)
	require.Equal(t, "unknown", summary.ResearchCoverage)

	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "malformed.invalid")
	require.NotContains(t, string(encoded), "private")
}

func TestReadPlatformReview_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	for _, input := range []mcpapproval.PlatformReviewReadInput{
		{OrganizationID: "", ProjectID: ti.projectID, TargetKind: "server_url", TargetKey: "x"},
		{OrganizationID: ti.organizationID, ProjectID: uuid.Nil, TargetKind: "server_url", TargetKey: "x"},
		{OrganizationID: ti.organizationID, ProjectID: ti.projectID, TargetKind: "other", TargetKey: "x"},
		{OrganizationID: ti.organizationID, ProjectID: ti.projectID, TargetKind: "server_url", TargetKey: "  "},
	} {
		_, err := ti.service.ReadPlatformReview(ctx, input)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
