package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNetworkIngressWorkerTemporalTLS(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, environment, cert, key string
		wantErr                      string
	}{
		{name: "local plaintext", environment: "local"},
		{name: "dev mtls", environment: "dev", cert: "cert", key: "key"},
		{name: "prod missing", environment: "prod", wantErr: "mTLS is required"},
		{name: "cert only", environment: "local", cert: "cert", wantErr: "configured together"},
		{name: "key only", environment: "local", key: "key", wantErr: "configured together"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateNetworkIngressWorkerTemporalTLS(test.environment, test.cert, test.key)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestNetworkIngressWorkerExposesGatewayFlags(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"network-ingress-gateway-tag", "network-ingress-gateway-slots", "network-ingress-tailscale-api-url",
	} {
		requireFlag(t, newNetworkIngressWorkerCommand().Flags, name)
	}
}

func TestNetworkIngressWorkerDropsOperatorFlags(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, flag := range newNetworkIngressWorkerCommand().Flags {
		names[flag.Names()[0]] = true
	}
	for _, name := range []string{
		"network-ingress-operator-provider-enabled", "network-ingress-operator-namespace", "network-ingress-gateway-enabled",
		"network-ingress-attestor-image", "network-ingress-backend-pod-labels", "network-ingress-cluster-cidrs",
	} {
		require.False(t, names[name], name)
	}
}
