package about

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/about"
	srv "github.com/speakeasy-api/gram/server/gen/http/about/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

type Service struct {
	logger *slog.Logger
	tracer trace.Tracer
}

var _ gen.Service = (*Service)(nil)

type githubRelease struct {
	TagName string `json:"tag_name"`
}

const (
	githubReleasesURL = "https://api.github.com/repos/speakeasy-api/gram/releases"
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider) *Service {
	return &Service{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/about"),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// Openapi implements about.Service.
func (s *Service) Openapi(context.Context) (res *gen.OpenapiResult, body io.ReadCloser, err error) {
	return &gen.OpenapiResult{
		ContentType:   "application/yaml",
		ContentLength: int64(len(openapiDoc)),
	}, io.NopCloser(bytes.NewReader(openapiDoc)), nil
}

// Version implements about.Service.
func (s *Service) Version(ctx context.Context) (res *gen.VersionResult, err error) {
	ctx, span := s.tracer.Start(ctx, "Version")
	defer span.End()

	serverVersion, dashboardVersion := s.getLatestVersions(ctx)

	return &gen.VersionResult{
		ServerVersion:    serverVersion,
		DashboardVersion: dashboardVersion,
		GitSha:           GitSHA,
	}, nil
}

func (s *Service) getLatestVersions(ctx context.Context) (serverVersion, dashboardVersion string) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", githubReleasesURL, nil)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to create GitHub releases request", attr.SlogError(err))
		return "unknown", "unknown"
	}

	resp, err := client.Do(req)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to fetch GitHub releases", attr.SlogError(err))
		return "unknown", "unknown"
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			s.logger.WarnContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		s.logger.WarnContext(ctx, "GitHub API returned non-200 status", attr.SlogHTTPResponseStatusCode(resp.StatusCode))
		return "unknown", "unknown"
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		s.logger.WarnContext(ctx, "failed to decode GitHub releases response", attr.SlogError(err))
		return "unknown", "unknown"
	}

	// Find latest server and dashboard versions
	serverVersion = "unknown"
	dashboardVersion = "unknown"

	for _, release := range releases {
		if strings.HasPrefix(release.TagName, "@gram/server@") && serverVersion == "unknown" {
			serverVersion = strings.TrimPrefix(release.TagName, "@gram/server@")
		}
		if strings.HasPrefix(release.TagName, "@gram/dashboard@") && dashboardVersion == "unknown" {
			dashboardVersion = strings.TrimPrefix(release.TagName, "@gram/dashboard@")
		}

		// Break early if we found both versions
		if serverVersion != "unknown" && dashboardVersion != "unknown" {
			break
		}
	}

	return serverVersion, dashboardVersion
}
