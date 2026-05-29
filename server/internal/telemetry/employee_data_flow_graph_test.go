package telemetry

import (
	"testing"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

func TestBuildEmployeeDataFlowGraph_DoesNotCollideEdgesWhenLabelsContainDelimiter(t *testing.T) {
	t.Parallel()

	rows := []repo.EmployeeDataFlowRow{
		{
			Origin:       "a",
			Client:       "b->server:c",
			CallCount:    2,
			SuccessCount: 2,
		},
		{
			Origin:       "a->client:b",
			Server:       "c",
			CallCount:    3,
			FailureCount: 3,
		},
	}

	_, edges := buildEmployeeDataFlowGraph(rows)

	sourceOne := employeeDataFlowNodeID(employeeGraphTupleNode{tier: "origin", label: "a"})
	targetOne := employeeDataFlowNodeID(employeeGraphTupleNode{tier: "client", label: "b->server:c"})
	sourceTwo := employeeDataFlowNodeID(employeeGraphTupleNode{tier: "origin", label: "a->client:b"})
	targetTwo := employeeDataFlowNodeID(employeeGraphTupleNode{tier: "server", label: "c"})

	edgeOne := findEmployeeDataFlowEdge(edges, sourceOne, targetOne)
	require.NotNil(t, edgeOne)
	require.Equal(t, int64(2), edgeOne.CallCount)
	require.Equal(t, int64(2), edgeOne.SuccessCount)

	edgeTwo := findEmployeeDataFlowEdge(edges, sourceTwo, targetTwo)
	require.NotNil(t, edgeTwo)
	require.Equal(t, int64(3), edgeTwo.CallCount)
	require.Equal(t, int64(3), edgeTwo.FailureCount)
	require.NotEqual(t, edgeOne.ID, edgeTwo.ID)
}

func findEmployeeDataFlowEdge(edges []*telem_gen.EmployeeDataFlowEdge, source, target string) *telem_gen.EmployeeDataFlowEdge {
	for _, edge := range edges {
		if edge.Source == source && edge.Target == target {
			return edge
		}
	}

	return nil
}
