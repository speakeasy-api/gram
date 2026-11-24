package skills

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/http/skills/server"
)

func TestSkillsRequestDecoderAllowsMissingApproveAllBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, srv.ApproveAllSuggestionsSkillsPath(), http.NoBody)
	payload, err := srv.DecodeApproveAllSuggestionsRequest(nil, skillsRequestDecoder)(request)

	require.NoError(t, err)
	require.Empty(t, payload.SuggestionIds)
}

func TestSkillsRequestDecoderBoundsOversizedBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		http.MethodPost,
		"/rpc/skills.update",
		bytes.NewReader(make([]byte, maxSkillsRequestBodyBytes+1)),
	)
	_ = skillsRequestDecoder(request)

	read, err := io.ReadAll(request.Body)
	var maxBytesErr *http.MaxBytesError
	require.Error(t, err)
	require.ErrorAs(t, err, &maxBytesErr)
	require.Equal(t, int64(maxSkillsRequestBodyBytes), maxBytesErr.Limit)
	require.Len(t, read, maxSkillsRequestBodyBytes)
}

func TestSkillsRequestDecoderAllowsBodyAtLimit(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("a"), maxSkillsRequestBodyBytes)
	request := httptest.NewRequest(http.MethodPost, "/rpc/skills.update", bytes.NewReader(body))
	_ = skillsRequestDecoder(request)

	read, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.Equal(t, body, read)
}

func TestSkillsRequestDecoderAllowsFileUploadsOnFileMethods(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/rpc/skills.create", "/rpc/skills.addVersion", "/rpc/skills.approveSuggestion"} {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(make([]byte, maxSkillFilesRequestBodyBytes+1)))
		_ = skillsRequestDecoder(request)

		read, err := io.ReadAll(request.Body)
		var maxBytesErr *http.MaxBytesError
		require.ErrorAs(t, err, &maxBytesErr, path)
		require.Equal(t, int64(maxSkillFilesRequestBodyBytes), maxBytesErr.Limit, path)
		require.Len(t, read, maxSkillFilesRequestBodyBytes, path)
	}
}
