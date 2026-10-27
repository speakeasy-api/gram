package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/signins"
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

// enforcedFlags turns sign-in enforcement on for org.
func enforcedFlags(org string) *feature.InMemory {
	flags := &feature.InMemory{}
	signins.EnableForTests(flags, org)
	return flags
}

func TestSpeakeasyConfigurationReadiness(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	for _, tc := range []struct {
		state     internalsessionissuers.State
		rolledOut bool
		want      remotesessions.EMAProviderUnavailableReason
	}{
		{state: internalsessionissuers.StateEnabled, rolledOut: true, want: ""},
		{state: internalsessionissuers.StateUnavailable, rolledOut: false, want: remotesessions.EMAProviderNotRolledOut},
		{state: internalsessionissuers.StateUnavailable, rolledOut: true, want: remotesessions.EMAProviderSigningUnavailable},
		{state: internalsessionissuers.StateVerifiedDomainRequired, rolledOut: true, want: remotesessions.EMAProviderVerifiedDomain},
		{state: internalsessionissuers.StateSetupRequired, rolledOut: true, want: remotesessions.EMAProviderSetupRequired},
		{state: internalsessionissuers.StateDisabled, rolledOut: true, want: remotesessions.EMAProviderDisabled},
		{state: internalsessionissuers.StateSigningPaused, rolledOut: true, want: remotesessions.EMAProviderSigningPaused},
		{state: internalsessionissuers.StateKeyless, rolledOut: true, want: remotesessions.EMAProviderKeyless},
	} {
		t.Run(fmt.Sprintf("%s rolled out %t", tc.state, tc.rolledOut), func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{
				g: new(Governor), issuerID: issuerID, signer: readinessSigner{}, flags: enforcedFlags("org-test"),
				gate: readinessGate{status: internalsessionissuers.Status{
					State: tc.state, RolledOut: tc.rolledOut, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey),
				}},
			}
			_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
			require.Equal(t, tc.want == "", outcome.Succeeded())
			require.Equal(t, tc.want, outcome.Unavailable, "the specific reason is kept")
			if tc.want != "" {
				require.Equal(t, ReasonProviderUnavailable, outcome.Reason)
				require.False(t, outcome.Retryable)
			}
		})
	}
	for _, tc := range []struct {
		name        string
		status      internalsessionissuers.Status
		gateErr     error
		signerErr   error
		reason      Reason
		unavailable remotesessions.EMAProviderUnavailableReason
		retryable   bool
	}{
		{name: "missing issuer", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, ActiveKey: new(jwksrepo.JsonWebKey)}, reason: ReasonProviderUnavailable, unavailable: remotesessions.EMAProviderSigningUnavailable},
		{name: "missing key", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}}, reason: ReasonProviderUnavailable, unavailable: remotesessions.EMAProviderSigningUnavailable},
		{name: "replaced issuer", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: uuid.New()}, ActiveKey: new(jwksrepo.JsonWebKey)}, reason: ReasonProviderUnavailable, unavailable: remotesessions.EMAProviderDeleted},
		{name: "status failure", gateErr: errors.New("lookup failed"), reason: ReasonTransientFailure, retryable: true},
		{name: "foreign deployment", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey)}, signerErr: internalsessionissuers.ErrMisconfigured, reason: ReasonConfigurationRequired},
		{name: "authority failure", status: internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey)}, signerErr: errors.New("authority lookup failed"), reason: ReasonTransientFailure, retryable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{g: new(Governor), issuerID: issuerID, signer: readinessSigner{err: tc.signerErr}, flags: enforcedFlags("org-test"), gate: readinessGate{status: tc.status, err: tc.gateErr}}
			_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
			require.False(t, outcome.Succeeded())
			require.Equal(t, tc.reason, outcome.Reason)
			require.Equal(t, tc.unavailable, outcome.Unavailable)
			require.Equal(t, tc.retryable, outcome.Retryable)
		})
	}
}

func TestSpeakeasyRequiresSignInEnforcement(t *testing.T) {
	t.Parallel()
	issuerID := uuid.New()
	enabled := internalsessionissuers.Status{State: internalsessionissuers.StateEnabled, RolledOut: true, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey)}
	off := &feature.InMemory{}
	off.SetFlag(feature.FlagSignInEnforcement, "org-test", false)
	for name, flags := range map[string]feature.Provider{
		"no flag provider":              nil,
		"flag unset":                    &feature.InMemory{},
		"flag off":                      off,
		"flag on for another org":       enforcedFlags("org-other"),
		"provider that errors on reads": &erroringFlags{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{g: new(Governor), issuerID: issuerID, signer: readinessSigner{}, flags: flags, gate: readinessGate{status: enabled}}
			_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
			require.Equal(t, ReasonProviderUnavailable, outcome.Reason, "Speakeasy fails closed without sign-in enforcement")
			require.Equal(t, remotesessions.EMAProviderSignInEnforcement, outcome.Unavailable)
		})
	}
	source := speakeasySource{g: new(Governor), issuerID: issuerID, signer: readinessSigner{}, flags: enforcedFlags("org-test"), gate: readinessGate{status: enabled}}
	_, outcome := source.configuration(t.Context(), testenv.NewLogger(t), "org-test")
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
}

// erroringFlags fails every evaluation.
type erroringFlags struct{ feature.InMemory }

func (*erroringFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, errors.New("flag provider unavailable")
}

func TestSpeakeasyReadinessView(t *testing.T) {
	t.Parallel()
	g := &Governor{logger: testenv.NewLogger(t), sources: make(map[remotesessions.EMAProviderKind]sourceFactory)}
	issuerID := uuid.New()
	status := internalsessionissuers.Status{State: internalsessionissuers.StateSigningPaused, RolledOut: true, Issuer: &jwksrepo.InternalSessionIssuer{ID: issuerID}, ActiveKey: new(jwksrepo.JsonWebKey), PauseReason: internalsessionissuers.PauseReasonFlagOn}
	gateErr := error(nil)
	g.sources[remotesessions.EMAProviderSpeakeasy] = func(p remotesessions.EMAProvider) tokenSource {
		return speakeasySource{g: g, issuerID: p.InternalSessionIssuerID, signer: readinessSigner{}, flags: enforcedFlags("org-test"), gate: readinessGate{status: status, err: gateErr}}
	}
	reason, err := g.speakeasyReadiness(t.Context(), "org-test", issuerID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.EMAProviderSigningPaused, reason)

	gateErr = errors.New("flag provider timed out")
	reason, err = g.speakeasyReadiness(t.Context(), "org-test", issuerID)
	require.Error(t, err, "an unknown state is never a definitive refusal")
	require.Equal(t, remotesessions.EMAProviderReadinessUnknown, reason)
}

// signingSigner is an available signer whose Sign fails with err.
type signingSigner struct{ err error }

func (signingSigner) CheckAuthority(context.Context, string, internalsessionissuers.Status) error {
	return nil
}

func (s signingSigner) Sign(context.Context, string, internalsessionissuers.Claims) (string, error) {
	return "", s.err
}

func TestSpeakeasySignRefusalsAreNotTransient(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		err       error
		reason    Reason
		retryable bool
	}{
		{name: "email off the verified domains", err: fmt.Errorf("%w: email is not on a verified domain of the organization", internalsessionissuers.ErrInvalidClaims), reason: ReasonIneligibleIdentity},
		{name: "verified domain removed", err: &internalsessionissuers.SigningRefusedError{State: internalsessionissuers.StateVerifiedDomainRequired}, reason: ReasonProviderUnavailable},
		{name: "paused", err: &internalsessionissuers.SigningRefusedError{State: internalsessionissuers.StateSigningPaused, Reason: internalsessionissuers.PauseReasonFlagOn}, reason: ReasonProviderUnavailable},
		{name: "misconfigured", err: internalsessionissuers.ErrMisconfigured, reason: ReasonConfigurationRequired},
		{name: "kms outage", err: errors.New("kms unavailable"), reason: ReasonTransientFailure, retryable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := speakeasySource{signer: signingSigner{err: tc.err}}
			_, outcome := source.sign(t.Context(), testenv.NewLogger(t), "org-test", internalsessionissuers.Claims{})
			require.Equal(t, tc.reason, outcome.Reason)
			require.Equal(t, tc.retryable, outcome.Retryable)
		})
	}
	refused := speakeasySource{signer: signingSigner{err: &internalsessionissuers.SigningRefusedError{State: internalsessionissuers.StateVerifiedDomainRequired}}}
	_, outcome := refused.sign(t.Context(), testenv.NewLogger(t), "org-test", internalsessionissuers.Claims{})
	require.Equal(t, remotesessions.EMAProviderVerifiedDomain, outcome.Unavailable)
}

func TestSpeakeasyNilSignerDisablesProvider(t *testing.T) {
	t.Parallel()
	g := &Governor{sources: make(map[remotesessions.EMAProviderKind]sourceFactory)}
	g.SetSpeakeasyIssuer(new(internalsessionissuers.Gate), nil, nil)
	require.Empty(t, g.sources)
}
