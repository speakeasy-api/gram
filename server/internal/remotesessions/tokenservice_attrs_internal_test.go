package remotesessions

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A refresh-failure log line names the session row itself, so one failing credential is findable.
func TestRefreshFailureAttrs_CarriesRemoteSessionID(t *testing.T) {
	t.Parallel()

	sess := remotesessions_repo.RemoteSession{
		ID:                    uuid.New(),
		SubjectUrn:            urn.NewUserSubject("user-1"),
		UserSessionIssuerID:   uuid.New(),
		RemoteSessionClientID: uuid.New(),
	}

	attrs := refreshFailureAttrs(sess, errors.New("upstream said no"))

	keys := map[string]string{}
	for _, a := range attrs {
		if sa, ok := a.(slog.Attr); ok {
			keys[sa.Key] = sa.Value.String()
		}
	}
	require.Equal(t, sess.ID.String(), keys[string(attr.RemoteSessionIDKey)])
	require.Equal(t, sess.RemoteSessionClientID.String(), keys[string(attr.RemoteSessionClientIDKey)])
	require.Equal(t, "gram.remote_session.id", string(attr.RemoteSessionIDKey))
}
