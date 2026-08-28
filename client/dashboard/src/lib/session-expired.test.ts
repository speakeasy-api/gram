import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

async function loadModule(pathname: string, search = "", sessionStatus = 401) {
  vi.resetModules();
  const assign = vi.fn();
  const fetch = vi.fn().mockResolvedValue({ status: sessionStatus });
  const localStorage = {
    clear: vi.fn(),
    getItem: vi.fn(),
    key: vi.fn(),
    length: 0,
    removeItem: vi.fn(),
    setItem: vi.fn(),
  };
  const sessionStorage = {
    ...localStorage,
    clear: vi.fn(),
  };
  vi.stubGlobal("fetch", fetch);
  vi.stubGlobal("window", {
    localStorage,
    location: { origin: "https://app.example", pathname, search, assign },
    sessionStorage,
  });
  const mod = await import("./session-expired");
  return { assign, fetch, localStorage, sessionStorage, ...mod };
}

describe("safeRedirectPath", () => {
  it("keeps same-origin paths", async () => {
    const { safeRedirectPath } = await loadModule("/");

    expect(safeRedirectPath("/acme/toolsets?tab=tools")).toBe(
      "/acme/toolsets?tab=tools",
    );
  });

  it("rejects values that resolve to another origin", async () => {
    const { safeRedirectPath } = await loadModule("/");

    expect(safeRedirectPath("//evil.example/path")).toBeUndefined();
    expect(safeRedirectPath("/\\evil.example/path")).toBeUndefined();
    expect(safeRedirectPath("https://evil.example")).toBeUndefined();
    expect(safeRedirectPath(null)).toBeUndefined();
  });

  // The URL parser strips these characters, so a literal prefix check would
  // pass the value through and the browser would resolve it off-origin.
  it("rejects paths that smuggle an origin past the leading slash", async () => {
    const { safeRedirectPath } = await loadModule("/");

    expect(safeRedirectPath("/\n/evil.example/path")).toBeUndefined();
    expect(safeRedirectPath("/\t/evil.example/path")).toBeUndefined();
    expect(safeRedirectPath("/\r/evil.example/path")).toBeUndefined();
  });
});

describe("redirectToLoginOnUnauthorized", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("redirects to login and preserves the current location", async () => {
    const {
      assign,
      localStorage,
      redirectToLoginOnUnauthorized,
      sessionStorage,
    } = await loadModule("/acme/projects/default/insights", "?range=7d");

    await redirectToLoginOnUnauthorized();

    expect(localStorage.clear).toHaveBeenCalledOnce();
    expect(sessionStorage.clear).toHaveBeenCalledOnce();
    expect(assign).toHaveBeenCalledWith(
      `/login?redirect=${encodeURIComponent("/acme/projects/default/insights?range=7d")}`,
    );
  });

  it("still redirects when the browser blocks storage access", async () => {
    const { assign, redirectToLoginOnUnauthorized } =
      await loadModule("/acme/toolsets");
    for (const name of ["localStorage", "sessionStorage"]) {
      Object.defineProperty(window, name, {
        configurable: true,
        get: () => {
          throw new DOMException("Storage disabled", "SecurityError");
        },
      });
    }

    await redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledWith(
      `/login?redirect=${encodeURIComponent("/acme/toolsets")}`,
    );
  });

  it("redirects once even when several queries fail together", async () => {
    const { assign, fetch, redirectToLoginOnUnauthorized } =
      await loadModule("/acme/toolsets");

    await Promise.all([
      redirectToLoginOnUnauthorized(),
      redirectToLoginOnUnauthorized(),
      redirectToLoginOnUnauthorized(),
    ]);

    expect(fetch).toHaveBeenCalledOnce();
    expect(assign).toHaveBeenCalledTimes(1);
  });

  it("drops a redirect that would leave the origin", async () => {
    const { assign, redirectToLoginOnUnauthorized } = await loadModule(
      "//evil.example/path",
    );

    await redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledWith("/login");
  });

  it("stays put on paths that render without a session", async () => {
    const { assign, fetch, redirectToLoginOnUnauthorized } =
      await loadModule("/login");

    await redirectToLoginOnUnauthorized();

    expect(fetch).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
  });

  it("stays put when the dashboard session is still valid", async () => {
    const {
      assign,
      fetch,
      localStorage,
      redirectToLoginOnUnauthorized,
      sessionStorage,
    } = await loadModule("/acme/setup", "?projectSlug=missing", 200);

    await redirectToLoginOnUnauthorized();

    expect(fetch).toHaveBeenCalledWith("https://app.example/rpc/auth.info", {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal: expect.any(AbortSignal),
    });
    expect(localStorage.clear).not.toHaveBeenCalled();
    expect(sessionStorage.clear).not.toHaveBeenCalled();
    expect(assign).not.toHaveBeenCalled();
  });

  it("stays put when session verification fails", async () => {
    const { assign, fetch, redirectToLoginOnUnauthorized } =
      await loadModule("/acme/toolsets");
    fetch.mockRejectedValueOnce(new TypeError("network down"));

    await redirectToLoginOnUnauthorized();

    expect(assign).not.toHaveBeenCalled();
  });

  it("releases a stuck session check after its timeout", async () => {
    vi.useFakeTimers();
    const { assign, fetch, redirectToLoginOnUnauthorized } =
      await loadModule("/acme/toolsets");
    fetch.mockImplementation(() => new Promise(() => undefined));

    const firstCheck = redirectToLoginOnUnauthorized();
    await vi.advanceTimersByTimeAsync(5_000);
    await firstCheck;
    const secondCheck = redirectToLoginOnUnauthorized();

    expect(fetch).toHaveBeenCalledTimes(2);
    expect(assign).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(5_000);
    await secondCheck;
  });
});
