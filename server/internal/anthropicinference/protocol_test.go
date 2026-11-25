package anthropicinference

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func signedHeaders(body []byte, key []byte, at time.Time) http.Header {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("request-example." + timestamp + "."))
	_, _ = mac.Write(body)
	headers := make(http.Header)
	headers.Set("webhook-id", "request-example")
	headers.Set("webhook-timestamp", timestamp)
	headers.Set("webhook-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return headers
}

func TestVerifySignatureAcceptsRotatingKeys(t *testing.T) {
	t.Parallel()
	now := time.Unix(1800000000, 0)
	body := []byte(`{"type":"prompt"}`)
	oldKey := []byte("EXAMPLE-old-signing-secret")
	newKey := []byte("EXAMPLE-new-signing-secret")
	headers := signedHeaders(body, oldKey, now)
	headers.Set("webhook-signature", "v2,unknown v1,invalid "+headers.Get("webhook-signature"))
	require.NoError(t, verifySignature(headers, body, [][]byte{newKey, oldKey}, now))
}

func TestVerifySignatureRejectsTampering(t *testing.T) {
	t.Parallel()
	now := time.Unix(1800000000, 0)
	body := []byte(`{"type":"prompt"}`)
	key := []byte("EXAMPLE-signing-secret")
	headers := signedHeaders(body, key, now)
	require.Error(t, verifySignature(headers, append(body, ' '), [][]byte{key}, now))
	headers.Set("webhook-id", "another-request")
	require.Error(t, verifySignature(headers, body, [][]byte{key}, now))
}

func TestVerifySignatureRejectsExpiredAndFutureRequests(t *testing.T) {
	t.Parallel()
	now := time.Unix(1800000000, 0)
	body := []byte(`{}`)
	key := []byte("EXAMPLE-signing-secret")
	require.Error(t, verifySignature(signedHeaders(body, key, now.Add(-301*time.Second)), body, [][]byte{key}, now))
	require.Error(t, verifySignature(signedHeaders(body, key, now.Add(301*time.Second)), body, [][]byte{key}, now))
	require.NoError(t, verifySignature(signedHeaders(body, key, now.Add(-300*time.Second)), body, [][]byte{key}, now))
}

func TestVerifySignatureRejectsMissingCredentials(t *testing.T) {
	t.Parallel()
	now := time.Unix(1800000000, 0)
	body := []byte(`{}`)
	require.Error(t, verifySignature(make(http.Header), body, nil, now))
	require.Error(t, verifySignature(signedHeaders(body, nil, now), body, [][]byte{nil}, now))
}

func TestMessageTextIncludesAttachmentsAndTools(t *testing.T) {
	t.Parallel()
	message := Message{Role: "user", Content: json.RawMessage(`[
 {"type":"text","text":"Review this"},
 {"type":"attachment","text":"EXAMPLE attachment content"},
 {"type":"tool_use","name":"read_file","input":{"path":"example.txt"}},
 {"type":"tool_result","tool_name":"read_file","content":"EXAMPLE tool output"},
 {"type":"future_block","new_field":true}
 ]`)}
	text, err := messageText(message)
	require.NoError(t, err)
	require.Equal(t, "Review this\nEXAMPLE attachment content\nread_file: {\"path\":\"example.txt\"}\nread_file: EXAMPLE tool output", text)
}
