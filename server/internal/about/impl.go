package about

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/about"
	srv "github.com/speakeasy-api/gram/server/gen/http/about/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// deviceAgentReleasesBaseURL is the public, unauthenticated bucket the
// device-agent release pipeline publishes to (device-agent's release.yml,
// release-pkg-macos job). The pkg itself has no "latest" alias there, same as
// the raw daemon/CLI binaries — every consumer resolves the current version
// off releases.json and builds the versioned URL, which is what this handler
// does server-side so docs/IT-admin instructions have one stable link.
const deviceAgentReleasesBaseURL = "https://storage.googleapis.com/speakeasy-device-agent-releases-prod"

const installDeviceAgentMacOSPath = "/v1/install/device-agent-macos.pkg"
const installDeviceAgentWindowsPath = "/v1/install/device-agent-windows.msi"

// maxManifestSize bounds how much of the releases manifest response we will
// read; releases.json is tiny, so this is generous headroom against a large
// or malicious upstream response.
const maxManifestSize = 1 << 20 // 1MiB

// deviceAgentVersionPattern matches the semver shape produced by the
// device-agent release pipeline (e.g. "1.2.3" or "1.2.3-beta.1"), so a
// malformed manifest can't smuggle path-traversal or other unexpected
// characters into the redirect target.
var deviceAgentVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

type deviceAgentReleasesManifest struct {
	Latest struct {
		Speakeasyd struct {
			Version string `json:"version"`
		} `json:"speakeasyd"`
	} `json:"latest"`
}

type Service struct {
	logger *slog.Logger
	tracer trace.Tracer

	guardianPolicy         *guardian.Policy
	deviceAgentManifestURL string
	deviceAgentReleasesURL string
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy) *Service {
	return &Service{
		logger:                 logger.With(attr.SlogComponent("about")),
		tracer:                 tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/about"),
		guardianPolicy:         guardianPolicy,
		deviceAgentManifestURL: deviceAgentReleasesBaseURL + "/releases.json",
		deviceAgentReleasesURL: deviceAgentReleasesBaseURL,
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

	// Raw HTTP handlers: stable, version-agnostic links to the current signed
	// macOS device-agent pkg and Windows msi, for docs/IT-admin instructions
	// that can't run the dashboard's own version-resolution logic.
	mux.Handle("GET", installDeviceAgentMacOSPath, service.handleInstallDeviceAgentMacOS)
	mux.Handle("GET", installDeviceAgentWindowsPath, service.handleInstallDeviceAgentWindows)
}

// Openapi implements about.Service.
func (s *Service) Openapi(context.Context) (res *gen.OpenapiResult, body io.ReadCloser, err error) {
	return &gen.OpenapiResult{
		ContentType:   "text/yaml",
		ContentLength: int64(len(openapiDoc)),
	}, io.NopCloser(bytes.NewReader(openapiDoc)), nil
}

// handleInstallDeviceAgentMacOS resolves the current device-agent version from
// the public releases manifest and 302-redirects to that version's signed
// pkg. Never hardcode the pkg URL elsewhere; link here instead so every
// consumer stays current without a docs edit on every release.
func (s *Service) handleInstallDeviceAgentMacOS(w http.ResponseWriter, r *http.Request) {
	s.redirectToDeviceAgentArtifact(w, r, "about.handleInstallDeviceAgentMacOS", "speakeasy-agent_%s.pkg")
}

// handleInstallDeviceAgentWindows is the Windows analog of
// handleInstallDeviceAgentMacOS: a stable link that 302-redirects to the
// current version's signed msi.
func (s *Service) handleInstallDeviceAgentWindows(w http.ResponseWriter, r *http.Request) {
	s.redirectToDeviceAgentArtifact(w, r, "about.handleInstallDeviceAgentWindows", "speakeasy-agent_%s.msi")
}

// redirectToDeviceAgentArtifact resolves the current device-agent version from
// the public releases manifest and 302-redirects to that version's copy of the
// artifact named by artifactPattern (a fmt pattern with one %s for the
// version). Installer artifacts follow the same on-ramp rule: published under
// v<version>/ in the bucket but deliberately absent from releases.json.
func (s *Service) redirectToDeviceAgentArtifact(w http.ResponseWriter, r *http.Request, spanName string, artifactPattern string) {
	ctx, span := s.tracer.Start(r.Context(), spanName)
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.deviceAgentManifestURL, nil)
	if err != nil {
		span.SetStatus(codes.Error, "build manifest request")
		s.logger.ErrorContext(ctx, "build device-agent manifest request", attr.SlogError(err))
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}

	client := s.guardianPolicy.Client()
	client.Timeout = 5 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		span.SetStatus(codes.Error, "fetch manifest")
		s.logger.ErrorContext(ctx, "fetch device-agent releases manifest", attr.SlogError(err))
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "manifest non-200")
		s.logger.ErrorContext(ctx, "device-agent releases manifest returned non-200", attr.SlogHTTPResponseStatusCode(resp.StatusCode))
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}

	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxManifestSize))

	var manifest deviceAgentReleasesManifest
	if err := decoder.Decode(&manifest); err != nil {
		span.SetStatus(codes.Error, "decode manifest")
		s.logger.ErrorContext(ctx, "decode device-agent releases manifest", attr.SlogError(err))
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}

	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("manifest body has trailing data after the JSON object")
		}
		span.SetStatus(codes.Error, "trailing data after manifest")
		s.logger.ErrorContext(ctx, "device-agent releases manifest has trailing data after JSON object", attr.SlogError(err))
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}

	version := manifest.Latest.Speakeasyd.Version
	if !deviceAgentVersionPattern.MatchString(version) {
		span.SetStatus(codes.Error, "invalid version")
		s.logger.ErrorContext(ctx, "device-agent releases manifest has invalid latest.speakeasyd.version")
		http.Error(w, "device-agent installer temporarily unavailable", http.StatusBadGateway)
		return
	}

	artifactURL := fmt.Sprintf("%s/v%s/%s", s.deviceAgentReleasesURL, version, fmt.Sprintf(artifactPattern, version))
	http.Redirect(w, r, artifactURL, http.StatusFound)
}
