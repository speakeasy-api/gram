/// <reference types="node" />

import fs from "node:fs";
import path from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

const useMutation = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useMutation };
});

import { isRedirectingToLogin as predecessorLatch } from "@/lib/gramAdminApi";
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
    expect(Object.keys(boundary).sort()).toEqual([
      "adminSessionQuery",
      "isRedirectingToLogin",
      "organizationFeaturesQuery",
      "redirectOnUnauthorized",
      "setAdminOrganizationFeature",
      "useSetAdminOrganizationFeatureMutation",
    ]);

    if (false) {
      // @ts-expect-error The session operation accepts no SDK/request options.
      boundary.adminSessionQuery({ serverURL: "https://example.invalid" });
      boundary.setAdminOrganizationFeature(
        { organizationId: "org_1", featureName: "sso", enabled: true },
        // @ts-expect-error Per-call transport and credentials are not accepted.
        { credentials: "include", headers: { Authorization: "Bearer x" } },
      );
    }
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

    const request = fetch.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).origin).toBe(window.location.origin);
    expect(request.credentials).toBe("same-origin");
    expect(request.mode).toBe("cors");
    expect(request.headers.has("Authorization")).toBe(false);
    expect(request.headers.has("Cookie")).toBe(false);
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
      /defaultOptions:\s*\{ queries:\s*\{ retry: false \} \}/,
    );
    expect(recordHeader).toMatch(
      /<form[\s\S]*method="post"[\s\S]*organizationDashboardUrl/,
    );
    expect(recordHeader).not.toMatch(/fetch\(|useMutation/);
  });
});
