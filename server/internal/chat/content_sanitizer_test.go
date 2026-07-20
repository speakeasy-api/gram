package chat

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeContentJSON_BareStringUntouched(t *testing.T) {
	t.Parallel()

	in := []byte(`"hello world"`)
	require.Equal(t, in, sanitizeContentJSON(in))
}

func TestSanitizeContentJSON_TextPartsUntouched(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"text","text":"one"},{"type":"text","text":"two"}]`)
	require.Equal(t, in, sanitizeContentJSON(in))
}

func TestSanitizeContentJSON_RemoteImageURLUntouched(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"https://example.com/cat.png","detail":"high"}}]`)
	require.Equal(t, in, sanitizeContentJSON(in))
}

func TestSanitizeContentJSON_DataURIReplacedWithPlaceholder(t *testing.T) {
	t.Parallel()

	payload := base64.StdEncoding.EncodeToString(make([]byte, 300))
	in := fmt.Appendf(nil, `[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]`, payload)

	out := sanitizeContentJSON(in)
	require.NotContains(t, string(out), "base64")
	require.NotContains(t, string(out), payload)
	require.JSONEq(t, `[{"type":"text","text":"see"},{"type":"text","text":"[image omitted: image/png, ~300 bytes]"}]`, string(out))
}

func TestSanitizeContentJSON_DataURIWithoutBase64Marker(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"image_url","image_url":{"url":"data:image/svg+xml,12345"}}]`)
	out := sanitizeContentJSON(in)
	require.JSONEq(t, `[{"type":"text","text":"[image omitted: image/svg+xml, ~5 bytes]"}]`, string(out))
}

func TestSanitizeContentJSON_MixedPartsOnlyDataURIRewritten(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,aGVsbG8="}},{"type":"text","text":"tail"}]`)
	out := sanitizeContentJSON(in)
	require.JSONEq(t, `[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}},{"type":"text","text":"[image omitted: image/jpeg, ~6 bytes]"},{"type":"text","text":"tail"}]`, string(out))
}

func TestSanitizeContentJSON_UnknownPartTypesPassThrough(t *testing.T) {
	t.Parallel()

	in := []byte(`[{"type":"input_audio","input_audio":{"data":"xyz","format":"wav"}}]`)
	require.Equal(t, in, sanitizeContentJSON(in))
}

func TestSanitizeContentJSON_InvalidJSONPassesThrough(t *testing.T) {
	t.Parallel()

	in := []byte(`[not json`)
	require.Equal(t, in, sanitizeContentJSON(in))
}
