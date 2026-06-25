package risk_export

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_export/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ManifestSchemaVersion is bumped when the JSONL record shape changes so
// downstream consumers can detect incompatibilities.
const ManifestSchemaVersion = 1

const ndjsonContentType = "application/x-ndjson"

// RiskExport runs the read-only export queries against the replica pool and
// writes JSONL part files to object storage (or a local directory in dev).
type RiskExport struct {
	logger   *slog.Logger
	tracer   trace.Tracer
	db       *pgxpool.Pool
	store    assets.BlobStore
	localDir string
}

func NewRiskExport(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, store assets.BlobStore, localDir string) *RiskExport {
	return &RiskExport{
		logger:   logger,
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_export"),
		db:       db,
		store:    store,
		localDir: localDir,
	}
}

// CountExportRows returns the size of the sampled chat population for the
// configured filters. Used for the dry-run gate and the audit record.
func (a *RiskExport) CountExportRows(ctx context.Context, args CountExportRowsArgs) (_ *CountExportRowsResult, err error) {
	ctx, span := a.tracer.Start(ctx, "riskExport.count", trace.WithAttributes(
		attribute.String("risk_export.organization_id", args.Filters.OrganizationID),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	projectID, err := nullableUUID(args.Filters.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project_id: %w", err)
	}
	policyID, err := nullableUUID(args.Filters.RiskPolicyID)
	if err != nil {
		return nil, fmt.Errorf("risk_policy_id: %w", err)
	}

	count, err := repo.New(a.db).CountSampledChats(ctx, repo.CountSampledChatsParams{
		OrganizationID:  args.Filters.OrganizationID,
		ProjectID:       projectID,
		CreatedFrom:     conv.PtrToPGTimestamptz(args.Filters.CreatedFrom),
		CreatedTo:       conv.PtrToPGTimestamptz(args.Filters.CreatedTo),
		ExternalUserID:  conv.PtrToPGText(args.Filters.ExternalUserID),
		SamplePct:       args.Sampling.Percent,
		SampleSeed:      args.Sampling.Seed,
		HasFindingsOnly: args.Filters.HasFindingsOnly,
		RiskPolicyID:    policyID,
		RuleIds:         nonNilStrings(args.Filters.RuleIDs),
		Sources:         nonNilStrings(args.Filters.Sources),
		Severities:      nonNilStrings(args.Filters.Severities),
	})
	if err != nil {
		return nil, fmt.Errorf("count sampled chats: %w", err)
	}

	span.SetAttributes(attribute.Int64("risk_export.chat_count", count))
	return &CountExportRowsResult{ChatCount: count}, nil
}

// FetchExportChatPage returns one keyset page of sampled chat IDs. It fetches
// PageSize+1 rows to detect whether more pages remain (backpressure probe).
func (a *RiskExport) FetchExportChatPage(ctx context.Context, args FetchExportChatPageArgs) (_ *FetchExportChatPageResult, err error) {
	ctx, span := a.tracer.Start(ctx, "riskExport.fetchChatPage")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	projectID, err := nullableUUID(args.Filters.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project_id: %w", err)
	}
	policyID, err := nullableUUID(args.Filters.RiskPolicyID)
	if err != nil {
		return nil, fmt.Errorf("risk_policy_id: %w", err)
	}
	afterID, err := nullableUUID(args.AfterID)
	if err != nil {
		return nil, fmt.Errorf("after_id: %w", err)
	}

	// Fetch one extra row to probe for a subsequent page.
	limit := args.PageSize + 1
	ids, err := repo.New(a.db).SelectSampledChatIDs(ctx, repo.SelectSampledChatIDsParams{
		OrganizationID:  args.Filters.OrganizationID,
		ProjectID:       projectID,
		CreatedFrom:     conv.PtrToPGTimestamptz(args.Filters.CreatedFrom),
		CreatedTo:       conv.PtrToPGTimestamptz(args.Filters.CreatedTo),
		ExternalUserID:  conv.PtrToPGText(args.Filters.ExternalUserID),
		SamplePct:       args.Sampling.Percent,
		SampleSeed:      args.Sampling.Seed,
		HasFindingsOnly: args.Filters.HasFindingsOnly,
		RiskPolicyID:    policyID,
		RuleIds:         nonNilStrings(args.Filters.RuleIDs),
		Sources:         nonNilStrings(args.Filters.Sources),
		Severities:      nonNilStrings(args.Filters.Severities),
		AfterID:         afterID,
		Lim:             limit,
	})
	if err != nil {
		return nil, fmt.Errorf("select sampled chat ids: %w", err)
	}

	hasMore := len(ids) > int(args.PageSize)
	if hasMore {
		ids = ids[:args.PageSize]
	}

	result := &FetchExportChatPageResult{ChatIDs: ids, LastID: nil, HasMore: hasMore}
	if len(ids) > 0 {
		last := ids[len(ids)-1]
		result.LastID = &last
	}

	span.SetAttributes(
		attribute.Int("risk_export.page_size", len(ids)),
		attribute.Bool("risk_export.has_more", hasMore),
	)
	return result, nil
}

// WriteExportChunk extracts a batch of chats and writes one JSONL part file.
func (a *RiskExport) WriteExportChunk(ctx context.Context, args WriteExportChunkArgs) (_ *WriteExportChunkResult, err error) {
	ctx, span := a.tracer.Start(ctx, "riskExport.writeChunk", trace.WithAttributes(
		attribute.Int("risk_export.chat_count", len(args.ChatIDs)),
		attribute.Int("risk_export.part_index", args.PartIndex),
		attribute.String("risk_export.mode", string(args.Mode)),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	if len(args.ChatIDs) == 0 {
		return &WriteExportChunkResult{ObjectPath: "", RowCount: 0, ChatCount: 0}, nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	var rowCount int64

	switch args.Mode {
	case ModeFindingCentric:
		policyID, perr := nullableUUID(args.Filters.RiskPolicyID)
		if perr != nil {
			return nil, fmt.Errorf("risk_policy_id: %w", perr)
		}
		rows, qerr := repo.New(a.db).ExportFindingCentric(ctx, repo.ExportFindingCentricParams{
			ContextSize:  args.ContextSize,
			Roles:        nonNilStrings(args.Filters.Roles),
			Models:       nonNilStrings(args.Filters.Models),
			ChatIds:      args.ChatIDs,
			RiskPolicyID: policyID,
			RuleIds:      nonNilStrings(args.Filters.RuleIDs),
			Sources:      nonNilStrings(args.Filters.Sources),
		})
		if qerr != nil {
			return nil, fmt.Errorf("export finding centric: %w", qerr)
		}
		for _, row := range rows {
			if encErr := enc.Encode(mapFindingCentricRow(row)); encErr != nil {
				return nil, fmt.Errorf("encode record: %w", encErr)
			}
			rowCount++
		}
	case ModeFullTranscript:
		rows, qerr := repo.New(a.db).ExportFullTranscript(ctx, repo.ExportFullTranscriptParams{
			ChatIds:    args.ChatIDs,
			Roles:      nonNilStrings(args.Filters.Roles),
			Models:     nonNilStrings(args.Filters.Models),
			MsgSources: nonNilStrings(args.Filters.MsgSources),
		})
		if qerr != nil {
			return nil, fmt.Errorf("export full transcript: %w", qerr)
		}
		for _, row := range rows {
			if encErr := enc.Encode(mapFullTranscriptRow(row)); encErr != nil {
				return nil, fmt.Errorf("encode record: %w", encErr)
			}
			rowCount++
		}
	default:
		return nil, fmt.Errorf("unknown export mode: %q", args.Mode)
	}

	store, cleanup, err := a.storeFor(args.TargetKind)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := cleanup(); cerr != nil {
			a.logger.WarnContext(ctx, "risk export store cleanup failed", attr.SlogError(cerr))
		}
	}()

	objectPath := fmt.Sprintf("%s/part-%05d.jsonl", args.OutputPrefix, args.PartIndex)
	if _, err = writeObject(ctx, store, objectPath, buf.Bytes()); err != nil {
		return nil, fmt.Errorf("write part %d: %w", args.PartIndex, err)
	}

	span.SetAttributes(attribute.Int64("risk_export.row_count", rowCount))
	return &WriteExportChunkResult{
		ObjectPath: objectPath,
		RowCount:   rowCount,
		ChatCount:  len(args.ChatIDs),
	}, nil
}

// FinalizeExport writes the run manifest and presigns its retrieval URL.
func (a *RiskExport) FinalizeExport(ctx context.Context, args FinalizeExportArgs) (_ *FinalizeExportResult, err error) {
	ctx, span := a.tracer.Start(ctx, "riskExport.finalize")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	data, err := json.MarshalIndent(args.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	store, cleanup, err := a.storeFor(args.TargetKind)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := cleanup(); cerr != nil {
			a.logger.WarnContext(ctx, "risk export store cleanup failed", attr.SlogError(cerr))
		}
	}()

	manifestPath := fmt.Sprintf("%s/manifest.json", args.OutputPrefix)
	if _, err = writeObject(ctx, store, manifestPath, data); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	signed, err := store.PresignRead(ctx, manifestPath, args.SignedTTL)
	if err != nil {
		return nil, fmt.Errorf("presign manifest: %w", err)
	}

	return &FinalizeExportResult{
		ManifestObjectPath: manifestPath,
		ManifestSignedURL:  signed.String(),
	}, nil
}

// storeFor resolves the blob store for the requested target kind. For "local"
// it opens an FS-backed store rooted at the configured directory; the returned
// cleanup closes the os.Root.
func (a *RiskExport) storeFor(kind string) (assets.BlobStore, func() error, error) {
	noop := func() error { return nil }
	if kind == "local" {
		if a.localDir == "" {
			return nil, noop, fmt.Errorf("local export target requested but no risk-export-local-dir is configured")
		}
		if err := os.MkdirAll(a.localDir, 0o750); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, noop, fmt.Errorf("create local export dir: %w", err)
		}
		root, err := os.OpenRoot(a.localDir)
		if err != nil {
			return nil, noop, fmt.Errorf("open local export dir: %w", err)
		}
		return assets.NewFSBlobStore(a.logger, root), root.Close, nil
	}
	return a.store, noop, nil
}

func writeObject(ctx context.Context, store assets.BlobStore, objectPath string, data []byte) (*url.URL, error) {
	w, u, err := store.Write(ctx, objectPath, ndjsonContentType, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open object writer: %w", err)
	}
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("write object body: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close object writer: %w", err)
	}
	return u, nil
}

func nullableUUID(id *uuid.UUID) (uuid.NullUUID, error) {
	if id == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	return uuid.NullUUID{UUID: *id, Valid: true}, nil
}

// nonNilStrings normalizes a nil slice to an empty (but non-nil) slice so the
// `cardinality(...) = 0` guards in the queries behave predictably.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ── JSONL records ─────────────────────────────────────────────────────────────

type exportRecord struct {
	ChatID          string          `json:"chat_id"`
	MessageID       string          `json:"message_id"`
	Seq             int64           `json:"seq"`
	Generation      int32           `json:"generation"`
	Rn              *int64          `json:"rn,omitempty"`
	Total           *int64          `json:"total,omitempty"`
	IsSeed          *bool           `json:"is_seed,omitempty"`
	Role            string          `json:"role"`
	Content         string          `json:"content"`
	ContentRaw      json.RawMessage `json:"content_raw,omitempty"`
	ContentAssetURL *string         `json:"content_asset_url,omitempty"`
	Model           *string         `json:"model,omitempty"`
	ToolCalls       json.RawMessage `json:"tool_calls,omitempty"`
	ToolURN         *string         `json:"tool_urn,omitempty"`
	Source          *string         `json:"source,omitempty"`
	ExternalUserID  *string         `json:"external_user_id,omitempty"`
	CreatedAt       *time.Time      `json:"created_at,omitempty"`
	Finding         *exportFinding  `json:"finding,omitempty"`
}

type exportFinding struct {
	FindingID         string          `json:"finding_id"`
	RiskPolicyID      *string         `json:"risk_policy_id,omitempty"`
	RiskPolicyVersion *int64          `json:"risk_policy_version,omitempty"`
	RuleID            *string         `json:"rule_id,omitempty"`
	Source            *string         `json:"source,omitempty"`
	Description       *string         `json:"description,omitempty"`
	Match             *string         `json:"match,omitempty"`
	StartPos          *int32          `json:"start_pos,omitempty"`
	EndPos            *int32          `json:"end_pos,omitempty"`
	Confidence        *float64        `json:"confidence,omitempty"`
	Tags              []string        `json:"tags,omitempty"`
	Spans             json.RawMessage `json:"spans,omitempty"`
	PolicyName        *string         `json:"policy_name,omitempty"`
	PolicyType        *string         `json:"policy_type,omitempty"`
	PolicyAction      *string         `json:"policy_action,omitempty"`
	RuleTitle         *string         `json:"rule_title,omitempty"`
	RuleSeverity      *string         `json:"rule_severity,omitempty"`
}

func mapFindingCentricRow(row repo.ExportFindingCentricRow) exportRecord {
	rn := row.Rn
	total := row.Total
	isSeed := row.IsSeed
	rec := exportRecord{
		ChatID:          row.ChatID.String(),
		MessageID:       row.MessageID.String(),
		Seq:             row.Seq,
		Generation:      row.Generation,
		Rn:              &rn,
		Total:           &total,
		IsSeed:          &isSeed,
		Role:            row.Role,
		Content:         row.Content,
		ContentRaw:      jsonRaw(row.ContentRaw),
		ContentAssetURL: conv.FromPGText[string](row.ContentAssetUrl),
		Model:           conv.FromPGText[string](row.Model),
		ToolCalls:       jsonRaw(row.ToolCalls),
		ToolURN:         pgTextPtr(row.ToolUrn),
		Source:          conv.FromPGText[string](row.Source),
		ExternalUserID:  conv.FromPGText[string](row.ExternalUserID),
		CreatedAt:       pgTimePtr(row.CreatedAt),
		Finding:         nil,
	}
	if row.FindingID.Valid {
		rec.Finding = buildFinding(findingFields{
			FindingID:         row.FindingID,
			RiskPolicyID:      row.RiskPolicyID,
			RiskPolicyVersion: row.RiskPolicyVersion,
			RuleID:            row.FindingRuleID,
			Source:            row.FindingSource,
			Description:       row.FindingDescription,
			Match:             row.FindingMatch,
			StartPos:          row.StartPos,
			EndPos:            row.EndPos,
			Confidence:        row.Confidence,
			Tags:              row.Tags,
			Spans:             row.Spans,
			PolicyName:        row.PolicyName,
			PolicyType:        row.PolicyType,
			PolicyAction:      row.PolicyAction,
			RuleTitle:         row.RuleTitle,
			RuleSeverity:      row.RuleSeverity,
		})
	}
	return rec
}

func mapFullTranscriptRow(row repo.ExportFullTranscriptRow) exportRecord {
	rec := exportRecord{
		ChatID:          row.ChatID.String(),
		MessageID:       row.MessageID.String(),
		Seq:             row.Seq,
		Generation:      row.Generation,
		Rn:              nil,
		Total:           nil,
		IsSeed:          nil,
		Role:            row.Role,
		Content:         row.Content,
		ContentRaw:      jsonRaw(row.ContentRaw),
		ContentAssetURL: conv.FromPGText[string](row.ContentAssetUrl),
		Model:           conv.FromPGText[string](row.Model),
		ToolCalls:       jsonRaw(row.ToolCalls),
		ToolURN:         toolURNPtr(row.ToolUrn),
		Source:          conv.FromPGText[string](row.Source),
		ExternalUserID:  conv.FromPGText[string](row.ExternalUserID),
		CreatedAt:       pgTimePtr(row.CreatedAt),
		Finding:         nil,
	}
	if row.FindingID.Valid {
		rec.Finding = buildFinding(findingFields{
			FindingID:         row.FindingID,
			RiskPolicyID:      row.RiskPolicyID,
			RiskPolicyVersion: row.RiskPolicyVersion,
			RuleID:            row.FindingRuleID,
			Source:            row.FindingSource,
			Description:       row.FindingDescription,
			Match:             row.FindingMatch,
			StartPos:          row.StartPos,
			EndPos:            row.EndPos,
			Confidence:        row.Confidence,
			Tags:              row.Tags,
			Spans:             row.Spans,
			PolicyName:        row.PolicyName,
			PolicyType:        row.PolicyType,
			PolicyAction:      row.PolicyAction,
			RuleTitle:         row.RuleTitle,
			RuleSeverity:      row.RuleSeverity,
		})
	}
	return rec
}

// findingFields carries the raw finding columns shared by both export modes so
// buildFinding can map them in one place.
type findingFields struct {
	FindingID         uuid.NullUUID
	RiskPolicyID      uuid.NullUUID
	RiskPolicyVersion pgtype.Int8
	RuleID            pgtype.Text
	Source            pgtype.Text
	Description       pgtype.Text
	Match             pgtype.Text
	StartPos          pgtype.Int4
	EndPos            pgtype.Int4
	Confidence        pgtype.Float8
	Tags              []string
	Spans             []byte
	PolicyName        pgtype.Text
	PolicyType        pgtype.Text
	PolicyAction      pgtype.Text
	RuleTitle         pgtype.Text
	RuleSeverity      pgtype.Text
}

func buildFinding(f findingFields) *exportFinding {
	return &exportFinding{
		FindingID:         f.FindingID.UUID.String(),
		RiskPolicyID:      conv.FromNullableUUID(f.RiskPolicyID),
		RiskPolicyVersion: pgInt8Ptr(f.RiskPolicyVersion),
		RuleID:            conv.FromPGText[string](f.RuleID),
		Source:            conv.FromPGText[string](f.Source),
		Description:       conv.FromPGText[string](f.Description),
		Match:             conv.FromPGText[string](f.Match),
		StartPos:          conv.FromPGInt4(f.StartPos),
		EndPos:            conv.FromPGInt4(f.EndPos),
		Confidence:        conv.FromPGFloat8(f.Confidence),
		Tags:              f.Tags,
		Spans:             jsonRaw(f.Spans),
		PolicyName:        conv.FromPGText[string](f.PolicyName),
		PolicyType:        conv.FromPGText[string](f.PolicyType),
		PolicyAction:      conv.FromPGText[string](f.PolicyAction),
		RuleTitle:         conv.FromPGText[string](f.RuleTitle),
		RuleSeverity:      conv.FromPGText[string](f.RuleSeverity),
	}
}

func jsonRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

func pgTextPtr(t pgtype.Text) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	v := t.String
	return &v
}

func pgTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func pgInt8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

func toolURNPtr(t urn.Tool) *string {
	if t.IsZero() {
		return nil
	}
	v := t.String()
	return &v
}
