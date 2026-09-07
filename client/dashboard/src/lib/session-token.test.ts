import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SessionTokenStore, sessionFetch } from "./session-token";

const BASE = "https://app.example.test";
vi.mock("@/lib/utils", () => ({
  getApiBaseURL: () => "https://app.example.test",
  getServerURL: () => "https://app.example.test",
}));
const refreshed = (token = "access-1") =>
  new Response(null, {
    status: 204,
    headers: { "Gram-Session": token },
  });

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("SessionTokenStore", () => {
  it("deduplicates startup refresh before dispatching concurrent reads and writes", async () => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const read = store.fetch(`${BASE}/rpc/auth.info`);
    const write = store.fetch(`${BASE}/rpc/tools.create`, {
      method: "POST",
      body: "payload",
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]).toEqual([
      `${BASE}/rpc/auth.refresh`,
      {
        method: "POST",
        credentials: "include",
        signal: expect.any(AbortSignal),
      },
    ]);
    pending.resolve(refreshed());
    await Promise.all([read, write]);
    expect(fetcher).toHaveBeenCalledTimes(3);
    const request = fetcher.mock.calls[2]![0] as Request;
    expect(request.headers.get("Gram-Session")).toBe("access-1");
    expect(request.credentials).toBe("include");
    expect(await request.text()).toBe("payload");
  });

  it("isolates a canceled auth.info from the next queued request", async () => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockReturnValueOnce(pending.promise)
      .mockImplementation(async (input) => {
        const request = input as Request;
        request.signal.throwIfAborted();
        return refreshed();
      });
    const store = new SessionTokenStore(() => BASE, fetcher);
    const controller = new AbortController();
    const first = store
      .fetch(`${BASE}/rpc/auth.info`, { signal: controller.signal })
      .catch((error: unknown) => error);
    controller.abort();
    const second = store.fetch(`${BASE}/rpc/auth.info`);
    const secondResult = expect(second).resolves.toHaveProperty("status", 204);
    expect(await first).toBeInstanceOf(DOMException);
    expect(fetcher).toHaveBeenCalledOnce();
    pending.resolve(refreshed());
    await secondResult;
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(store.getSnapshot()).toBe("access-1");
  });

  it.each([
    "/rpc/tools.create",
    "/chat/turnstream",
    "/rpc/auth.info",
    "/rpc/auth.switchScopes",
    "/rpc/auth.enterDemo",
    "/rpc/auth.logout",
  ])("aborts %s promptly without canceling shared refresh", async (path) => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const refresh = store.refresh();
    const controller = new AbortController();
    const canceled = store.fetch(`${BASE}${path}`, {
      method: "POST",
      signal: controller.signal,
    });
    controller.abort();
    await expect(canceled).rejects.toBe(controller.signal.reason);
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0]![1]!.signal!.aborted).toBe(false);
    const survivor = store.fetch(`${BASE}/rpc/tools.create`, {
      method: "POST",
    });
    pending.resolve(refreshed());
    await Promise.all([refresh, survivor]);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(
      (fetcher.mock.calls[1]![0] as Request).headers.get("Gram-Session"),
    ).toBe("access-1");
  });

  it.each([
    "/rpc/auth.info",
    "/rpc/auth.switchScopes",
    "/rpc/auth.enterDemo",
    "/rpc/auth.logout",
  ])("isolates dispatched %s from caller cancellation", async (path) => {
    const pending = deferred<Response>();
    const nextPending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockImplementationOnce((input) => {
        const request = input as Request;
        return new Promise<Response>((resolve, reject) => {
          request.signal.addEventListener("abort", () => {
            reject(request.signal.reason);
          });
          void pending.promise.then(resolve);
        });
      })
      .mockReturnValueOnce(nextPending.promise)
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    const controller = new AbortController();
    const canceled = store.fetch(`${BASE}${path}`, {
      method: "POST",
      signal: controller.signal,
    });
    await vi.advanceTimersByTimeAsync(0);
    expect(fetcher).toHaveBeenCalledTimes(2);
    const dispatched = fetcher.mock.calls[1]![0] as Request;
    const next = store.fetch(`${BASE}/rpc/auth.info`);
    const survivor = store.fetch(`${BASE}/rpc/tools.create`, {
      method: "POST",
    });
    controller.abort();
    await expect(canceled).rejects.toBe(controller.signal.reason);
    await vi.advanceTimersByTimeAsync(0);
    expect(dispatched.signal.aborted).toBe(false);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(store.getSnapshot()).toBe("access-1");

    pending.resolve(refreshed("mutation-token"));
    await vi.advanceTimersByTimeAsync(0);
    const loggedOut = path === "/rpc/auth.logout";
    expect(store.getSnapshot()).toBe(loggedOut ? "" : "mutation-token");
    expect(fetcher).toHaveBeenCalledTimes(3);
    const nextRequest = fetcher.mock.calls[2]![0] as Request;
    expect(nextRequest.url).toBe(`${BASE}/rpc/auth.info`);
    expect(nextRequest.headers.get("Gram-Session")).toBe(
      loggedOut ? null : "mutation-token",
    );

    nextPending.resolve(refreshed("canonical-token"));
    await Promise.all([next, survivor]);
    expect(store.getSnapshot()).toBe(loggedOut ? "" : "canonical-token");
    expect(fetcher).toHaveBeenCalledTimes(4);
    expect(
      (fetcher.mock.calls[3]![0] as Request).headers.get("Gram-Session"),
    ).toBe(loggedOut ? null : "canonical-token");
  });

  it("still cancels an ordinary response stream after dispatch", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockImplementationOnce(async (input) => {
        const request = input as Request;
        return new Response(
          new ReadableStream({
            start(stream) {
              request.signal.addEventListener("abort", () => {
                stream.error(request.signal.reason);
              });
            },
          }),
        );
      });
    const store = new SessionTokenStore(() => BASE, fetcher);
    const controller = new AbortController();
    const response = await store.fetch(`${BASE}/chat/turnstream`, {
      method: "POST",
      signal: controller.signal,
    });
    const read = response.body!.getReader().read();
    controller.abort();
    await expect(read).rejects.toBe(controller.signal.reason);
    expect((fetcher.mock.calls[1]![0] as Request).signal.aborted).toBe(true);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each([
    "/rpc/auth.info",
    "/rpc/auth.switchScopes",
    "/rpc/auth.enterDemo",
    "/rpc/auth.logout",
  ])(
    "aborts queued %s without releasing the mutation ordering barrier",
    async (path) => {
      const pending = deferred<Response>();
      const fetcher = vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(refreshed())
        .mockReturnValueOnce(pending.promise)
        .mockResolvedValue(new Response(null, { status: 204 }));
      const store = new SessionTokenStore(() => BASE, fetcher);
      await store.refresh();
      const scope = store.fetch(`${BASE}/rpc/auth.switchScopes`, {
        method: "POST",
      });
      await vi.advanceTimersByTimeAsync(0);
      const controller = new AbortController();
      const queued = store.fetch(`${BASE}${path}`, {
        signal: controller.signal,
      });
      controller.abort();
      await expect(queued).rejects.toBe(controller.signal.reason);
      const writeController = new AbortController();
      const canceledWrite = store.fetch(`${BASE}/rpc/tools.create`, {
        method: "POST",
        signal: writeController.signal,
      });
      writeController.abort();
      await expect(canceledWrite).rejects.toBe(writeController.signal.reason);
      const survivor = store.fetch(`${BASE}/rpc/tools.create`, {
        method: "POST",
      });
      await vi.advanceTimersByTimeAsync(0);
      expect(fetcher).toHaveBeenCalledTimes(2);
      pending.resolve(refreshed("scoped-token"));
      await Promise.all([scope, survivor]);
      expect(fetcher).toHaveBeenCalledTimes(3);
      expect(
        (fetcher.mock.calls[2]![0] as Request).headers.get("Gram-Session"),
      ).toBe("scoped-token");
    },
  );

  it("does not start refresh for an already aborted request", async () => {
    const fetcher = vi.fn<typeof fetch>();
    const store = new SessionTokenStore(() => BASE, fetcher);
    const controller = new AbortController();
    controller.abort();
    await expect(
      store.fetch(`${BASE}/rpc/tools.create`, {
        method: "POST",
        signal: controller.signal,
      }),
    ).rejects.toBe(controller.signal.reason);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("still rejects callers of the same failed refresh without dispatching their writes", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockRejectedValue(new TypeError("offline"));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await expect(
      store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" }),
    ).rejects.toThrow("offline");
    expect(fetcher).toHaveBeenCalledOnce();
  });

  it("refreshes before the fixed ten-minute expiry and updates subscribers", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockResolvedValue(refreshed("access-2"));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const listener = vi.fn<() => void>();
    const unsubscribe = store.subscribe(listener);
    await store.refresh();
    await vi.advanceTimersByTimeAsync(8 * 60_000);
    await store.refresh();
    expect(fetcher).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(60_000);
    await store.refresh();
    expect(store.getSnapshot()).toBe("access-2");
    expect(listener).toHaveBeenCalledTimes(2);
    unsubscribe();
  });

  it("refreshes on reactivation and removes its listeners on cleanup", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(refreshed());
    const store = new SessionTokenStore(() => BASE, fetcher);
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
    const stop = store.start();
    await store.refresh();
    window.dispatchEvent(new Event("focus"));
    document.dispatchEvent(new Event("visibilitychange"));
    await store.refresh();
    expect(fetcher).toHaveBeenCalledTimes(2);
    stop();
    window.dispatchEvent(new Event("focus"));
    await vi.advanceTimersByTimeAsync(10 * 60_000);
    expect(fetcher).toHaveBeenCalledTimes(2);
    vi.restoreAllMocks();
  });

  it("does not renew parked tabs but refreshes on visibility and real background requests", async () => {
    const visibility = vi
      .spyOn(document, "visibilityState", "get")
      .mockReturnValue("hidden");
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(refreshed());
    const store = new SessionTokenStore(() => BASE, fetcher);
    const stop = store.start();
    try {
      // Startup still refreshes, even when the tab initially opens hidden.
      await store.refresh();
      expect(fetcher).toHaveBeenCalledOnce();
      vi.advanceTimersByTime(73 * 60 * 60_000);
      expect(fetcher).toHaveBeenCalledOnce();

      visibility.mockReturnValue("visible");
      document.dispatchEvent(new Event("visibilitychange"));
      await store.refresh();
      expect(fetcher).toHaveBeenCalledTimes(2);

      visibility.mockReturnValue("hidden");
      vi.advanceTimersByTime(10 * 60_000);
      expect(fetcher).toHaveBeenCalledTimes(2);
      // A real request remains legitimate activity while hidden: refresh it
      // before dispatch, but never replay it.
      await store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" });
      expect(fetcher).toHaveBeenCalledTimes(4);
      expect((fetcher.mock.calls[3]![0] as Request).method).toBe("POST");
    } finally {
      stop();
      visibility.mockRestore();
    }
  });

  it("never refreshes or replays an unrelated 401 or a failed write", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockResolvedValue(new Response(null, { status: 401 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    const response = await store.fetch(`${BASE}/rpc/tools.create`, {
      method: "POST",
    });
    expect(response.status).toBe(401);
    expect(store.getSnapshot()).toBe("access-1");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("does not discard a session on transient refresh failures", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockRejectedValue(new TypeError("offline"));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    await expect(store.refresh(true)).rejects.toThrow("offline");
    expect(store.getSnapshot()).toBe("access-1");
    await store.refresh();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("never dispatches expired access during refresh failure cooldowns", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(refreshed());
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    vi.advanceTimersByTime(9 * 60_000 + 50_000);
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    await expect(store.refresh()).rejects.toThrow("offline");
    fetcher.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" });
    expect(fetcher).toHaveBeenCalledTimes(3);
    vi.advanceTimersByTime(10_000);
    await expect(
      store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" }),
    ).rejects.toThrow("Session access token expired");
    expect(fetcher).toHaveBeenCalledTimes(3);
    expect(store.getSnapshot()).toBe("access-1");
    vi.advanceTimersByTime(20_000);
    fetcher.mockRejectedValueOnce(new TypeError("offline again"));
    await expect(store.refresh()).rejects.toThrow("offline again");
    await expect(
      store.fetch(`${BASE}/chat/turnstream`, { method: "POST" }),
    ).rejects.toThrow("Session access token expired");
    expect(fetcher).toHaveBeenCalledTimes(4);
    vi.advanceTimersByTime(30_000);
    fetcher
      .mockResolvedValueOnce(refreshed("access-2"))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    await store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" });
    expect(fetcher).toHaveBeenCalledTimes(6);
    expect(
      (fetcher.mock.calls[5]![0] as Request).headers.get("Gram-Session"),
    ).toBe("access-2");
  });

  it("does not extend hard expiry when auth.info returns a token", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(refreshed());
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    vi.advanceTimersByTime(9 * 60_000 + 50_000);
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    await expect(store.refresh()).rejects.toThrow("offline");
    fetcher.mockResolvedValueOnce(refreshed("info-token"));
    await store.fetch(`${BASE}/rpc/auth.info`);
    expect(store.getSnapshot()).toBe("info-token");
    vi.advanceTimersByTime(10_000);
    await expect(
      store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" }),
    ).rejects.toThrow("Session access token expired");
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("starts a scope token's lifetime at dispatch, not response arrival", async () => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockReturnValueOnce(pending.promise);
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    vi.advanceTimersByTime(8 * 60_000);
    const scope = store.fetch(`${BASE}/rpc/auth.switchScopes`, {
      method: "POST",
    });
    await vi.advanceTimersByTimeAsync(0);
    vi.advanceTimersByTime(2 * 60_000);
    pending.resolve(refreshed("scoped-token"));
    await scope;
    fetcher.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" });
    expect(fetcher).toHaveBeenCalledTimes(3);
    vi.advanceTimersByTime(7 * 60_000 + 50_000);
    fetcher.mockRejectedValueOnce(new TypeError("offline"));
    await expect(store.refresh()).rejects.toThrow("offline");
    vi.advanceTimersByTime(10_000);
    await expect(
      store.fetch(`${BASE}/rpc/tools.create`, { method: "POST" }),
    ).rejects.toThrow("Session access token expired");
    expect(fetcher).toHaveBeenCalledTimes(4);
  });

  it("clears the shared token when refresh itself confirms expiration", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockResolvedValue(new Response(null, { status: 401 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    await store.refresh(true);
    expect(store.getSnapshot()).toBe("");
  });

  it("does not restore a rejected session from an old auth.info response", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockResolvedValueOnce(new Response(null, { status: 401 }))
      .mockResolvedValueOnce(
        new Response(null, {
          status: 200,
          headers: { "Gram-Session": "access-1" },
        }),
      );
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    await store.refresh(true);
    await store.fetch(`${BASE}/rpc/auth.info`);
    expect(store.getSnapshot()).toBe("");
  });

  it("serializes scope switching against refresh and preserves project headers", async () => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(refreshed("scoped-token"))
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const refresh = store.refresh();
    const scope = store.fetch(`${BASE}/rpc/auth.switchScopes`, {
      method: "POST",
    });
    const write = store.fetch(`${BASE}/rpc/tools.create`, {
      method: "POST",
      headers: { "Gram-Project": "selected-project", "Gram-Session": "stale" },
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    pending.resolve(refreshed());
    await Promise.all([refresh, scope, write]);
    const request = fetcher.mock.calls[2]![0] as Request;
    expect(request.headers.get("Gram-Session")).toBe("scoped-token");
    expect(request.headers.get("Gram-Project")).toBe("selected-project");
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("waits for auth.info before changing scope", async () => {
    const pending = deferred<Response>();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValueOnce(refreshed("scoped-token"));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    const info = store.fetch(`${BASE}/rpc/auth.info`);
    await vi.advanceTimersByTimeAsync(0);
    const scope = store.fetch(`${BASE}/rpc/auth.switchScopes`, {
      method: "POST",
    });
    expect(fetcher).toHaveBeenCalledTimes(2);
    pending.resolve(refreshed("access-1"));
    await Promise.all([info, scope]);
    expect(store.getSnapshot()).toBe("scoped-token");
  });

  it("posts logout to its RPC endpoint without requiring a refresh", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 200 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const response = await store.fetch(`${BASE}/rpc/auth.logout`, {
      method: "POST",
      headers: { "Gram-Session": "expired-access" },
    });
    const request = fetcher.mock.calls[0]![0] as Request;
    expect(request.url).toBe(`${BASE}/rpc/auth.logout`);
    expect(request.method).toBe("POST");
    expect(request.credentials).toBe("include");
    expect(request.headers.get("Gram-Session")).toBe("expired-access");
    expect(response.status).toBe(200);
    expect(store.getSnapshot()).toBe("");
    await store.refresh(true);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("uses the canonical header for logout even after hard expiry", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed("current"))
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    vi.advanceTimersByTime(11 * 60_000);
    await store.fetch(`${BASE}/rpc/auth.logout`, {
      method: "POST",
      headers: { "Gram-Session": "captured" },
    });
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(
      (fetcher.mock.calls[1]![0] as Request).headers.get("Gram-Session"),
    ).toBe("current");
    // Once explicitly cleared, neither the canonical nor captured token is sent.
    await store.fetch(`${BASE}/rpc/auth.logout`, {
      method: "POST",
      headers: { "Gram-Session": "captured" },
    });
    expect(
      (fetcher.mock.calls[2]![0] as Request).headers.has("Gram-Session"),
    ).toBe(false);
  });

  it.each([
    ["/rpc/auth.logout", 200],
    ["/rpc/auth.logout", 204],
    ["/rpc/tools.create", 204],
    ["/rpc/auth.logout", 401],
    ["/rpc/auth.logout", 500],
  ])("returns responses unchanged: %s %i", async (path, status) => {
    const original = new Response(status >= 400 ? "logout failed" : null, {
      status,
      headers: { "X-Logout-Test": "preserved" },
    });
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed())
      .mockResolvedValue(original);
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    const response = await store.fetch(`${BASE}${path}`, { method: "POST" });
    expect(response).toBe(original);
    expect(response.status).toBe(status);
    expect(response.headers.get("X-Logout-Test")).toBe("preserved");
    if (status >= 400) {
      expect(await response.text()).toBe("logout failed");
      expect(store.getSnapshot()).toBe("access-1");
    }
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("dispatches the canonical newly rotated header instead of a still-live captured token", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(refreshed("access-1"))
      .mockResolvedValueOnce(refreshed("access-2"))
      .mockResolvedValue(new Response(null, { status: 204 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    await store.refresh();
    await store.refresh(true);
    await store.fetch(`${BASE}/chat/turnstream`, {
      headers: { "Gram-Session": "access-1" },
    });
    expect(
      (fetcher.mock.calls[2]![0] as Request).headers.get("Gram-Session"),
    ).toBe("access-2");
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("allows logout even when a pending refresh fails", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockRejectedValueOnce(new TypeError("offline"))
      .mockResolvedValue(new Response(null, { status: 200 }));
    const store = new SessionTokenStore(() => BASE, fetcher);
    const refresh = store.refresh().catch(() => {});
    await store.fetch(`${BASE}/rpc/auth.logout`, { method: "POST" });
    await refresh;
    expect(store.getSnapshot()).toBe("");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("does not attach a session or refresh for third-party fetches", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetcher);
    await sessionFetch("https://third-party.example/resource");
    expect(fetcher).toHaveBeenCalledExactlyOnceWith(
      "https://third-party.example/resource",
      undefined,
    );
  });
});
