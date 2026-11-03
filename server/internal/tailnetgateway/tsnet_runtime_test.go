package tailnetgateway_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestTsnetRuntimeStartNamesEveryMissingSpecField(t *testing.T) {
	t.Parallel()
	runtime := tailnetgateway.NewTsnetRuntime(tailnetgateway.WithLogger(testenv.NewLogger(t)))

	_, err := runtime.Start(t.Context(), tailnetgateway.NodeSpec{})
	require.Error(t, err)
	for _, field := range []string{"Store", "Hostname", "ServiceName", "Tag", "StateDir", "StartTimeout"} {
		require.ErrorContains(t, err, field)
	}
}

func TestNewTsnetRuntimeAcceptsOptions(t *testing.T) {
	t.Parallel()
	require.NotNil(t, tailnetgateway.NewTsnetRuntime())
	require.NotNil(t, tailnetgateway.NewTsnetRuntime(
		tailnetgateway.WithLogger(nil),
		tailnetgateway.WithShutdownTimeout(5*time.Second),
		tailnetgateway.WithRequestReadTimeout(0),
	))
}
