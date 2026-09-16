import { QueryClient } from "@tanstack/react-query";
import {
  queryKeyAdminListOrganizations,
  queryKeyAdminListOrganizationsInfinite,
} from "@gram/admin-client/react-query/adminListOrganizations.core";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GramCore } from "@gram/admin-client/core";
import { HTTPClient } from "@gram/admin-client/lib/http";
import { adminListOrganizations } from "@gram/admin-client/funcs/adminListOrganizations";

import {
  GramAdminError,
  bulkUpdateAccountType,
  cancelStripeSubscription,
  errorMessage,
  getStripeCustomer,
  getInferenceKeys,
  getInferenceSpendHistory,
  getPaygBillingSummary,
  getStripeSubscription,
  getProject,
  listOrganizations,
  logout,
  markEnterpriseTrialConverted,
  organizationDashboardUrl,
  MAX_TRIAL_EXTENSION_DAYS,
  MAX_TRIAL_REARM_DAYS,
  MAX_TRIAL_START_DAYS,
  MIN_TRIAL_EXTENSION_DAYS,
  MIN_TRIAL_REARM_DAYS,
  MIN_TRIAL_START_DAYS,
  resumeStripeSubscription,
  setInferenceKeyMonthlyLimit,
  setStripeCustomer,
  toSearchParams,
  omitUnset,
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
      flag: false,
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
describe("generated organization filter serialization", () => {
  it("sends native bigint bounds without precision loss alongside status", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response('{"organizations":[]}', {
        headers: { "Content-Type": "application/json" },
      }),
    );
    const client = new GramCore({
      serverURL: "https://admin.example.test",
      httpClient: new HTTPClient({ fetcher }),
    });
    await adminListOrganizations(client, {
      minMembers: 9007199254740993n,
      maxMembers: 9223372036854775807n,
      disabledStatus: "all",
      createdFrom: "2024-02-29",
      createdTo: "2024-03-01",
    });
    const request = fetcher.mock.calls[0]?.[0] as Request;
    const url = new URL(request.url);
    expect(Object.fromEntries(url.searchParams)).toEqual({
      min_members: "9007199254740993",
      max_members: "9223372036854775807",
      disabled_status: "all",
      created_from: "2024-02-29",
      created_to: "2024-03-01",
    });
  });
});

describe("listOrganizations", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("preserves explicit all in both request parameters and cache keys", () => {
    expect(omitUnset({ disabled_status: "all" })).toEqual({
      disabled_status: "all",
    });
    expect(toSearchParams({ disabled_status: "all" }).toString()).toBe(
      "disabled_status=all",
    );
    expect(toSearchParams({ disabled_status: undefined }).toString()).toBe("");
  });

  it.each(["all", "active", "disabled"] as const)(
    "sends disabled_status=%s",
    async (disabled_status) => {
      const fetch = vi
        .fn()
        .mockResolvedValue(new Response('{"organizations":[]}'));
      vi.stubGlobal("fetch", fetch);
      await listOrganizations({
        disabled_status,
      });
      expect(fetch.mock.calls[0]?.[0]).toBe(
        `/admin/organizations.list?disabled_status=${disabled_status}`,
      );
    },
  );

  it.each([
    0,
    Number.MAX_SAFE_INTEGER,
    "9007199254740992",
    "9223372036854775807",
  ])("sends member bound %s without loss of precision", async (value) => {
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response('{"organizations":[]}'));
    vi.stubGlobal("fetch", fetch);
    await listOrganizations({ min_members: value, max_members: value });
    expect(fetch.mock.calls[0]?.[0]).toBe(
      `/admin/organizations.list?min_members=${value}&max_members=${value}`,
    );
  });

  it.each([
    -1,
    1.5,
    NaN,
    Infinity,
    Number.MAX_SAFE_INTEGER + 1,
    "-1",
    "1.5",
    "1e3",
    "",
    " 1",
    "9223372036854775808",
  ])(
    "rejects invalid or imprecise member bound %s before fetching",
    (value) => {
      const fetch = vi.fn();
      vi.stubGlobal("fetch", fetch);
      for (const key of ["min_members", "max_members"] as const) {
        expect(() => listOrganizations({ [key]: value })).toThrow(RangeError);
      }
      expect(fetch).not.toHaveBeenCalled();
    },
  );

  it.each(["2024-02-29", "0000-01-01", "9999-12-31"])(
    "sends inclusive UTC date %s unchanged",
    async (value) => {
      const fetch = vi
        .fn()
        .mockResolvedValue(new Response('{"organizations":[]}'));
      vi.stubGlobal("fetch", fetch);
      await listOrganizations({ created_from: value, created_to: value });
      expect(fetch.mock.calls[0]?.[0]).toBe(
        `/admin/organizations.list?created_from=${value}&created_to=${value}`,
      );
    },
  );

  it.each([
    "",
    "2023-02-29",
    "2024-02-30",
    "2024-04-31",
    "2024-13-01",
    "2024-00-01",
    "2024-01-00",
    "2024-1-01",
    "2024-01-1",
    "2024-01-01T00:00:00Z",
    " 2024-01-01",
  ])("rejects non-calendar date %s before fetching", (value) => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    for (const key of ["created_from", "created_to"] as const) {
      expect(() => listOrganizations({ [key]: value })).toThrow(RangeError);
    }
    expect(fetch).not.toHaveBeenCalled();
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
      disabled_status: "all",
    });

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/organizations.list?q=acme" +
        "&account_types=free&account_types=pro" +
        "&trial_states=running&trial_states=ending_soon" +
        "&disabled_status=all",
    );
  });

  it("sends Created direction and page to the admin API", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ organizations: [], total: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    const result = await listOrganizations({
      sort: "created_at",
      direction: "asc",
      page: 2,
      limit: 50,
    });

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/organizations.list?sort=created_at&direction=asc&page=2&limit=50",
    );
    expect(result.total).toBe(0);
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
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }

  it("preserves classified empty and legacy unclassified cause semantics", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify([
            {
              key_type: "chat",
              credits_used: 0,
              monthly_credits: 100,
              disabled: false,
              disable_causes_classified: true,
            },
            {
              key_type: "internal",
              credits_used: 0,
              monthly_credits: 50,
              disabled: true,
              disable_causes_classified: false,
            },
          ]),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    await expect(getInferenceKeys("org one")).resolves.toMatchObject([
      { disable_causes: [], disable_causes_classified: true },
      { disable_causes: null, disable_causes_classified: false },
    ]);
  });

  it("reads billing state from explicit admin organization endpoints", async () => {
    const fetch = stubFetch();

    await getInferenceKeys("org one");
    await getInferenceSpendHistory("org one");
    await getPaygBillingSummary("org one");
    await getStripeSubscription("org one");

    expect(fetch.mock.calls[0]?.[0]).toBe(
      "/admin/organization.inferenceKeys?organization_id=org+one",
    );
    expect(fetch.mock.calls[1]?.[0]).toBe(
      "/admin/organization.inferenceSpendHistory?organization_id=org+one",
    );
    expect(fetch.mock.calls[2]?.[0]).toBe(
      "/admin/organization.paygBillingSummary?organization_id=org+one",
    );
    expect(fetch.mock.calls[3]?.[0]).toBe(
      "/admin/organization.stripeSubscription?organization_id=org+one",
    );
  });

  it("fetches a live Stripe customer preview for the exact organization and ID", async () => {
    const fetch = stubFetch();

    await getStripeCustomer("org one", "cus_placeholder_1");

    expect(fetch.mock.calls.at(-1)?.[0]).toBe(
      "/admin/organization.stripeCustomer?organization_id=org+one&stripe_customer_id=cus_placeholder_1",
    );
    expect(fetch.mock.calls.at(-1)?.[1]).toMatchObject({ cache: "no-store" });
  });

  it("posts the canonical organization and materialized key when setting a monthly limit", async () => {
    const fetch = stubFetch();

    await setInferenceKeyMonthlyLimit({
      organizationID: "org_1",
      keyType: "internal",
      monthlyCredits: 750,
    });

    expect(fetch).toHaveBeenCalledWith(
      "/admin/organization.setInferenceKeyMonthlyLimit",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          organization_id: "org_1",
          key_type: "internal",
          monthly_credits: 750,
        }),
      }),
    );
  });

  it("posts the initial Stripe customer ID to the guarded organization endpoint", async () => {
    const fetch = stubFetch();

    await setStripeCustomer({
      organization_id: "org_1",
      stripe_customer_id: "cus_placeholder_1",
    });

    expect(fetch).toHaveBeenCalledWith(
      "/admin/organization.setStripeCustomer",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          organization_id: "org_1",
          stripe_customer_id: "cus_placeholder_1",
        }),
      }),
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

// The writes that still leave through this hand-written client: enterprise
// conversion and the bulk account-type update. A test naming each path is what
// keeps a review from reading two identical-looking calls as the same one. The
// trial day-count bounds are checked here too, because the browser mirrors them
// by hand rather than reading them from the design.
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

  it("posts only the id to the dedicated enterprise conversion path and returns the privacy-minimal result", async () => {
    const result = {
      organization_id: ORG.id,
      converted_at: "2026-03-08T12:34:56Z",
    };
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(result), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    await expect(markEnterpriseTrialConverted({ id: ORG.id })).resolves.toEqual(
      result,
    );
    expect(requestOf(fetch)).toEqual({
      path: "/admin/trial.convert",
      method: "POST",
      contentType: "application/json",
      body: { id: ORG.id },
    });
  });

  it("reports a conversion 401 in place without starting login", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ message: "admin session expired" }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const before = window.location.href;

    await expect(
      markEnterpriseTrialConverted({ id: ORG.id }),
    ).rejects.toMatchObject({
      status: 401,
      message: expect.stringContaining("gram admin 401"),
      body: { message: "admin session expired" },
    });
    expect(window.location.href).toBe(before);
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

  it("mirrors the server's start bounds exactly", () => {
    expect(MIN_TRIAL_START_DAYS).toBe(1);
    expect(MAX_TRIAL_START_DAYS).toBe(365);
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

describe("generated organization list query keys", () => {
  it.each([
    ["normal", queryKeyAdminListOrganizations],
    ["infinite", queryKeyAdminListOrganizationsInfinite],
  ] as const)(
    "%s keys hash, retrieve and invalidate losslessly",
    async (_, factory) => {
      const client = new QueryClient();
      const bounds = [0n, 9007199254740993n, 9223372036854775807n];
      try {
        const keys = bounds.map((bound) =>
          factory({ minMembers: bound, maxMembers: bound }),
        );
        keys.forEach((key, i) => client.setQueryData(key, { value: i }));
        expect(client.getQueryCache().getAll()).toHaveLength(bounds.length);
        for (const [i, bound] of bounds.entries()) {
          const key = factory({ minMembers: bound, maxMembers: bound });
          expect(client.getQueryData(key)).toEqual({ value: i });
          expect(key.at(-1)).toEqual({
            minMembers: bound.toString(),
            maxMembers: bound.toString(),
          });
          expect(JSON.stringify(key)).toContain(`"${bound}"`);
          await client.invalidateQueries({ queryKey: key, exact: true });
          expect(client.getQueryState(key)?.isInvalidated).toBe(true);
        }
        const normal = queryKeyAdminListOrganizations({ minMembers: 0n });
        const infinite = queryKeyAdminListOrganizationsInfinite({
          minMembers: 0n,
        });
        expect(JSON.stringify(normal)).not.toBe(JSON.stringify(infinite));
      } finally {
        client.clear();
      }
    },
  );
});
