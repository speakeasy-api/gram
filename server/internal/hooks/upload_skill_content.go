package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
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

	if _, err := skills.CaptureSkillContent(ctx, s.db, *authCtx.ProjectID, payload.Content); err != nil {
		switch {
		case errors.Is(err, skills.ErrInvalidCapture):
			return oops.E(oops.CodeBadRequest, nil, "%s", strings.TrimPrefix(err.Error(), skills.ErrInvalidCapture.Error()+": "))
		case errors.Is(err, skills.ErrCaptureHashConflict):
			return oops.E(oops.CodeConflict, nil, "raw_sha256 is already associated with different skill content")
		default:
			return oops.E(oops.CodeUnexpected, fmt.Errorf("capture uploaded skill content: %w", err), "store skill content")
		}
	}
	return nil
}
