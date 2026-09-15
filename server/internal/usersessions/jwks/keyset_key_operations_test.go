package jwks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func keyWithOperations(t *testing.T, kid, operations string) json.RawMessage {
	t.Helper()

	raw, err := testKey(t, kid).MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, byte('}'), raw[len(raw)-1])
	return append(raw[:len(raw)-1], []byte(`,"key_ops":`+operations+`}`)...)
}

func TestParseKeySet_KeyOperationsPermitVerification(t *testing.T) {
	t.Parallel()

	withoutOperations, err := testKey(t, "absent").MarshalJSON()
	require.NoError(t, err)

	for _, test := range []struct {
		name string
		key  json.RawMessage
	}{
		{name: "absent", key: withoutOperations},
		{name: "verify", key: keyWithOperations(t, "verify", `["verify"]`)},
		{name: "sign and verify", key: keyWithOperations(t, "sign-verify", `["sign","verify"]`)},
		{name: "verify and encrypt", key: keyWithOperations(t, "verify-encrypt", `["verify","encrypt"]`)},
		{name: "duplicate verify", key: keyWithOperations(t, "duplicate", `["verify","verify"]`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			set, err := parseKeySet(json.RawMessage(`{"keys":[` + string(test.key) + `]}`))
			require.NoError(t, err)
			require.Len(t, set.Keys, 1)
		})
	}
}

func TestParseKeySet_SkipsUnusableKeyOperations(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		operations string
	}{
		{name: "encrypt", operations: `["encrypt"]`},
		{name: "sign only", operations: `["sign"]`},
		{name: "empty", operations: `[]`},
		{name: "null", operations: `null`},
		{name: "non-array", operations: `"verify"`},
		{name: "non-string entry", operations: `["verify",1]`},
		{name: "null entry", operations: `["verify",null]`},
		{name: "empty entry", operations: `["verify",""]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			unusable := keyWithOperations(t, "unusable", test.operations)
			sibling, err := testKey(t, "usable").MarshalJSON()
			require.NoError(t, err)
			set, err := parseKeySet(json.RawMessage(`{"keys":[` + string(unusable) + `,` + string(sibling) + `]}`))
			require.NoError(t, err)
			require.Len(t, set.Keys, 1, "the usable sibling must survive")
			require.Equal(t, "usable", set.Keys[0].KeyID)

			_, err = selectKey(set, "unusable")
			require.ErrorIs(t, err, ErrKeyNotFound)
		})
	}
}
