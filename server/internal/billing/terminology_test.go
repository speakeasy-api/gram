package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdditionalChatCreditsBullet(t *testing.T) {
	require.Equal(
		t,
		"$11 per 10 additional chat credits",
		AdditionalChatCreditsBullet("$11", 10),
	)
}
