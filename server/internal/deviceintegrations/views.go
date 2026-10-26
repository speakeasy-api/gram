package deviceintegrations

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
)

func formatTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}

func providerView(d providers.Descriptor) *gen.DeviceIntegrationProvider {
	capabilities := make([]string, 0, len(d.Capabilities))
	for _, c := range d.Capabilities {
		capabilities = append(capabilities, string(c))
	}
	fields := make([]*gen.DeviceIntegrationProviderField, 0, len(d.Fields))
	for _, f := range d.Fields {
		fields = append(fields, &gen.DeviceIntegrationProviderField{
			Key:      f.Key,
			Label:    f.Label,
			Kind:     string(f.Kind),
			Secret:   f.Secret,
			Required: f.Required,
		})
	}
	schedules := make([]*gen.DeviceIntegrationProviderSchedule, 0, len(d.Schedules))
	for _, s := range d.Schedules {
		schedules = append(schedules, &gen.DeviceIntegrationProviderSchedule{
			Schedule:        s.Schedule,
			IntervalMinutes: int(s.Interval / time.Minute),
		})
	}
	return &gen.DeviceIntegrationProvider{
		ID:           d.ID,
		DisplayName:  d.DisplayName,
		Capabilities: capabilities,
		Fields:       fields,
		Schedules:    schedules,
	}
}

func emptyConfigView(orgID string, provider string) *gen.DeviceIntegrationConfig {
	return &gen.DeviceIntegrationConfig{
		ID:             nil,
		OrganizationID: orgID,
		Provider:       provider,
		Enabled:        false,
		HasCredentials: false,
		Settings:       map[string]string{},
		CreatedAt:      nil,
		UpdatedAt:      nil,
	}
}

func buildConfigView(cfg Config) *gen.DeviceIntegrationConfig {
	id := cfg.ID.String()
	settings := cfg.Settings
	if settings == nil {
		settings = providers.Settings{}
	}
	// Secrets are required to connect providers that declare secret fields
	// and rotation replaces them wholesale, so an existing config has
	// credentials exactly when its provider expects any.
	hasCredentials := false
	if desc, ok := providers.Lookup(cfg.Provider); ok {
		hasCredentials = len(desc.SecretFields()) > 0
	}
	return &gen.DeviceIntegrationConfig{
		ID:             &id,
		OrganizationID: cfg.OrganizationID,
		Provider:       cfg.Provider,
		Enabled:        cfg.Enabled,
		HasCredentials: hasCredentials,
		Settings:       settings,
		CreatedAt:      formatTime(cfg.CreatedAt),
		UpdatedAt:      formatTime(cfg.UpdatedAt),
	}
}

// scheduleState is the merged schedule+sync row shape shared by the list and
// get queries.
type scheduleState struct {
	Schedule            string
	DisabledAt          pgtype.Timestamptz
	NextPollAfter       pgtype.Timestamptz
	LastPollSuccessAt   pgtype.Timestamptz
	LastPollFailedAt    pgtype.Timestamptz
	LastPollError       pgtype.Text
	ConsecutiveFailures int32
	AutoPausedAt        pgtype.Timestamptz
}

func stateFromListRow(row repo.ListSchedulesWithSyncRow) scheduleState {
	return scheduleState{
		Schedule:            row.Schedule,
		DisabledAt:          row.DisabledAt,
		NextPollAfter:       row.NextPollAfter,
		LastPollSuccessAt:   row.LastPollSuccessAt,
		LastPollFailedAt:    row.LastPollFailedAt,
		LastPollError:       row.LastPollError,
		ConsecutiveFailures: row.ConsecutiveFailures,
		AutoPausedAt:        row.AutoPausedAt,
	}
}

func stateFromGetRow(row repo.GetScheduleWithSyncRow) scheduleState {
	return scheduleState{
		Schedule:            row.Schedule,
		DisabledAt:          row.DisabledAt,
		NextPollAfter:       row.NextPollAfter,
		LastPollSuccessAt:   row.LastPollSuccessAt,
		LastPollFailedAt:    row.LastPollFailedAt,
		LastPollError:       row.LastPollError,
		ConsecutiveFailures: row.ConsecutiveFailures,
		AutoPausedAt:        row.AutoPausedAt,
	}
}

func scheduleStatus(state scheduleState) string {
	switch {
	case state.DisabledAt.Valid:
		return "disabled"
	case state.AutoPausedAt.Valid:
		return "auto_paused"
	case !state.LastPollSuccessAt.Valid && !state.LastPollFailedAt.Valid:
		return "pending"
	case state.LastPollFailedAt.Valid && (!state.LastPollSuccessAt.Valid || state.LastPollFailedAt.Time.After(state.LastPollSuccessAt.Time)):
		return "failed"
	default:
		return "success"
	}
}

func scheduleView(state scheduleState) *gen.DeviceIntegrationScheduleState {
	var lastError *string
	if state.LastPollError.Valid && state.LastPollError.String != "" {
		lastError = &state.LastPollError.String
	}
	return &gen.DeviceIntegrationScheduleState{
		Schedule:            state.Schedule,
		Enabled:             !state.DisabledAt.Valid,
		Status:              scheduleStatus(state),
		LastSyncSuccessAt:   conv.PtrEmpty(conv.FromPGTimestamptz(state.LastPollSuccessAt)),
		LastSyncFailedAt:    conv.PtrEmpty(conv.FromPGTimestamptz(state.LastPollFailedAt)),
		LastSyncError:       lastError,
		NextSyncAfter:       conv.PtrEmpty(conv.FromPGTimestamptz(state.NextPollAfter)),
		ConsecutiveFailures: int(state.ConsecutiveFailures),
		AutoPausedAt:        conv.PtrEmpty(conv.FromPGTimestamptz(state.AutoPausedAt)),
	}
}

// pgTextPtr is conv.FromPGText plus empty-string elision: these are optional
// API fields where "" and absent mean the same thing.
func pgTextPtr(t pgtype.Text) *string {
	v := conv.FromPGText[string](t)
	if v == nil || *v == "" {
		return nil
	}
	return v
}

func deviceView(ctx context.Context, logger *slog.Logger, row repo.ListManagedDevicesRow) *gen.ManagedDevice {
	return &gen.ManagedDevice{
		ID:               row.ID.String(),
		Provider:         row.Provider,
		ExternalID:       row.ExternalID,
		SerialNumber:     pgTextPtr(row.SerialNumber),
		Hostname:         pgTextPtr(row.Hostname),
		OsName:           pgTextPtr(row.OsName),
		OsVersion:        pgTextPtr(row.OsVersion),
		UserEmail:        pgTextPtr(row.UserEmail),
		UserID:           pgTextPtr(row.UserID),
		MdmLastCheckInAt: conv.PtrEmpty(conv.FromPGTimestamptz(row.MdmLastCheckInAt)),
		AgentLastSeenAt:  conv.PtrEmpty(conv.FromPGTimestamptz(row.AgentLastSeenAt)),
		CoverageBucket:   row.CoverageBucket,
		PolicyState:      conv.PtrEmpty(row.PolicyState),
		AgentState:       agentStateView(ctx, logger, row),
		MissingSince:     conv.PtrEmpty(conv.FromPGTimestamptz(row.MissingSince)),
		FirstSeenAt:      row.FirstSeenAt.Time.UTC().Format(time.RFC3339),
		LastSeenAt:       row.LastSeenAt.Time.UTC().Format(time.RFC3339),
	}
}

// reportedTool is one entry of device_agent_state_reports.tools, as the
// agent service stores it.
type reportedTool struct {
	Tool             string `json:"tool"`
	Layer            string `json:"layer"`
	Outcome          string `json:"outcome"`
	PendingReason    string `json:"pending_reason,omitempty"`
	ErrorKind        string `json:"error_kind,omitempty"`
	RestartRequired  bool   `json:"restart_required"`
	ActionRequired   bool   `json:"action_required"`
	DriftCorrections *int   `json:"drift_corrections,omitempty"`
}

func agentStateView(ctx context.Context, logger *slog.Logger, row repo.ListManagedDevicesRow) *gen.ManagedDeviceAgentState {
	if !row.HasReport {
		return nil
	}
	var reported []reportedTool
	if len(row.ReportTools) > 0 {
		if err := json.Unmarshal(row.ReportTools, &reported); err != nil {
			// The agent service wrote this column, so this is a bug, not bad
			// input. The rest of the row is still true, so the page renders it
			// without the tools rather than failing.
			logger.ErrorContext(ctx, "decode stored agent state tools", attr.SlogError(err), attr.SlogOrganizationID(row.OrganizationID))
			reported = nil
		}
	}
	tools := make([]*gen.ManagedDeviceTool, 0, len(reported))
	for _, t := range reported {
		tools = append(tools, &gen.ManagedDeviceTool{
			Tool:             t.Tool,
			Layer:            t.Layer,
			Outcome:          t.Outcome,
			PendingReason:    conv.PtrEmpty(t.PendingReason),
			ErrorKind:        conv.PtrEmpty(t.ErrorKind),
			RestartRequired:  t.RestartRequired,
			ActionRequired:   t.ActionRequired,
			DriftCorrections: t.DriftCorrections,
		})
	}
	return &gen.ManagedDeviceAgentState{
		ReportedAt:         row.ReportReportedAt.Time.UTC().Format(time.RFC3339),
		AppliedEtag:        pgTextPtr(row.ReportAppliedEtag),
		AppliedAt:          conv.PtrEmpty(conv.FromPGTimestamptz(row.ReportAppliedAt)),
		EnforcementHalted:  row.ReportEnforcementHalted,
		CurrentBlocked:     row.ReportCurrentBlocked,
		SpeakeasydVersion:  pgTextPtr(row.ReportSpeakeasydVersion),
		SpeakeasyVersion:   pgTextPtr(row.ReportSpeakeasyVersion),
		SpeakeasyUIVersion: pgTextPtr(row.ReportSpeakeasyUiVersion),
		Os:                 pgTextPtr(row.ReportOs),
		Arch:               pgTextPtr(row.ReportArch),
		Tools:              tools,
		DriftCountingSince: conv.PtrEmpty(conv.FromPGTimestamptz(row.ReportDriftCountingSince)),
		AutoUpdate:         pgTextPtr(row.ReportAutoUpdate),
		UpdateChannel:      pgTextPtr(row.ReportUpdateChannel),
		PinnedTarget:       pgTextPtr(row.ReportPinnedTarget),
		LastRollback:       rollbackView(row),
	}
}

// rollbackView is nil until the agent has rolled itself back.
func rollbackView(row repo.ListManagedDevicesRow) *gen.ManagedDeviceRollback {
	if !row.ReportLastRollbackAt.Valid {
		return nil
	}
	return &gen.ManagedDeviceRollback{
		At:   row.ReportLastRollbackAt.Time.UTC().Format(time.RFC3339),
		From: pgTextPtr(row.ReportLastRollbackFrom),
		To:   pgTextPtr(row.ReportLastRollbackTo),
	}
}
