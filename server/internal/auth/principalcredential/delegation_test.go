package principalcredential_test

import (
	"testing"

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
	issuer := principalcredential.New(newSigner(t), nil)
	workload := urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime")

	_, _, err := issuer.MintDelegation(principalcredential.Credential{
		OrganizationID: "org-test", ProjectID: uuid.New(), AuthorizerUserID: "",
		Principal: urn.NewWorkloadPrincipal(uuid.New(), "trigger"),
	}, workload)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)

	_, _, err = issuer.MintDelegation(agentCredential(), urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}

func TestDelegationFromAnotherSignerIsRejected(t *testing.T) {
	t.Parallel()
	raw, _, err := principalcredential.New(newSigner(t), nil).MintDelegation(agentCredential(), urn.NewWorkloadPrincipal(uuid.New(), "kit-runtime"))
	require.NoError(t, err)
	_, _, err = principalcredential.New(newSigner(t), nil).ValidateDelegation(raw)
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
}
