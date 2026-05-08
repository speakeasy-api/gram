package mdm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mdm/server"
	gen "github.com/speakeasy-api/gram/server/gen/mdm"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	auth      *auth.Auth
	serverURL *url.URL
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsMgr *sessions.Manager,
	authzEngine *authz.Engine,
	serverURL *url.URL,
) *Service {
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mdm"),
		logger:    logger.With(attr.SlogComponent("mdm")),
		auth:      auth.New(logger, db, sessionsMgr, authzEngine),
		serverURL: serverURL,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// GetInstallScript returns the MDM shell script. Hosted dynamically so we can update
// logic without customers touching their MDM policy — only the API key stays in Jamf.
func (s *Service) GetInstallScript(ctx context.Context) ([]byte, error) {
	base := s.serverURL.String()
	script := fmt.Sprintf(`#!/bin/bash
# Gram Claude Code MDM Install Script — auto-served from %s
# Usage: curl -fsSL '%s/rpc/mdm.getInstallScript' | bash -s -- <GRAM_API_KEY>
# Only dependency: curl (always present on macOS).
set -euo pipefail
GRAM_API_KEY="${1:?GRAM_API_KEY required as \$1}"
SETTINGS="$HOME/.claude/settings.json"
mkdir -p "$HOME/.claude"
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"
curl -fsSL \
  -X POST \
  -H "X-Api-Key: $GRAM_API_KEY" \
  -H "Content-Type: application/json" \
  --data-binary @"$SETTINGS" \
  '%s/rpc/mdm.patchClaudeSettings' \
  -o "$SETTINGS.tmp"
mv "$SETTINGS.tmp" "$SETTINGS"
chmod 600 "$SETTINGS"
echo "Gram: settings applied to $SETTINGS"
`, base, base, base)

	return []byte(script), nil
}

// PatchClaudeSettings accepts the current settings.json, injects Gram observability
// config into the relevant keys, and returns the patched JSON. All existing settings
// are preserved — only the specific Gram keys are written.
func (s *Service) PatchClaudeSettings(ctx context.Context, payload *gen.PatchClaudeSettingsPayload, body io.ReadCloser) (io.ReadCloser, error) {
	defer body.Close()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("read body: %w", err), "failed to read settings")
	}

	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		settings = map[string]any{}
	}

	projectSlug := "default"
	if authCtx.ProjectSlug != nil && *authCtx.ProjectSlug != "" {
		projectSlug = *authCtx.ProjectSlug
	}

	apiKey := ""
	if payload.ApikeyToken != nil {
		apiKey = *payload.ApikeyToken
	}

	patch(settings, projectSlug, apiKey, s.serverURL.String())

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("marshal settings: %w", err), "failed to marshal settings")
	}

	return io.NopCloser(bytes.NewReader(out)), nil
}

// patch injects Gram keys into settings without touching any other fields.
func patch(settings map[string]any, projectSlug, apiKey, serverURL string) {
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["CLAUDE_CODE_ENABLE_TELEMETRY"] = "1"
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = serverURL + "/rpc/hooks.otel"
	env["OTEL_EXPORTER_OTLP_HEADERS"] = fmt.Sprintf("Gram-Project=%s,Gram-Key=%s", projectSlug, apiKey)
	env["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/json"
	env["OTEL_LOGS_EXPORTER"] = "otlp"
	env["OTEL_METRICS_EXPORTER"] = "otlp"
	settings["env"] = env

	marketplaces, _ := settings["extraKnownMarketplaces"].(map[string]any)
	if marketplaces == nil {
		marketplaces = map[string]any{}
	}
	marketplaces["gram"] = map[string]any{
		"autoUpdate": true,
		"source": map[string]any{
			"repo":   "speakeasy-api/gram",
			"source": "github",
		},
	}
	settings["extraKnownMarketplaces"] = marketplaces

	plugins, _ := settings["enabledPlugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
	}
	plugins["gram-hooks@gram"] = true
	settings["enabledPlugins"] = plugins
}
