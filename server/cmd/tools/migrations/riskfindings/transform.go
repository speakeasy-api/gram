package riskfindings

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
)

// FindingRow is one risk_findings row ready to insert into ClickHouse. The ch
// tags map each field to its column; clickhouse-go's AppendStruct binds by tag.
// The struct is intentionally flat — AppendStruct does not recurse into embedded
// structs. inserted_at is omitted so ClickHouse stamps its DEFAULT now64(9).
type FindingRow struct {
	ID                       uuid.UUID  `ch:"id"`
	CreatedAt                time.Time  `ch:"created_at"`
	OrganizationID           string     `ch:"organization_id"`
	ProjectID                string     `ch:"project_id"`
	RequestID                string     `ch:"request_id"`
	ChatMessageID            string     `ch:"chat_message_id"`
	ContentPartID            string     `ch:"content_part_id"`
	RiskPolicyID             string     `ch:"risk_policy_id"`
	RiskPolicyVersion        int64      `ch:"risk_policy_version"`
	RuleID                   string     `ch:"rule_id"`
	Description              string     `ch:"description"`
	Source                   string     `ch:"source"`
	Confidence               float64    `ch:"confidence"`
	Tags                     []string   `ch:"tags"`
	StartPos                 int32      `ch:"start_pos"`
	EndPos                   int32      `ch:"end_pos"`
	DeadLetterReason         string     `ch:"dead_letter_reason"`
	ChatID                   string     `ch:"chat_id"`
	UserID                   string     `ch:"user_id"`
	ExternalUserID           string     `ch:"external_user_id"`
	Category                 string     `ch:"category"`
	MatchLen                 uint32     `ch:"match_len"`
	MatchRedacted            string     `ch:"match_redacted"`
	FingerprintPepperVersion string     `ch:"fingerprint_pepper_version"`
	FingerprintGlobalHS256   string     `ch:"fingerprint_global_hs256"`
	FingerprintTenantHS256   string     `ch:"fingerprint_tenant_hs256"`
	ExcludedAt               *time.Time `ch:"excluded_at"`
	ExclusionID              *uuid.UUID `ch:"exclusion_id"`
	FalsePositiveAt          *time.Time `ch:"false_positive_at"`
	MessageCreatedAt         time.Time  `ch:"message_created_at"`
	AssistantID              string     `ch:"assistant_id"`
	Surface                  string     `ch:"surface"`
	Field                    string     `ch:"field"`
	Path                     string     `ch:"path"`
	ToolCallID               string     `ch:"tool_call_id"`
}

// Surface values stamped into risk_findings.surface — which text the row's
// start_pos/end_pos offsets index. Kept in sync with the column's schema
// comment (server/clickhouse/schema.sql).
const (
	surfaceContent        = "content"
	surfaceScanSurface    = "scan_surface"
	surfaceToolArgs       = "tool_args"
	surfaceJSONPath       = "json_path"
	surfaceDerived        = "derived"
	surfaceLegacyPresidio = "legacy_presidio"
	surfaceNone           = "none"
)

// Transformer maps a Postgres SourceRow to one or more ClickHouse FindingRows,
// mirroring what the live ingest path stamps at write time: the global and
// tenant-qualified HMAC-SHA256 fingerprints of the match, the partial-mask
// display string (maskdisplay), denormalized attribution, and the reveal
// metadata (surface/field/path). The raw match is never carried into
// ClickHouse.
type Transformer struct {
	fingerprinter risk.Fingerprinter

	// keyCache memoizes per-tenant HKDF keys across rows so repeated orgs don't
	// re-derive. Guarded because Transform may be called concurrently.
	mu       sync.Mutex
	keyCache map[string][]byte
}

// NewTransformer builds a transformer using fingerprinter to fingerprint matches.
func NewTransformer(fingerprinter risk.Fingerprinter) *Transformer {
	return &Transformer{
		fingerprinter: fingerprinter,
		mu:            sync.Mutex{},
		keyCache:      make(map[string][]byte),
	}
}

// Transform implements pipeline.Transformer.
//
// A row without spans becomes exactly one FindingRow. A row whose spans JSONB
// is non-empty fans out to ONE ROW PER SPAN: span index 0 keeps the Postgres
// row id (so webhooks and false-positive marks keyed on it keep resolving)
// while span i >= 1 gets a deterministic SHA1-derived id, making a re-run emit
// the same ids. Every span row carries the parent's attribution, exclusion,
// and false-positive state; match-derived columns (offsets, length, mask,
// fingerprints) and the reveal metadata are computed per span.
func (t *Transformer) Transform(_ context.Context, in SourceRow) ([]FindingRow, error) {
	// Only real findings become ClickHouse events. Drop the "nothing found"
	// SourceNone sentinels (found=false) and any row without a rule, matching the
	// live outbox emission. The source already filters these out; this guard keeps
	// the transform correct if it is ever fed an unfiltered source.
	if !in.Found || conv.PtrValOr(in.RuleID, "") == "" {
		return nil, nil
	}

	orgID := strings.TrimSpace(in.OrganizationID)
	ruleID := conv.PtrValOr(in.RuleID, "")
	deadLetter := conv.PtrValOr(in.DeadLetterReason, "") != ""

	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}

	// Rows surviving the found/rule guard above are never dead-letter
	// sentinels, so classification always applies — same canonical
	// (source, rule_id) mapping the live writer stamps at ingest.
	category := string(categories.Classify(in.Source, ruleID))

	base := FindingRow{
		ID:                       in.ID,
		CreatedAt:                in.CreatedAt,
		OrganizationID:           in.OrganizationID,
		ProjectID:                in.ProjectID.String(),
		RequestID:                "", // not recorded in Postgres risk_results
		ChatMessageID:            conv.Ternary(in.ChatMessageID.Valid, in.ChatMessageID.UUID.String(), ""),
		ContentPartID:            conv.Ternary(in.ContentPartID.Valid, in.ContentPartID.UUID.String(), ""),
		RiskPolicyID:             in.RiskPolicyID.String(),
		RiskPolicyVersion:        in.RiskPolicyVersion,
		RuleID:                   ruleID,
		Description:              conv.PtrValOr(in.Description, ""),
		Source:                   in.Source,
		Confidence:               conv.PtrValOr(in.Confidence, 0),
		Tags:                     tags,
		StartPos:                 conv.PtrValOr(in.StartPos, 0),
		EndPos:                   conv.PtrValOr(in.EndPos, 0),
		DeadLetterReason:         conv.PtrValOr(in.DeadLetterReason, ""),
		ChatID:                   in.ChatID,
		UserID:                   in.UserID,
		ExternalUserID:           in.ExternalUserID,
		Category:                 category,
		MatchLen:                 0,
		MatchRedacted:            "",
		FingerprintPepperVersion: "",
		FingerprintGlobalHS256:   "",
		FingerprintTenantHS256:   "",
		ExcludedAt:               in.ExcludedAt,
		ExclusionID:              in.ExclusionID,
		FalsePositiveAt:          in.FalsePositiveAt,
		MessageCreatedAt:         in.MessageCreatedAt,
		AssistantID:              in.AssistantID,
		Surface:                  "",
		Field:                    "",
		Path:                     "",
		// tool_call_id stays empty for backfilled rows: Postgres spans only
		// carried the tool NAME (celenv span ToolCallID is the call's name, not
		// a recorded call id), so there is no real id to stamp and faking one
		// would poison reveal lookups.
		ToolCallID: "",
	}

	// Spans are the per-span match attribution recorded since the CEL
	// custom-rule work (risk_results.spans, risk_analysis.FindingSpan). "null"
	// unmarshals to nil, so legacy rows and empty arrays both take the
	// single-row path below.
	var spans []risk_analysis.FindingSpan
	if len(in.Spans) > 0 {
		if err := json.Unmarshal(in.Spans, &spans); err != nil {
			return nil, fmt.Errorf("parse spans for %s: %w", in.ID, err)
		}
	}

	if len(spans) == 0 {
		row := base
		row.Surface = sourceSurface(in.Source)
		if err := t.stampMatch(&row, orgID, deadLetter, in.Source, ruleID, conv.PtrValOr(in.Match, "")); err != nil {
			return nil, err
		}
		return []FindingRow{row}, nil
	}

	out := make([]FindingRow, 0, len(spans))
	for i, span := range spans {
		row := base
		if i > 0 {
			row.ID = spanRowID(in.ID, i)
		}
		row.StartPos = conv.SafeInt32(span.StartPos)
		row.EndPos = conv.SafeInt32(span.EndPos)
		row.Surface = spanSurface(in.Source, span.Field, span.Path)
		row.Field = span.Field
		row.Path = span.Path
		if err := t.stampMatch(&row, orgID, deadLetter, in.Source, ruleID, span.Match); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// stampMatch computes the match-derived columns (byte length, partial-mask
// display string, one-way fingerprints) onto row. A dead-letter sentinel or an
// empty match has nothing to stamp and leaves them zeroed.
func (t *Transformer) stampMatch(row *FindingRow, orgID string, deadLetter bool, source, ruleID, match string) error {
	if deadLetter || match == "" {
		return nil
	}

	row.MatchLen = uint32(len(match)) //nolint:gosec // match length cannot exceed uint32 in practice
	// The shared partial-mask display, so backfilled match_redacted stays
	// byte-identical to what the live ClickHouse writer produces for the same
	// value (replacing the historical <redacted len=N sha=XXXX> form).
	row.MatchRedacted = maskdisplay.Display(source, ruleID, match)

	sum, version, err := t.fingerprinter.HS256([]byte(match))
	if err != nil {
		return fmt.Errorf("global fingerprint for %s: %w", row.ID, err)
	}
	row.FingerprintGlobalHS256 = base64.RawURLEncoding.EncodeToString(sum)
	row.FingerprintPepperVersion = version

	if orgID != "" {
		t.mu.Lock()
		tsum, tversion, terr := t.fingerprinter.TenantedHS256(orgID, []byte(match), risk.WithKeyCache(t.keyCache))
		t.mu.Unlock()
		if terr != nil {
			return fmt.Errorf("tenant fingerprint for %s: %w", row.ID, terr)
		}
		row.FingerprintTenantHS256 = base64.RawURLEncoding.EncodeToString(tsum)
		row.FingerprintPepperVersion = tversion
	}
	return nil
}

// spanRowID derives the deterministic ClickHouse row id for span index i >= 1
// of a Postgres finding. Span 0 keeps the Postgres row id so anything keyed on
// it (webhooks, false-positive marks) keeps resolving; the extra span rows get
// ids that are stable across re-runs so a repeated backfill re-emits the same
// rows instead of minting new ones.
func spanRowID(pgRowID uuid.UUID, i int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:risk:finding:pgspan:"+pgRowID.String()+":"+strconv.Itoa(i)))
}

// sourceSurface maps a row with no span attribution to the text its
// start_pos/end_pos offsets index. These are the pre-spans-column rows (spans
// landed with the CEL custom-rule work, June 2026) plus any source that never
// recorded spans:
//
//   - gitleaks: the composed scan surface (message content plus tool-call
//     arguments — batchMessage.scanSurface); sync-era offsets index the same
//     composition.
//   - presidio: a YAML-reformatted transform of the message, not a raw content
//     slice — reveal refuses these fast instead of slicing the wrong text.
//   - prompt_injection / llm_judge: batch-era matches are rendered JSON
//     artifacts of the judged content, so the offsets index no stored text.
//   - shadow_mcp / account_identity / destructive_tool / cli_destructive: the
//     match is derived metadata (server identifier, account email, tool name),
//     not a slice of any recorded surface.
//   - anything else (e.g. pre-CEL custom rules): unknown — leave the surface
//     empty so reveal falls back to its verified candidate cascade.
func sourceSurface(source string) string {
	switch source {
	case "gitleaks":
		return surfaceScanSurface
	case "presidio":
		return surfaceLegacyPresidio
	case "prompt_injection", "llm_judge":
		return surfaceNone
	case "shadow_mcp", "account_identity", "destructive_tool", "cli_destructive":
		return surfaceDerived
	default:
		return ""
	}
}

// spanSurface maps one recorded span's (field, path) attribution to the text
// its offsets index. Non-custom scanners record a single span with no field
// attribution; those keep the per-source semantics of sourceSurface. An
// unrecognized field (e.g. tool.name) leaves the surface empty so reveal falls
// back to its verified candidate cascade rather than slicing the wrong text.
func spanSurface(source, field, path string) string {
	switch {
	case path != "":
		return surfaceJSONPath
	case field == "tool.args":
		return surfaceToolArgs
	case field == "content", field == "prompt", field == "assistant", field == "tool_result":
		return surfaceContent
	case field == "tool.server", field == "tool.function":
		return surfaceDerived
	case field == "":
		return sourceSurface(source)
	default:
		return ""
	}
}
