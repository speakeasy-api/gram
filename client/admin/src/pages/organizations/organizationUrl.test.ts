import { describe, expect, it } from "vitest";
import { organizationUrlPreview } from "./organizationUrl";

describe("organization URL preview", () => {
  it.each([
    ["example.com", "example.com"],
    ["  HTTPS://EXAMPLE.COM./about?q=1#team  ", "example.com"],
    ["http://example.com", "example.com"],
    ["www.example.com", "www.example.com"],
    ["https://a.b.example.co.uk/path", "a.b.example.co.uk"],
    ["tenant.github.io", "tenant.github.io"],
    ["xn--bcher-kva.de", "xn--bcher-kva.de"],
    ["example.com/path?q=https://other.com#fragment", "example.com"],
    ["example.com" + " ".repeat(3989), "example.com"],
    [
      "a".repeat(63) + "." + "b".repeat(32) + ".com",
      "a".repeat(63) + "." + "b".repeat(32) + ".com",
    ],
  ])("preserves the exact host of %s", (input, hostname) => {
    expect(organizationUrlPreview(input)).toEqual({ hostname, error: "" });
  });

  it.each([
    "Example Company",
    "com",
    "localhost",
    "app.localhost",
    "http://localhost",
    "ftp://example.com",
    "mailto:person@example.com",
    "https:example.com",
    "//example.com",
    "https:///example.com",
    "https://user:pass@example.com",
    "https://@example.com",
    "example.com:443",
    "https://example.com:443",
    "http://example.com:80",
    "https://example.com:",
    "127.0.0.1",
    "127.1",
    "2130706433",
    "0x7f000001",
    "0177.0.0.1",
    "example.123",
    "http://[::1]",
    "[2001:db8::1]",
    "*.example.com",
    "-bad.example.com",
    "bad-.example.com",
    "bad_label.example.com",
    ".example.com",
    "example..com",
    "example.com..",
    "https://example%2ecom",
    "https://example.com\\@other.com",
    "https://exa\tmple.com",
    "https://example.com/\npath",
    "https://b\u00fccher.de",
    "https://\u212a.com",
    "https://example\u3002com",
    "ab--cd.com",
    "xn---a-xka.com", // Decodes to -üa (leading hyphen).
    "xn--ab---3ra.com", // Decodes to ab--ü (reserved hyphens).
    "xn--a--xka.com", // Decodes to aü- (trailing hyphen).
    "xn--ab-m1t.com", // Decodes to a + ZWJ + b (invalid joiner context).
    "xn--a-xbb.com", // Decodes to a + combining acute (not NFC).
    "xn--a.com",
    "xn--.com",
    "xn--invalidpunycode-.com",
    "a".repeat(64) + ".com",
    "a".repeat(63) + "." + "b".repeat(33) + ".com",
    "example.com" + " ".repeat(3990),
  ])("rejects normalization traps in %s", (input) => {
    const result = organizationUrlPreview(input);
    expect(result.hostname).toBe("");
    expect(result.error).not.toBe("");
  });

  it.each(Array.from({ length: 33 }, (_, i) => (i === 32 ? 127 : i)))(
    "rejects control character U+%i anywhere in the submitted URL",
    (code) => {
      const control = String.fromCharCode(code);
      for (const input of [
        `${control}example.com`,
        `example.com${control}`,
        `https://exa${control}mple.com`,
        `https://example.com/a${control}b`,
        `https://example.com/?q=a${control}b`,
        `https://example.com/#a${control}b`,
      ]) {
        expect(organizationUrlPreview(input).hostname).toBe("");
        expect(organizationUrlPreview(input).error).not.toBe("");
      }
    },
  );

  it("leaves public-suffix policy to the server", () => {
    expect(organizationUrlPreview("github.io").hostname).toBe("github.io");
  });

  it("keeps an empty input neutral", () => {
    expect(organizationUrlPreview("   ")).toEqual({ hostname: "", error: "" });
  });
});
