import { afterEach, describe, expect, it } from "vitest";
import {
  alreadyMoved,
  moveKey,
  organizationHostRedirectTarget,
  recordMove,
} from "./organization-host";

const current = {
  protocol: "https:",
  host: "app.example.com",
  pathname: "/acme/mcp/servers",
  search: "?tab=logs&q=a%20b",
  hash: "#recent",
};

/** The transferStart URL on origin that hands over the session from sourceHost. */
function transfer(
  origin: string,
  redirect: string,
  sourceHost = current.host,
): string {
  const params = new URLSearchParams({ source_host: sourceHost, redirect });
  return `${origin}/rpc/auth.transferIn?${params.toString()}`;
}

const PAGE = "/acme/mcp/servers?tab=logs&q=a%20b";

describe("organizationHostRedirectTarget", () => {
  it("starts a session transfer on the organization's host", () => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", current),
    ).toBe(transfer("https://ai.example.com", PAGE));
  });

  it("encodes the path and query into the redirect parameter", () => {
    const target = organizationHostRedirectTarget("https://ai.example.com", {
      ...current,
      pathname: "/acme/a b/&c",
      search: "?x=1&redirect=%2Fevil",
      hash: "#frag?y=2",
    });
    const url = new URL(target!);
    expect(url.origin).toBe("https://ai.example.com");
    expect(url.pathname).toBe("/rpc/auth.transferIn");
    expect([...url.searchParams.keys()]).toEqual(["source_host", "redirect"]);
    expect(url.searchParams.get("source_host")).toBe("app.example.com");
    expect(url.searchParams.get("redirect")).toBe(
      "/acme/a b/&c?x=1&redirect=%2Fevil",
    );
    expect(url.hash).toBe("");
  });

  it("never puts the hash in the server-visible transfer URL", () => {
    const target = organizationHostRedirectTarget("https://ai.example.com", {
      ...current,
      hash: "#access_token=secret",
    });
    expect(target).not.toContain("secret");
    expect(new URL(target!).searchParams.get("redirect")).toBe(
      "/acme/mcp/servers?tab=logs&q=a%20b",
    );
  });

  it("ignores any path on the dashboard URL", () => {
    expect(
      organizationHostRedirectTarget(
        "https://ai.example.com/ignored/",
        current,
      ),
    ).toBe(transfer("https://ai.example.com", PAGE));
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
    ).toBe(transfer("https://ai.example.com", `${pathname}${search}`));
  });

  it("still moves a page below a hand-off path's name", () => {
    expect(
      organizationHostRedirectTarget("https://ai.example.com", {
        ...current,
        pathname: "/acme/shadow-mcp/request-log",
        search: "",
        hash: "",
      }),
    ).toBe(transfer("https://ai.example.com", "/acme/shadow-mcp/request-log"));
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
    ).toBe(transfer("http://localhost:5174", PAGE, "localhost:5173"));
  });

  it("treats a different port as a different host", () => {
    expect(
      organizationHostRedirectTarget("https://app.example.com:8443", current),
    ).toBe(transfer("https://app.example.com:8443", PAGE));
  });
});

describe("move guard", () => {
  afterEach(() => {
    sessionStorage.clear();
  });

  it("remembers each organization and host this tab moved to", () => {
    const key = moveKey("org-1", "https://ai.example.com/acme");
    expect(alreadyMoved(key)).toBe(false);

    expect(recordMove(key)).toBe(true);

    expect(alreadyMoved(moveKey("org-1", "https://ai.example.com/other"))).toBe(
      true,
    );
    expect(alreadyMoved(moveKey("org-1", "https://app.example.com/acme"))).toBe(
      false,
    );
    // Another organization on the same host still moves.
    expect(alreadyMoved(moveKey("org-2", "https://ai.example.com/acme"))).toBe(
      false,
    );
  });

  it("lets the same move happen again once the window has passed", () => {
    const key = moveKey("org-1", "https://ai.example.com/acme");
    expect(recordMove(key, 1_000)).toBe(true);

    expect(alreadyMoved(key, 1_000)).toBe(true);
    expect(alreadyMoved(key, 15_999)).toBe(true);
    expect(alreadyMoved(key, 16_000)).toBe(false);
  });

  it("keeps the guard on if the clock goes backwards", () => {
    const key = moveKey("org-1", "https://ai.example.com/acme");
    recordMove(key, 50_000);
    expect(alreadyMoved(key, 40_000)).toBe(true);
  });

  it("stops a slow loop after three moves in ten minutes", () => {
    const key = moveKey("org-1", "https://ai.example.com/acme");
    // Each hop outlasts the short window, so only the count can stop it.
    recordMove(key, 0);
    expect(alreadyMoved(key, 20_000)).toBe(false);
    recordMove(key, 20_000);
    expect(alreadyMoved(key, 40_000)).toBe(false);
    recordMove(key, 40_000);

    expect(alreadyMoved(key, 60_000)).toBe(true);
    expect(alreadyMoved(key, 599_999)).toBe(true);
    // The first move has aged out, so one more is allowed.
    expect(alreadyMoved(key, 600_000)).toBe(false);
  });

  it("counts moves per organization and host", () => {
    const key = moveKey("org-1", "https://ai.example.com/acme");
    recordMove(key, 0);
    recordMove(key, 20_000);
    recordMove(key, 40_000);

    expect(
      alreadyMoved(moveKey("org-2", "https://ai.example.com/acme"), 60_000),
    ).toBe(false);
  });

  it("drops moves older than ten minutes when recording a new one", () => {
    const old = moveKey("org-1", "https://ai.example.com/acme");
    const fresh = moveKey("org-2", "https://ai.example.com/acme");
    recordMove(old, 0);
    recordMove(fresh, 600_000);

    expect(
      JSON.parse(sessionStorage.getItem("organizationHostMoveTimes")!),
    ).toEqual({ [fresh]: [600_000] });
  });

  it("ignores the old permanent move list", () => {
    sessionStorage.setItem(
      "organizationHostMoves",
      JSON.stringify(["org-1 ai.example.com"]),
    );
    expect(alreadyMoved(moveKey("org-1", "https://ai.example.com/acme"))).toBe(
      false,
    );
  });

  it.each([
    ["corrupt JSON", "{not json"],
    ["a list", JSON.stringify(["org-1 ai.example.com"])],
    ["a non-numeric time", JSON.stringify({ "org-1 ai.example.com": ["now"] })],
    [
      "a time that is not a list",
      JSON.stringify({ "org-1 ai.example.com": Date.now() }),
    ],
  ])("ignores a record holding %s", (_name, stored) => {
    sessionStorage.setItem("organizationHostMoveTimes", stored);
    expect(alreadyMoved(moveKey("org-1", "https://ai.example.com/acme"))).toBe(
      false,
    );
  });
});
