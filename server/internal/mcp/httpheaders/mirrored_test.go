package httpheaders_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
)

func TestDecodeMirroredValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain value", value: "get_weather", want: "get_weather"},
		{name: "empty value", value: "", want: ""},
		{name: "base64 sentinel", value: "=?base64?w6lsw6h2ZQ==?=", want: "élève"},
		{name: "uppercase sentinel is literal", value: "=?BASE64?SGVsbG8=?=", want: "=?BASE64?SGVsbG8=?="},
		{name: "missing suffix is literal", value: "=?base64?SGVsbG8=", want: "=?base64?SGVsbG8="},
		{name: "missing prefix is literal", value: "SGVsbG8=?=", want: "SGVsbG8=?="},
		{name: "overlapping sentinel is literal", value: "=?base64?=", want: "=?base64?="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := httpheaders.DecodeMirroredValue(tc.value)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestDecodeMirroredValue_InvalidBase64(t *testing.T) {
	t.Parallel()

	_, err := httpheaders.DecodeMirroredValue("=?base64?SGVsbG8?=")
	require.ErrorIs(t, err, httpheaders.ErrInvalidBase64)
}

func TestMirroredValue_Absent(t *testing.T) {
	t.Parallel()

	value, present, err := httpheaders.MirroredValue(http.Header{}, httpheaders.NameHeader)
	require.NoError(t, err)
	require.False(t, present)
	require.Empty(t, value)
}

func TestMirroredValue_DecodesSingleValue(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("mcp-name", "=?base64?w6lsw6h2ZQ==?=")

	value, present, err := httpheaders.MirroredValue(header, httpheaders.NameHeader)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, "élève", value)
}

func TestMirroredValue_RejectsRepeatedHeader(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Add(httpheaders.MethodHeader, "tools/call")
	header.Add(httpheaders.MethodHeader, "tools/list")

	_, present, err := httpheaders.MirroredValue(header, httpheaders.MethodHeader)
	require.True(t, present)
	require.ErrorIs(t, err, httpheaders.ErrRepeatedHeader)
}

func TestMirroredValue_MethodDoesNotDecodeBase64(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set(httpheaders.MethodHeader, "=?base64?dG9vbHMvbGlzdA==?=")

	value, present, err := httpheaders.MirroredValue(header, httpheaders.MethodHeader)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, header.Get(httpheaders.MethodHeader), value)
}

func TestMirroredNameField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method    string
		wantField string
		wantOK    bool
	}{
		{method: "tools/call", wantField: "name", wantOK: true},
		{method: "prompts/get", wantField: "name", wantOK: true},
		{method: "resources/read", wantField: "uri", wantOK: true},
		{method: "tools/list", wantField: "", wantOK: false},
		{method: "", wantField: "", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			field, ok := httpheaders.MirroredNameField(tc.method)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantField, field)
		})
	}
}
