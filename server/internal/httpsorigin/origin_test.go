package httpsorigin

import "testing"

func TestNormalization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw, want string
		strict    bool
	}{
		{"https://EXAMPLE.com:443/", "https://example.com", true},
		{"HTTPS://EXAMPLE.com", "https://example.com", true},
		{"https://example.com:8443", "https://example.com:8443", true},
		{"https://[2001:DB8::1]:443/", "https://[2001:db8::1]", true},
		{"https://[2001:db8::443]", "https://[2001:db8::443]", true},
		{"https://[2001:DB8::1]:8443", "https://[2001:db8::1]:8443", true},
		{"https://[fe80::1%25eth0]:443/", "https://[fe80::1%25eth0]", true},
		{"https://EXAMPLE.com:443/mcp?q=1#fragment", "https://example.com", false},
		{"https://example.com?", "https://example.com", false},
		{"https://example.com/%2f", "https://example.com", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := FromURL(tc.raw)
			if tc.strict {
				got, err = Parse(tc.raw)
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRejectInvalidOrigins(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "http://example.com", "//example.com", "https:///", "https:example.com", "https://user:password@example.com", "https://user@example.com/mcp", "https://example.com:bad", "https://[::1"} {
		t.Run(raw, func(t *testing.T) {
			for _, parse := range []func(string) (string, error){Parse, FromURL} {
				got, err := parse(raw)
				if err == nil || got != "" {
					t.Fatalf("accepted invalid URL: %q, %v", got, err)
				}
			}
		})
	}
	for _, raw := range []string{"https://example.com/mcp", "https://example.com?", "https://example.com?q=1", "https://example.com/#fragment", "https://example.com/%2f"} {
		t.Run(raw, func(t *testing.T) {
			if got, err := Parse(raw); err == nil || got != "" {
				t.Fatalf("accepted non-origin: %q, %v", got, err)
			}
			if _, err := FromURL(raw); err != nil {
				t.Fatalf("rejected destination URL: %v", err)
			}
		})
	}
}
