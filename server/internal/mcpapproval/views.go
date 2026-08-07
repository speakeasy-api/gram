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
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

// timeFormat is how timestamps cross the API boundary.
const timeFormat = time.RFC3339

// summaryView projects a request row into its API shape.
//
// ArtifactRef stays a pointer so an unidentified server reaches the surface as
// absent rather than as an empty string. The two must not be conflated: one
// means we could not place the server, the other would read as a blank field.
func summaryView(request requestFields) *gen.ApprovalRequestSummary {
	return &gen.ApprovalRequestSummary{
		ID:             request.ID.String(),
		TargetKind:     request.TargetKind,
		TargetRaw:      request.TargetRaw,
		ArtifactRef:    fromPGText(request.ArtifactRef),
		VersionPinned:  request.VersionPinned,
		Status:         request.Status,
		RequesterCount: int(request.RequesterCount),
		CreatedAt:      request.CreatedAt.Time.Format(timeFormat),
		UpdatedAt:      request.UpdatedAt.Time.Format(timeFormat),
	}
}

// requestFields is the subset of an approval-request row the views need.
// sqlc emits a distinct flat struct per query, so this keeps one view rather
// than one per row type.
type requestFields struct {
	ID             uuid.UUID
	TargetKind     string
	TargetRaw      string
	ArtifactRef    pgtype.Text
	VersionPinned  bool
	Status         string
	RequesterCount int64
	CreatedAt      pgtype.Timestamptz
	UpdatedAt      pgtype.Timestamptz
}

func fromListRow(row repo.ListApprovalRequestsRow) requestFields {
	return requestFields{
		ID: row.ID, TargetKind: row.TargetKind, TargetRaw: row.TargetRaw,
		ArtifactRef: row.ArtifactRef, VersionPinned: row.VersionPinned,
		Status: row.Status, RequesterCount: row.RequesterCount,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func fromGetRow(row repo.GetApprovalRequestRow) requestFields {
	return requestFields{
		ID: row.ID, TargetKind: row.TargetKind, TargetRaw: row.TargetRaw,
		ArtifactRef: row.ArtifactRef, VersionPinned: row.VersionPinned,
		Status: row.Status, RequesterCount: row.RequesterCount,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func decisionView(decision repo.McpApprovalDecision) *gen.ApprovalDecision {
	return &gen.ApprovalDecision{
		ID:                   decision.ID.String(),
		Decision:             decision.Decision,
		DecidedBy:            decision.DecidedBy,
		Rationale:            fromPGText(decision.Rationale),
		GrantedPrincipalUrns: decision.GrantedPrincipalUrns,
		ResearchReportID:     nullUUIDString(decision.McpResearchReportID),
		Evidence:             rawEvidence(decision.EvidenceSnapshot),
		EvidenceVersion:      evidenceVersion(decision.EvidenceVersion),
		DecidedAt:            decision.DecidedAt.Time.Format(timeFormat),
	}
}

func researchReportView(report repo.McpResearchReport) *gen.ResearchReport {
	return &gen.ResearchReport{
		ID:            report.ID.String(),
		Status:        report.Status,
		Report:        rawEvidence(report.Report),
		ReportVersion: int(report.ReportVersion),
		Model:         fromPGText(report.Model),
		PromptVersion: fromPGText(report.PromptVersion),
		RequestedBy:   fromPGText(report.RequestedBy),
		StartedAt:     optionalTime(report.StartedAt),
		CompletedAt:   optionalTime(report.CompletedAt),
		Error:         fromPGText(report.Error),
		CreatedAt:     report.CreatedAt.Time.Format(timeFormat),
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

func optionalTime(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}

	formatted := value.Time.Format(timeFormat)

	return &formatted
}
