package triggers

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebhookRenderSignTemplateIsSinglePass(t *testing.T) {
	t.Parallel()

	// A body containing the literal "{timestamp}" token must not be
	// corrupted by the timestamp substitution: a two-pass ReplaceAll would
	// overwrite the in-body token, producing a divergent MAC.
	body := []byte(`{"note":"{timestamp}"}`)
	got := renderSignTemplate("v0:{timestamp}:{body}", body, "1700000000")
	require.Equal(t, `v0:1700000000:{"note":"{timestamp}"}`, string(got))
}

func TestHMACSchemeHexBareBody(t *testing.T) {
	t.Parallel()

	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Sig", hex.EncodeToString(mac.Sum(nil)))

	scheme := HMACScheme{
		NewHash:  func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
		Header:   "X-Sig",
		Encoding: "hex",
	}
	require.NoError(t, scheme.Verify(body, headers, "shh"))
}

func TestHMACSchemeSlackTimestampedTemplate(t *testing.T) {
	t.Parallel()

	body := []byte(`{"event":{"type":"app_mention"}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	headers := http.Header{}
	headers.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Slack-Request-Timestamp", timestamp)

	scheme := HMACScheme{
		NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
		Header:          "X-Slack-Signature",
		Encoding:        "hex",
		Prefix:          "v0=",
		Template:        "v0:{timestamp}:{body}",
		TimestampHeader: "X-Slack-Request-Timestamp",
		TimestampSkew:   300 * time.Second,
	}
	require.NoError(t, scheme.Verify(body, headers, "shh"))
}

func TestHMACSchemeRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()

	body := []byte(`{}`)
	timestamp := strconv.FormatInt(time.Now().Add(-5*time.Minute).Unix(), 10)
	mac := hmac.New(sha256.New, []byte("shh"))
	mac.Write([]byte(timestamp + "." + string(body)))
	headers := http.Header{}
	headers.Set("X-Sig", hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Ts", timestamp)

	scheme := HMACScheme{
		NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
		Header:          "X-Sig",
		Encoding:        "hex",
		Template:        "{timestamp}.{body}",
		TimestampHeader: "X-Ts",
		TimestampSkew:   60 * time.Second,
	}
	err := scheme.Verify(body, headers, "shh")
	require.Error(t, err)
	require.Contains(t, err.Error(), "skew")
}

func TestHMACSchemeRejectsBadSecret(t *testing.T) {
	t.Parallel()

	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte("correct"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Sig", hex.EncodeToString(mac.Sum(nil)))

	scheme := HMACScheme{
		NewHash:  func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
		Header:   "X-Sig",
		Encoding: "hex",
	}
	err := scheme.Verify(body, headers, "wrong")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mismatch")
}

func TestHMACSchemeBase64SHA1(t *testing.T) {
	t.Parallel()

	body := []byte(`{"a":1}`)
	mac := hmac.New(sha1.New, []byte("shh"))
	mac.Write(body)
	headers := http.Header{}
	headers.Set("X-Sig", base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	scheme := HMACScheme{
		NewHash:  func(key []byte) hash.Hash { return hmac.New(sha1.New, key) },
		Header:   "X-Sig",
		Encoding: "base64",
	}
	require.NoError(t, scheme.Verify(body, headers, "shh"))
}

func TestHMACSchemeRejectsMissingSignatureHeader(t *testing.T) {
	t.Parallel()

	scheme := HMACScheme{
		NewHash:  func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
		Header:   "X-Sig",
		Encoding: "hex",
	}
	err := scheme.Verify([]byte(`{}`), http.Header{}, "shh")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing signature header")
}
