package background

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDrainWorkflowID(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	wfID := drainWorkflowID(id)
	assert.Equal(t, "v1:drain-risk-analysis:550e8400-e29b-41d4-a716-446655440000", wfID)
}
