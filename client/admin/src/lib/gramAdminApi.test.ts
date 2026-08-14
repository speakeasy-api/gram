import { afterEach, describe, expect, it, vi } from "vitest";
import {
  GramAdminError,
  disableOrganization,
  enableOrganization,
  errorMessage,
  extendTrial,
  logout,
  toSearchParams,
  type AdminOrganization,
} from "@/lib/gramAdminApi";

describe("toSearchParams", () => {
  it("repeats the key for each item of an array", () => {
    const qs = toSearchParams({ type: ["free", "pro"], q: "", page: 2 });
    expect(qs.toString()).toBe("type=free&type=pro&page=2");
  });

  it("omits the values the API reads as unset", () => {
    const qs = toSearchParams({
      q: undefined,
      cursor: "",
      type: [],
      include_disabled: false,
    });
    expect(qs.toString()).toBe("");
  });

  it("encodes a value that needs escaping", () => {
    const qs = toSearchParams({ q: "a b&c" });
    expect(qs.toString()).toBe("q=a+b%26c");
  });
});

describe("errorMessage", () => {
  it("prefers the message the server sent when the operator can act on it", () => {
    const e = new GramAdminError(
      400,
      { name: "bad_request", message: "at least one field must be supplied" },
      "gram admin 400 Bad Request",
    );
    expect(errorMessage(e)).toBe("at least one field must be supplied");
  });

  // A 5xx message is the handler's verb phrase, "list organizations", not a
  // sentence. The status line says more.
  it("keeps the status line for a server fault", () => {
    const e = new GramAdminError(
      500,
      { name: "internal", message: "list organizations" },
      "gram admin 500 Internal Server Error",
    );
    expect(errorMessage(e)).toBe("gram admin 500 Internal Server Error");
  });

  it("falls back to the status line when the body carries no message", () => {
    const e = new GramAdminError(404, null, "gram admin 404 Not Found");
    expect(errorMessage(e)).toBe("gram admin 404 Not Found");
  });
});

// The admin API is stripped from both generated SDKs, so nothing checks these
// paths against the design. A test naming each one is the only thing between a
// disable that enables and a review that reads two identical-looking calls.
describe("the organization write endpoints", () => {
  const ORG = {
    id: "org_placeholder_one",
    name: "Placeholder One",
    slug: "placeholder-one",
    account_type: "enterprise",
    whitelisted: false,
    member_count: 1,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-07T00:00:00Z",
  } satisfies AdminOrganization;

  function stubFetch(): ReturnType<typeof vi.fn> {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(ORG), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }

  function requestOf(fetch: ReturnType<typeof vi.fn>): {
    path: unknown;
    method: unknown;
    body: unknown;
  } {
    const call = fetch.mock.calls.at(-1);
    const init = call?.[1] as RequestInit | undefined;
    // A body these calls did not serialize is left as it is, so the assertion
    // reads the shape it was handed rather than "[object Object]".
    const body = init?.body;
    return {
      path: call?.[0],
      method: init?.method,
      body: typeof body === "string" ? (JSON.parse(body) as unknown) : body,
    };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("posts the id to the disable path", async () => {
    const fetch = stubFetch();

    await expect(disableOrganization({ id: ORG.id })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/organization.disable",
      method: "POST",
      body: { id: ORG.id },
    });
  });

  it("posts the id to the enable path", async () => {
    const fetch = stubFetch();

    await expect(enableOrganization({ id: ORG.id })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/organization.enable",
      method: "POST",
      body: { id: ORG.id },
    });
  });

  it("posts the id and the day count to the trial path", async () => {
    const fetch = stubFetch();

    await expect(extendTrial({ id: ORG.id, days: 30 })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/trial.extend",
      method: "POST",
      body: { id: ORG.id, days: 30 },
    });
  });

  // The 409 the server answers when a trial has converted, been demoted or
  // already expired. The body carries the sentence the operator has to read,
  // and it only survives if the call goes through gramAdminFetch.
  it("carries the conflict the server sends back", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            name: "conflict",
            message: "organization has no running enterprise trial to extend",
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const failure = await extendTrial({ id: ORG.id, days: 30 }).catch(
      (e: unknown) => e,
    );

    expect(failure).toBeInstanceOf(GramAdminError);
    expect(errorMessage(failure)).toBe(
      "organization has no running enterprise trial to extend",
    );
  });
});

describe("logout", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads a 204 as success and asks for the account chooser", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );

    await logout();

    expect(window.location.href).toContain("prompt=select_account");
  });

  // The 401 handler retries with prompt=none, which the provider honours
  // silently. Taking it here would sign the operator back in behind the Logout
  // they just pressed.
  it("reports a 401 instead of signing the operator back in", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 401 })),
    );
    const before = window.location.href;

    await expect(logout()).rejects.toThrow(GramAdminError);

    expect(window.location.href).toBe(before);
  });
});
