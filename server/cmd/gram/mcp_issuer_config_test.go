package gram

import (
	"flag"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestInternalIssuerSignerConfigTrimsKeyRing(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("issuer-config", flag.ContinueOnError)
	flags.String(identityProviderKMSKeyRingFlag, "  projects/test-project/locations/global/keyRings/test-ring\n", "")
	flags.String("environment", "production", "")
	flags.String(identityProviderSigningServiceAccount, "signer@example.test", "")
	origin, err := url.Parse("https://issuer.example.test")
	require.NoError(t, err)
	cfg := internalIssuerSignerConfig(cli.NewContext(nil, flags, nil), origin)
	require.Equal(t, "projects/test-project/locations/global/keyRings/test-ring", cfg.Deployment.KeyRing)
}
