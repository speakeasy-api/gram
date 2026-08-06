package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	hookSkillContentSchemaV1       = "hook.skill-content.v1"
	maxSkillUploadRequestBodyBytes = 512 * 1024
)

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

	// Content is immutable per version, so only a newly created version needs
	// judging - a re-upload of an already-known hash would just repeat the
	// same verdict.
	if result.CreatedVersion && s.piScanner != nil {
		s.scanCapturedSkillVersion(ctx, authCtx, result.SkillVersionID, payload.Content)
	}
	return nil
}

// scanCapturedSkillVersion judges a newly captured skill version for prompt
// injection and records any finding. Capture has already committed by the
// time this runs, so a scanning failure is logged and swallowed rather than
// turned into a failed upload. Scan itself never returns an error - it drops
// findings on a judge failure - so only the insert needs handling here.
//
// ponytail: synchronous judge call on the upload path; move off-request if
// upload latency shows up in traces.
func (s *Service) scanCapturedSkillVersion(ctx context.Context, authCtx *contextvalues.AuthContext, skillVersionID uuid.UUID, content string) {
	msg := judgemessage.New(message.PromptAttachment, "", content)
	findings, _ := s.piScanner.Scan(ctx, content, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), authCtx.UserID, msg)
	if len(findings) == 0 {
		return
	}

	f := findings[0]
	if err := riskRepo.New(s.db).InsertSkillPromptInjectionResults(ctx, riskRepo.InsertSkillPromptInjectionResultsParams{
		SkillVersionID: uuid.NullUUID{UUID: skillVersionID, Valid: true},
		RuleID:         f.RuleID,
		Description:    f.Description,
		Match:          f.Match,
		Confidence:     f.Confidence,
		ProjectID:      *authCtx.ProjectID,
	}); err != nil {
		s.logger.WarnContext(ctx, "insert skill prompt injection finding", attr.SlogError(err))
	}
}
