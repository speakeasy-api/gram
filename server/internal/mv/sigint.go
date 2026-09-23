package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// BuildSigintSignalView converts a custom signal row into its API model.
func BuildSigintSignalView(signal repo.SigintCustomSignal) *types.SigintSignal {
	return &types.SigintSignal{
		ID:                 signal.ID.String(),
		ProjectID:          signal.ProjectID.String(),
		Name:               signal.Name,
		Description:        conv.FromPGText[string](signal.Description),
		ClassifierCriteria: conv.FromPGText[string](signal.ClassifierCriteria),
		CreatedAt:          signal.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          signal.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildSigintSignalListView converts custom signal rows into API models.
func BuildSigintSignalListView(signals []repo.SigintCustomSignal) []*types.SigintSignal {
	result := make([]*types.SigintSignal, len(signals))
	for i, signal := range signals {
		result[i] = BuildSigintSignalView(signal)
	}
	return result
}

// BuildSigintSensorView converts a sensor-with-membership row into its API model.
func BuildSigintSensorView(sensor repo.GetSensorRow) *types.SigintSensor {
	signalIDs := make([]string, len(sensor.SignalIds))
	for i, id := range sensor.SignalIds {
		signalIDs[i] = id.String()
	}

	return &types.SigintSensor{
		ID:           sensor.ID.String(),
		ProjectID:    sensor.ProjectID.String(),
		Name:         sensor.Name,
		Description:  conv.FromPGText[string](sensor.Description),
		Instructions: conv.FromPGText[string](sensor.Instructions),
		Mode:         types.SigintSensorMode(sensor.Mode),
		SignalIds:    signalIDs,
		CreatedAt:    sensor.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    sensor.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildSigintSensorListView converts list rows into API models.
func BuildSigintSensorListView(sensors []repo.ListSensorsRow) []*types.SigintSensor {
	result := make([]*types.SigintSensor, len(sensors))
	for i, sensor := range sensors {
		signalIDs := make([]string, len(sensor.SignalIds))
		for j, id := range sensor.SignalIds {
			signalIDs[j] = id.String()
		}
		result[i] = &types.SigintSensor{
			ID:           sensor.ID.String(),
			ProjectID:    sensor.ProjectID.String(),
			Name:         sensor.Name,
			Description:  conv.FromPGText[string](sensor.Description),
			Instructions: conv.FromPGText[string](sensor.Instructions),
			Mode:         types.SigintSensorMode(sensor.Mode),
			SignalIds:    signalIDs,
			CreatedAt:    sensor.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:    sensor.UpdatedAt.Time.Format(time.RFC3339),
		}
	}
	return result
}

// BuildSigintSensorByIDsView converts a post-mutation sensor row into its API model.
func BuildSigintSensorByIDsView(sensor repo.GetSensorsByIDsRow) *types.SigintSensor {
	signalIDs := make([]string, len(sensor.SignalIds))
	for i, id := range sensor.SignalIds {
		signalIDs[i] = id.String()
	}
	return &types.SigintSensor{
		ID:           sensor.ID.String(),
		ProjectID:    sensor.ProjectID.String(),
		Name:         sensor.Name,
		Description:  conv.FromPGText[string](sensor.Description),
		Instructions: conv.FromPGText[string](sensor.Instructions),
		Mode:         types.SigintSensorMode(sensor.Mode),
		SignalIds:    signalIDs,
		CreatedAt:    sensor.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    sensor.UpdatedAt.Time.Format(time.RFC3339),
	}
}
