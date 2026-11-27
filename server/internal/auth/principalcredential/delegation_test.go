package principalcredential_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDelegationRoundTripsAndNamesItsActor(t *testing.T) {
	t.Parallel()
	issuer := principalcredential.New(newSigner(t), nil)
	c := agentCredential()
	c.SelectionProfileVersionID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	actor := urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime")

	raw, id, err := issuer.MintDelegation(c, actor)
	require.NoError(t, err)
	got, gotActor, err := issuer.ValidateDelegation(raw)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, c, got.Credential)
	require.Equal(t, actor, gotActor)
}

func TestDelegationNeverAuthenticatesAsACredential(t *testing.T) {
	t.Parallel()
	issuer := principalcredential.New(newSigner(t), nil)
	raw, _, err := issuer.MintDelegation(agentCredential(), urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime"))
	require.NoError(t, err)
	require.False(t, principalcredential.IsToken(raw))
	_, err = issuer.Validate(raw)
	require.ErrorIs(t, err, principalcredential.ErrNotCredential)

	// Nor does a credential validate as a delegation.
	credential, _, err := issuer.Mint(agentCredential())
	require.NoError(t, err)
	_, _, err = issuer.ValidateDelegation(credential)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}

func TestDelegationRequiresAnAgentForAUserAndAWorkloadActor(t *testing.T) {
	t.Parallel()
	workload := urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime")
	workloadCredential := agentCredential()
	workloadCredential.Principal = urn.NewWorkloadPrincipal(uuid.New(), "trigger")
	// Well-shaped on its own: a workload acts for no user. Only the agent
	// guard refuses it.
	workloadCredential.AuthorizerUserID = ""

	for name, tc := range map[string]struct {
		credential principalcredential.Credential
		actor      urn.Principal
	}{
		"principal is not an agent": {credential: workloadCredential, actor: workload},
		"actor is not a workload":   {credential: agentCredential(), actor: urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := principalcredential.New(newSigner(t), nil).MintDelegation(tc.credential, tc.actor)
			require.ErrorIs(t, err, principalcredential.ErrInvalid)
		})
	}
}

func TestExpiredDelegationIsRejected(t *testing.T) {
	t.Parallel()
	signer := newSigner(t)
	c := agentCredential()
	past := time.Now().Add(-time.Hour)
	raw, err := signer.SignRS256(jwt.MapClaims{
		"org_id": c.OrganizationID, "project_id": c.ProjectID.String(), "authorizer_user_id": c.AuthorizerUserID,
		"iss": signer.URL(), "sub": c.Principal.String(), "aud": []string{principalcredential.DelegationAudience},
		"iat": past.Add(-principalcredential.Lifetime).Unix(), "exp": past.Unix(), "jti": uuid.NewString(),
		"may_act": map[string]string{"sub": urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime").String()},
	}, principalcredential.DelegationTokenType)
	require.NoError(t, err)
	_, _, err = principalcredential.New(signer, nil).ValidateDelegation(raw)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}

func TestDelegationFromAnotherSignerIsRejected(t *testing.T) {
	t.Parallel()
	raw, _, err := principalcredential.New(newSigner(t), nil).MintDelegation(agentCredential(), urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime"))
	require.NoError(t, err)
	_, _, err = principalcredential.New(newSigner(t), nil).ValidateDelegation(raw)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}
