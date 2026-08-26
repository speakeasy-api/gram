package explore

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
)

func TestMetaExposesLabeledSemanticDatasets(t *testing.T) {
	t.Parallel()

	ctx, instance := newExploreTestService(t)
	result, err := instance.service.Meta(ctx, &gen.MetaPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Datasets, 3)
	require.Equal(t, []string{"events", "turn_usage", "user_usage"}, []string{
		result.Datasets[0].Name,
		result.Datasets[1].Name,
		result.Datasets[2].Name,
	})
	require.Equal(t, []string{"Events", "Turn usage", "User usage"}, []string{
		result.Datasets[0].Label,
		result.Datasets[1].Label,
		result.Datasets[2].Label,
	})
	require.Equal(t, []string{"event", "usage", "usage"}, []string{
		result.Datasets[0].Category,
		result.Datasets[1].Category,
		result.Datasets[2].Category,
	})
	require.Equal(t, []string{
		"Individual agent activity events.",
		"Cost and token usage for each agent turn.",
		"Cost and token usage for each user and reporting interval.",
	}, []string{
		result.Datasets[0].Description,
		result.Datasets[1].Description,
		result.Datasets[2].Description,
	})
	require.Equal(t, []string{"event", "turn", "user and reporting interval"}, []string{
		result.Datasets[0].Grain,
		result.Datasets[1].Grain,
		result.Datasets[2].Grain,
	})

	turnUsage := result.Datasets[1]
	inputTokens := turnUsage.Fields[0]
	for _, field := range turnUsage.Fields {
		if field.Name == "input_tokens" {
			inputTokens = field
			break
		}
	}
	require.Equal(t, "input_tokens", inputTokens.Name)
	require.Equal(t, "Input tokens", inputTokens.Label)
	require.Equal(t, "tokens", inputTokens.Unit)

	for _, dataset := range result.Datasets {
		var modelFields []string
		for _, field := range dataset.Fields {
			if field.Name == "request_model" || field.Name == "response_model" {
				modelFields = append(modelFields, field.Name)
			}
		}
		require.Equal(t, []string{"request_model", "response_model"}, modelFields, dataset.Name)
	}
}
