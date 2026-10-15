package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func turnOffSignInRegistrar(t *testing.T, attachment CatalogIdentityProviderAttachment) *Registrar {
	t.Helper()
	store := &recordingRegistrationStore{project: ResolvedProject{ID: uuid.New(), Slug: "project"}}
	service := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, store).
		WithIdentityProviderAttachment(attachment)
	registrar := newRegistrar(newTestMCPServer())
	registerTurnOffMCPSignInTool(registrar, service)
	return registrar
}

func turnOffSignIn(t *testing.T, registrar *Registrar, arguments string) (TurnOffMCPSignInToolOutput, error) {
	t.Helper()
	raw, err := descriptorByName(t, registrar, "turn_off_mcp_sign_in").Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(arguments),
	)
	if err != nil {
		return TurnOffMCPSignInToolOutput{}, err
	}
	output, ok := raw.(TurnOffMCPSignInToolOutput)
	require.True(t, ok, "the tool must answer with its own output type")
	return output, nil
}

// An unconfirmed call describes the change and makes none, and hands back the
// version a confirmation has to name.
func TestTurnOffMCPSignInToolPreviewsWithoutChangingAnything(t *testing.T) {
	t.Parallel()

	attachment := &recordingIdentityProviderAttachment{preview: SignInRemovalPreview{
		Configured: true, ProviderURL: "https://auth.example.test", ClientStaysBound: true, Version: "fingerprint-1",
	}}
	registrar := turnOffSignInRegistrar(t, attachment)

	output, err := turnOffSignIn(t, registrar, `{"project_slug":"project","registration_id":"`+uuid.NewString()+`","confirmed":false}`)
	require.NoError(t, err)
	require.False(t, output.Applied)
	require.True(t, output.SignInConfigured)
	require.True(t, output.OtherServersKeepTheClient)
	require.False(t, output.PeopleMustSignInAgain)
	require.Equal(t, "confirm", output.NextAction)
	require.Equal(t, "fingerprint-1", output.Version, "the preview must name the state a confirmation has to carry back")
	require.Zero(t, attachment.removals, "a preview must not perform the removal")
}

// A confirmation carries the preview's version through to the removal.
func TestTurnOffMCPSignInToolConfirmsWithThePreviewedVersion(t *testing.T) {
	t.Parallel()

	attachment := &recordingIdentityProviderAttachment{
		preview: SignInRemovalPreview{Configured: true, ProviderURL: "https://auth.example.test", Version: "fingerprint-2"},
		removal: SignInRemovalResult{Removed: true, ProviderURL: "https://auth.example.test", ClientUnbound: true},
	}
	registrar := turnOffSignInRegistrar(t, attachment)

	output, err := turnOffSignIn(t, registrar, `{"project_slug":"project","registration_id":"`+uuid.NewString()+`","confirmed":true,"expected_version":"fingerprint-2"}`)
	require.NoError(t, err)
	require.True(t, output.Applied)
	require.True(t, output.PeopleMustSignInAgain)
	require.Equal(t, []string{"fingerprint-2"}, attachment.removeVersions)
}

// A stale confirmation is answered with guidance to preview again, not with a
// retry of the same request.
func TestTurnOffMCPSignInToolReportsAStaleConfirmation(t *testing.T) {
	t.Parallel()

	attachment := &recordingIdentityProviderAttachment{
		preview:   SignInRemovalPreview{Configured: true, ProviderURL: "https://auth.example.test", Version: "fingerprint-3"},
		removeErr: ErrSignInRemovalStale,
	}
	registrar := turnOffSignInRegistrar(t, attachment)

	_, err := turnOffSignIn(t, registrar, `{"project_slug":"project","registration_id":"`+uuid.NewString()+`","confirmed":true,"expected_version":"stale"}`)
	require.Error(t, err)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, "sign_in_removal_stale")
	require.Contains(t, refusal.Payload, "confirmed: false")
}

// The removal commits and the read-back afterwards fails. The caller must be
// told the change is done, not invited to retry a request that already signed
// people out — and must not be told other servers keep a client, which this
// path cannot know.
func TestTurnOffMCPSignInToolReportsACommittedRemovalItCannotVerify(t *testing.T) {
	t.Parallel()

	attachment := &recordingIdentityProviderAttachment{
		preview:    SignInRemovalPreview{Configured: true, ProviderURL: "https://auth.example.test", Version: "fingerprint-4"},
		removal:    SignInRemovalResult{Removed: true, ProviderURL: "https://auth.example.test", ClientUnbound: false},
		previewErr: nil,
	}
	registrar := turnOffSignInRegistrar(t, attachment)
	attachment.previewAfterRemoval = ErrRegistrationUnavailable

	output, err := turnOffSignIn(t, registrar, `{"project_slug":"project","registration_id":"`+uuid.NewString()+`","confirmed":true,"expected_version":"fingerprint-4"}`)
	require.NoError(t, err)
	require.True(t, output.Applied, "the removal committed")
	require.Equal(t, "none", output.NextAction)
	require.Contains(t, output.Message, "could not be read back")
	require.False(t, output.OtherServersKeepTheClient, "nothing read back can say another server keeps the client")
}
