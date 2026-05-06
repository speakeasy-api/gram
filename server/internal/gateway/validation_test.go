package gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const objectSchema = `{
  "type": "object",
  "properties": {
    "name": { "type": "string" },
    "count": { "type": "integer" }
  },
  "required": ["name"]
}`

func TestValidateToolCallBody_Valid(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"name":"alice","count":3}`)
	require.NoError(t, validateToolCallBody(t.Context(), logger, body, objectSchema))
}

func TestValidateToolCallBody_MissingRequired(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"count":3}`)
	err := validateToolCallBody(t.Context(), logger, body, objectSchema)
	require.Error(t, err)
	require.Contains(t, err.Error(), "input to toolschema validation failure")
}

func TestValidateToolCallBody_WrongType(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"name":"alice","count":"three"}`)
	err := validateToolCallBody(t.Context(), logger, body, objectSchema)
	require.Error(t, err)
}

func TestValidateToolCallBody_StripsGramAddedFields(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	// gram-request-summary and environmentVariables are not in the schema,
	// but validation must succeed because they get stripped before validation.
	body := []byte(`{"name":"alice","gram-request-summary":"some summary","environmentVariables":{"FOO":"bar"}}`)
	require.NoError(t, validateToolCallBody(t.Context(), logger, body, objectSchema))
}

func TestValidateToolCallBody_InvalidSchema_Lenient(t *testing.T) {
	t.Parallel()

	// An uncompilable schema must NOT fail the request — it logs and returns
	// nil so requests aren't blocked by author errors.
	logger := testenv.NewLogger(t)
	require.NoError(t, validateToolCallBody(t.Context(), logger, []byte(`{"name":"alice"}`), `{not valid json`))
}

func TestValidateToolCallBody_InvalidJSONBody_Lenient(t *testing.T) {
	t.Parallel()

	// An unparseable body must also be tolerated (logged, not returned).
	logger := testenv.NewLogger(t)
	require.NoError(t, validateToolCallBody(t.Context(), logger, []byte(`{not json`), objectSchema))
}
