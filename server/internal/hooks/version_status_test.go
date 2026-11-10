package hooks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/wire"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// hookRequestContext runs a request through HookDeviceTelemetry and returns
// the context the hooks handler would see.
func hookRequestContext(t *testing.T, path, binaryVersion string) context.Context {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, nil)
	if binaryVersion != "" {
		req.Header.Set(wire.HeaderDeviceBinaryVersion, binaryVersion)
	}
	var ctx context.Context
	middleware.HookDeviceTelemetry(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), req)
	require.NotNil(t, ctx)
	return ctx
}

func TestWithVersionStatus(t *testing.T) {
	t.Parallel()

	svc := &Service{versionPolicy: hookversions.Policy{Latest: semver.MustParse("0.3.46"), Minimum: semver.MustParse("0.3.0")}}
	tests := []struct {
		name    string
		version string
		want    map[string]any
	}{
		{name: "current", version: "0.3.46", want: map[string]any{"status": "current", "version": "0.3.46", "latest": "0.3.46", "minimum": "0.3.0"}},
		{name: "behind", version: "0.3.20", want: map[string]any{"status": "behind", "version": "0.3.20", "latest": "0.3.46", "minimum": "0.3.0"}},
		{name: "unsupported", version: "0.2.9", want: map[string]any{"status": "unsupported", "version": "0.2.9", "latest": "0.3.46", "minimum": "0.3.0"}},
		{name: "script sender", version: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := hookRequestContext(t, "/rpc/hooks.ingest", tt.version)
			res := svc.withVersionStatus(ctx, &gen.IngestHookResult{Decision: "deny"})
			require.Equal(t, "deny", res.Decision)
			if tt.want == nil {
				require.NotContains(t, res.Effects, effectHooksVersion)
				return
			}
			require.Equal(t, tt.want, res.Effects[effectHooksVersion])
		})
	}
}

func TestWithVersionStatusKeepsOtherEffects(t *testing.T) {
	t.Parallel()

	svc := &Service{versionPolicy: hookversions.Policy{Latest: semver.MustParse("0.3.46"), Minimum: nil}}
	ctx := hookRequestContext(t, "/rpc/hooks.ingest", "0.3.46")
	res := svc.withVersionStatus(ctx, &gen.IngestHookResult{Decision: "allow", Effects: map[string]any{"org_settings": map[string]any{"fail_open": true}}})
	require.Contains(t, res.Effects, "org_settings")
	require.Equal(t, map[string]any{"status": "current", "version": "0.3.46", "latest": "0.3.46"}, res.Effects[effectHooksVersion])
}

func TestWithVersionStatusOffWithoutPolicy(t *testing.T) {
	t.Parallel()

	svc := &Service{versionPolicy: hookversions.Policy{Latest: nil, Minimum: nil}}
	ctx := hookRequestContext(t, "/rpc/hooks.ingest", "0.2.9")
	res := svc.withVersionStatus(ctx, &gen.IngestHookResult{Decision: "allow"})
	require.Nil(t, res.Effects)
}

func TestClaudeSessionStartNoticesLegacyEndpoint(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	sessionID := "legacy-session"

	result, err := ti.service.Claude(t.Context(), &gen.ClaudePayload{HookEventName: "SessionStart", SessionID: &sessionID})
	require.NoError(t, err)
	require.Nil(t, result.SystemMessage, "no notice without a version policy")

	ti.service.SetVersionPolicy(hookversions.Policy{Latest: semver.MustParse("0.3.46"), Minimum: semver.MustParse("0.3.0")})
	result, err = ti.service.Claude(t.Context(), &gen.ClaudePayload{HookEventName: "SessionStart", SessionID: &sessionID})
	require.NoError(t, err)
	require.NotNil(t, result.Continue)
	require.True(t, *result.Continue)
	require.NotNil(t, result.SystemMessage)
	require.Equal(t, legacyHooksNotice, *result.SystemMessage)
}
