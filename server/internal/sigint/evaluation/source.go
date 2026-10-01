// Package evaluation classifies source events and publishes sensor readings.
package evaluation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// Sensor is the effective, ordered configuration used by one evaluation.
type Sensor struct {
	// ID is the project-scoped sensor identity.
	ID string

	// Slug is the sensor's human-readable external identifier.
	Slug string

	// Mode determines how signals are compiled and interpreted.
	Mode string

	// Instructions is nil for an incomplete draft lacking required instructions.
	Instructions *string

	// Signals contains active signals in membership order.
	Signals []classifier.Option

	// SignalSlugs maps each signal identity to its external identifier.
	SignalSlugs map[classifier.OptionKey]string
}

// Source loads tenant-scoped definitions eligible for an event kind in a
// consistent snapshot. Adapters must not broaden the selected sensor set.
type Source interface {
	Load(context.Context, string, uuid.UUID, string) ([]Sensor, error)
}

// Repository loads configuration from the primary database.
type Repository struct{ db *pgxpool.Pool }

// NewRepository constructs a configuration source.
func NewRepository(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

// Load pins ownership to the organization and project in the same SQL statement.
// Empty sensors are retained for draft accounting; deleted/mismatched projects
// yield no definitions.
func (r *Repository) Load(ctx context.Context, org string, project uuid.UUID, kind string) ([]Sensor, error) {
	// Existing configuration is conversation-only. New receivers must explicitly
	// introduce sensor applicability before evaluating this configuration.
	if kind != ConversationMessageKind {
		return nil, nil
	}
	rows, err := repo.New(r.db).LoadEvaluationSensors(ctx, repo.LoadEvaluationSensorsParams{ProjectID: project, OrganizationID: org})
	if err != nil {
		return nil, fmt.Errorf("load evaluation sensors: %w", err)
	}
	var sensors []Sensor
	for _, row := range rows {
		id := row.SensorID.String()
		if len(sensors) == 0 || sensors[len(sensors)-1].ID != id {
			var instructions *string
			if row.Instructions.Valid {
				instructions = &row.Instructions.String
			}
			sensors = append(sensors, Sensor{ID: id, Slug: row.SensorSlug, Mode: row.Mode, Instructions: instructions, Signals: nil, SignalSlugs: make(map[classifier.OptionKey]string)})
		}
		if !row.SignalID.Valid {
			continue
		}
		var description classifier.Entry
		if row.ClassifierCriteria.Valid {
			description = classifier.Text(row.ClassifierCriteria.String)
		}
		sensors[len(sensors)-1].Signals = append(sensors[len(sensors)-1].Signals, classifier.NewOption(classifier.OptionKey(row.SignalID.UUID.String()), description))
		sensors[len(sensors)-1].SignalSlugs[classifier.OptionKey(row.SignalID.UUID.String())] = row.SignalSlug.String
	}
	return sensors, nil
}
