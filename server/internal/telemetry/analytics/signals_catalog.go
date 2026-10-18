package analytics

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	sigintrepo "github.com/speakeasy-api/gram/server/internal/sigint/repo"
	signalquery "github.com/speakeasy-api/gram/server/internal/telemetry/analytics/signals"
)

// SignalCatalog describes a named metric contract, separately from composed aggregates.
type SignalCatalog struct {
	// Dataset is the query namespace.
	Dataset string `json:"dataset"`

	// Unit names one counted observation.
	Unit string `json:"unit"`

	// Population explains the shared denominator.
	Population string `json:"population"`

	// Steps includes automatic point-budget selection and UTC calendar intervals.
	Steps []string `json:"steps"`

	// Operators apply to winning attribute snapshots.
	Operators []string `json:"operators"`

	// SystemDimensions names typed dimensions outside the arbitrary-label namespace.
	SystemDimensions []string `json:"system_dimensions"`

	// InitialAttributes are standard capture labels, not a closed vocabulary.
	InitialAttributes []string `json:"initial_attributes"`

	// MaxSpanDays bounds query span independently of request-time retention.
	MaxSpanDays int `json:"max_span_days"`

	// MaxGroups bounds selected complete series.
	MaxGroups int `json:"max_groups"`

	// MaxPoints bounds all returned chart points.
	MaxPoints int `json:"max_points"`

	// Sensors provides current names, modes and ordered signal membership.
	Sensors []SignalSensor `json:"sensors"`

	// Signals maps stable signal IDs to current user-facing names.
	Signals []SignalName `json:"signals"`
}

// SignalSensor is bounded, current configuration for measurement discovery.
type SignalSensor struct {
	// ID is the stable project-scoped sensor identity.
	ID string `json:"id"`

	// Name is the current display name.
	Name string `json:"name"`

	// Mode determines the matching rule.
	Mode string `json:"mode"`

	// Enabled controls new evaluations, not historical query availability.
	Enabled bool `json:"enabled"`

	// SignalIDs preserves configured order for cumulative ordered thresholds.
	SignalIDs []string `json:"signal_ids"`
}

// SignalName is a live name for a stable signal identity.
type SignalName struct {
	// ID is the stable signal identity.
	ID string `json:"id"`

	// Name is the current display name.
	Name string `json:"name"`
}

// SignalDefinition is an immutable evaluated definition, independent of current configuration.
type SignalDefinition struct {
	// SensorID identifies the contributing sensor.
	SensorID string `json:"sensor_id"`

	// DefinitionHash matches the reading's semantic fingerprint.
	DefinitionHash string `json:"definition_hash"`

	// Definition contains the canonical semantic question, options and input contract.
	Definition map[string]any `json:"definition"`

	// CapturedAt is the first time this definition was retained.
	CapturedAt string `json:"captured_at"`
}

// DescribeSignals binds the semantic catalog to current project configuration.
func (e *Engine) DescribeSignals(ctx context.Context, tenant Tenant) (*SignalCatalog, error) {
	if !e.catalog.signalMetrics || e.db == nil {
		return nil, fmt.Errorf("signal catalog is unavailable")
	}
	project, err := uuid.Parse(tenant.ProjectID)
	if err != nil {
		return nil, newError(ErrUnsatisfiable, signalquery.Dataset, "project_id", "", "project UUID is invalid")
	}
	if _, err := projectsrepo.New(e.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: project, OrganizationID: tenant.OrganizationID}); err != nil {
		return nil, fmt.Errorf("resolve signal catalog project: %w", err)
	}
	// Authoring admits at most 1000 active sensors and signals per project.
	const configurationLimit = 1000
	q := sigintrepo.New(e.db)
	sensors, err := q.ListSensors(ctx, sigintrepo.ListSensorsParams{ProjectID: project, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: configurationLimit + 1})
	if err != nil {
		return nil, fmt.Errorf("load signal catalog sensors: %w", err)
	}
	labels, err := q.ListSignals(ctx, sigintrepo.ListSignalsParams{ProjectID: project, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: configurationLimit + 1})
	if err != nil {
		return nil, fmt.Errorf("load signal catalog names: %w", err)
	}
	if len(sensors) > configurationLimit || len(labels) > configurationLimit {
		return nil, newError(ErrLimitExceeded, signalquery.Dataset, "configuration", "", "project configuration exceeds the discoverable authoring limit")
	}
	out := &SignalCatalog{Dataset: signalquery.Dataset, Unit: "original_message", Population: "answered original-message readings that measured the selected signal or ordered level", Steps: []string{"auto", "minute", "hour", "day", "week", "month"}, Operators: []string{"equals", "in", "exists", "missing"}, SystemDimensions: []string{"source", "role", "actor", "assistant", "definition"}, InitialAttributes: []string{"model_family", "model_provider", "model_name", "department"}, MaxSpanDays: signalquery.MaxSpanDays, MaxGroups: signalquery.MaxGroups, MaxPoints: signalquery.MaxPoints, Sensors: []SignalSensor{}, Signals: []SignalName{}}
	for _, sensor := range sensors {
		ids := make([]string, len(sensor.SignalIds))
		for i, id := range sensor.SignalIds {
			ids[i] = id.String()
		}
		out.Sensors = append(out.Sensors, SignalSensor{ID: sensor.ID.String(), Name: sensor.Name, Mode: sensor.Mode, Enabled: sensor.Enabled, SignalIDs: ids})
	}
	for _, label := range labels {
		out.Signals = append(out.Signals, SignalName{ID: label.ID.String(), Name: label.Name})
	}
	return out, nil
}

// ReadSignalDefinition loads only the exact tenant/sensor/hash requested.
func (e *Engine) ReadSignalDefinition(ctx context.Context, tenant Tenant, sensorID, hash string) (*SignalDefinition, error) {
	if e.db == nil {
		return nil, fmt.Errorf("signal definitions are unavailable")
	}
	project, projectErr := uuid.Parse(tenant.ProjectID)
	sensor, sensorErr := uuid.Parse(sensorID)
	decoded, hashErr := hex.DecodeString(hash)
	if projectErr != nil || sensorErr != nil || hashErr != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != hash {
		return nil, newError(ErrUnsatisfiable, signalquery.Dataset, "definition_hash", "", "name an exact sensor UUID and 64-character definition hash")
	}
	row, err := sigintrepo.New(e.db).GetDefinitionSnapshot(ctx, sigintrepo.GetDefinitionSnapshotParams{OrganizationID: tenant.OrganizationID, ProjectID: project, SensorID: sensor, DefinitionHash: hash})
	if err != nil {
		return nil, fmt.Errorf("load measured definition: %w", err)
	}
	var definition map[string]any
	if err := json.Unmarshal(row.Definition, &definition); err != nil {
		return nil, fmt.Errorf("decode measured definition: %w", err)
	}
	return &SignalDefinition{SensorID: sensorID, DefinitionHash: hash, Definition: definition, CapturedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)}, nil
}
