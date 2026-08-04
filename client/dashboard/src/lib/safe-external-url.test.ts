import { afterEach, describe, expect, it, vi } from "vitest";
import {
  openSafeExternalUrl,
  safeExternalHttpUrl,
  safeSameOriginUrl,
} from "./safe-external-url";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("safeExternalHttpUrl", () => {
  it("accepts and normalizes absolute HTTP(S) URLs", () => {
    expect(safeExternalHttpUrl("https://example.com/docs/../start")).toBe(
      "https://example.com/start",
    );
    expect(safeExternalHttpUrl("HTTP://EXAMPLE.COM:80/path")).toBe(
      "http://example.com/path",
    );
  });

  it.each([
    "javascript:alert(1)",
    "JaVaScRiPt:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "vbscript:msgbox(1)",
    "blob:https://example.com/id",
    "file:///etc/passwd",
    "custom://example.com/path",
  ])("rejects the non-HTTP scheme in %s", (raw) => {
    expect(safeExternalHttpUrl(raw)).toBeNull();
  });

  it.each([
    "//evil.com",
    "/relative/path",
    "relative/path",
    "",
    "not a url",
    "https://",
  ])("rejects the non-absolute or malformed value %j", (raw) => {
    expect(safeExternalHttpUrl(raw)).toBeNull();
  });

  it("rejects absent values", () => {
    expect(safeExternalHttpUrl(null)).toBeNull();
    expect(safeExternalHttpUrl(undefined)).toBeNull();
  });
});

describe("openSafeExternalUrl", () => {
  it("opens the normalized URL in an isolated tab", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);

    expect(openSafeExternalUrl("HTTPS://EXAMPLE.COM:443/docs")).toBe(true);
    expect(open).toHaveBeenCalledWith(
      "https://example.com/docs",
      "_blank",
      "noopener,noreferrer",
    );
  });

  it("does not open a rejected URL", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);

    expect(openSafeExternalUrl("javascript:alert(1)")).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });
});

describe("safeSameOriginUrl", () => {
  it("accepts relative paths", () => {
    expect(safeSameOriginUrl("/mcp/tools?tab=overview#details")).toBe(
      new URL("/mcp/tools?tab=overview#details", window.location.origin).href,
    );
  });

  it("accepts same-origin absolute URLs", () => {
    const absolute = new URL("/plugins?installed=true", window.location.origin);
    expect(safeSameOriginUrl(absolute.href)).toBe(absolute.href);
  });

  it.each([
    "https://evil.example/path",
    "javascript:alert(1)",
    "//evil.example/path",
  ])("rejects the unsafe redirect %s", (raw) => {
    expect(safeSameOriginUrl(raw)).toBeNull();
  });
});
