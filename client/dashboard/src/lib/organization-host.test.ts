import { afterEach, describe, expect, it } from "vitest";
import {
  alreadyMovedTo,
  organizationHostRedirectTarget,
  recordMoveTo,
} from "./organization-host";

const current = {
  protocol: "https:",
  host: "app.example.com",
  pathname: "/acme/mcp/servers",
  search: "?tab=logs&q=a%20b",
  hash: "#recent",
};

describe("organizationHostRedirectTarget", () => {
  it("keeps the path, query and hash on the organization's host", () => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", current),
    ).toBe("https://ai.example.com/acme/mcp/servers?tab=logs&q=a%20b#recent");
  });

  it("ignores any path on the dashboard URL", () => {
    expect(
      organizationHostRedirectTarget(
        "https://ai.example.com/ignored/",
        current,
      ),
    ).toBe("https://ai.example.com/acme/mcp/servers?tab=logs&q=a%20b#recent");
  });

  it.each([
    ["no URL", undefined],
    ["an empty URL", ""],
    ["the current host", "https://app.example.com"],
    ["a relative URL", "/elsewhere"],
    ["a protocol-relative URL", "//ai.example.com"],
    ["a script URL", "javascript:alert(1)"],
    ["an unparseable URL", "https://"],
  ])("stays put for %s", (_name, dashboardUrl) => {
    expect(organizationHostRedirectTarget(dashboardUrl, current)).toBe(
      undefined,
    );
  });

  it.each([
    "/shadow-mcp/request",
    "/risk-policy-bypass/request",
    "/risk-policy-challenge/acknowledge",
    "/risk-policy-challenge/acknowledge/",
  ])("stays on the hand-off page %s", (pathname) => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", {
        ...current,
        pathname,
        search: "",
        hash: "#token=secret",
      }),
    ).toBe(undefined);
  });

  it("stays during the CLI login hand-off", () => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", {
        ...current,
        pathname: "/",
        search: "?from_cli=true&cli_callback_url=http%3A%2F%2Flocalhost%3A1234",
        hash: "",
      }),
    ).toBe(undefined);
  });

  it.each([
    ["without a callback URL", "/", "?from_cli=true"],
    ["on a deep link", "/acme/mcp", "?from_cli=true&cli_callback_url=x"],
  ])("still moves a from_cli page %s", (_name, pathname, search) => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", {
        ...current,
        pathname,
        search,
        hash: "",
      }),
    ).toBe(`https://ai.example.com${pathname}${search}`);
  });

  it("still moves a page below a hand-off path's name", () => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", {
        ...current,
        pathname: "/acme/shadow-mcp/request-log",
        search: "",
        hash: "",
      }),
    ).toBe("https://ai.example.com/acme/shadow-mcp/request-log");
  });

  it("refuses an http target from an https page", () => {
    expect(
      organizationHostRedirectTarget("http://ai.example.com", current),
    ).toBe(undefined);
  });

  it("allows an http target from an http page", () => {
    expect(
      organizationHostRedirectTarget("http://localhost:5174", {
        ...current,
        protocol: "http:",
        host: "localhost:5173",
      }),
    ).toBe("http://localhost:5174/acme/mcp/servers?tab=logs&q=a%20b#recent");
  });

  it("treats a different port as a different host", () => {
    expect(
      organizationHostRedirectTarget("https://app.example.com:8443", current),
    ).toBe(
      "https://app.example.com:8443/acme/mcp/servers?tab=logs&q=a%20b#recent",
    );
  });
});

describe("move guard", () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it("remembers each host this tab moved to", () => {
    const target = "https://ai.example.com/acme";
    expect(alreadyMovedTo(target)).toBe(false);

    expect(recordMoveTo(target)).toBe(true);

    expect(alreadyMovedTo("https://ai.example.com/other")).toBe(true);
    expect(alreadyMovedTo("https://app.example.com/acme")).toBe(false);
  });

  it("ignores a corrupt record", () => {
    sessionStorage.setItem("organizationHostMoves", "{not json");
    expect(alreadyMovedTo("https://ai.example.com/acme")).toBe(false);
  });
});
