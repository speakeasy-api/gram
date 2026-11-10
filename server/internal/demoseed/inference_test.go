package demoseed

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInferenceGrantSelectorsAreProjectScoped(t *testing.T) {
	t.Parallel()
	require.Equal(t, "inference:execute", demoInferenceScope)
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		t.Run(spec.OrgID, func(t *testing.T) {
			t.Parallel()
			selectors, err := inferenceGrantSelectors(uuid.MustParse(spec.ProjectID()))
			require.NoError(t, err)
			require.JSONEq(t, `{"resource_kind":"inference","resource_id":"*","project_id":"`+spec.ProjectID()+`"}`, string(selectors))
		})
	}
}
