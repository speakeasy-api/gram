import { getApiBaseURL, getServerURL } from "@/lib/utils";

// Access tokens live only in memory. The browser alone handles gram_refresh,
// two HttpOnly cookies scoped to /rpc/auth.refresh and /rpc/auth.logout.
// Access tokens last ten minutes.
const REFRESH_AFTER_MS = 9 * 60_000;
const FAILURE_COOLDOWN_MS = 30_000;

export class SessionTokenStore {
  private token: string | null = null;
  private refreshAt = 0;
  private revision = 0;
  private loggedOut = false;
  private inFlight: Promise<string | null> | null = null;
  private mutation: Promise<Response> | null = null;
  private listeners = new Set<() => void>();

  private readonly baseURL: () => string;
  private readonly fetcher: typeof fetch;

  constructor(
    baseURL: () => string,
    fetcher: typeof fetch = (...args) => fetch(...args),
  ) {
    this.baseURL = baseURL;
    this.fetcher = fetcher;
  }

  getSnapshot = (): string | null => this.token;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  private publish(token: string, refreshAt: number) {
    this.token = token;
    this.refreshAt = refreshAt;
    this.revision++;
    this.listeners.forEach((listener) => listener());
  }

  refresh = async (force = false): Promise<string | null> => {
    // Prior mutations are ordering barriers, not this caller's result. A
    // canceled auth.info (e.g. StrictMode cleanup) must not cancel the next
    // independent request. Failures of this caller's active refresh still
    // propagate below; no application request is retried.
    if (this.mutation) await this.mutation.catch(() => {});
    if (this.loggedOut) return this.token;
    if (this.inFlight) return this.inFlight;
    if (!force && Date.now() < this.refreshAt) return this.token;

    this.inFlight = (async () => {
      const startedAt = Date.now();
      try {
        const response = await this.fetcher(
          `${this.baseURL()}/rpc/auth.refresh`,
          {
            method: "POST",
            credentials: "include",
            signal: AbortSignal.timeout(10_000),
          },
        );
        if (response.status === 401) {
          this.publish("", Infinity);
          return this.token;
        }
        if (!response.ok) throw new Error("Session refresh failed");
        const token = response.headers.get("Gram-Session");
        if (!token)
          throw new Error("Session refresh did not return an access token");
        this.publish(token, startedAt + REFRESH_AFTER_MS);
        return token;
      } catch (error) {
        // A network error is not a logout. Never replay the caller's request.
        this.refreshAt = Date.now() + FAILURE_COOLDOWN_MS;
        throw error;
      } finally {
        this.inFlight = null;
      }
    })();
    return this.inFlight;
  };

  // SDK requests and long-lived chat closures read the same token at dispatch,
  // not a token captured when their component or transport was constructed.
  fetch = async (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const request = new Request(input, init);
    const path = new URL(request.url).pathname;
    const logout = path === "/rpc/auth.logout";
    const switchScope =
      path === "/rpc/auth.switchScopes" || path === "/rpc/auth.enterDemo";
    const sessionInfo = path === "/rpc/auth.info";

    const send = async () => {
      const revision = this.revision;
      const headers = new Headers(request.headers);
      if (this.token !== null) {
        headers.delete("Gram-Session");
        if (this.token) headers.set("Gram-Session", this.token);
      }
      // Logout authenticates from cookies, including an expired access cookie.
      if (logout) headers.delete("Gram-Session");
      const response = await this.fetcher(
        new Request(request, {
          headers,
          credentials: "include",
        }),
      );
      if (response.ok && logout) {
        this.loggedOut = true;
        this.publish("", Infinity);
      } else if (response.ok && (switchScope || sessionInfo)) {
        const token = response.headers.get("Gram-Session");
        // An old auth.info response must not undo a concurrent scope change
        // or a refresh, nor restore an identity after refresh rejection.
        // auth.info does not extend the token's lifetime.
        if (
          token &&
          this.token !== "" &&
          revision === this.revision &&
          token !== this.token
        ) {
          this.publish(
            token,
            switchScope ? Date.now() + REFRESH_AFTER_MS : this.refreshAt,
          );
        }
      }
      return response;
    };

    if (logout || switchScope || sessionInfo) {
      // Serialize scope changes and logout with refresh. Keep auth.info in
      // this sequence too so its returned header cannot overwrite a newer
      // scope token. Reserve the slot synchronously, before yielding.
      const previous = this.mutation;
      const refresh = logout
        ? this.inFlight?.catch(() => null)
        : this.refresh();
      const mutation = (async () => {
        await previous?.catch(() => {});
        await refresh;
        return send();
      })();
      this.mutation = mutation;
      try {
        return await mutation;
      } finally {
        if (this.mutation === mutation) this.mutation = null;
      }
    }

    await this.refresh();
    return send();
  };

  start = (): (() => void) => {
    const refresh = () => {
      void this.refresh().catch(() => {});
    };
    const reactivate = () => {
      if (document.visibilityState === "visible") {
        void this.refresh(true).catch(() => {});
      }
    };
    refresh();
    // A parked tab is not activity. Only proactive timer refreshes are
    // visibility-gated; startup and real request dispatch still refresh.
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") refresh();
    }, 30_000);
    window.addEventListener("focus", reactivate);
    window.addEventListener("online", reactivate);
    document.addEventListener("visibilitychange", reactivate);
    return () => {
      clearInterval(timer);
      window.removeEventListener("focus", reactivate);
      window.removeEventListener("online", reactivate);
      document.removeEventListener("visibilitychange", reactivate);
    };
  };
}

export const sessionTokens = new SessionTokenStore(() => getApiBaseURL());

/** Only attach dashboard credentials to known first-party API origins. */
export const sessionFetch: typeof fetch = (input, init) => {
  const url = new URL(
    input instanceof Request ? input.url : input.toString(),
    window.location.origin,
  );
  const origins = [getApiBaseURL(), getServerURL()].map(
    (base) => new URL(base, window.location.origin).origin,
  );
  if (!origins.includes(url.origin)) return fetch(input, init);
  return sessionTokens.fetch(input instanceof Request ? input : url, init);
};
