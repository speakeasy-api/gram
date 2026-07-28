package skills

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkillsRequestDecoderBoundsOversizedBody(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(
		http.MethodPost,
		"/rpc/skills.create",
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
	request := httptest.NewRequest(http.MethodPost, "/rpc/skills.create", bytes.NewReader(body))
	_ = skillsRequestDecoder(request)

	read, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.Equal(t, body, read)
}
