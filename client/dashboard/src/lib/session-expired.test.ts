import { beforeEach, describe, expect, it, vi } from "vitest";

async function loadModule(pathname: string, search = "") {
  vi.resetModules();
  const assign = vi.fn();
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
  vi.stubGlobal("window", {
    localStorage,
    location: { origin: "https://app.example", pathname, search, assign },
    sessionStorage,
  });
  const mod = await import("./session-expired");
  return { assign, localStorage, sessionStorage, ...mod };
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

  it("redirects to login and preserves the current location", async () => {
    const {
      assign,
      localStorage,
      redirectToLoginOnUnauthorized,
      sessionStorage,
    } = await loadModule("/acme/projects/default/insights", "?range=7d");

    redirectToLoginOnUnauthorized();

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

    redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledWith(
      `/login?redirect=${encodeURIComponent("/acme/toolsets")}`,
    );
  });

  it("redirects once even when several queries fail together", async () => {
    const { assign, redirectToLoginOnUnauthorized } =
      await loadModule("/acme/toolsets");

    redirectToLoginOnUnauthorized();
    redirectToLoginOnUnauthorized();
    redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledTimes(1);
  });

  it("drops a redirect that would leave the origin", async () => {
    const { assign, redirectToLoginOnUnauthorized } = await loadModule(
      "//evil.example/path",
    );

    redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledWith("/login");
  });

  it("stays put on paths that render without a session", async () => {
    const { assign, redirectToLoginOnUnauthorized } =
      await loadModule("/login");

    redirectToLoginOnUnauthorized();

    expect(assign).not.toHaveBeenCalled();
  });
});
