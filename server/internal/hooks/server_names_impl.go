package hooks

import (
	"context"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"

	gen "github.com/speakeasy-api/gram/server/gen/hooks_server_names"
	srv "github.com/speakeasy-api/gram/server/gen/http/hooks_server_names/server"
)

var _ gen.Service = (*Service)(nil)

func AttachServerNames(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// List lists all server name display overrides for the authenticated project
func (s *Service) List(ctx context.Context, payload *gen.ListPayload) ([]*gen.ServerNameOverride, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListHooksServerNameOverrides(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to list hooks server name overrides",
		).Log(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()))
	}

	result := make([]*gen.ServerNameOverride, len(rows))
	for i, row := range rows {
		result[i] = &gen.ServerNameOverride{
			ID:            row.ID.String(),
			RawServerName: row.RawServerName,
			DisplayName:   row.DisplayName,
		}
	}

	return result, nil
}

// Upsert creates or updates a server name display override
func (s *Service) Upsert(ctx context.Context, payload *gen.UpsertPayload) (*gen.ServerNameOverride, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "project_id required")
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	if payload.RawServerName == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "raw_server_name cannot be empty")
	}

	if payload.DisplayName == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "display_name cannot be empty")
	}

	override, err := s.repo.UpsertHooksServerNameOverride(ctx, repo.UpsertHooksServerNameOverrideParams{
		ProjectID:     *authCtx.ProjectID,
		RawServerName: payload.RawServerName,
		DisplayName:   payload.DisplayName,
	})
	if err != nil {
		return nil, oops.E(
			oops.CodeUnexpected,
			err,
			"failed to upsert hooks server name override",
		).Log(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()))
	}

	return &gen.ServerNameOverride{
		ID:            override.ID.String(),
		RawServerName: override.RawServerName,
		DisplayName:   override.DisplayName,
	}, nil
}

// Delete deletes a server name display override
func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ProjectID == nil {
		return oops.E(oops.CodeBadRequest, nil, "project_id required")
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return err
	}

	overrideUUID, err := uuid.Parse(payload.OverrideID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid override ID")
	}

	err = s.repo.DeleteHooksServerNameOverride(ctx, repo.DeleteHooksServerNameOverrideParams{
		ID:        overrideUUID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(
			oops.CodeUnexpected,
			err,
			"failed to delete hooks server name override",
		).Log(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()), attr.SlogHookServerNameOverrideID(payload.OverrideID))
	}

	return nil
}
