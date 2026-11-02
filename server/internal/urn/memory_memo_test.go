package urn

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestParseMemoryMemoRejectsNilID(t *testing.T) {
	t.Parallel()
	_, err := ParseMemoryMemo("memory_memo:" + uuid.Nil.String())
	require.ErrorIs(t, err, ErrInvalid)

	id := uuid.New()
	parsed, err := ParseMemoryMemo("memory_memo:" + id.String())
	require.NoError(t, err)
	require.Equal(t, id, parsed.ID)
}

func TestMemoryMemoValidateFollowsID(t *testing.T) {
	t.Parallel()
	memo := NewMemoryMemo(uuid.Nil)
	_, err := memo.MarshalText()
	require.ErrorIs(t, err, ErrInvalid)

	memo.ID = uuid.New()
	text, err := memo.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "memory_memo:"+memo.ID.String(), string(text))
}
