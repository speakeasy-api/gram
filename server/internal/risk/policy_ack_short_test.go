package risk

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGeneratePolicyAckShortURL(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	c := cache.NewRedisCacheAdapter(client)

	site, err := url.Parse("https://ai.example.com")
	require.NoError(t, err)
	input := PolicyAckTokenInput{
		OrganizationID:   "org-1",
		ProjectID:        uuid.NewString(),
		UserID:           "user-1",
		RiskPolicyID:     uuid.NewString(),
		PolicyName:       "Government IDs (warn)",
		ToolName:         nil,
		CallFingerprint:  "fp",
		ChallengeMessage: "618-27-4093 looks like sensitive information.",
		RememberFor:      0,
	}

	link, _, err := GeneratePolicyAckShortURL(t.Context(), c, site, input, 0)
	require.NoError(t, err)
	require.Regexp(t, `^https://ai\.example\.com/ack#[0-9A-Za-z]{10}$`, link, "a bare 10-character id, no rpak1. prefix")

	id := link[strings.Index(link, "#")+1:]
	record, err := lookupPolicyAckRecord(t.Context(), c, id)
	require.NoError(t, err)
	require.Equal(t, "user-1", record.UserID)
	require.Equal(t, "fp", record.CallFingerprint)

	other, _, err := GeneratePolicyAckShortURL(t.Context(), c, site, input, 0)
	require.NoError(t, err)
	require.NotEqual(t, link, other, "each warning gets its own id")

	for _, bad := range []string{"", "short", id + "x", "abc-def_gh", "rpak1."} {
		_, err := lookupPolicyAckRecord(t.Context(), c, bad)
		require.Error(t, err, bad)
	}

	long, _, err := GeneratePolicyAckToken(t.Context(), c, input, 0)
	require.NoError(t, err)
	_, err = lookupPolicyAckRecord(t.Context(), c, long)
	require.NoError(t, err, "the long rpak1. token still resolves")
}

func TestNewPolicyAckShortIDIsBase62(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 200 {
		id, err := newPolicyAckShortID()
		require.NoError(t, err)
		require.True(t, isPolicyAckShortID(id), id)
		seen[id] = true
	}
	require.Len(t, seen, 200)
}

func TestInvalidatePolicyAckTokenEvictsShortIDs(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	c := cache.NewRedisCacheAdapter(client)

	site, err := url.Parse("https://ai.example.com")
	require.NoError(t, err)
	link, _, err := GeneratePolicyAckShortURL(t.Context(), c, site, PolicyAckTokenInput{
		OrganizationID:   "org-1",
		ProjectID:        uuid.NewString(),
		UserID:           "user-1",
		RiskPolicyID:     uuid.NewString(),
		PolicyName:       "Government IDs (warn)",
		ToolName:         nil,
		CallFingerprint:  "fp",
		ChallengeMessage: "",
		RememberFor:      0,
	}, 0)
	require.NoError(t, err)
	id := link[strings.Index(link, "#")+1:]

	invalidatePolicyAckToken(t.Context(), c, id)
	_, err = lookupPolicyAckRecord(t.Context(), c, id)
	require.Error(t, err, "a used short link cannot be redeemed again")
}

func TestPolicyAckNotYoursMatchesInvalidToken(t *testing.T) {
	t.Parallel()

	var notYours, unknown *oops.ShareableError
	require.ErrorAs(t, errPolicyAckNotYours(), &notYours)
	require.ErrorAs(t, errPolicyAckInvalid(errors.New("risk policy ack not found or expired")), &unknown)
	require.Equal(t, unknown.Code, notYours.Code, "same code")
	require.Equal(t, unknown.Error(), notYours.Error(), "same public message")
}
