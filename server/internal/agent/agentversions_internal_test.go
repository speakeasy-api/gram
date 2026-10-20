package agent

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deviceagentversions"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAgentMachine(t *testing.T) {
	t.Parallel()

	serial := "c02abc"
	withSerial := &gen.GetPluginsPayload{SerialNumber: &serial}
	require.Equal(t, "serial:c02abc", agentMachine(withSerial, "dev@example.com", deviceidentity.EnvironmentEndpoint))
	// A serial from a non-endpoint box is ignored, matching the sync tables.
	require.Equal(t, "server:dev@example.com", agentMachine(withSerial, "dev@example.com", "server"))
	require.Equal(t, "endpoint:dev@example.com", agentMachine(&gen.GetPluginsPayload{}, "dev@example.com", deviceidentity.EnvironmentEndpoint))
}

func TestRecordAgentVersion_RecordsSpeakeasydUserAgentOnly(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	store := deviceagentversions.NewStore(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	svc := new(Service)
	svc.logger = testenv.NewLogger(t)
	svc.SetAgentVersions(store)

	agentCtx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{UserAgent: "speakeasyd/0.3.31"})
	svc.recordAgentVersion(agentCtx, "org-a", "m-1")
	otherCtx := contextvalues.SetRequestContext(t.Context(), &contextvalues.RequestContext{UserAgent: "curl/8.4.0"})
	svc.recordAgentVersion(otherCtx, "org-a", "m-2")
	svc.recordAgentVersion(t.Context(), "org-a", "m-3")

	// Two days, so the read still covers the write if the test crosses UTC
	// midnight; the assertion ignores which day it landed on.
	rows, err := store.Read(t.Context(), 2, time.Now())
	require.NoError(t, err)
	versions := map[string]int64{}
	for _, row := range rows {
		versions[row.Version] += row.Machines
	}
	require.Equal(t, map[string]int64{"": 1, "0.3.31": 1}, versions)
}

func TestAgentPrincipalMachine(t *testing.T) {
	t.Parallel()

	serial := "c02abc"
	require.Equal(t, "serial:c02abc", agentPrincipalMachine(&gen.GetPluginsPayload{SerialNumber: &serial}, "agent:123"))
	require.Equal(t, "agent:123", agentPrincipalMachine(&gen.GetPluginsPayload{}, "agent:123"))
}
