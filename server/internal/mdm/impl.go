package mdm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mdm/server"
	gen "github.com/speakeasy-api/gram/server/gen/mdm"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	auth      *auth.Auth
	authz     *authz.Engine
	keyPrefix string
	audit     *audit.Logger
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
	auditLogger *audit.Logger,
	env string,
	serverURL *url.URL,
) *Service {
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mdm"),
		logger:    logger.With(attr.SlogComponent("mdm")),
		db:        db,
		auth:      auth.New(logger, db, sessionsMgr, authzEngine),
		authz:     authzEngine,
		keyPrefix: auth.APIKeyPrefix(env),
		audit:     auditLogger,
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

// GenerateDeployScript creates a Hooks-scoped API key for the caller's org and returns a
// ready-to-use MDM deploy script with that key embedded. Upload the returned script to any
// MDM platform (Jamf, Kandji, Mosyle, etc.) — no further configuration needed.
func (s *Service) GenerateDeployScript(ctx context.Context, _ *gen.GenerateDeployScriptPayload) ([]byte, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
	}); err != nil {
		return nil, err
	}

	token, err := generateToken()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to generate key token")
	}
	fullKey := s.keyPrefix + token

	keyHash, err := auth.GetAPIKeyHash(fullKey)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to hash api key")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction")
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	kr := keysrepo.New(s.db).WithTx(dbtx)

	email := ""
	if authCtx.Email != nil {
		email = *authCtx.Email
	}
	keyName := fmt.Sprintf("mdm-hooks %s %s", email, time.Now().UTC().Format("2006-01-02"))
	createdKey, err := kr.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Name:            keyName,
		KeyHash:         keyHash,
		KeyPrefix:       s.keyPrefix + token[:5],
		Scopes:          []string{auth.APIKeyScopeHooks.String()},
		CreatedByUserID: authCtx.UserID,
		ProjectID:       uuid.NullUUID{},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to create api key").Log(ctx, s.logger)
	}

	if err := s.audit.LogKeyCreate(ctx, dbtx, audit.LogKeyCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        uuid.NullUUID{},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(createdKey.ID),
		KeyName:          keyName,
		Scopes:           []string{auth.APIKeyScopeHooks.String()},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to log key creation").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to commit key creation")
	}

	return s.renderDeployScript(fullKey), nil
}

// renderDeployScript returns the MDM deploy script with the given API key embedded.
func (s *Service) renderDeployScript(apiKey string) []byte {
	base := s.serverURL.String()
	script := fmt.Sprintf(`#!/bin/bash
# Speakeasy Claude Code MDM Deploy Script
#
# Works with any MDM that supports arbitrary shell scripts (Jamf, Kandji, Mosyle, etc.)
# Set policy trigger to "Login" or "Recurring check-in" for idempotent rollout.
# Only dependency: curl (always present on macOS).
set -euo pipefail

SPEAKEASY_API_KEY="%s"
SPEAKEASY_INSTALL_SCRIPT="%s/rpc/mdm.getInstallScript"

CONSOLE_USER=$(stat -f '%%Su' /dev/console 2>/dev/null || true)
[[ "$CONSOLE_USER" =~ ^(root|loginwindow|)$ ]] && { echo "Speakeasy: no console user logged in, skipping" >&2; exit 0; }

USER_UID=$(id -u "$CONSOLE_USER")
USER_HOME=$(/usr/sbin/dscl . -read "/Users/$CONSOLE_USER" NFSHomeDirectory | awk '{print $2}')

echo "Speakeasy: applying settings for $CONSOLE_USER..."

WRAPPER=$(mktemp)
trap 'rm -f "$WRAPPER"' EXIT
cat > "$WRAPPER" <<WEOF
#!/bin/bash
export HOME="$USER_HOME"
curl -fsSL "$SPEAKEASY_INSTALL_SCRIPT" | bash -s -- "$SPEAKEASY_API_KEY"
WEOF
chmod +x "$WRAPPER"

/bin/launchctl asuser "$USER_UID" "$WRAPPER"
echo "Speakeasy: done."
`, apiKey, base)
	return []byte(script)
}

func generateToken() (string, error) {
	const randomKeyLength = 64
	randomBytes := make([]byte, randomKeyLength/2)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate random token bytes: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

// GetInstallScript returns the per-user install script. The deploy script fetches and runs
// this on each login — logic updates automatically without MDM policy changes.
func (s *Service) GetInstallScript(ctx context.Context) ([]byte, error) {
	base := s.serverURL.String()
	script := fmt.Sprintf(`#!/bin/bash
# Speakeasy Claude Code Install Script — auto-served from %s
# Usage: curl -fsSL '%s/rpc/mdm.getInstallScript' | bash -s -- <SPEAKEASY_API_KEY>
# Only dependency: curl (always present on macOS).
set -euo pipefail
SPEAKEASY_API_KEY="${1:?SPEAKEASY_API_KEY required as \$1}"
SETTINGS="$HOME/.claude/settings.json"
mkdir -p "$HOME/.claude"
[ -f "$SETTINGS" ] || echo '{}' > "$SETTINGS"
curl -fsSL \
  -X POST \
  -H "X-Api-Key: $SPEAKEASY_API_KEY" \
  -H "Content-Type: application/json" \
  --data-binary @"$SETTINGS" \
  '%s/rpc/mdm.patchClaudeSettings' \
  -o "$SETTINGS.tmp"
mv "$SETTINGS.tmp" "$SETTINGS"
chmod 600 "$SETTINGS"
echo "Speakeasy: settings applied to $SETTINGS"
`, base, base, base)

	return []byte(script), nil
}

// PatchClaudeSettings accepts the current settings.json, injects Speakeasy observability
// config into the relevant keys, and returns the patched JSON. All existing settings
// are preserved — only the specific Speakeasy keys are written.
func (s *Service) PatchClaudeSettings(ctx context.Context, payload *gen.PatchClaudeSettingsPayload, body io.ReadCloser) (io.ReadCloser, error) {
	defer o11y.NoLogDefer(func() error { return body.Close() })

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

// patch injects Speakeasy keys into settings without touching any other fields.
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
