import { afterEach, describe, expect, it, vi } from "vitest";
import {
  GramAdminError,
  bulkUpdateAccountType,
  cancelStripeSubscription,
  disableOrganization,
  enableOrganization,
  errorMessage,
  extendTrial,
  getInferenceKeys,
  getPaygBillingSummary,
  getStripeSubscription,
  getProject,
  listOrganizations,
  logout,
  organizationDashboardUrl,
  MAX_TRIAL_EXTENSION_DAYS,
  MAX_TRIAL_REARM_DAYS,
  MIN_TRIAL_EXTENSION_DAYS,
  MIN_TRIAL_REARM_DAYS,
  rearmTrial,
  resumeStripeSubscription,
  toSearchParams,
  type AdminOrganization,
} from "@/lib/gramAdminApi";

describe("toSearchParams", () => {
  describe("organizationDashboardUrl", () => {
    it("targets the same-origin handoff endpoint with an encoded organization id", () => {
      expect(organizationDashboardUrl("org/id & value")).toBe(
        "/admin/organization.open-dashboard?organization_id=org%2Fid+%26+value",
      );
    });
  });

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

// The whole URL, not the params object, because a set the server reads as a set
// has to arrive as one key per value. A comma-joined `account_types=free,pro`
// parses on the server as a single account type named "free,pro", which matches
// no organization: the browser would show an empty list and no error.
describe("listOrganizations", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("repeats a key per value of each filter", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ organizations: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    await listOrganizations({
      q: "acme",
      account_types: ["free", "pro"],
      trial_states: ["running", "ending_soon"],
      disabled_states: ["active", "disabled"],
    });

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/organizations.list?q=acme" +
        "&account_types=free&account_types=pro" +
        "&trial_states=running&trial_states=ending_soon" +
        "&disabled_states=active&disabled_states=disabled",
    );
  });

  it("asks for the unfiltered list with no query string at all", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ organizations: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    await listOrganizations({ account_types: [], trial_states: [] });

    expect(fetch.mock.calls.at(-1)?.[0]).toBe("/admin/organizations.list");
  });
});

// Every page that reads a project mocks this function, so the query string it
// builds is asserted here or nowhere. The organization is what makes a slug
// unambiguous, and a parameter that silently never leaves the browser looks
// exactly like one that works.
describe("getProject", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function stubFetch(): ReturnType<typeof vi.fn> {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({}), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }

  it("sends the organization alongside the project", async () => {
    const fetch = stubFetch();

    await getProject("default", "one");

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/project.get?id_or_slug=default&organization_id_or_slug=one",
    );
  });

  it("omits the organization where there is none to send", async () => {
    const fetch = stubFetch();

    await getProject("default");

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/project.get?id_or_slug=default",
    );
  });
});

describe("organization billing endpoints", () => {
  afterEach(() => vi.unstubAllGlobals());

  function stubFetch(): ReturnType<typeof vi.fn> {
    const fetch = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(JSON.stringify({}), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }

  it("reads billing state from explicit admin organization endpoints", async () => {
    const fetch = stubFetch();

    await getInferenceKeys("org one");
    await getPaygBillingSummary("org one");
    await getStripeSubscription("org one");

    expect(fetch.mock.calls[0]?.[0]).toBe(
      "/admin/organization.inferenceKeys?organization_id=org+one",
    );
    expect(fetch.mock.calls[1]?.[0]).toBe(
      "/admin/organization.paygBillingSummary?organization_id=org+one",
    );
    expect(fetch.mock.calls[2]?.[0]).toBe(
      "/admin/organization.stripeSubscription?organization_id=org+one",
    );
  });

  it("posts only the canonical organization id to lifecycle controls", async () => {
    const fetch = stubFetch();

    await cancelStripeSubscription("org_1");
    await resumeStripeSubscription("org_1");

    for (const [path, init] of fetch.mock.calls) {
      expect(path).toMatch(
        /^\/admin\/organization\.(cancel|resume)StripeSubscription$/,
      );
      expect(init).toMatchObject({
        method: "POST",
        body: JSON.stringify({ organization_id: "org_1" }),
      });
    }
  });

  it("reports a lifecycle 401 without redirecting to login", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 401 })),
    );
    const before = window.location.href;

    await expect(cancelStripeSubscription("org_1")).rejects.toThrow(
      GramAdminError,
    );

    expect(window.location.href).toBe(before);
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
    contentType: string | null;
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
      // Read through Headers, because gramAdminRequest normalises whatever it
      // was handed and adds an Accept of its own. Asserted at all because a
      // POST that drops it is answered with a 415, and a 415 is not observable
      // from the client suite: nothing else here would change.
      contentType: new Headers(init?.headers).get("Content-Type"),
      body: typeof body === "string" ? (JSON.parse(body) as unknown) : body,
    };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The two ends of the range the server takes, written out rather than
  // derived. Every other bounds test in this repository reads these constants
  // for its expectation, so moving one moves the whole suite with it and the
  // browser starts refusing day counts the server would have taken.
  //
  // They must equal MinTrialExtensionDays and MaxTrialExtensionDays in
  // server/internal/constants/trials.go. Nothing ties the two files together
  // and nothing can: the admin API is stripped from both generated SDKs.
  it("mirrors the server's day-count bounds exactly", () => {
    expect(MIN_TRIAL_EXTENSION_DAYS).toBe(1);
    expect(MAX_TRIAL_EXTENSION_DAYS).toBe(365);
  });

  it("posts the id to the disable path", async () => {
    const fetch = stubFetch();

    await expect(disableOrganization({ id: ORG.id })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/organization.disable",
      method: "POST",
      contentType: "application/json",
      body: { id: ORG.id },
    });
  });

  it("posts the id to the enable path", async () => {
    const fetch = stubFetch();

    await expect(enableOrganization({ id: ORG.id })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/organization.enable",
      method: "POST",
      contentType: "application/json",
      body: { id: ORG.id },
    });
  });

  it("posts the id and the day count to the trial path", async () => {
    const fetch = stubFetch();

    await expect(extendTrial({ id: ORG.id, days: 30 })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/trial.extend",
      method: "POST",
      contentType: "application/json",
      body: { id: ORG.id, days: 30 },
    });
  });

  // MinTrialRearmDays and MaxTrialRearmDays in
  // server/internal/constants/trials.go, which alias the extension bounds there
  // today. Written out rather than compared to the extension constants: the two
  // pairs are separate names so they can diverge, and an assertion that only
  // said they matched would go on passing on the day one of them moves.
  it("mirrors the server's re-arm bounds exactly", () => {
    expect(MIN_TRIAL_REARM_DAYS).toBe(1);
    expect(MAX_TRIAL_REARM_DAYS).toBe(365);
  });

  // A different path and a different action from extend: this one restores the
  // account type and the whitelist flag and revives the model provider keys,
  // and its days are the whole length of a fresh run rather than an addition.
  it("posts the id and the day count to the re-arm path", async () => {
    const fetch = stubFetch();

    await expect(rearmTrial({ id: ORG.id, days: 14 })).resolves.toEqual(ORG);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/trial.rearm",
      method: "POST",
      contentType: "application/json",
      body: { id: ORG.id, days: 14 },
    });
  });

  it("posts the ids and one account type to the bulk path", async () => {
    const answer = {
      updated_ids: [ORG.id],
      missing_ids: ["org_placeholder_two"],
    };
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(answer), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    // The only write here that answers with ids rather than a record, so the
    // two lists have to come back out of the client whole.
    await expect(
      bulkUpdateAccountType({
        ids: [ORG.id, "org_placeholder_two"],
        account_type: "enterprise",
      }),
    ).resolves.toEqual(answer);

    expect(requestOf(fetch)).toEqual({
      path: "/admin/organizations.bulkUpdateAccountType",
      method: "POST",
      contentType: "application/json",
      body: {
        ids: [ORG.id, "org_placeholder_two"],
        account_type: "enterprise",
      },
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

  // Taking the read-side 401 handler here would start a new login behind the
  // Logout the operator just pressed.
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
