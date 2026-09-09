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
		// DecodeHeader preserves malformed encoded-word fragments. Validate every
		// raw marker independently rather than relying on whitespace token
		// boundaries; valid decoded user text may still contain a literal "=?".
		if err := validateEncodedWordMarkers(raw); err != nil {
			return "", fmt.Errorf("invalid RFC 2047 encoding in %s: %w", name, err)
		}
	}
	if decoded == "" || strings.TrimSpace(decoded) != decoded || !utf8.ValidString(decoded) || strings.ContainsAny(decoded, "\r\n\x00") {
		return "", fmt.Errorf("invalid decoded %s value", name)
	}
	return decoded, nil
}

func looksLikeCompleteEncodedWord(candidate string) bool {
	rest, ok := strings.CutPrefix(candidate, "=?")
	if !ok {
		return false
	}
	charset, rest, ok := strings.Cut(rest, "?")
	if !ok || charset == "" {
		return false
	}
	encoding, rest, ok := strings.Cut(rest, "?")
	return ok && len(encoding) == 1 && strings.ContainsAny(encoding, "bBqQ") && strings.HasSuffix(rest, "?=")
}

func validateEncodedWordMarkers(raw string) error {
	decoder := &mime.WordDecoder{CharsetReader: nil}
	for offset := 0; ; {
		relativeStart := strings.Index(raw[offset:], "=?")
		if relativeStart < 0 {
			return nil
		}
		start := offset + relativeStart
		relativeEnd := strings.Index(raw[start+2:], "?=")
		if relativeEnd < 0 {
			return fmt.Errorf("unterminated encoded word")
		}
		end := start + 2 + relativeEnd + 2
		candidate := raw[start:end]
		if !looksLikeCompleteEncodedWord(candidate) {
			return fmt.Errorf("malformed encoded word")
		}
		if _, err := decoder.DecodeHeader(candidate); err != nil {
			return fmt.Errorf("decode encoded word: %w", err)
		}
		offset = end
	}
}
