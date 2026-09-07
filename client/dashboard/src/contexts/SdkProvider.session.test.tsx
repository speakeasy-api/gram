import { act, cleanup, renderHook } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { Gram } from "@gram/client";
import { PREFERRED_THEME_STORAGE_KEY } from "@/lib/local-storage-keys";
import { useSdkClient, queryClient } from "./Sdk";
import { SdkProvider } from "./SdkProvider";

const mocks = vi.hoisted(() => ({
  stopSession: vi.fn(),
  clearUser: vi.fn(),
  reset: vi.fn(),
}));
vi.mock("@datadog/browser-rum", () => ({ datadogRum: mocks }));
vi.mock("./Telemetry", () => ({
  useTelemetry: () => ({ reset: mocks.reset }),
}));

afterEach(() => {
  cleanup();
  queryClient.clear();
  localStorage.clear();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

it("resolves actual SDK logout through its normal RPC endpoint and runs production cleanup hooks", async () => {
  const fetcher = vi.fn<typeof fetch>().mockImplementation(async (input) => {
    const request = input instanceof Request ? input : new Request(input);
    if (new URL(request.url).pathname === "/rpc/auth.refresh") {
      return new Response(null, {
        status: 204,
        headers: { "Gram-Session": "fresh-access" },
      });
    }
    expect(new URL(request.url).pathname).toBe("/rpc/auth.logout");
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("include");
    expect(request.headers.get("Gram-Session")).toBe("fresh-access");
    // Simulate Clear-Site-Data before the SDK response hook. The beforeRequest
    // hook must already have captured preferences to restore after logout.
    localStorage.clear();
    sessionStorage.clear();
    return new Response(null, {
      status: 200,
      headers: { "Clear-Site-Data": '"storage"', "X-Logout-Test": "preserved" },
    });
  });
  vi.stubGlobal("fetch", fetcher);
  localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
  localStorage.setItem("stale-user-data", "remove-me");
  sessionStorage.setItem("stale-session-data", "remove-me");
  document.cookie = "gram_admin_override=old; path=/";

  const { result } = renderHook(useSdkClient, {
    wrapper: ({ children }) => (
      <MemoryRouter>
        <SdkProvider>{children}</SdkProvider>
      </MemoryRouter>
    ),
  });
  expect(result.current).toBeInstanceOf(Gram);
  await act(async () => {
    await result.current.auth.logout();
  });
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(
    fetcher.mock.calls.map(
      ([input]) =>
        new URL(input instanceof Request ? input.url : String(input)).pathname,
    ),
  ).toEqual(["/rpc/auth.refresh", "/rpc/auth.logout"]);
  expect(mocks.stopSession).toHaveBeenCalledOnce();
  expect(mocks.clearUser).toHaveBeenCalledOnce();
  expect(mocks.reset).toHaveBeenCalledOnce();
  expect(localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe("dark");
  expect(localStorage.getItem("stale-user-data")).toBeNull();
  expect(sessionStorage.getItem("stale-session-data")).toBeNull();
  expect(document.cookie).not.toContain("gram_admin_override");
});
