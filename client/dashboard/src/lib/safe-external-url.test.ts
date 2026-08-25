import { afterEach, describe, expect, it, vi } from "vitest";
import {
  openSafeExternalUrl,
  safeExternalHttpUrl,
  safeSameOriginPath,
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
  it("opens the normalized URL in an isolated tab without a referrer", () => {
    const opened = { opener: window } as unknown as Window;
    const open = vi.spyOn(window, "open").mockImplementation(() => opened);
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});

    expect(openSafeExternalUrl("HTTPS://EXAMPLE.COM:443/docs")).toBe(true);

    const target = open.mock.calls[0]?.[1];
    expect(open).toHaveBeenCalledWith(
      "",
      expect.stringMatching(/^gram-external-[0-9a-f-]+$/),
    );
    expect(opened.opener).toBeNull();
    const clicked = click.mock.contexts[0] as HTMLAnchorElement | undefined;
    expect(clicked?.href).toBe("https://example.com/docs");
    expect(clicked?.target).toBe(target);
    expect(clicked?.referrerPolicy).toBe("no-referrer");
  });

  it("reports a popup-blocked tab as not opened", () => {
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => {});

    expect(openSafeExternalUrl("https://example.com/docs")).toBe(false);
    expect(open).toHaveBeenCalled();
    expect(click).not.toHaveBeenCalled();
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

  it("converts same-origin URLs to router locations", () => {
    expect(
      safeSameOriginPath(
        new URL("/projects/default?tab=tools#details", window.location.origin)
          .href,
      ),
    ).toBe("/projects/default?tab=tools#details");
  });

  it.each([
    "https://evil.example/path",
    "javascript:alert(1)",
    "//evil.example/path",
  ])("rejects the unsafe redirect %s", (raw) => {
    expect(safeSameOriginUrl(raw)).toBeNull();
  });
});
