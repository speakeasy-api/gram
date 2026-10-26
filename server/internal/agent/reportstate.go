package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// StateReportRate bounds state reports per device. The agent sends at
// most once a reconcile tick (~60s); the limit stops a leaked org key from
// churning one device's attested state. Keyed per device, not per key,
// because a fleet shares one org key.
var StateReportRate = ratelimit.PerMinute(10)

// WithStateReportLimiter sets the per-device limit on reportState. Nil leaves
// it unlimited.
func (s *Service) WithStateReportLimiter(limiter *ratelimit.Limiter) *Service {
	s.stateReportLimiter = limiter
	return s
}

// Report enum values, matching device-agent ADR-0029. Values outside these
// sets are stored as stateUnknown: a newer agent may send one this server
// does not know, and rejecting the report would lose everything else in it.
var (
	stateLayers         = []string{"user", "managed", "off"}
	stateOutcomes       = []string{"armed", "pending", "skipped", "error", "unmanaged"}
	statePendingReasons = []string{"privileged_writer_not_ready", "plugin_bundle_not_cloned"}
	stateErrorKinds     = []string{"permission_denied", "not_found", "invalid_config", "unknown"}
	stateWithheldReason = []string{"user_disabled", "not_opted_in"}
	stateAutoUpdates    = []string{"disabled", "notify", "automatic"}
)

const stateUnknown = "unknown"

// stateTool is the stored shape of one tools[] entry.
type stateTool struct {
	Tool             string `json:"tool"`
	Layer            string `json:"layer"`
	Outcome          string `json:"outcome"`
	PendingReason    string `json:"pending_reason,omitempty"`
	ErrorKind        string `json:"error_kind,omitempty"`
	RestartRequired  bool   `json:"restart_required"`
	ActionRequired   bool   `json:"action_required"`
	DriftCorrections *int   `json:"drift_corrections,omitempty"`
}

// stateWithheldPlugin is the stored shape of one withheld_plugins[] entry.
type stateWithheldPlugin struct {
	Marketplace string `json:"marketplace,omitempty"`
	Slug        string `json:"slug"`
	Reason      string `json:"reason"`
}

// maxStateReportBytes bounds a reportState body before Goa decodes it. The
// declared caps (32 tools, 500 withheld plugins, short strings) fit in well
// under 256 KiB; unknown fields from newer agents are tolerated, not unbounded.
const maxStateReportBytes = 512 << 10

// limitStateReportBody caps the request body, so the decoder fails on an
// oversized report instead of buffering it.
func limitStateReportBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxStateReportBytes)
		next.ServeHTTP(w, r)
	})
}

// ReportState stores the latest applied state a device agent reports. The
// reporting identity mirrors ReportAIScan; agent identities are refused
// because attestation is about a person's endpoint. A non-endpoint box is
// acknowledged without storing, for the reason GetPlugins keeps those out
// of the device heartbeat tables.
func (s *Service) ReportState(ctx context.Context, payload *gen.ReportStatePayload) (*gen.ReportStateResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, ok := deviceagenttoken.EffectiveAgent(ctx); ok {
		return nil, oops.E(oops.CodeForbidden, nil, "state reports attest a person's device; an agent identity cannot send one")
	}
	email, err := pollingEmail(authCtx, conv.PtrValOr(payload.Email, ""))
	if err != nil {
		return nil, err
	}

	// Charged before the environment check, so a report that is not stored
	// still spends the device's budget.
	serial := deviceidentity.NormalizeSerial(payload.SerialNumber)
	if err := s.allowStateReport(ctx, authCtx.ActiveOrganizationID, serial, email); err != nil {
		return nil, err
	}

	receivedAt := time.Now().UTC()
	if deviceidentity.NormalizeEnvironment(payload.Environment) != deviceidentity.EnvironmentEndpoint {
		return &gen.ReportStateResult{Stored: false, AppliedEtag: nil, ReceivedAt: receivedAt.Format(time.RFC3339)}, nil
	}

	params, err := stateReportParams(authCtx.ActiveOrganizationID, email, serial, payload)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.UpsertDeviceAgentStateReport(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording device agent state report").LogError(ctx, s.logger)
	}

	return &gen.ReportStateResult{
		Stored:      true,
		AppliedEtag: conv.FromPGText[string](row.AppliedEtag),
		ReceivedAt:  row.ReportedAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

// allowStateReport charges one report to the device's budget. The budget is
// per serial when there is one, so a caller cannot reset it by changing the
// vouched email; two people sharing a Mac share it, well inside the limit.
// A limiter store outage fails open: it isn't a throttle.
func (s *Service) allowStateReport(ctx context.Context, organizationID, serial, email string) error {
	if s.stateReportLimiter == nil {
		return nil
	}
	key := organizationID + ":serial:" + serial
	if serial == "" {
		key = organizationID + ":email:" + email
	}
	res, err := s.stateReportLimiter.Allow(ctx, key)
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "reportState rate limit unavailable", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
		return nil
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "too many state reports from this device; retry shortly")
	}
	return nil
}

// stateReportParams validates the payload and maps it onto the upsert.
func stateReportParams(organizationID, email, serial string, payload *gen.ReportStatePayload) (repo.UpsertDeviceAgentStateReportParams, error) {
	var zero repo.UpsertDeviceAgentStateReportParams

	generatedAt, err := parseReportTime("generated_at", &payload.GeneratedAt)
	if err != nil {
		return zero, err
	}
	if generatedAt == nil {
		return zero, oops.E(oops.CodeBadRequest, nil, "generated_at is required")
	}
	policy := payload.Policy
	if payload.Agent == nil || policy == nil {
		return zero, oops.E(oops.CodeBadRequest, nil, "agent and policy are required")
	}
	appliedAt, err := parseReportTime("policy.applied_at", policy.AppliedAt)
	if err != nil {
		return zero, err
	}
	driftSince, err := parseReportTime("drift_counting_since", payload.DriftCountingSince)
	if err != nil {
		return zero, err
	}

	tools := make([]stateTool, 0, len(payload.Tools))
	degraded := 0
	for _, t := range payload.Tools {
		if t == nil {
			return zero, oops.E(oops.CodeBadRequest, nil, "tools must not contain null entries")
		}
		tool := stateTool{
			Tool:             strings.TrimSpace(t.Tool),
			Layer:            normalizeStateValue(t.Layer, stateLayers),
			Outcome:          normalizeStateValue(t.Outcome, stateOutcomes),
			PendingReason:    "",
			ErrorKind:        "",
			RestartRequired:  conv.PtrValOr(t.RestartRequired, false),
			ActionRequired:   conv.PtrValOr(t.ActionRequired, false),
			DriftCorrections: t.DriftCorrections,
		}
		if tool.Tool == "" {
			return zero, oops.E(oops.CodeBadRequest, nil, "tool must not be blank")
		}
		if t.PendingReason != nil && *t.PendingReason != "" {
			tool.PendingReason = normalizeStateValue(*t.PendingReason, statePendingReasons)
		}
		if t.ErrorKind != nil && *t.ErrorKind != "" {
			tool.ErrorKind = normalizeStateValue(*t.ErrorKind, stateErrorKinds)
		}
		// Skipped means the tool is not installed, which is nothing to fix.
		if tool.Outcome == "error" || tool.Outcome == "pending" {
			degraded++
		}
		tools = append(tools, tool)
	}

	withheld := make([]stateWithheldPlugin, 0, len(policy.WithheldPlugins))
	for _, p := range policy.WithheldPlugins {
		if p == nil {
			return zero, oops.E(oops.CodeBadRequest, nil, "withheld_plugins must not contain null entries")
		}
		slug := strings.TrimSpace(p.Slug)
		if slug == "" {
			return zero, oops.E(oops.CodeBadRequest, nil, "withheld_plugins slug must not be blank")
		}
		withheld = append(withheld, stateWithheldPlugin{
			Marketplace: strings.TrimSpace(conv.PtrValOr(p.Marketplace, "")),
			Slug:        slug,
			Reason:      normalizeStateValue(p.Reason, stateWithheldReason),
		})
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "encode tools")
	}
	withheldJSON, err := json.Marshal(withheld)
	if err != nil {
		return zero, oops.E(oops.CodeUnexpected, err, "encode withheld plugins")
	}

	var (
		autoUpdate, channel, pinned pgtype.Text
		currentBlocked              bool
		rollbackAt                  *time.Time
		rollbackFrom, rollbackTo    pgtype.Text
	)
	if update := payload.Update; update != nil {
		if update.AutoUpdate != nil && *update.AutoUpdate != "" {
			autoUpdate = conv.ToPGText(normalizeStateValue(*update.AutoUpdate, stateAutoUpdates))
		}
		channel = conv.PtrToPGTextTrimmed(update.Channel)
		pinned = conv.PtrToPGTextTrimmed(update.PinnedTarget)
		currentBlocked = conv.PtrValOr(update.CurrentBlocked, false)
		if rollback := update.LastRollback; rollback != nil {
			if rollbackAt, err = parseReportTime("update.last_rollback.at", &rollback.At); err != nil {
				return zero, err
			}
			rollbackFrom = conv.PtrToPGTextTrimmed(rollback.From)
			rollbackTo = conv.PtrToPGTextTrimmed(rollback.To)
		}
	}

	return repo.UpsertDeviceAgentStateReportParams{
		OrganizationID:     organizationID,
		Email:              email,
		SerialNumber:       conv.ToPGTextEmpty(serial),
		Hostname:           conv.PtrToPGTextTrimmed(payload.Hostname),
		SchemaVersion:      int32(payload.SchemaVersion), // #nosec G115 -- the Goa payload bounds it to the int32 range.
		GeneratedAt:        conv.ToPGTimestamptz(*generatedAt),
		AppliedEtag:        conv.PtrToPGTextTrimmed(policy.AppliedEtag),
		AppliedAt:          conv.PtrToPGTimestamptz(appliedAt),
		EnforcementHalted:  payload.EnforcementHalted,
		RemoteConfigEtag:   conv.PtrToPGTextTrimmed(policy.RemoteConfigEtag),
		RemoteConfigFailed: conv.PtrValOr(policy.RemoteConfigFailed, false),
		WithheldPlugins:    withheldJSON,
		Tools:              toolsJSON,
		DegradedToolCount:  int32(degraded), // #nosec G115 -- the Goa payload caps tools at 32 entries.
		DriftCountingSince: conv.PtrToPGTimestamptz(driftSince),
		SpeakeasydVersion:  conv.ToPGTextEmpty(strings.TrimSpace(payload.Agent.Speakeasyd)),
		SpeakeasyVersion:   conv.PtrToPGTextTrimmed(payload.Agent.Speakeasy),
		SpeakeasyUiVersion: conv.PtrToPGTextTrimmed(payload.Agent.SpeakeasyUI),
		Os:                 conv.PtrToPGTextTrimmed(payload.Agent.Os),
		Arch:               conv.PtrToPGTextTrimmed(payload.Agent.Arch),
		AutoUpdate:         autoUpdate,
		UpdateChannel:      channel,
		PinnedTarget:       pinned,
		CurrentBlocked:     currentBlocked,
		LastRollbackAt:     conv.PtrToPGTimestamptz(rollbackAt),
		LastRollbackFrom:   rollbackFrom,
		LastRollbackTo:     rollbackTo,
	}, nil
}

// parseReportTime parses an optional RFC 3339 field. Goa checks the format;
// this turns it into a time without re-validating the shape.
func parseReportTime(field string, value *string) (*time.Time, error) {
	if value == nil || *value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s must be a valid RFC 3339 timestamp", field)
	}
	t = t.UTC()
	return &t, nil
}

// normalizeStateValue lowercases v and maps anything outside known to
// stateUnknown.
func normalizeStateValue(v string, known []string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if slices.Contains(known, v) {
		return v
	}
	return stateUnknown
}
