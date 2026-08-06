package relay

import (
	"os"
	"strings"

	"github.com/speakeasy-api/agenthooks/telemetry"
)

// telemetryEndpointPath is gram's OTLP/HTTP logs ingestion route — it must
// match the POST route declared in server/design/hooks/design.go
// ("/rpc/hooks.otel/v1/logs", OpenTelemetry's /v1/logs convention).
const telemetryEndpointPath = "/rpc/hooks.otel/v1/logs"

// newTelemetryRecorder builds the OTel log recorder NewRunner installs via
// agenthooks.WithTelemetry: one observational log record per hook event,
// batched in process and shipped to gram's OTLP logs endpoint authenticated
// with the same credential the ingest path resolves (env key, then the
// cached auth file, then the plugin's baked org key).
//
// The recorder only earns its keep in the long-lived hook server
// (`agenthooks server`), which flushes it on idle shutdown and signals; a
// per-hook process (plain `run` mode) usually exits before a batch ships,
// and that loss is accepted — telemetry is best-effort by design.
//
// Fail-open by construction: any reason a recorder cannot be built — an
// unreadable plugin config, an insecure server URL, no credential yet, a
// constructor error — logs at debug and returns nil, and the runner simply
// runs without telemetry. The credential is resolved once, here: a key
// minted or rotated later is picked up when the server process is next
// spawned, not mid-flight.
func newTelemetryRecorder(r *Relay) *telemetry.Recorder {
	cfg := r.cfg
	// Operational kill switch, and how the package's tests keep the batch
	// exporter from posting to their fake ingest servers mid-assertion.
	if v := strings.TrimSpace(os.Getenv("GRAM_HOOKS_DISABLE_TELEMETRY")); v == "1" || strings.EqualFold(v, "true") {
		r.debugf("telemetry: disabled via GRAM_HOOKS_DISABLE_TELEMETRY")
		return nil
	}
	// The same guards deliver applies before sending anything: an unknown
	// deployment identity must not export to the default server, and a
	// credential must never cross plaintext HTTP.
	if cfg.ConfigError != "" {
		r.debugf("telemetry: config-error path=%s err=%s; recorder disabled", cfg.ConfigPath, cfg.ConfigError)
		return nil
	}
	if insecureServerURL(cfg.ServerURL) {
		r.debugf("telemetry: insecure server URL %s; recorder disabled", cfg.ServerURL)
		return nil
	}
	c, ok := resolveAuth(cfg)
	if !ok {
		r.debugf("telemetry: no credentials; recorder disabled")
		return nil
	}
	headers := map[string]string{"Gram-Key": c.APIKey}
	if c.Project != "" {
		headers["Gram-Project"] = c.Project
	}
	rec, err := telemetry.New(telemetry.Config{
		Endpoint: cfg.ServerURL + telemetryEndpointPath,
		Headers:  headers,
		// service.version from Go build info is empty for goreleaser builds;
		// BinaryVersion carries the stamped release, mirroring the
		// X-Gram-Device-Binary-Version header on the ingest path.
		Resource:         map[string]string{"service.version": BinaryVersion},
		Capture:          telemetry.CaptureAttributes,
		Redactor:         nil,
		HonorTraceparent: false,
	})
	if err != nil {
		r.debugf("telemetry: recorder construction failed: %v", err)
		return nil
	}
	return rec
}
