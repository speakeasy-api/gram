package accesscontrol

import "testing"

func TestCanonicalizeMatchValue_URL(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, " HTTPS://Example.COM/path/ ")
	if got != "https://example.com/path/" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/path/")
	}
}

func TestCanonicalizeMatchValue_FullURLStripsDefaultPortAndFragment(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, "https://example.com:443/mcp#tools")
	if got != "https://example.com/mcp" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/mcp")
	}
}

func TestCanonicalizeMatchValue_FullURLSortsQueryKeys(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindFullURL, "https://example.com/mcp?z=last&a=first")
	if got != "https://example.com/mcp?a=first&z=last" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "https://example.com/mcp?a=first&z=last")
	}
}

func TestCanonicalizeMatchValue_URLHostExtractsDefaultPortHostFromURL(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindURLHost, "HTTPS://Example.COM:443/path")
	if got != "example.com" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "example.com")
	}
}

func TestCanonicalizeMatchValue_CommandWhitespace(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindServerIdentity, "  mcp__github__   ")
	if got != "mcp__github__" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "mcp__github__")
	}
}

func TestCanonicalizeMatchValue_ServerIdentityLowercases(t *testing.T) {
	got := CanonicalizeMatchValue(MatchKindServerIdentity, "  Linear MCP  ")
	if got != "linear mcp" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "linear mcp")
	}
}

func TestCanonicalizeMatchValue_UnknownKindCollapsesWhitespace(t *testing.T) {
	got := CanonicalizeMatchValue("command", "  run   this\ttool  ")
	if got != "run this tool" {
		t.Fatalf("CanonicalizeMatchValue() = %q, want %q", got, "run this tool")
	}
}
