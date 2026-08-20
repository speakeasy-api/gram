package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type setAdminChatAnalysisSettingsRequest struct {
	OrganizationID string `json:"organization_id"`
	Judge          string `json:"judge"`
	Enabled        *bool  `json:"enabled"`
	DailyCap       *int   `json:"daily_cap"`
}

type triggerAdminChatAnalysisRequest struct {
	OrganizationID string `json:"organization_id"`
}

type triggerAdminChatAnalysisResponse struct {
	ProjectsSignaled int `json:"projects_signaled"`
}

var adminChatAnalysisJudges = map[string]struct{}{
	analysis.WorkUnitsJudgeName: {},
	businessmemory.JudgeName:    {},
}

func (s *Service) writeAdminChatAnalysisSettings(w http.ResponseWriter, ctx context.Context, organizationID string) error {
	settings, err := chatanalysis.LoadSettings(ctx, s.db, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load chat analysis settings").LogError(ctx, s.logger)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode chat analysis settings").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) handleGetChatAnalysisSettings(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.authorizeAdminRequest(r)
	if err != nil {
		return err
	}
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, r.URL.Query().Get("organization_id"))
	if err != nil {
		return err
	}
	return s.writeAdminChatAnalysisSettings(w, ctx, organizationID)
}

func (s *Service) handleTriggerChatAnalysis(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.authorizeAdminRequest(r)
	if err != nil {
		return err
	}

	var body triggerAdminChatAnalysisRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode chat analysis trigger request")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return oops.E(oops.CodeBadRequest, err, "decode chat analysis trigger request")
	}

	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, body.OrganizationID)
	if err != nil {
		return err
	}
	projectsSignaled, err := chatanalysis.TriggerOrganization(ctx, s.db, s.chatAnalysisSignaler, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "trigger chat analysis").LogError(ctx, s.logger)
	}

	_, operatorEmail := adminActor(ctx)
	s.logger.InfoContext(ctx, "triggered chat analysis",
		attr.SlogOrganizationID(organizationID),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(triggerAdminChatAnalysisResponse{ProjectsSignaled: projectsSignaled}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode chat analysis trigger response").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) handleSetChatAnalysisSettings(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.authorizeAdminRequest(r)
	if err != nil {
		return err
	}

	var body setAdminChatAnalysisSettingsRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return oops.E(oops.CodeBadRequest, err, "decode chat analysis settings request")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return oops.E(oops.CodeBadRequest, err, "decode chat analysis settings request")
	}
	if body.Enabled == nil || body.DailyCap == nil {
		return oops.C(oops.CodeBadRequest)
	}
	if _, ok := adminChatAnalysisJudges[body.Judge]; !ok {
		return oops.C(oops.CodeBadRequest)
	}
	if *body.DailyCap < 0 || *body.DailyCap > chatanalysis.MaxDailyCap {
		return oops.C(oops.CodeBadRequest)
	}

	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, body.OrganizationID)
	if err != nil {
		return err
	}
	actor, operatorEmail := adminActor(ctx)
	settings, err := chatanalysis.UpsertSettings(
		ctx, s.db, s.audit, organizationID, body.Judge, *body.Enabled, *body.DailyCap,
		actor, conv.PtrEmpty(audit.SpeakeasyTeamActorLabel),
	)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "update chat analysis settings").LogError(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "updated chat analysis settings",
		attr.SlogOrganizationID(organizationID),
		attr.SlogAuthUserEmail(conv.PtrValOr(operatorEmail, "unknown")),
	)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode chat analysis settings").LogError(ctx, s.logger)
	}
	return nil
}
