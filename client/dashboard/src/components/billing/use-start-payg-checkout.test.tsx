import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Gram } from "@gram/client";
import { HTTPClient, type Fetcher } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { resetPaygCheckoutLocks } from "./payg-checkout-lock";
import {
  PAYG_CHECKOUT_ERROR_MESSAGE,
  useStartPaygCheckout,
} from "./use-start-payg-checkout";

const mocks = vi.hoisted(() => ({
  theme: "light" as "light" | "dark",
  capture: vi.fn(),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: mocks.theme }),
}));

const ORGANIZATION_ID = "org-checkout-test";
const OTHER_ORGANIZATION_ID = "org-other-test";
const SESSION = {
  clientSecret: "cs_test_secret",
  publishableKey: "pk_test_publishable",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function checkoutResponse(session = SESSION) {
  return Response.json({
    client_secret: session.clientSecret,
    publishable_key: session.publishableKey,
  });
}
const queryClients: QueryClient[] = [];
function setup() {
  const fetcher = vi.fn<Fetcher>();
  const client = new Gram({
    serverURL: "https://checkout.example",
    httpClient: new HTTPClient({ fetcher }),
    retryConfig: { strategy: "none" },
  });
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  queryClients.push(queryClient);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <GramProvider client={client}>{children}</GramProvider>
    </QueryClientProvider>
  );
  return { fetcher, wrapper };
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.theme = "light";
  resetPaygCheckoutLocks();
});
afterEach(() => {
  cleanup();
  for (const client of queryClients.splice(0)) client.clear();
  resetPaygCheckoutLocks();
});

describe("useStartPaygCheckout", () => {
  it.each(["light", "dark"] as const)(
    "creates checkout only on demand with the latest %s theme, then dismisses it",
    async (theme) => {
      const { fetcher, wrapper } = setup();
      fetcher.mockResolvedValueOnce(checkoutResponse());
      mocks.theme = theme === "light" ? "dark" : "light";
      const { result, rerender } = renderHook(
        () => useStartPaygCheckout(ORGANIZATION_ID),
        { wrapper },
      );
      expect(fetcher).not.toHaveBeenCalled();
      expect(result.current.session).toBeNull();
      mocks.theme = theme;
      rerender();
      act(() => result.current.startCheckout());
      await waitFor(() =>
        expect(result.current.session).toMatchObject(SESSION),
      );
      const request = new Request(fetcher.mock.calls[0]![0]);
      expect(request.method).toBe("POST");
      expect(new URL(request.url).pathname).toBe(
        "/rpc/usage.createStripeCheckout",
      );
      expect(new URL(request.url).searchParams.get("theme")).toBe(theme);
      expect(result.current.error).toBeNull();
      act(() => result.current.dismissCheckout());
      expect(result.current.session).toBeNull();
    },
  );

  it("starts one request for rapid clicks across mounted instances and unlocks both on completion", async () => {
    const { fetcher, wrapper } = setup();
    const pending = deferred<Response>();
    fetcher.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(
      () =>
        [
          useStartPaygCheckout(ORGANIZATION_ID),
          useStartPaygCheckout(ORGANIZATION_ID),
        ] as const,
      { wrapper },
    );
    act(() => {
      result.current[0].startCheckout();
      result.current[0].startCheckout();
      result.current[1].startCheckout();
    });
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    expect(result.current.map((checkout) => checkout.isPending)).toEqual([
      true,
      true,
    ]);
    await act(async () => pending.resolve(checkoutResponse()));
    await waitFor(() =>
      expect(result.current.map((checkout) => checkout.isPending)).toEqual([
        false,
        false,
      ]),
    );
    expect(result.current[0].session).toMatchObject(SESSION);
    expect(result.current[1].session).toBeNull();
  });

  it("keeps the organization locked across unmount/remount until the abandoned request settles", async () => {
    const { fetcher, wrapper } = setup();
    const pending = deferred<Response>();
    fetcher
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(checkoutResponse());
    const first = renderHook(() => useStartPaygCheckout(ORGANIZATION_ID), {
      wrapper,
    });
    act(() => first.result.current.startCheckout());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    first.unmount();
    const remounted = renderHook(() => useStartPaygCheckout(ORGANIZATION_ID), {
      wrapper,
    });
    expect(remounted.result.current.isPending).toBe(true);
    act(() => remounted.result.current.startCheckout());
    expect(fetcher).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve(checkoutResponse()));
    await waitFor(() => expect(remounted.result.current.isPending).toBe(false));
    expect(remounted.result.current.session).toBeNull();
    act(() => remounted.result.current.startCheckout());
    await waitFor(() =>
      expect(remounted.result.current.session).toMatchObject(SESSION),
    );
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("discards a stale organization's response and unlocks the original organization", async () => {
    const { fetcher, wrapper } = setup();
    const pending = deferred<Response>();
    fetcher
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(checkoutResponse());
    const { result, rerender } = renderHook(
      ({ organizationId }) => useStartPaygCheckout(organizationId),
      { wrapper, initialProps: { organizationId: ORGANIZATION_ID } },
    );
    act(() => result.current.startCheckout());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    rerender({ organizationId: OTHER_ORGANIZATION_ID });
    await act(async () => pending.resolve(checkoutResponse()));
    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.session).toBeNull();
    rerender({ organizationId: ORGANIZATION_ID });
    expect(result.current.isPending).toBe(false);
    expect(result.current.session).toBeNull();
    act(() => result.current.startCheckout());
    await waitFor(() => expect(result.current.session).toMatchObject(SESSION));
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each([
    { clientSecret: "", publishableKey: SESSION.publishableKey },
    { clientSecret: SESSION.clientSecret, publishableKey: "" },
    { clientSecret: "not-a-secret", publishableKey: SESSION.publishableKey },
    { clientSecret: SESSION.clientSecret, publishableKey: "not-a-key" },
  ])("rejects an unusable checkout session: %j", async (session) => {
    const { fetcher, wrapper } = setup();
    fetcher.mockResolvedValueOnce(checkoutResponse(session));
    const { result } = renderHook(() => useStartPaygCheckout(ORGANIZATION_ID), {
      wrapper,
    });
    act(() => result.current.startCheckout());
    await waitFor(() =>
      expect(result.current.error).toBe(PAYG_CHECKOUT_ERROR_MESSAGE),
    );
    expect(result.current.session).toBeNull();
    expect(result.current.isPending).toBe(false);
    expect(mocks.capture).toHaveBeenCalledWith("payg_checkout_error", {
      error: "unusable checkout session",
    });
  });

  it("reports an API failure and clears the error while a successful retry is pending", async () => {
    const { fetcher, wrapper } = setup();
    const failed = deferred<Response>();
    const retry = deferred<Response>();
    fetcher
      .mockReturnValueOnce(failed.promise)
      .mockReturnValueOnce(retry.promise);
    const { result } = renderHook(() => useStartPaygCheckout(ORGANIZATION_ID), {
      wrapper,
    });
    act(() => result.current.startCheckout());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
    await act(async () =>
      failed.resolve(new Response("stripe unavailable", { status: 503 })),
    );
    await waitFor(() =>
      expect(result.current.error).toBe(PAYG_CHECKOUT_ERROR_MESSAGE),
    );
    expect(result.current.session).toBeNull();
    expect(result.current.isPending).toBe(false);
    expect(mocks.capture).toHaveBeenCalledWith("payg_checkout_error", {
      error: expect.stringContaining("stripe unavailable"),
    });
    act(() => result.current.startCheckout());
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(result.current.error).toBeNull();
    expect(result.current.isPending).toBe(true);
    await act(async () => retry.resolve(checkoutResponse()));
    await waitFor(() => expect(result.current.session).toMatchObject(SESSION));
    expect(result.current.isPending).toBe(false);
  });
});
