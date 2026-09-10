package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	hookSkillContentSchemaV1       = "hook.skill-content.v1"
	maxSkillUploadRequestBodyBytes = 512 * 1024
)

func skillScanOperationID(skillVersionID uuid.UUID, policyGenerations []string) string {
	policyGenerations = slices.Clone(policyGenerations)
	slices.Sort(policyGenerations)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("skill_upload:"+skillVersionID.String()+":policies:"+strings.Join(policyGenerations, ","))).String()
}

func (s *Service) UploadSkillContent(ctx context.Context, payload *gen.UploadSkillContentPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if s.productFeatures == nil {
		return oops.E(oops.CodeUnexpected, nil, "skill capture settings are unavailable")
	}

	skillsEnabled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check skills entitlement")
	}
	if !skillsEnabled {
		return nil
	}
	metadataOnly, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkillCaptureMetadataOnly)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check skill capture privacy setting")
	}
	if metadataOnly {
		return nil
	}

	if payload == nil || payload.SchemaVersion != hookSkillContentSchemaV1 {
		return oops.E(oops.CodeBadRequest, nil, "unsupported skill content schema_version")
	}
	if len(payload.RawSha256) != 64 || payload.RawSha256 != strings.ToLower(payload.RawSha256) {
		return oops.E(oops.CodeBadRequest, nil, "raw_sha256 must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(payload.RawSha256); err != nil {
		return oops.E(oops.CodeBadRequest, nil, "raw_sha256 must be 64 lowercase hexadecimal characters")
	}
	digest := sha256.Sum256([]byte(payload.Content))
	if actual := hex.EncodeToString(digest[:]); actual != payload.RawSha256 {
		return oops.E(oops.CodeBadRequest, nil, "skill content does not match raw_sha256")
	}
	observed, err := s.repo.HasSkillObservationRawHash(ctx, repo.HasSkillObservationRawHashParams{
		ProjectID: *authCtx.ProjectID,
		RawSha256: conv.ToPGText(payload.RawSha256),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check observed skill content hash")
	}
	if !observed {
		return oops.E(oops.CodeBadRequest, nil, "raw_sha256 has not been observed for this project")
	}

	result, err := skills.CaptureSkillContent(ctx, s.db, *authCtx.ProjectID, payload.Content)
	if err != nil {
		switch {
		case errors.Is(err, skills.ErrInvalidCapture):
			return oops.E(oops.CodeBadRequest, nil, "%s", strings.TrimPrefix(err.Error(), skills.ErrInvalidCapture.Error()+": "))
		case errors.Is(err, skills.ErrCaptureHashConflict):
			return oops.E(oops.CodeConflict, nil, "raw_sha256 is already associated with different skill content")
		default:
			return oops.E(oops.CodeUnexpected, fmt.Errorf("capture uploaded skill content: %w", err), "store skill content")
		}
	}

	if s.piScanner != nil {
		s.scanCapturedSkillVersion(ctx, authCtx, result.SkillVersionID, payload.Content)
	}
	return nil
}

// scanCapturedSkillVersion judges a captured skill version for prompt
// injection and records the outcome. Capture has already committed by the time
// this runs, so every failure here is logged and swallowed rather than failing
// the upload. See ScanStrictWithVerdict for why a judge failure records nothing.
//
// ponytail: synchronous judge call on the upload path; move off-request if
// upload latency shows up in traces.
func (s *Service) scanCapturedSkillVersion(ctx context.Context, authCtx *contextvalues.AuthContext, skillVersionID uuid.UUID, content string) {
	repo := riskRepo.New(s.db)
	anchor := uuid.NullUUID{UUID: skillVersionID, Valid: true}

	needed, err := repo.SkillVersionNeedsPromptInjectionScan(ctx, riskRepo.SkillVersionNeedsPromptInjectionScanParams{
		ProjectID:      *authCtx.ProjectID,
		SkillVersionID: anchor,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "check skill prompt injection scan state", attr.SlogError(err))
		return
	}
	if !needed {
		return
	}
	policies, err := repo.ListEnabledRiskPoliciesByProject(ctx, *authCtx.ProjectID)
	var policyGenerations []string
	if err != nil {
		s.logger.WarnContext(ctx, "load skill prompt injection policy generation", attr.SlogError(err))
		policyGenerations = []string{"policy_generation_unavailable"}
	} else {
		policyGenerations = make([]string, 0, len(policies))
		for _, policy := range policies {
			if slices.Contains(policy.Sources, promptinjection.Source) {
				policyGenerations = append(policyGenerations, fmt.Sprintf("%s:%d", policy.ID, policy.Version))
			}
		}
	}

	msg := judgemessage.New(message.PromptAttachment, "", content)
	occurredAt := time.Now().UTC()
	result, verdict, err := s.piScanner.ScanStrictWithVerdict(ctx, content, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, msg)
	if err != nil {
		s.logger.WarnContext(ctx, "skill prompt injection scan failed; leaving version unscanned", attr.SlogError(err))
		return
	}
	if result.Completed {
		provenance := metering.RiskProvenance{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			RiskPolicyID:           uuid.Nil,
			RiskPolicyVersion:      0,
			PolicyLinkReason:       "skill_upload_no_policy",
			ChatID:                 uuid.Nil,
			ExternalConversationID: "",
			ChatMessageID:          uuid.Nil,
			ContentPartID:          uuid.Nil,
			MessageLinkReason:      "skill_content_not_chat_message",
			OperationID:            skillScanOperationID(skillVersionID, policyGenerations),
			ExecutionPath:          "skill_upload",
			RequestID:              "",
			MessageType:            message.PromptAttachment,
			HookSource:             "",
			UserID:                 authCtx.UserID,
			ToolCallID:             "",
			ToolName:               "",
			Model:                  verdict.Model,
			Provider:               verdict.Provider,
		}
		go func() {
			recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.riskRecorder.Record(recordCtx, metering.RiskPromptInjection(), provenance, result.STokens, occurredAt)
		}()
	}

	params := riskRepo.RecordSkillPromptInjectionScanParams{
		SkillVersionID: anchor,
		ProjectID:      *authCtx.ProjectID,
		Source:         ra.SourceNone,
		Found:          false,
		RuleID:         pgtype.Text{String: "", Valid: false},
		Description:    pgtype.Text{String: "", Valid: false},
		Match:          pgtype.Text{String: "", Valid: false},
		Confidence:     pgtype.Float8{Float64: 0, Valid: false},
	}
	if len(result.Findings) > 0 {
		f := result.Findings[0]
		params.Source = promptinjection.Source
		params.Found = true
		params.RuleID = pgtype.Text{String: f.RuleID, Valid: true}
		params.Description = pgtype.Text{String: f.Description, Valid: true}
		params.Match = pgtype.Text{String: f.Match, Valid: true}
		params.Confidence = pgtype.Float8{Float64: f.Confidence, Valid: true}
	}

	if err := repo.RecordSkillPromptInjectionScan(ctx, params); err != nil {
		s.logger.WarnContext(ctx, "record skill prompt injection scan", attr.SlogError(err))
	}
}
