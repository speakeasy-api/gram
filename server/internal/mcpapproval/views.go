package mcpapproval

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidencediff"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// summaryView projects a request row into its API shape.
//
// ArtifactRef stays a pointer so an unidentified server reaches the surface as
// absent rather than as an empty string. The two must not be conflated: one
// means we could not place the server, the other would read as a blank field.
func summaryView(request requestFields) *gen.ApprovalRequestSummary {
	// The slug is derived from the same canonical URL the inventory pages
	// key on, so a server_url request always links to its server page.
	var serverSlug *string
	if request.TargetKind == targetKindServerURL && request.TargetKey != "" {
		slug := shadowmcp.ServerSlug(request.TargetKey)
		serverSlug = &slug
	}

	return &gen.ApprovalRequestSummary{
		ID:                request.ID.String(),
		TargetKind:        request.TargetKind,
		TargetRaw:         request.TargetRaw,
		ServerSlug:        serverSlug,
		ArtifactRef:       fromPGText(request.ArtifactRef),
		VersionPinned:     request.VersionPinned,
		Status:            request.Status,
		RequesterCount:    int(request.RequesterCount),
		EvidenceChangedAt: optionalTime(request.EvidenceChangedAt),
		CreatedAt:         conv.FromPGTimestamptz(request.CreatedAt),
		UpdatedAt:         conv.FromPGTimestamptz(request.UpdatedAt),
	}
}

// requestFields is the subset of an approval-request row the views need.
// sqlc emits a distinct flat struct per query, so this keeps one view rather
// than one per row type.
type requestFields struct {
	ID             uuid.UUID
	TargetKind     string
	TargetRaw      string
	TargetKey      string
	ArtifactRef    pgtype.Text
	VersionPinned  bool
	Status         string
	RequesterCount int64

	// EvidenceChangedAt is the drift flag the daily recheck sets and only a
	// new decision clears.
	EvidenceChangedAt pgtype.Timestamptz

	CreatedAt pgtype.Timestamptz
	UpdatedAt pgtype.Timestamptz
}

func fromListRow(row repo.ListApprovalRequestsRow) requestFields {
	return requestFields{
		ID: row.ID, TargetKind: row.TargetKind, TargetRaw: row.TargetRaw, TargetKey: row.TargetKey,
		ArtifactRef: row.ArtifactRef, VersionPinned: row.VersionPinned,
		Status: row.Status, RequesterCount: row.RequesterCount,
		EvidenceChangedAt: row.EvidenceChangedAt,
		CreatedAt:         row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func fromGetRow(row repo.GetApprovalRequestRow) requestFields {
	return requestFields{
		ID: row.ID, TargetKind: row.TargetKind, TargetRaw: row.TargetRaw, TargetKey: row.TargetKey,
		ArtifactRef: row.ArtifactRef, VersionPinned: row.VersionPinned,
		Status: row.Status, RequesterCount: row.RequesterCount,
		EvidenceChangedAt: row.EvidenceChangedAt,
		CreatedAt:         row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func fromTargetRow(row repo.GetApprovalRequestByTargetRow) requestFields {
	return requestFields{
		ID: row.ID, TargetKind: row.TargetKind, TargetRaw: row.TargetRaw, TargetKey: row.TargetKey,
		ArtifactRef: row.ArtifactRef, VersionPinned: row.VersionPinned,
		Status: row.Status, RequesterCount: row.RequesterCount,
		EvidenceChangedAt: row.EvidenceChangedAt,
		CreatedAt:         row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func decisionView(decision repo.McpApprovalDecision) *gen.ApprovalDecision {
	// The column is NOT NULL with an array default, but the field is required
	// at the API boundary, so a nil scan result must still surface as an
	// empty set rather than fail response validation.
	granted := decision.GrantedPrincipalUrns
	if granted == nil {
		granted = []string{}
	}

	return &gen.ApprovalDecision{
		ID:                   decision.ID.String(),
		Decision:             decision.Decision,
		DecidedBy:            decision.DecidedBy,
		Rationale:            fromPGText(decision.Rationale),
		GrantedPrincipalUrns: granted,
		ResearchReportID:     nullUUIDString(decision.McpResearchReportID),
		Evidence:             rawEvidence(decision.EvidenceSnapshot),
		EvidenceVersion:      int(decision.EvidenceVersion),
		DecidedAt:            conv.FromPGTimestamptz(decision.DecidedAt),
	}
}

func researchReportView(report repo.McpResearchReport) *gen.ResearchReport {
	status := report.Status
	errText := fromPGText(report.Error)
	// A running row older than the workflow deadline is stranded, not live
	// (see ResearchRunStaleAfter). Presenting it as failed here is what lets
	// the page's polling resolve and the Run button re-enable — the durable
	// resolution only happens on the next StartResearch, which this unblocks.
	if status == researchStatusRunning && report.StartedAt.Valid && time.Since(report.StartedAt.Time) > ResearchRunStaleAfter {
		status = "failed"
		stale := "the research run was interrupted and did not resolve"
		errText = &stale
	}
	return &gen.ResearchReport{
		ID:            report.ID.String(),
		Status:        status,
		Report:        rawEvidence(report.Report),
		ReportVersion: int(report.ReportVersion),
		Model:         fromPGText(report.Model),
		PromptVersion: fromPGText(report.PromptVersion),
		RequestedBy:   fromPGText(report.RequestedBy),
		StartedAt:     optionalTime(report.StartedAt),
		CompletedAt:   optionalTime(report.CompletedAt),
		Error:         errText,
		ToolCalls:     researchToolCallsView(report.ToolCalls),
		CreatedAt:     conv.FromPGTimestamptz(report.CreatedAt),
	}
}

// researchToolCallsView decodes the stored per-action trace for the API
// boundary. A payload that will not decode yields an empty slice rather than
// a partial trace: a half-read trace would misrepresent what the run did, and
// the trace is observability, never load-bearing for a decision.
func researchToolCallsView(raw []byte) []*gen.ResearchToolCall {
	if len(raw) == 0 {
		return nil
	}
	var records []researchagent.ToolCallRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil
	}

	out := make([]*gen.ResearchToolCall, 0, len(records))
	for _, record := range records {
		out = append(out, &gen.ResearchToolCall{
			Sequence: record.Sequence,
			Tool:     record.Tool,
			Error:    conv.PtrEmpty(record.Error),
			Search:   researchWebSearchCallView(record.Search),
			Fetch:    researchPageFetchCallView(record.Fetch),
		})
	}
	return out
}

func researchWebSearchCallView(call *researchagent.SearchCall) *gen.ResearchWebSearchCall {
	if call == nil {
		return nil
	}
	return &gen.ResearchWebSearchCall{
		Query:            conv.PtrEmpty(call.Query),
		ResultCount:      conv.PtrEmpty(call.ResultCount),
		PromptTokens:     conv.PtrEmpty(int(call.PromptTokens)),
		CompletionTokens: conv.PtrEmpty(int(call.CompletionTokens)),
	}
}

func researchPageFetchCallView(call *researchagent.FetchCall) *gen.ResearchPageFetchCall {
	if call == nil {
		return nil
	}
	return &gen.ResearchPageFetchCall{
		URL:              conv.PtrEmpty(call.URL),
		FinalURL:         conv.PtrEmpty(call.FinalURL),
		ContentType:      conv.PtrEmpty(call.ContentType),
		ContentBytes:     conv.PtrEmpty(call.ContentBytes),
		Truncated:        conv.PtrEmpty(call.Truncated),
		Judged:           conv.PtrEmpty(call.Judged),
		InjectionFlagged: conv.PtrEmpty(call.InjectionFlagged),
		JudgeRationale:   conv.PtrEmpty(call.JudgeRationale),
		ContentPreview:   conv.PtrEmpty(call.ContentPreview),
	}
}

// rawEvidence decodes the stored evidence document for the API boundary.
//
// A payload that will not decode yields nil rather than a partial value: the
// surface has to be able to say the evidence is unavailable, and half a
// document would be read as the whole of what is known.
func rawEvidence(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}

	// UseNumber keeps numerics as their literal text rather than float64, so
	// a large integer in the evidence re-encodes exactly as stored.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}

	// The whole input must be one value. Trailing content means a payload
	// this function does not understand, and half a document read as the
	// whole of what is known is exactly what the nil contract forbids.
	// Token rather than More: More reports false for a trailing `]` or `}`,
	// so `{"a":1}]` would otherwise slip through.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil
	}

	return decoded
}

// evidenceVersion widens the stored version for the API boundary.
func evidenceVersion(version int32) *int {
	widened := int(version)

	return &widened
}

func nullUUIDString(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}

	formatted := value.UUID.String()

	return &formatted
}

func pgText(value *string) pgtype.Text {
	return conv.PtrToPGText(value)
}

func fromPGText(value pgtype.Text) *string {
	return conv.FromPGText[string](value)
}

// optionalTime normalizes an optional timestamp for the API boundary. Like the
// required fields it goes through conv.FromPGTimestamptz, so a row whose
// timestamptz carries a non-UTC offset serializes as the same UTC instant
// everywhere instead of leaking whichever zone the connection handed back.
func optionalTime(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}

	return conv.PtrEmpty(conv.FromPGTimestamptz(value))
}

// evidenceDiffView compares the latest decision's frozen snapshot against
// the request's current evidence, on read. Nil when there is no decision to
// compare against or either side cannot be decoded — the section is absent
// rather than pretending nothing moved.
func evidenceDiffView(latest repo.McpApprovalDecision, currentEvidence []byte, currentVersion int32) *gen.EvidenceDiff {
	snapshot, err := evidence.DecodeDocument(latest.EvidenceSnapshot, int(latest.EvidenceVersion))
	if err != nil {
		return nil
	}
	current, err := evidence.DecodeDocument(currentEvidence, int(currentVersion))
	if err != nil {
		return nil
	}

	diff := evidencediff.Compare(snapshot, current)

	fields := make([]*gen.EvidenceFieldChange, 0, len(diff.Fields))
	for _, change := range diff.Fields {
		fields = append(fields, &gen.EvidenceFieldChange{Field: change.Field, Before: change.Before, After: change.After})
	}
	advisories := make([]*gen.EvidenceAdvisoryChange, 0, len(diff.AdvisoriesAdded))
	for _, advisory := range diff.AdvisoriesAdded {
		advisories = append(advisories, &gen.EvidenceAdvisoryChange{
			ID:       advisory.ID,
			Summary:  conv.PtrEmpty(advisory.Summary),
			Severity: conv.PtrEmpty(advisory.Severity),
		})
	}

	return &gen.EvidenceDiff{
		Changed:         diff.Changed,
		ScopesAdded:     diff.ScopesAdded,
		ScopesRemoved:   diff.ScopesRemoved,
		SecretsAdded:    diff.SecretsAdded,
		SecretsRemoved:  diff.SecretsRemoved,
		Fields:          fields,
		AdvisoriesAdded: advisories,
	}
}
