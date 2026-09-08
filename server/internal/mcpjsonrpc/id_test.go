package mcpjsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestID_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshals_int64_id", func(t *testing.T) {
		t.Parallel()
		result, err := json.Marshal(NumberID(42))
		require.NoError(t, err)
		require.Equal(t, "42", string(result))
	})

	t.Run("marshals_string_id", func(t *testing.T) {
		t.Parallel()
		result, err := json.Marshal(StringID("test-id"))
		require.NoError(t, err)
		require.Equal(t, `"test-id"`, string(result))
	})

	t.Run("marshals_null_id", func(t *testing.T) {
		t.Parallel()
		result, err := json.Marshal(NullID())
		require.NoError(t, err)
		require.Equal(t, "null", string(result))
	})
}

func TestID_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshals_int64_id", func(t *testing.T) {
		t.Parallel()
		var id ID
		err := json.Unmarshal([]byte("42"), &id)
		require.NoError(t, err)
		require.True(t, id.IsSet())
		require.Equal(t, idFormatNumber, id.format)
		require.Equal(t, int64(42), id.Number)
		require.Equal(t, "42", id.Value())
	})

	t.Run("unmarshals_string_id", func(t *testing.T) {
		t.Parallel()
		var id ID
		err := json.Unmarshal([]byte(`"test-id"`), &id)
		require.NoError(t, err)
		require.True(t, id.IsSet())
		require.Equal(t, idFormatString, id.format)
		require.Equal(t, "test-id", id.String)
		require.Equal(t, "test-id", id.Value())
	})

	t.Run("unmarshals_null_id", func(t *testing.T) {
		t.Parallel()
		var id ID
		err := json.Unmarshal([]byte("null"), &id)
		require.NoError(t, err)
		require.True(t, id.IsSet())
		require.Equal(t, idFormatNull, id.format)

		result, err := json.Marshal(id)
		require.NoError(t, err)
		require.Equal(t, "null", string(result))
	})

	t.Run("returns_error_for_invalid_json_rpc_id", func(t *testing.T) {
		t.Parallel()
		var id ID
		err := json.Unmarshal([]byte(`{}`), &id)
		require.Error(t, err)
	})
}

func TestID_Value(t *testing.T) {
	t.Parallel()

	require.Equal(t, "123", NumberID(123).Value())
	require.Equal(t, "my-id", StringID("my-id").Value())
	require.Empty(t, NullID().Value())
}

// TestID_IsNullCoversAbsentAndExplicitNull covers both ways a JSON-RPC message
// ends up naming no request. They arrive by different routes — an absent id
// never reaches UnmarshalJSON at all, while an explicit null does — and
// callers deciding how to answer such a message must not have to tell them
// apart.
func TestID_IsNullCoversAbsentAndExplicitNull(t *testing.T) {
	t.Parallel()

	var absent ID
	require.True(t, absent.IsNull(), "a notification carries no id field")

	var explicit ID
	require.NoError(t, json.Unmarshal([]byte(`null`), &explicit))
	require.True(t, explicit.IsNull(), "an explicit null names no request either")

	require.True(t, NullID().IsNull())
}

// TestID_IsNullIsFalseForRealIDs guards the other direction: a request that
// does name itself must never be mistaken for one that does not, including the
// zero-valued number and empty string a naive emptiness check would swallow.
func TestID_IsNullIsFalseForRealIDs(t *testing.T) {
	t.Parallel()

	require.False(t, NumberID(1).IsNull())
	require.False(t, NumberID(0).IsNull(), "zero is a valid JSON-RPC id")
	require.False(t, StringID("req-1").IsNull())
	require.False(t, StringID("").IsNull(), "an empty string id is still an id")
}
