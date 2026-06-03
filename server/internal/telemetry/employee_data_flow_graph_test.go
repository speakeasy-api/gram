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

func TestBuildEmployeeDataFlowGraph_PrunesNodesNotReachableFromOrigin(t *testing.T) {
	t.Parallel()

	rows := []repo.EmployeeDataFlowRow{
		// Full path anchored at an origin: everything here is reachable.
		{
			Origin:       "host-a",
			Client:       "cursor",
			Server:       "github",
			ServerClass:  "external",
			Tool:         "list_issues",
			CallCount:    2,
			SuccessCount: 2,
		},
		// Dangling path with no origin tier: the client (and everything that
		// hangs off it) must be pruned.
		{
			Client:    "orphan-client",
			Server:    "orphan-server",
			Tool:      "orphan-tool",
			CallCount: 5,
		},
	}

	nodes, edges := buildEmployeeDataFlowGraph(rows)

	require.NotNil(t, findEmployeeDataFlowNode(nodes, "origin", "host-a"))
	require.NotNil(t, findEmployeeDataFlowNode(nodes, "client", "cursor"))
	require.NotNil(t, findEmployeeDataFlowNode(nodes, "server", "github"))
	require.NotNil(t, findEmployeeDataFlowNode(nodes, "tool", "list_issues"))

	require.Nil(t, findEmployeeDataFlowNode(nodes, "client", "orphan-client"))
	require.Nil(t, findEmployeeDataFlowNode(nodes, "server", "orphan-server"))
	require.Nil(t, findEmployeeDataFlowNode(nodes, "tool", "orphan-tool"))

	for _, edge := range edges {
		require.NotNil(t, findEmployeeDataFlowNodeByID(nodes, edge.Source))
		require.NotNil(t, findEmployeeDataFlowNodeByID(nodes, edge.Target))
	}
}

func findEmployeeDataFlowNode(nodes []*telem_gen.EmployeeDataFlowNode, tier, label string) *telem_gen.EmployeeDataFlowNode {
	for _, node := range nodes {
		if node.Tier == tier && node.Label == label {
			return node
		}
	}

	return nil
}

func findEmployeeDataFlowNodeByID(nodes []*telem_gen.EmployeeDataFlowNode, id string) *telem_gen.EmployeeDataFlowNode {
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}

	return nil
}

func findEmployeeDataFlowEdge(edges []*telem_gen.EmployeeDataFlowEdge, source, target string) *telem_gen.EmployeeDataFlowEdge {
	for _, edge := range edges {
		if edge.Source == source && edge.Target == target {
			return edge
		}
	}

	return nil
}
