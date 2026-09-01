package netingress

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	ProviderTailscale = "tailscale"

	TailscaleUserLoginHeader      = "Tailscale-User-Login"
	TailscaleUserNameHeader       = "Tailscale-User-Name"
	TailscaleUserProfilePicHeader = "Tailscale-User-Profile-Pic"
)

var tailscaleIdentityHeaders = map[string]struct{}{
	TailscaleUserLoginHeader:      {},
	TailscaleUserNameHeader:       {},
	TailscaleUserProfilePicHeader: {},
}

type TailscaleIdentityParser struct{}

func (TailscaleIdentityParser) ParseIdentity(headers http.Header) (*requestorigin.NetworkIdentity, error) {
	login, err := parseTailscaleIdentityValue(headers, TailscaleUserLoginHeader, true)
	if err != nil {
		return nil, err
	}
	name, err := parseTailscaleIdentityValue(headers, TailscaleUserNameHeader, true)
	if err != nil {
		return nil, err
	}
	if _, err := parseTailscaleIdentityValue(headers, TailscaleUserProfilePicHeader, false); err != nil {
		return nil, err
	}

	if login == "" && name == "" {
		return nil, nil
	}
	if login == "" || name == "" {
		return nil, fmt.Errorf("incomplete Tailscale user identity")
	}
	return &requestorigin.NetworkIdentity{Login: login, Name: name}, nil
}

// StripUnsupportedTailscaleHeaders removes every Tailscale-owned header except
// the three identity headers validated during AIS-664. Authorization and all
// non-Tailscale headers are intentionally untouched.
func StripUnsupportedTailscaleHeaders(headers http.Header) {
	for name := range headers {
		canonical := http.CanonicalHeaderKey(name)
		if !strings.HasPrefix(canonical, "Tailscale-") {
			continue
		}
		if _, ok := tailscaleIdentityHeaders[canonical]; !ok {
			headers.Del(name)
		}
	}
}

func DeleteTailscaleIdentityHeaders(headers http.Header) {
	for name := range tailscaleIdentityHeaders {
		headers.Del(name)
	}
}

func parseTailscaleIdentityValue(headers http.Header, name string, decodeWord bool) (string, error) {
	values := headers.Values(name)
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 {
		return "", fmt.Errorf("multiple %s values", name)
	}
	raw := values[0]
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00") {
		return "", fmt.Errorf("invalid %s value", name)
	}

	decoded := raw
	if decodeWord && strings.Contains(raw, "=?") {
		var err error
		decoded, err = (&mime.WordDecoder{CharsetReader: nil}).DecodeHeader(raw)
		if err != nil {
			return "", fmt.Errorf("decode %s: %w", name, err)
		}
		// mime.WordDecoder deliberately leaves malformed encoded-words intact.
		// Treat an unchanged marker as invalid rather than accepting ambiguous
		// provider identity text.
		if decoded == raw {
			return "", fmt.Errorf("invalid RFC 2047 encoding in %s", name)
		}
	}
	if decoded == "" || strings.TrimSpace(decoded) != decoded || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "\r\n\x00") {
		return "", fmt.Errorf("invalid decoded %s value", name)
	}
	return decoded, nil
}
