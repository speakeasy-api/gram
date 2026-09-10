package background

import (
	"reflect"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
)

type recordingNetworkIngressRegistry struct {
	activities []string
	workflows  []string
}

func (r *recordingNetworkIngressRegistry) RegisterActivityWithOptions(_ any, options activity.RegisterOptions) {
	r.activities = append(r.activities, options.Name)
}

func (r *recordingNetworkIngressRegistry) RegisterWorkflow(workflow any) {
	name := runtime.FuncForPC(reflect.ValueOf(workflow).Pointer()).Name()
	r.workflows = append(r.workflows, name)
}

func TestRegisterNetworkIngressOwnsOnlyIngressWork(t *testing.T) {
	t.Parallel()

	registry := &recordingNetworkIngressRegistry{}
	registerNetworkIngress(registry, nil, nil, "private-ingress")

	require.ElementsMatch(t, []string{
		NetworkIngressReconcileActivityName,
		"SweepNetworkIngresses",
		"FindNetworkIngressOrphans",
	}, registry.activities)
	require.Len(t, registry.workflows, 2)
	require.Contains(t, registry.workflows[0]+registry.workflows[1], "NetworkIngressReconcileWorkflow")
	require.Contains(t, registry.workflows[0]+registry.workflows[1], "NetworkIngressSweepWorkflow")
}
