/// <reference types="node" />

import fs from "node:fs";
import path from "node:path";
import { afterEach, describe, expect, expectTypeOf, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";

const useMutation = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useMutation };
});

import type { SetOrganizationFeatureRequestBody } from "@gram/admin-client/models/components/setorganizationfeaturerequestbody";
import { queryKeyAdminListOrganizationActivityInfinite } from "@gram/admin-client/react-query/adminListOrganizationActivity.core";

import {
  errorMessage,
  isRedirectingToLogin as predecessorLatch,
  type AdminOrganization,
} from "@/lib/gramAdminApi";
import * as boundary from "@/lib/gramAdminClient";

const unauthorizedBody = JSON.stringify({
  fault: false,
  id: "placeholder",
  message: "unauthorized",
  name: "unauthorized",
  temporary: false,
  timeout: false,
});

afterEach(() => {
  useMutation.mockClear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("generated admin boundary", () => {
  it("does not export generated clients or configurable request controls", () => {
    expect(Object.keys(boundary).sort()).toEqual(
      [
        "adminSessionQuery",
        "adminGetGlobalIssuerQuery",
        "adminListGlobalIssuersQuery",
        "adminListGlobalIssuerConvergenceCandidatesQuery",
        "adminGetGlobalIssuerDuplicatePreflightQuery",
        "adminGetGlobalIssuerMigratePreflightQuery",
        "adminCreateGlobalIssuer",
        "adminUpdateGlobalIssuer",
        "adminDeleteGlobalIssuer",
        "adminFetchGlobalIssuerMetadata",
        "adminRefreshGlobalIssuerMetadata",
        "adminMigrateToGlobalIssuer",
        "adminUploadPlatformImage",
        "adminIssuerImageQuery",
        "disableOrganization",
        "enableOrganization",
        "extendTrial",
        "rearmTrial",
        "startTrial",
        "organizationFromSdk",
        "isRedirectingToLogin",
        "organizationActivityQuery",
        "organizationFeaturesQuery",
        "redirectOnUnauthorized",
        "setAdminOrganizationFeature",
        "useSetAdminOrganizationFeatureMutation",
      ].sort(),
    );

    expectTypeOf(boundary.adminSessionQuery).parameters.toEqualTypeOf<[]>();
    expectTypeOf(boundary.setAdminOrganizationFeature).parameters.toEqualTypeOf<
      [request: SetOrganizationFeatureRequestBody]
    >();
  });

  it("does not allow forged mutation options to replace its key or function", () => {
    const forgedMutationFn = vi.fn();

    boundary.useSetAdminOrganizationFeatureMutation({
      mutationKey: ["forged"],
      mutationFn: forgedMutationFn,
    } as never);

    expect(useMutation).toHaveBeenCalledWith(
      expect.objectContaining({
        mutationKey: [
          "@gram/admin-client",
          "admin",
          "adminSetOrganizationFeature",
        ],
        mutationFn: boundary.setAdminOrganizationFeature,
      }),
    );
  });

  it("always calls the generated session operation at this origin with ambient cookie behavior", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ email: "operator@example.test" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    const query = boundary.adminSessionQuery();
    await query.queryFn?.({ signal: new AbortController().signal } as never);

    const request = fetch.mock.calls[0]![0] as Request;
    expect(new URL(request.url).origin).toBe(window.location.origin);
    expect(request.credentials).toBe("same-origin");
    expect(request.mode).toBe("cors");
    expect(request.headers.has("Authorization")).toBe(false);
    expect(request.headers.has("Cookie")).toBe(false);
  });

  it("uses the guarded generated infinite activity operation and exposes its page shape", async () => {
    const fetch = vi.fn().mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            logs: [
              {
                acting_surface: "platform_mcp",
                action: "organization:settings_updated",
                actor_id: "actor_1",
                actor_type: "user",
                created_at: "2026-03-12T15:30:00Z",
                id: "log_1",
                subject_id: "org_1",
                subject_type: "organization",
              },
            ],
            next_cursor: "opaque+/=",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );
    vi.stubGlobal("fetch", fetch);

    const query = boundary.organizationActivityQuery("org explicit");
    const firstPage = await query.queryFn?.({
      pageParam: undefined,
      signal: new AbortController().signal,
    } as never);

    expect(query.queryKey).toEqual(
      queryKeyAdminListOrganizationActivityInfinite({
        organizationId: "org explicit",
      }),
    );
    expect(firstPage?.result.logs[0]?.createdAt).toEqual(
      new Date("2026-03-12T15:30:00Z"),
    );
    expect(firstPage?.["~next"]).toEqual({ cursor: "opaque+/=" });
    let url = new URL((fetch.mock.calls[0]![0] as Request).url);
    expect(url.pathname).toBe("/admin/organization.activity");
    expect(url.searchParams.get("organization_id")).toBe("org explicit");
    expect(url.searchParams.has("cursor")).toBe(false);

    await query.queryFn?.({
      pageParam: firstPage?.["~next"],
      signal: new AbortController().signal,
    } as never);
    url = new URL((fetch.mock.calls[1]![0] as Request).url);
    expect(url.searchParams.get("cursor")).toBe("opaque+/=");
  });

  it("redirects a generated read before parsing a malformed 401 body", async () => {
    vi.resetModules();
    const freshBoundary = await import("@/lib/gramAdminClient");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("not json", {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const href = vi.spyOn(window.location, "href", "set");
    const query = freshBoundary.adminSessionQuery();

    await expect(
      query.queryFn?.({ signal: new AbortController().signal } as never),
    ).rejects.toBeInstanceOf(SyntaxError);

    expect(freshBoundary.isRedirectingToLogin()).toBe(true);
    expect(href).toHaveBeenCalledOnce();
  });

  it("redirects one time for redirecting operations and shares the latch", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(() =>
        Promise.resolve(
          new Response(unauthorizedBody, {
            status: 401,
            headers: { "Content-Type": "application/json" },
          }),
        ),
      ),
    );
    const href = vi.spyOn(window.location, "href", "set");
    const query = boundary.adminSessionQuery();

    await expect(
      query.queryFn?.({ signal: new AbortController().signal } as never),
    ).rejects.toMatchObject({ statusCode: 401 });
    await expect(
      query.queryFn?.({ signal: new AbortController().signal } as never),
    ).rejects.toMatchObject({ statusCode: 401 });

    expect(boundary.isRedirectingToLogin()).toBe(true);
    expect(predecessorLatch()).toBe(true);
    expect(href).toHaveBeenCalledOnce();
    expect(href).toHaveBeenCalledWith(
      expect.stringMatching(
        /^\/admin\/auth\.login\?return_to=.*&prompt=consent$/,
      ),
    );
  });

  it("surfaces a feature mutation 401 without redirecting", async () => {
    vi.resetModules();
    const freshBoundary = await import("@/lib/gramAdminClient");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(unauthorizedBody, {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const href = vi.spyOn(window.location, "href", "set");

    await expect(
      freshBoundary.setAdminOrganizationFeature({
        organizationId: "org_1",
        featureName: "sso",
        enabled: true,
      }),
    ).rejects.toMatchObject({ statusCode: 401 });

    expect(freshBoundary.isRedirectingToLogin()).toBe(false);
    expect(href).not.toHaveBeenCalled();
  });

  it("keeps global query retries disabled and dashboard handoff as form POST navigation", () => {
    const src = path.resolve(
      path.dirname(new URL(import.meta.url).pathname),
      "..",
    );
    const main = fs.readFileSync(path.join(src, "main.tsx"), "utf8");
    const recordHeader = fs.readFileSync(
      path.join(src, "pages/organization/RecordHeader.tsx"),
      "utf8",
    );

    expect(main).toMatch(
      /defaultOptions\s*:\s*\{\s*queries\s*:\s*\{\s*retry\s*:\s*false,?\s*\},?\s*\}/,
    );
    expect(recordHeader).toMatch(
      /<form[\s\S]*method="post"[\s\S]*organizationDashboardUrl/,
    );
    expect(recordHeader).not.toMatch(/fetch\(|useMutation/);
  });
});

// The five writes that answer with the organization in its new state. Each
// one is named against its path, because the admin API is stripped from the
// public SDK and nothing else checks a disable does not enable. The wire shape
// is snake_case with ISO dates; the resolved value is the hand-written record
// the list and peek read, so the cache never sees a camelCase field or a Date.
describe("organization writes through the generated client", () => {
  const WIRE = {
    id: "org_placeholder_one",
    name: "Placeholder One",
    slug: "placeholder-one",
    account_type: "enterprise",
    whitelisted: false,
    trial_state: "running",
    trial_ends_at: "2026-05-06T00:00:00.000Z",
    member_count: 1,
    created_at: "2026-01-02T00:00:00.000Z",
    updated_at: "2026-01-07T00:00:00.000Z",
  };

  const RECORD: AdminOrganization = {
    id: WIRE.id,
    name: WIRE.name,
    slug: WIRE.slug,
    account_type: WIRE.account_type,
    workos_id: undefined,
    stripe_customer_id: undefined,
    stripe_subscription_id: undefined,
    whitelisted: WIRE.whitelisted,
    disabled_at: undefined,
    trial_state: "running",
    trial_ends_at: WIRE.trial_ends_at,
    trial_tier: undefined,
    trial_converted_at: undefined,
    trial_demoted_at: undefined,
    member_count: WIRE.member_count,
    created_at: WIRE.created_at,
    updated_at: WIRE.updated_at,
  };

  function stubFetch(): ReturnType<typeof vi.fn> {
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify(WIRE), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);
    return fetch;
  }

  async function requestOf(fetch: ReturnType<typeof vi.fn>): Promise<{
    path: string;
    method: string;
    contentType: string | null;
    body: unknown;
  }> {
    const request = fetch.mock.calls.at(-1)?.[0] as Request;
    return {
      path: new URL(request.url).pathname,
      method: request.method,
      contentType: request.headers.get("Content-Type"),
      body: (await request.json()) as unknown,
    };
  }

  it("posts the id to the disable path", async () => {
    const fetch = stubFetch();

    await expect(
      boundary.disableOrganization({ id: WIRE.id }),
    ).resolves.toEqual(RECORD);

    expect(await requestOf(fetch)).toEqual({
      path: "/admin/organization.disable",
      method: "POST",
      contentType: "application/json",
      body: { id: WIRE.id },
    });
  });

  it("posts the id to the enable path", async () => {
    const fetch = stubFetch();

    await expect(boundary.enableOrganization({ id: WIRE.id })).resolves.toEqual(
      RECORD,
    );

    expect(await requestOf(fetch)).toEqual({
      path: "/admin/organization.enable",
      method: "POST",
      contentType: "application/json",
      body: { id: WIRE.id },
    });
  });

  it("posts the id and the day count to the extend path", async () => {
    const fetch = stubFetch();

    await expect(
      boundary.extendTrial({ id: WIRE.id, days: 30 }),
    ).resolves.toEqual(RECORD);

    expect(await requestOf(fetch)).toEqual({
      path: "/admin/trial.extend",
      method: "POST",
      contentType: "application/json",
      body: { id: WIRE.id, days: 30 },
    });
  });

  it("posts the id and the day count to the re-arm path", async () => {
    const fetch = stubFetch();

    await expect(
      boundary.rearmTrial({ id: WIRE.id, days: 14 }),
    ).resolves.toEqual(RECORD);

    expect(await requestOf(fetch)).toEqual({
      path: "/admin/trial.rearm",
      method: "POST",
      contentType: "application/json",
      body: { id: WIRE.id, days: 14 },
    });
  });

  it("posts the id and the day count to the start path", async () => {
    const fetch = stubFetch();

    await expect(
      boundary.startTrial({ id: WIRE.id, days: 14 }),
    ).resolves.toEqual(RECORD);

    expect(await requestOf(fetch)).toEqual({
      path: "/admin/trial.start",
      method: "POST",
      contentType: "application/json",
      body: { id: WIRE.id, days: 14 },
    });
  });

  // A record write takes the login redirect on a 401 like every read of the
  // record does. The start is the exception below.
  it("redirects an expired session on a record write", async () => {
    vi.resetModules();
    const freshBoundary = await import("@/lib/gramAdminClient");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(unauthorizedBody, {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const href = vi.spyOn(window.location, "href", "set");

    await expect(
      freshBoundary.extendTrial({ id: WIRE.id, days: 30 }),
    ).rejects.toMatchObject({ statusCode: 401 });

    expect(freshBoundary.isRedirectingToLogin()).toBe(true);
    expect(href).toHaveBeenCalledOnce();
  });

  it("reports a start 401 in place without starting login", async () => {
    vi.resetModules();
    const freshBoundary = await import("@/lib/gramAdminClient");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(unauthorizedBody, {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const href = vi.spyOn(window.location, "href", "set");

    await expect(
      freshBoundary.startTrial({ id: WIRE.id, days: 14 }),
    ).rejects.toMatchObject({ statusCode: 401 });

    expect(freshBoundary.isRedirectingToLogin()).toBe(false);
    expect(href).not.toHaveBeenCalled();
  });

  // The server refuses an extension of a trial that has converted or already
  // expired. The sentence the operator has to read travels on the error.
  it("carries the conflict the server sends back", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            name: "conflict",
            message: "organization has no running enterprise trial to extend",
            id: "placeholder",
            fault: false,
            temporary: false,
            timeout: false,
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        ),
      ),
    );

    const failure = await boundary
      .extendTrial({ id: WIRE.id, days: 30 })
      .catch((e: unknown) => e);

    expect(errorMessage(failure)).toBe(
      "organization has no running enterprise trial to extend",
    );
  });
});

describe("organizationFromSdk", () => {
  it("reduces a fully populated record to the snake_case shape with ISO dates", () => {
    expect(
      boundary.organizationFromSdk({
        id: "org_placeholder_one",
        name: "Placeholder One",
        slug: "placeholder-one",
        accountType: "enterprise",
        workosId: "org_workos_placeholder",
        stripeCustomerId: "cus_placeholder",
        stripeSubscriptionId: "sub_placeholder",
        whitelisted: true,
        disabledAt: new Date("2026-02-01T00:00:00Z"),
        trialState: "converted",
        trialEndsAt: new Date("2026-05-06T00:00:00Z"),
        trialTier: "enterprise",
        trialConvertedAt: new Date("2026-04-01T00:00:00Z"),
        trialDemotedAt: new Date("2026-04-02T00:00:00Z"),
        memberCount: 3,
        createdAt: new Date("2026-01-02T00:00:00Z"),
        updatedAt: new Date("2026-01-07T00:00:00Z"),
      }),
    ).toEqual({
      id: "org_placeholder_one",
      name: "Placeholder One",
      slug: "placeholder-one",
      account_type: "enterprise",
      workos_id: "org_workos_placeholder",
      stripe_customer_id: "cus_placeholder",
      stripe_subscription_id: "sub_placeholder",
      whitelisted: true,
      disabled_at: "2026-02-01T00:00:00.000Z",
      trial_state: "converted",
      trial_ends_at: "2026-05-06T00:00:00.000Z",
      trial_tier: "enterprise",
      trial_converted_at: "2026-04-01T00:00:00.000Z",
      trial_demoted_at: "2026-04-02T00:00:00.000Z",
      member_count: 3,
      created_at: "2026-01-02T00:00:00.000Z",
      updated_at: "2026-01-07T00:00:00.000Z",
    } satisfies AdminOrganization);
  });

  it("leaves every optional field absent when the server sent none", () => {
    const record = boundary.organizationFromSdk({
      id: "org_placeholder_two",
      name: "Placeholder Two",
      slug: "placeholder-two",
      accountType: "free",
      whitelisted: false,
      memberCount: 0,
      createdAt: new Date("2026-01-02T00:00:00Z"),
      updatedAt: new Date("2026-01-07T00:00:00Z"),
    });

    expect(record.disabled_at).toBeUndefined();
    expect(record.trial_state).toBeUndefined();
    expect(record.trial_ends_at).toBeUndefined();
    expect(record.trial_converted_at).toBeUndefined();
    expect(record.trial_demoted_at).toBeUndefined();
    expect(record.workos_id).toBeUndefined();
    expect(record.stripe_customer_id).toBeUndefined();
  });
});

it("serves issuer logos through the generated same-origin image operation", async () => {
  const fetch = vi.fn().mockResolvedValue(
    new Response(new Uint8Array([1, 2, 3]), {
      status: 200,
      headers: { "Content-Type": "image/png" },
    }),
  );
  vi.stubGlobal("fetch", fetch);
  const query = boundary.adminIssuerImageQuery("image-placeholder");
  const cache = new QueryClient();
  const blob = await cache.fetchQuery(query);
  const cached = await cache.ensureQueryData(query);
  expect(cached).toBe(blob);
  expect(await cached.arrayBuffer()).toEqual(await blob.arrayBuffer());
  expect(fetch).toHaveBeenCalledTimes(1);
  cache.clear();
  expect(blob).toBeInstanceOf(Blob);
  expect((blob as Blob).size).toBe(3);
  expect((blob as Blob).type).toBe("image/png");
  const request = fetch.mock.calls[0]![0] as Request;
  expect(new URL(request.url).origin).toBe(window.location.origin);
  expect(request.headers.has("gram-session")).toBe(false);
  expect(request.headers.has("Authorization")).toBe(false);
});
