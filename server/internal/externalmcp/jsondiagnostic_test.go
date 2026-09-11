package externalmcp

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeJSONDiagnostic(t *testing.T) {
	t.Parallel()
	t.Run("syntax", func(t *testing.T) {
		t.Parallel()
		var v any
		err := json.Unmarshal([]byte(`{"secret":!}`), &v)
		require.EqualError(t, safeJSONDiagnostic(err), "JSON syntax error at byte offset 11")
	})
	t.Run("numeric-value", func(t *testing.T) {
		t.Parallel()
		var v int64
		err := json.Unmarshal([]byte(`123456789012345678901234567890`), &v)
		require.ErrorContains(t, err, "123456789012345678901234567890")
		diagnostic := safeJSONDiagnostic(err)
		require.EqualError(t, diagnostic, "JSON type mismatch at byte offset 30 (destination int64)")
	})
	t.Run("custom-marshaler", func(t *testing.T) {
		t.Parallel()
		_, err := json.Marshal(sensitiveJSONMarshaler{})
		require.ErrorContains(t, err, "secret-value")
		require.EqualError(t, safeJSONDiagnostic(err), "JSON encoding or decoding failed")
	})
	t.Run("unsupported-value", func(t *testing.T) {
		t.Parallel()
		_, err := json.Marshal(math.Inf(1))
		require.EqualError(t, safeJSONDiagnostic(err), "unsupported JSON value")
	})
	t.Run("unsupported-type", func(t *testing.T) {
		t.Parallel()
		_, err := json.Marshal(make(chan int)) //nolint:staticcheck // Exercise safe diagnostics for unsupported types.
		require.EqualError(t, safeJSONDiagnostic(err), "unsupported JSON type chan int")
	})
}

type sensitiveJSONMarshaler struct{}

func (sensitiveJSONMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("secret-value")
}
