package admin

import (
	"bytes"
	"context"
	"fmt"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	legacy "github.com/speakeasy-api/gram/server/gen/admin_assets"
	assetgen "github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"io"
	"net/http"
)

func (s *Service) SetAssetService(service *assets.Service) { s.assets = service }

func (s *Service) UploadPlatformImage(ctx context.Context, payload *gen.UploadPlatformImagePayload, reader io.ReadCloser) (*gen.UploadImageResult, error) {
	defer o11y.LogDefer(ctx, s.logger, "failed to close admin image upload reader", reader.Close)
	a, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || a == nil || a.SessionID == "" || a.OIDCSubject == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if s.assets == nil {
		return nil, oops.C(oops.CodeUnavailable)
	}
	// Browsers cannot supply Content-Length. Derive it from the bounded body.
	data, err := io.ReadAll(io.LimitReader(reader, assets.MaxFileSizeImage+1))
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "read image body")
	}
	if len(data) > assets.MaxFileSizeImage {
		return nil, oops.E(oops.CodeBadRequest, nil, "image exceeds 4 MiB")
	}
	if payload.ContentType == "application/octet-stream" {
		payload.ContentType = http.DetectContentType(data)
	}
	result, err := s.assets.UploadPlatformImage(ctx, &legacy.UploadPlatformImageForm{SessionToken: nil, ContentType: payload.ContentType, ContentLength: int64(len(data))}, io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("upload admin image: %w", err)
	}
	aresult := result.Asset
	return &gen.UploadImageResult{Asset: &gen.Asset{ID: aresult.ID, Kind: aresult.Kind, Sha256: aresult.Sha256, ContentType: aresult.ContentType, ContentLength: aresult.ContentLength, CreatedAt: aresult.CreatedAt, UpdatedAt: aresult.UpdatedAt}}, nil
}

func (s *Service) ServeImage(ctx context.Context, payload *gen.ServeImageForm) (*gen.ServeImageResult, io.ReadCloser, error) {
	if s.assets == nil {
		return nil, nil, oops.C(oops.CodeUnavailable)
	}
	result, body, err := s.assets.ServeImage(ctx, &assetgen.ServeImageForm{ID: payload.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("serve admin image: %w", err)
	}
	return &gen.ServeImageResult{ContentType: result.ContentType, ContentLength: result.ContentLength, LastModified: result.LastModified, AccessControlAllowOrigin: result.AccessControlAllowOrigin, CrossOriginResourcePolicy: result.CrossOriginResourcePolicy}, body, nil
}
