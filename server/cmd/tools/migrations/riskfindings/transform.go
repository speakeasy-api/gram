package riskfindings

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
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
	MatchLen                 uint32     `ch:"match_len"`
	MatchRedacted            string     `ch:"match_redacted"`
	FingerprintPepperVersion string     `ch:"fingerprint_pepper_version"`
	FingerprintGlobalHS256   string     `ch:"fingerprint_global_hs256"`
	FingerprintTenantHS256   string     `ch:"fingerprint_tenant_hs256"`
	ExcludedAt               *time.Time `ch:"excluded_at"`
	ExclusionID              *uuid.UUID `ch:"exclusion_id"`
}

// Transformer maps a Postgres SourceRow to a ClickHouse FindingRow, mirroring the
// live ingest path (server/internal/risk/finding_bq.go): it computes the global
// and tenant-qualified HMAC-SHA256 fingerprints of the match and a redacted
// display string. The raw match is never carried into ClickHouse.
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
func (t *Transformer) Transform(_ context.Context, in SourceRow) ([]FindingRow, error) {
	// Only real findings become ClickHouse events. Drop the "nothing found"
	// SourceNone sentinels (found=false) and any row without a rule, matching the
	// live outbox emission. The source already filters these out; this guard keeps
	// the transform correct if it is ever fed an unfiltered source.
	if !in.Found || conv.PtrValOr(in.RuleID, "") == "" {
		return nil, nil
	}

	orgID := strings.TrimSpace(in.OrganizationID)

	match := conv.PtrValOr(in.Match, "")
	deadLetter := conv.PtrValOr(in.DeadLetterReason, "") != ""

	var globalFP, tenantFP, pepperVersion string
	// A dead-letter sentinel or an empty match has nothing to fingerprint.
	if !deadLetter && match != "" {
		sum, version, err := t.fingerprinter.HS256([]byte(match))
		if err != nil {
			return nil, fmt.Errorf("global fingerprint for %s: %w", in.ID, err)
		}
		globalFP = base64.RawURLEncoding.EncodeToString(sum)
		pepperVersion = version

		if orgID != "" {
			t.mu.Lock()
			tsum, tversion, terr := t.fingerprinter.TenantedHS256(orgID, []byte(match), risk.WithKeyCache(t.keyCache))
			t.mu.Unlock()
			if terr != nil {
				return nil, fmt.Errorf("tenant fingerprint for %s: %w", in.ID, terr)
			}
			tenantFP = base64.RawURLEncoding.EncodeToString(tsum)
			pepperVersion = tversion
		}
	}

	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}

	return []FindingRow{{
		ID:                in.ID,
		CreatedAt:         in.CreatedAt,
		OrganizationID:    in.OrganizationID,
		ProjectID:         in.ProjectID.String(),
		RequestID:         "", // not recorded in Postgres risk_results
		ChatMessageID:     in.ChatMessageID.String(),
		RiskPolicyID:      in.RiskPolicyID.String(),
		RiskPolicyVersion: in.RiskPolicyVersion,
		RuleID:            conv.PtrValOr(in.RuleID, ""),
		Description:       conv.PtrValOr(in.Description, ""),
		Source:            in.Source,
		Confidence:        conv.PtrValOr(in.Confidence, 0),
		Tags:              tags,
		StartPos:          conv.PtrValOr(in.StartPos, 0),
		EndPos:            conv.PtrValOr(in.EndPos, 0),
		DeadLetterReason:  conv.PtrValOr(in.DeadLetterReason, ""),
		MatchLen:          uint32(len(match)), //nolint:gosec // match length cannot exceed uint32 in practice
		// Redact every source (no shadow_mcp/account_identity passthrough) via the
		// shared helper, so backfilled match_redacted stays byte-identical to what
		// the future live ClickHouse writer produces for the same secret.
		MatchRedacted:            risk.RedactMatchAll(match, orgID),
		FingerprintPepperVersion: pepperVersion,
		FingerprintGlobalHS256:   globalFP,
		FingerprintTenantHS256:   tenantFP,
		ExcludedAt:               in.ExcludedAt,
		ExclusionID:              in.ExclusionID,
	}}, nil
}
