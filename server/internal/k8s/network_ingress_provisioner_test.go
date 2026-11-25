package k8s

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type stubNetworkIngressProvisioner struct{}

func (stubNetworkIngressProvisioner) Apply(context.Context, NetworkIngressDesired) (NetworkIngressObservation, error) {
	return NetworkIngressObservation{}, nil
}

func (stubNetworkIngressProvisioner) Observe(context.Context, NetworkIngressResourceNames) (NetworkIngressObservation, error) {
	return NetworkIngressObservation{}, nil
}

func (stubNetworkIngressProvisioner) Delete(context.Context, NetworkIngressResourceNames) error {
	return nil
}

func TestNetworkIngressResourceNamesStableAndRoundTrip(t *testing.T) {
	t.Parallel()

	ingressID := uuid.MustParse("0199aabb-ccdd-7000-8000-001122334455")
	first, err := NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	second, err := NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, "gram-netingress-86c7c63bb062541a6ba1", first.Namespace)
	require.NotContains(t, first.Namespace, ".")

	encoded, err := first.Marshal()
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "organization")
	parsed, err := ParseNetworkIngressResourceNames(encoded)
	require.NoError(t, err)
	require.Equal(t, first, parsed)

	drifted := first
	drifted.Ingress = "valid-but-wrong"
	require.ErrorIs(t, drifted.Validate(), ErrNetworkIngressInvalidDesiredState)
}

func TestNetworkIngressResourceNamesRejectIncompleteState(t *testing.T) {
	t.Parallel()

	_, err := NewNetworkIngressResourceNames(uuid.Nil)
	require.ErrorIs(t, err, ErrNetworkIngressInvalidDesiredState)
	_, err = ParseNetworkIngressResourceNames([]byte(`{"namespace":"only-one-field"}`))
	require.ErrorIs(t, err, ErrNetworkIngressInvalidDesiredState)
	_, err = ParseNetworkIngressResourceNames([]byte(`not-json`))
	require.Error(t, err)
}

func TestParseTailscaleCredentials(t *testing.T) {
	t.Parallel()

	parsed, err := ParseTailscaleCredentials([]byte(`{"client_id":"cid","client_secret":"csecret"}`))
	require.NoError(t, err)
	require.Equal(t, TailscaleCredentials{ClientID: "cid", ClientSecret: "csecret"}, parsed)

	for _, tc := range []struct{ name, encoded string }{
		{"missing secret", `{"client_id":"cid"}`},
		{"unknown field", `{"client_id":"cid","client_secret":"csecret","extra":true}`},
		{"trailing object", `{"client_id":"cid","client_secret":"csecret"}{}`},
		{"trailing null", `{"client_id":"cid","client_secret":"csecret"} null`},
		{"not JSON", `not-json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseTailscaleCredentials([]byte(tc.encoded))
			require.ErrorIs(t, err, ErrNetworkIngressInvalidDesiredState)
		})
	}
}

func TestNetworkIngressProvisionerRegistry(t *testing.T) {
	t.Parallel()

	provisioner := stubNetworkIngressProvisioner{}
	registry, err := NewNetworkIngressProvisionerRegistry(map[string]NetworkIngressProvisioner{
		NetworkIngressProviderTailscale: provisioner,
	}, nil, nil)
	require.NoError(t, err)

	got, err := registry.Provisioner(NetworkIngressProviderTailscale)
	require.NoError(t, err)
	require.IsType(t, &observedNetworkIngressProvisioner{}, got)
	_, err = registry.Provisioner("unknown")
	require.ErrorIs(t, err, ErrNetworkIngressUnsupportedProvider)

	_, err = NewNetworkIngressProvisionerRegistry(map[string]NetworkIngressProvisioner{"": provisioner}, nil, nil)
	require.ErrorIs(t, err, ErrNetworkIngressInvalidDesiredState)
	_, err = NewNetworkIngressProvisionerRegistry(map[string]NetworkIngressProvisioner{"nil": nil}, nil, nil)
	require.ErrorIs(t, err, ErrNetworkIngressInvalidDesiredState)

}
