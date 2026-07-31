import { beforeEach, describe, expect, it, vi } from "vitest";

async function loadModule(pathname: string, search = "") {
  vi.resetModules();
  const assign = vi.fn();
  vi.stubGlobal("window", { location: { pathname, search, assign } });
  const mod = await import("./session-expired");
  return { assign, ...mod };
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
});

describe("redirectToLoginOnUnauthorized", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
  });

  it("redirects to login and preserves the current location", async () => {
    const { assign, redirectToLoginOnUnauthorized } = await loadModule(
      "/acme/projects/default/insights",
      "?range=7d",
    );

    redirectToLoginOnUnauthorized();

    expect(assign).toHaveBeenCalledWith(
      `/login?redirect=${encodeURIComponent("/acme/projects/default/insights?range=7d")}`,
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
