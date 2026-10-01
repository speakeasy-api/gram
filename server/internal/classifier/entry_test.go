package classifier_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
)

func TestEntryRoundTripsAllowedShapes(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`"instructions"`,
		`null`,
		`{}`,
		`[]`,
		`{"nested":[true,false,null,9007199254740993,1.234567890123456789,{"x":"y"}]}`,
		`["example",42,true,null,{"levels":[1,2]}]`,
	} {
		var entry classifier.Entry
		require.NoError(t, json.Unmarshal([]byte(input), &entry), input)
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		// Exact comparison proves large integers and decimal precision survive.
		require.Equal(t, input, string(encoded))
	}
}

func TestEntryRejectsInvalidShapesWithoutChangingReceiver(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", " ", "true", "false", "42", "1.5", `{"x":}`, `[] null`, `{"x":NaN}`, "\u00a0null", "{}\u2003", "\v[]"} {
		_, err := classifier.ParseEntry([]byte(input))
		require.Error(t, err, input)
		entry := classifier.Text("preserved")
		require.Error(t, entry.UnmarshalJSON([]byte(input)), input)
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		require.Equal(t, `"preserved"`, string(encoded))
	}
}

func TestEntryOwnsInputAndOutputBytes(t *testing.T) {
	t.Parallel()
	input := []byte(`{"key":"value"}`)
	entry, err := classifier.ParseEntry(input)
	require.NoError(t, err)
	copyOfEntry := entry
	input[2] = 'X'
	encoded, err := entry.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"key":"value"}`, string(encoded))
	encoded[2] = 'Y'
	for _, value := range []classifier.Entry{entry, copyOfEntry} {
		got, err := json.Marshal(value)
		require.NoError(t, err)
		require.JSONEq(t, `{"key":"value"}`, string(got))
	}
}

func TestEntryZeroValueAndNilEncodeAsNull(t *testing.T) {
	t.Parallel()
	var zero classifier.Entry
	nilEntry, err := classifier.NewEntry(nil)
	require.NoError(t, err)
	parsed, err := classifier.ParseEntry([]byte(" \nnull\t"))
	require.NoError(t, err)
	for _, entry := range []classifier.Entry{zero, nilEntry, parsed} {
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		require.Equal(t, "null", string(encoded))
	}
}

func TestEntryTextEscapesAndRoundTrips(t *testing.T) {
	t.Parallel()
	text := "quotes: \"; slash: \\; newline: \n; unicode: 日本語; control: \x00"
	encoded, err := json.Marshal(classifier.Text(text))
	require.NoError(t, err)
	var decoded string
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, text, decoded)
}

func TestNewEntryEncodesStructuredValues(t *testing.T) {
	t.Parallel()
	value := map[string]any{
		"instructions": "classify",
		"examples":     []any{"one", true, nil, json.Number("9007199254740993")},
	}
	entry, err := classifier.NewEntry(value)
	require.NoError(t, err)
	value["instructions"] = "changed"
	encoded, err := json.Marshal(entry)
	require.NoError(t, err)
	require.Equal(t, `{"examples":["one",true,null,9007199254740993],"instructions":"classify"}`, string(encoded)) //nolint:testifylint // JSONEq converts numbers to float64, hiding precision loss.
}

func TestNewEntryRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()
	for _, value := range []any{true, 42, math.NaN(), make(chan int), json.RawMessage(`false`), json.RawMessage(`{"bad":}`)} {
		_, err := classifier.NewEntry(value)
		require.Error(t, err)
	}
}
