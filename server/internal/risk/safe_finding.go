package risk

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// safeMatchVersion identifies the complete metadata contract understood by all
// finding consumers. Presence without this version must not bypass raw checks.
const safeMatchVersion = 1

// CaptureRedactionTag marks deterministic privacy findings without attributing
// them to a different enforcement engine or changing source-based exclusions.
const CaptureRedactionTag = "capture_redaction"

// SafeFindingPreparer computes raw-dependent finding metadata before a producer
// erases the raw match. Decisions use the same evaluators as FindingCHWriter,
// captured at preparation time. It accepts only trusted internal findings.
type SafeFindingPreparer struct {
	// fingerprinter computes the existing global and tenant HMACs.
	fingerprinter Fingerprinter

	// builtinPresets evaluates the reviewed false-positive catalog.
	builtinPresets *presetlib.Library

	// exclusions resolves exact, regex, source and rule exclusions while raw text exists.
	exclusions *FindingExclusionResolver

	// policyFilters resolves disabled rules and built-in preset configuration.
	policyFilters *findingPolicyFilterResolver

	// now timestamps the decision snapshot and any newly applied exclusion.
	now func() time.Time
}

// NewSafeFindingPreparer creates a reusable preparer for trusted internal
// producers. Preparation errors leave the finding unchanged and must be retried
// before the producer discards its raw scanner result.
func NewSafeFindingPreparer(db repo.DBTX, fingerprinter Fingerprinter, builtinPresets *presetlib.Library) *SafeFindingPreparer {
	return &SafeFindingPreparer{
		fingerprinter:  fingerprinter,
		builtinPresets: builtinPresets,
		exclusions:     NewFindingExclusionResolver(db),
		policyFilters:  newFindingPolicyFilterResolver(db),
		now:            time.Now,
	}
}

// Prepare computes safe metadata and clears match only after every computation
// succeeds. Safe findings retain their original ID and location. A valid
// prepared finding is accepted unchanged, making retries idempotent.
func (p *SafeFindingPreparer) Prepare(ctx context.Context, finding *riskv1.Finding) error {
	if finding == nil {
		return errors.New("finding is nil")
	}
	if finding.GetSafeMatch() != nil {
		return ValidateSafeFinding(finding)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("prepare safe finding: %w", err)
	}

	now := p.now().UTC().Format(time.RFC3339Nano)
	safe := riskv1.Finding_SafeMatch_builder{
		Version:                  new(uint32(safeMatchVersion)),
		PreparedAt:               &now,
		MatchLen:                 nil,
		MatchRedacted:            nil,
		FingerprintPepperVersion: nil,
		FingerprintGlobalHs256:   nil,
		FingerprintTenantHs256:   nil,
		DisabledRule:             nil,
		BuiltinPreset:            nil,
		ExcludedAt:               nil,
		ExclusionId:              nil,
		ExcludedReason:           nil,
		ExcludedDetail:           nil,
		CaptureRedacted:          nil,
	}.Build()
	if finding.GetDeadLetterReason() == "" {
		filters, err := p.policyFilters.filtersFor(ctx, finding, make(map[findingPolicyFilterKey]struct{}))
		if err != nil {
			return fmt.Errorf("resolve safe finding policy filters: %w", err)
		}
		match := finding.GetMatch()
		safe.SetDisabledRule(filters.disabledRules.Contains(finding.GetRuleId()))
		safe.SetBuiltinPreset(!safe.GetDisabledRule() && filters.builtinPresetsEnabled && risk_analysis.IsBuiltinFalsePositive(p.builtinPresets, finding.GetSource(), finding.GetRuleId(), match))
		kind := finding.GetEventKind()
		stateChange := kind == chrepo.EventKindSuppression || kind == chrepo.EventKindUnsuppression || (kind != chrepo.EventKindFinding && (finding.GetExcludedAt() != "" || finding.GetFalsePositiveAt() != ""))
		if !stateChange && (safe.GetDisabledRule() || safe.GetBuiltinPreset()) {
			finding.SetSafeMatch(safe)
			finding.ClearMatch()
			return nil
		}
		if finding.GetExcludedAt() == "" {
			exclusionID, excluded, err := p.exclusions.ExcludedBy(ctx, finding)
			if err != nil {
				return fmt.Errorf("resolve safe finding exclusion: %w", err)
			}
			if excluded {
				safe.SetExcludedAt(now)
				safe.SetExclusionId(exclusionID.String())
				safe.SetExcludedReason(chrepo.ExcludedReasonRule)
			}
		}
		if match != "" {
			if len(p.fingerprinter.Versions()) == 0 {
				return errors.New("safe finding fingerprint pepper is unavailable")
			}
			global, version, err := p.fingerprinter.HS256([]byte(match))
			if err != nil {
				return fmt.Errorf("compute safe finding global fingerprint: %w", err)
			}
			safe.SetFingerprintGlobalHs256(EncodeFingerprint(global))
			safe.SetFingerprintPepperVersion(version)
			if orgID := strings.TrimSpace(finding.GetOrganizationId()); orgID != "" {
				tenant, _, err := p.fingerprinter.TenantedHS256(orgID, []byte(match))
				if err != nil {
					return fmt.Errorf("compute safe finding tenant fingerprint: %w", err)
				}
				safe.SetFingerprintTenantHs256(EncodeFingerprint(tenant))
			}
			matchLen := uint32(math.MaxUint32)
			if length := len(match); length >= 0 && length <= math.MaxUint32 {
				matchLen = uint32(length)
			}
			safe.SetMatchLen(matchLen)
			safe.SetMatchRedacted(maskdisplay.Display(finding.GetSource(), finding.GetRuleId(), match))
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("prepare safe finding: %w", err)
	}
	finding.SetSafeMatch(safe)
	finding.ClearMatch()
	return nil
}

// ValidateSafeFinding checks the mutually exclusive safe representation before
// any consumer evaluates raw-dependent rules or accepts carried metadata.
func ValidateSafeFinding(finding *riskv1.Finding) error {
	if finding == nil {
		return errors.New("finding is nil")
	}
	safe := finding.GetSafeMatch()
	if safe == nil {
		return nil
	}
	if finding.HasMatch() {
		return errors.New("finding carries both raw match and safe metadata")
	}
	if safe.GetVersion() != safeMatchVersion {
		return errors.New("finding safe metadata has unsupported version")
	}
	if safe.GetCaptureRedacted() && (finding.GetSource() != "gitleaks" || finding.GetDeadLetterReason() != "" || !slices.Contains(finding.GetTags(), CaptureRedactionTag)) {
		return errors.New("capture-redacted finding has invalid source or provenance")
	}
	if _, err := time.Parse(time.RFC3339, safe.GetPreparedAt()); err != nil {
		return fmt.Errorf("parse safe finding preparation time: %w", err)
	}
	if safe.GetExcludedAt() != "" {
		if _, err := time.Parse(time.RFC3339, safe.GetExcludedAt()); err != nil {
			return fmt.Errorf("parse safe finding exclusion time: %w", err)
		}
		if _, err := uuid.Parse(safe.GetExclusionId()); err != nil {
			return fmt.Errorf("parse safe finding exclusion id: %w", err)
		}
		if safe.GetExcludedReason() != chrepo.ExcludedReasonRule || safe.GetExcludedDetail() != "" {
			return errors.New("safe finding exclusion has invalid rule annotation")
		}
	} else if safe.GetExclusionId() != "" || safe.GetExcludedReason() != "" || safe.GetExcludedDetail() != "" {
		return errors.New("safe finding has exclusion state without a timestamp")
	}
	if finding.GetDeadLetterReason() != "" || safe.GetMatchLen() == 0 {
		if safe.GetMatchLen() != 0 || safe.GetMatchRedacted() != "" || safe.GetFingerprintPepperVersion() != "" || safe.GetFingerprintGlobalHs256() != "" || safe.GetFingerprintTenantHs256() != "" {
			return errors.New("safe finding without a match carries match metadata")
		}
		return nil
	}
	if safe.GetFingerprintPepperVersion() == "" || !validSafeFingerprint(safe.GetFingerprintGlobalHs256()) {
		return errors.New("safe finding global fingerprint is invalid")
	}
	if strings.TrimSpace(finding.GetOrganizationId()) != "" {
		if !validSafeFingerprint(safe.GetFingerprintTenantHs256()) {
			return errors.New("safe finding tenant fingerprint is invalid")
		}
	} else if safe.GetFingerprintTenantHs256() != "" {
		return errors.New("safe finding tenant fingerprint has no organization")
	}
	return nil
}

func validSafeFingerprint(fingerprint string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(fingerprint)
	return err == nil && len(decoded) == sha256.Size
}
