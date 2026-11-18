package remotesessions

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestTypedAuthorizationStateRoundTrip(t *testing.T) {
	t.Parallel()
	subject := urn.NewUserSubject("synthetic-user")
	authorization := &TypedAuthorization{OrganizationID: "synthetic-org", ProjectID: uuid.New(), ServerID: uuid.New(), AuthClientID: uuid.New(), RemoteSessionClientID: uuid.New(), Subject: subject}
	state := RemoteLoginState{TypedAuthorization: authorization}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	var restored RemoteLoginState
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Equal(t, authorization, restored.TypedAuthorization)
	require.Equal(t, authorization, restored.parent().TypedAuthorization, "resource retry must preserve the exact authorization")
}

func TestTypedAuthorizationValidatorFailsClosed(t *testing.T) {
	t.Parallel()
	manager := &ChallengeManager{}
	require.NoError(t, manager.validateTypedAuthorization(t.Context(), RemoteLoginState{}), "legacy state does not opt into typed authorization")
	require.Error(t, manager.validateTypedAuthorization(t.Context(), RemoteLoginState{TypedAuthorization: &TypedAuthorization{}}))
}
