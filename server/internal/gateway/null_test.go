package gateway

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNullable_IsValid_NilReceiver(t *testing.T) {
	t.Parallel()

	var n *Nullable[string]
	require.False(t, n.IsValid())
}

func TestNullable_IsValid_FalseValid(t *testing.T) {
	t.Parallel()

	n := Nullable[string]{Valid: false, Value: "anything"}
	require.False(t, n.IsValid())
}

func TestNullable_IsValid_TrueValid(t *testing.T) {
	t.Parallel()

	n := Nullable[string]{Valid: true, Value: "x"}
	require.True(t, n.IsValid())
}

func TestNullable_MarshalJSON_Invalid(t *testing.T) {
	t.Parallel()

	n := Nullable[string]{Valid: false, Value: "ignored"}
	got, err := json.Marshal(n)
	require.NoError(t, err)
	require.Equal(t, "null", string(got))
}

func TestNullable_MarshalJSON_ValidString(t *testing.T) {
	t.Parallel()

	n := Nullable[string]{Valid: true, Value: "hi"}
	got, err := json.Marshal(n)
	require.NoError(t, err)
	require.Equal(t, `"hi"`, string(got))
}

func TestNullable_MarshalJSON_ValidInt(t *testing.T) {
	t.Parallel()

	n := Nullable[int]{Valid: true, Value: 42}
	got, err := json.Marshal(n)
	require.NoError(t, err)
	require.Equal(t, "42", string(got))
}

func TestNullable_UnmarshalJSON_Null(t *testing.T) {
	t.Parallel()

	var n Nullable[string]
	require.NoError(t, json.Unmarshal([]byte("null"), &n))
	require.False(t, n.Valid)
	require.Equal(t, "", n.Value)
}

func TestNullable_UnmarshalJSON_String(t *testing.T) {
	t.Parallel()

	var n Nullable[string]
	require.NoError(t, json.Unmarshal([]byte(`"hello"`), &n))
	require.True(t, n.Valid)
	require.Equal(t, "hello", n.Value)
}

func TestNullable_UnmarshalJSON_EmptyString(t *testing.T) {
	t.Parallel()

	// An explicit "" is a valid value, distinct from null.
	var n Nullable[string]
	require.NoError(t, json.Unmarshal([]byte(`""`), &n))
	require.True(t, n.Valid)
	require.Equal(t, "", n.Value)
}

func TestNullable_UnmarshalJSON_Int(t *testing.T) {
	t.Parallel()

	var n Nullable[int]
	require.NoError(t, json.Unmarshal([]byte("7"), &n))
	require.True(t, n.Valid)
	require.Equal(t, 7, n.Value)
}

func TestNullable_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()

	var n Nullable[int]
	err := json.Unmarshal([]byte(`"not-an-int"`), &n)
	require.Error(t, err)
}

func TestNullable_RoundTrip_Valid(t *testing.T) {
	t.Parallel()

	original := Nullable[string]{Valid: true, Value: "round trip"}
	bs, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Nullable[string]
	require.NoError(t, json.Unmarshal(bs, &restored))
	require.Equal(t, original, restored)
}

func TestNullable_RoundTrip_Invalid(t *testing.T) {
	t.Parallel()

	original := Nullable[string]{Valid: false, Value: ""}
	bs, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Nullable[string]
	require.NoError(t, json.Unmarshal(bs, &restored))
	require.Equal(t, original, restored)
}

func TestNullString_Alias(t *testing.T) {
	t.Parallel()

	// NullString is an alias for Nullable[string]; ensure it round-trips.
	var n NullString
	require.NoError(t, json.Unmarshal([]byte(`"x"`), &n))
	require.True(t, n.Valid)
	require.Equal(t, "x", n.Value)
}
