// @vitest-environment happy-dom

import { afterEach, describe, expect, it, vi } from "vitest";

import { LOGOUT_WAIT_MS, logoutToLogin } from "./logout-to-login";
import { restoreLocation, stubLocationReplace } from "./stub-location-replace";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  restoreLocation();
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
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    const logout = vi.fn().mockRejectedValue(new Error("network"));

    await logoutToLogin({ auth: { logout } });

    expect(logout).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith("/login");
    expect(logged).toHaveBeenCalledOnce();
  });

  it("still replaces the page with /login when logout never settles", async () => {
    vi.useFakeTimers();
    const replace = stubLocationReplace();
    const logged = vi.spyOn(console, "error").mockImplementation(() => {});
    const logout = vi.fn().mockReturnValue(new Promise(() => {}));

    const done = logoutToLogin({ auth: { logout } });
    await vi.advanceTimersByTimeAsync(LOGOUT_WAIT_MS);
    await done;

    expect(logout).toHaveBeenCalledOnce();
    expect(replace).toHaveBeenCalledWith("/login");
    expect(logged).toHaveBeenCalledOnce();
  });
});
