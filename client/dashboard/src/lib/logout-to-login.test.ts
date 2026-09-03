// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from "vitest";

import { logoutToLogin } from "./logout-to-login";

let originalLocation: Location | undefined;

function stubLocationReplace() {
  originalLocation = window.location;
  const replace = vi.fn();
  // @ts-expect-error happy-dom-compatible location replacement for redirect assertion
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      // oxlint-disable-next-line typescript/no-misused-spread -- happy-dom Location is plain enough for tests
      ...originalLocation,
      replace,
    },
  });
  return replace;
}

afterEach(() => {
  vi.restoreAllMocks();
  if (originalLocation) {
    Object.defineProperty(window, "location", {
      configurable: true,
      value: originalLocation,
    });
    originalLocation = undefined;
  }
});

describe("logoutToLogin", () => {
  it("replaces the page with /login after logout succeeds", async () => {
    const replace = stubLocationReplace();
    const logout = vi.fn().mockResolvedValue(undefined);

    await logoutToLogin({ auth: { logout } });

    expect(logout).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith("/login");
  });

  it("still replaces the page with /login when logout rejects", async () => {
    const replace = stubLocationReplace();
    const logout = vi.fn().mockRejectedValue(new Error("network"));

    await logoutToLogin({ auth: { logout } });

    expect(logout).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith("/login");
  });
});
