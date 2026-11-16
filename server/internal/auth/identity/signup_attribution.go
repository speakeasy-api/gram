package identity

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// maxAttributionValueLength bounds each attribution value. Ad platforms never
// send anything close to this; it caps what a crafted sign-up link can push
// into the login nonce store and onward into analytics.
const maxAttributionValueLength = 200

// SignupAttribution is where a sign-up came from, as the sign-up page saw it:
// the UTMs it was opened with (carried over from the marketing site), the
// browser's product analytics session, and the host it was served on.
//
// It travels from the login request to the auth callback alongside the
// sign-up intent and is attached to the server-side sign-up events, which
// otherwise carry no browser context and cannot be attributed to a campaign.
//
// No serialization tags: the nonce store marshals with msgpack, which keys by
// Go field name.
type SignupAttribution struct {
	UTMSource   string
	UTMMedium   string
	UTMCampaign string
	UTMTerm     string
	UTMContent  string
	// SessionID is the browser's product analytics session id. Setting it on
	// a server event joins that event to the browser session, and with it the
	// session's entry UTMs, referrer and replay.
	SessionID string
	// Host is the host the sign-up page was served from.
	Host string
}

// NewSignupAttribution normalizes raw, caller-supplied attribution values.
// Every value is optional; anything blank is dropped and anything oversized
// is truncated rather than rejected, so attribution can never block a login.
// Returns nil when there is nothing worth carrying.
func NewSignupAttribution(utmSource, utmMedium, utmCampaign, utmTerm, utmContent, sessionID, baseURL string) *SignupAttribution {
	a := SignupAttribution{
		UTMSource:   cleanAttributionValue(utmSource),
		UTMMedium:   cleanAttributionValue(utmMedium),
		UTMCampaign: cleanAttributionValue(utmCampaign),
		UTMTerm:     cleanAttributionValue(utmTerm),
		UTMContent:  cleanAttributionValue(utmContent),
		SessionID:   cleanAttributionValue(sessionID),
		Host:        "",
	}
	if !a.hasSignal() {
		// The host alone says nothing about where the visitor came from.
		return nil
	}
	if u, err := url.Parse(baseURL); err == nil {
		a.Host = u.Host
	}
	return &a
}

func (a SignupAttribution) hasSignal() bool {
	return a.UTMSource != "" || a.UTMMedium != "" || a.UTMCampaign != "" ||
		a.UTMTerm != "" || a.UTMContent != "" || a.SessionID != ""
}

// Properties returns the attribution as product analytics event properties.
// The UTMs use the same names the browser SDK records, so a server-side
// sign-up event breaks down by campaign exactly like a browser event, and
// $set_once fills the person's initial-touch UTMs when the browser never
// recorded them. Safe to call on a nil receiver.
func (a *SignupAttribution) Properties() map[string]any {
	if a == nil {
		return map[string]any{}
	}

	props := map[string]any{}
	initial := map[string]any{}
	for key, value := range map[string]string{
		"utm_source":   a.UTMSource,
		"utm_medium":   a.UTMMedium,
		"utm_campaign": a.UTMCampaign,
		"utm_term":     a.UTMTerm,
		"utm_content":  a.UTMContent,
	} {
		if value == "" {
			continue
		}
		props[key] = value
		initial["$initial_"+key] = value
	}
	if len(initial) > 0 {
		props["$set_once"] = initial
	}
	if a.SessionID != "" {
		props["$session_id"] = a.SessionID
	}
	if a.Host != "" {
		props["$host"] = a.Host
	}
	return props
}

func cleanAttributionValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxAttributionValueLength {
		return value
	}
	// Back the cut up to a rune boundary so it never splits a multi-byte
	// character. Only the cut point matters: invalid bytes earlier in the
	// value are kept as they are rather than emptying the whole value.
	cut := maxAttributionValueLength
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
