package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

func TestInMemoryStore_ConsumesGrantOnce(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	consumed, err := store.ConsumeGrant(t.Context(), consumeInput(grant))
	require.NoError(t, err)
	require.Equal(t, grant.Connection, consumed.Connection)

	_, err = store.ConsumeGrant(t.Context(), consumeInput(grant))
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_RejectsDuplicateGrantCode(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	require.ErrorIs(t, store.IssueGrant(t.Context(), grant), platformoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_RejectsGrantForDifferentClientWithoutConsumingIt(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	input := consumeInput(grant)
	input.ClientID = "other-client"
	_, err := store.ConsumeGrant(t.Context(), input)
	require.ErrorIs(t, err, platformoauth.ErrClientMismatch)

	_, err = store.ConsumeGrant(t.Context(), consumeInput(grant))
	require.NoError(t, err)
}

func TestInMemoryStore_RejectsMalformedPKCE(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	malformedChallenge := grant
	malformedChallenge.Code = "authorization-code-malformed-challenge"
	malformedChallenge.CodeChallenge = "malformed"
	require.ErrorIs(t, store.IssueGrant(t.Context(), malformedChallenge), platformoauth.ErrPKCE)

	input := consumeInput(grant)
	input.CodeVerifier = "short"
	_, err := store.ConsumeGrant(t.Context(), input)
	require.ErrorIs(t, err, platformoauth.ErrPKCE)
}

func TestInMemoryStore_RejectsGrantWithMismatchedRedirectOrPKCE(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	input := consumeInput(grant)
	input.RedirectURI = "https://client.example/other"
	_, err := store.ConsumeGrant(t.Context(), input)
	require.ErrorIs(t, err, platformoauth.ErrRedirectURI)

	input = consumeInput(grant)
	input.CodeVerifier = "wrong-verifier"
	_, err = store.ConsumeGrant(t.Context(), input)
	require.ErrorIs(t, err, platformoauth.ErrPKCE)

	_, err = store.ConsumeGrant(t.Context(), consumeInput(grant))
	require.NoError(t, err)
}

func TestInMemoryStore_AuthorizeConnectionCreatesAndReauthorizesAtomically(t *testing.T) {
	t.Parallel()

	store := adminoauth.NewInMemoryStore()
	client := adminoauth.Client{ID: "client-1"}
	require.NoError(t, store.RegisterClient(t.Context(), client))

	first := adminoauth.AuthorizeConnectionInput{
		Connection: adminoauth.Connection{ID: "connection-1", ClientID: client.ID, Subject: "user:user-1", OrganizationID: "organization-1", Generation: "generation-1"},
		Grant:      adminoauth.Grant{Code: "code-1", ClientID: client.ID, RedirectURI: "https://client.example/callback", CodeChallenge: pkceChallenge("test-verifier"), ExpiresAt: time.Now().Add(time.Minute)},
		Now:        time.Now(),
	}
	connection, err := store.AuthorizeConnection(t.Context(), first)
	require.NoError(t, err)
	require.Equal(t, first.Connection, connection)

	session := sessionFor(connection, client.ID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))
	second := first
	second.Connection = adminoauth.Connection{ID: "connection-new", ClientID: client.ID, Subject: "user:user-1", OrganizationID: "organization-1", Generation: "generation-2"}
	second.Grant.Code = "code-2"
	second.Now = time.Now()

	connection, err = store.AuthorizeConnection(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, "connection-1", connection.ID)
	require.Equal(t, "generation-2", connection.Generation)

	_, err = store.RotateSession(t.Context(), rotateInput(session, sessionFor(session.Connection, client.ID, "refresh-new")))
	require.ErrorIs(t, err, adminoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_RotatesRefreshTokenOnce(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))

	old, err := store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-new")))
	require.NoError(t, err)
	require.Equal(t, session.ID, old.ID)

	_, err = store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-newer")))
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_RefreshReuseRevokesGeneration(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))

	attackerSession := sessionFor(grant.Connection, grant.ClientID, "refresh-attacker")
	_, err := store.RotateSession(t.Context(), rotateInput(session, attackerSession))
	require.NoError(t, err)

	_, err = store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-legitimate")))
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)

	_, err = store.RotateSession(t.Context(), rotateInput(attackerSession, sessionFor(grant.Connection, grant.ClientID, "refresh-after-reuse")))
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_RefreshRaceHasOneWinner(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, refreshHash := range []string{"refresh-one", "refresh-two"} {
		wg.Go(func() {
			_, err := store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, refreshHash)))
			results <- err
		})
	}
	wg.Wait()
	close(results)

	var successes int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
	}
	require.Equal(t, 1, successes)
}

func TestInMemoryStore_RejectsRefreshForDifferentConnection(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))

	replacement := sessionFor(grant.Connection, grant.ClientID, "refresh-new")
	replacement.Connection.ID = "connection-other"
	_, err := store.RotateSession(t.Context(), rotateInput(session, replacement))
	require.ErrorIs(t, err, platformoauth.ErrGeneration)
}

func TestInMemoryStore_RotationInvalidatesOldGeneration(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))

	connection, err := store.RotateConnectionGeneration(t.Context(), grant.Connection.ID, "generation-next", time.Now())
	require.NoError(t, err)
	require.Equal(t, "generation-next", connection.Generation)

	_, err = store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-new")))
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
}

func TestInMemoryStore_ClientRevocationRevokesSessionFamily(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	require.NoError(t, store.CreateSession(t.Context(), session))
	require.NoError(t, store.RevokeClient(t.Context(), grant.ClientID, time.Now()))

	_, err := store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-new")))
	require.ErrorIs(t, err, platformoauth.ErrRevoked)
}

func TestInMemoryStore_ClientRevocationCannotBeUndone(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	require.NoError(t, store.RevokeClient(t.Context(), grant.ClientID, time.Now()))
	require.ErrorIs(t, store.RegisterClient(t.Context(), platformoauth.Client{ID: grant.ClientID}), platformoauth.ErrAlreadyUsed)

	anotherGrant := grant
	anotherGrant.Code = "authorization-code-next"
	require.ErrorIs(t, store.IssueGrant(t.Context(), anotherGrant), platformoauth.ErrRevoked)
}

func TestInMemoryStore_ConnectionRevocationCannotBeRotated(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	require.NoError(t, store.RevokeConnection(t.Context(), grant.Connection.ID, time.Now()))

	_, err := store.RotateConnectionGeneration(t.Context(), grant.Connection.ID, "generation-next", time.Now())
	require.ErrorIs(t, err, platformoauth.ErrRevoked)
}

func TestInMemoryStore_RejectsCredentialsAtExpiry(t *testing.T) {
	t.Parallel()

	store, grant := seededGrant(t)
	input := consumeInput(grant)
	input.Now = grant.ExpiresAt
	_, err := store.ConsumeGrant(t.Context(), input)
	require.ErrorIs(t, err, platformoauth.ErrExpired)

	store, grant = consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-expiring")
	session.RefreshExpiresAt = time.Now()
	require.NoError(t, store.CreateSession(t.Context(), session))
	inputRotation := rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-new"))
	inputRotation.Now = session.RefreshExpiresAt
	_, err = store.RotateSession(t.Context(), inputRotation)
	require.ErrorIs(t, err, platformoauth.ErrExpired)
}

func TestInMemoryStore_RejectsExpiredRefreshToken(t *testing.T) {
	t.Parallel()

	store, grant := consumedGrant(t)
	session := sessionFor(grant.Connection, grant.ClientID, "refresh-old")
	session.RefreshExpiresAt = time.Now().Add(-time.Minute)
	require.NoError(t, store.CreateSession(t.Context(), session))

	_, err := store.RotateSession(t.Context(), rotateInput(session, sessionFor(grant.Connection, grant.ClientID, "refresh-new")))
	require.ErrorIs(t, err, platformoauth.ErrExpired)
}

func seededGrant(t *testing.T) (*platformoauth.InMemoryStore, platformoauth.Grant) {
	t.Helper()

	store := platformoauth.NewInMemoryStore()
	client := platformoauth.Client{ID: "client-1"}
	require.NoError(t, store.RegisterClient(t.Context(), client))

	connection := platformoauth.Connection{
		ID:             "connection-1",
		ClientID:       client.ID,
		Subject:        "user:user-1",
		OrganizationID: "organization-1",
		Generation:     "generation-1",
	}
	require.NoError(t, store.RegisterConnection(t.Context(), connection))

	grant := platformoauth.Grant{
		Code:          "authorization-code-1",
		ClientID:      client.ID,
		Connection:    connection,
		RedirectURI:   "https://client.example/callback",
		CodeChallenge: pkceChallenge(testVerifier),
		ExpiresAt:     time.Now().Add(time.Minute),
	}
	require.NoError(t, store.IssueGrant(t.Context(), grant))
	return store, grant
}

func consumedGrant(t *testing.T) (*platformoauth.InMemoryStore, platformoauth.Grant) {
	t.Helper()

	store, grant := seededGrant(t)
	_, err := store.ConsumeGrant(t.Context(), consumeInput(grant))
	require.NoError(t, err)
	return store, grant
}

func consumeInput(grant platformoauth.Grant) platformoauth.ConsumeGrantInput {
	return platformoauth.ConsumeGrantInput{
		Code:         grant.Code,
		ClientID:     grant.ClientID,
		RedirectURI:  grant.RedirectURI,
		CodeVerifier: testVerifier,
		Now:          time.Now(),
	}
}

func rotateInput(session, replacement platformoauth.Session) platformoauth.RotateSessionInput {
	return platformoauth.RotateSessionInput{
		RefreshHash: session.RefreshHash,
		ClientID:    session.ClientID,
		Generation:  session.Connection.Generation,
		Now:         time.Now(),
		Replacement: replacement,
	}
}

func sessionFor(connection platformoauth.Connection, clientID, refreshHash string) platformoauth.Session {
	return platformoauth.Session{
		ID:               "session-" + refreshHash,
		ClientID:         clientID,
		Connection:       connection,
		JTI:              "jti-" + refreshHash,
		RefreshHash:      refreshHash,
		ExpiresAt:        time.Now().Add(time.Hour),
		RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}
}

const testVerifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcde"

func pkceChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
