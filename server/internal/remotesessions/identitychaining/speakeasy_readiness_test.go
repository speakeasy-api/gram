package identitychaining

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type readinessGate struct {
	status internalsessionissuers.Status
	err    error
}

func (g readinessGate) Status(context.Context, string) (internalsessionissuers.Status, error) {
	return g.status, g.err
}

type readinessSigner struct {
	err error
}

func (s readinessSigner) CheckAuthority(context.Context, string, internalsessionissuers.Status) error {
	return s.err
}

func (readinessSigner) Sign(context.Context, string, internalsessionissuers.Claims) (string, error) {
	panic("readiness must never sign an assertion")
}

func TestSpeakeasyConfigurationReadiness(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	for _, state := range []internalsessionissuers.State{
		internalsessionissuers.StateEnabled,
		internalsessionissuers.StateUnavailable,
		internalsessionissuers.StateEntitlementRequired,
		internalsessionissuers.StateSetupRequired,
		internalsessionissuers.StateDisabled,
		internalsessionissuers.StateSigningPaused,
		internalsessionissuers.StateKeyless,
	} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{
				g: new(Governor), issuerID: issuerID, signer: readinessSigner{},
				gate: readinessGate{status: internalsessionissuers.Status{
					State: state, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey),
				}},
			}
			_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
			require.Equal(t, state == internalsessionissuers.StateEnabled, outcome.Succeeded())
		})
	}
	for _, tc := range []struct {
		name      string
		status    internalsessionissuers.Status
		gateErr   error
		signerErr error
	}{
		{name: "missing issuer", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, ActiveKey: new(jwksrepo.JsonWebKey)}},
		{name: "missing key", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}}},
		{name: "replaced issuer", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: uuid.New()}, ActiveKey: new(jwksrepo.JsonWebKey)}},
		{name: "status failure", gateErr: errors.New("lookup failed")},
		{name: "foreign deployment", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey)}, signerErr: internalsessionissuers.ErrMisconfigured},
		{name: "authority failure", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey)}, signerErr: errors.New("authority lookup failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{g: new(Governor), issuerID: issuerID, signer: readinessSigner{err: tc.signerErr}, gate: readinessGate{status: tc.status, err: tc.gateErr}}
			_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
			require.False(t, outcome.Succeeded())
		})
	}
}

func TestSpeakeasyNilSignerDisablesProvider(t *testing.T) {
	t.Parallel()
	g := &Governor{sources: make(map[remotesessions.EMAProviderKind]sourceFactory)}
	g.SetSpeakeasyIssuer(new(internalsessionissuers.Gate), nil)
	require.Empty(t, g.sources)
}
