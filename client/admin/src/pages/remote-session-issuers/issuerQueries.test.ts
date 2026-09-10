import { queryKeyAdminGetGlobalIssuer } from "@gram/admin-client/react-query/adminGetGlobalIssuer.core";
import {
  queryKeyAdminListGlobalIssuerConvergenceCandidates,
  queryKeyAdminListGlobalIssuerConvergenceCandidatesInfinite,
} from "@gram/admin-client/react-query/adminListGlobalIssuerConvergenceCandidates.core";
import { queryKeyAdminGetGlobalIssuerMigratePreflight } from "@gram/admin-client/react-query/adminGetGlobalIssuerMigratePreflight.core";
import {
  queryKeyAdminListGlobalIssuers,
  queryKeyAdminListGlobalIssuersInfinite,
} from "@gram/admin-client/react-query/adminListGlobalIssuers.core";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
import { invalidateIssuerQueries } from "./issuerQueries";
it("invalidates issuer pages and preflight without invalidating unrelated admin data", async () => {
  const cache = new QueryClient();
  const issuer = [
    "@gram/admin-client",
    "admin",
    "listGlobalIssuers",
    { cursor: "next" },
  ];
  const preflight = [
    "@gram/admin-client",
    "admin",
    "getGlobalIssuerMigratePreflight",
    { sourceId: "source" },
  ];
  const session = ["@gram/admin-client", "admin", "getSession"];
  for (const key of [issuer, preflight, session]) cache.setQueryData(key, {});
  await invalidateIssuerQueries(cache);
  expect(cache.getQueryState(issuer)?.isInvalidated).toBe(true);
  expect(cache.getQueryState(preflight)?.isInvalidated).toBe(true);
  expect(cache.getQueryState(session)?.isInvalidated).toBe(false);
});

it.each([undefined, "deleted"])(
  "refreshes active issuer queries, evicting only an explicitly deleted ID (%s)",
  async (deletedId) => {
    const cache = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const deleted = [
      queryKeyAdminGetGlobalIssuer({ id: "deleted" }),
      queryKeyAdminListGlobalIssuerConvergenceCandidates({
        targetId: "deleted",
      }),
      queryKeyAdminListGlobalIssuerConvergenceCandidates({
        targetId: "deleted",
        cursor: "next",
      }),
      queryKeyAdminListGlobalIssuerConvergenceCandidatesInfinite({
        targetId: "deleted",
      }),
      queryKeyAdminGetGlobalIssuerMigratePreflight({
        sourceId: "local",
        targetId: "deleted",
      }),
    ];
    const surviving = [
      queryKeyAdminListGlobalIssuers({}),
      queryKeyAdminListGlobalIssuers({ cursor: "next" }),
      queryKeyAdminListGlobalIssuersInfinite({}),
      queryKeyAdminGetGlobalIssuer({ id: "surviving" }),
      queryKeyAdminListGlobalIssuerConvergenceCandidates({
        targetId: "surviving",
      }),
      queryKeyAdminGetGlobalIssuerMigratePreflight({
        sourceId: "local",
        targetId: "surviving",
      }),
    ];
    const rows = [...deleted, ...surviving].map((queryKey) => {
      cache.setQueryData(queryKey, { cached: true });
      const queryFn = vi.fn(async () => ({ cached: false }));
      const observer = new QueryObserver(cache, { queryKey, queryFn });
      return { queryKey, queryFn, unsubscribe: observer.subscribe(() => {}) };
    });
    try {
      await invalidateIssuerQueries(cache, deletedId);
      for (const row of rows.slice(0, deleted.length)) {
        if (deletedId) {
          expect(row.queryFn).not.toHaveBeenCalled();
          expect(cache.getQueryState(row.queryKey)).toBeUndefined();
        } else {
          expect(row.queryFn).toHaveBeenCalledTimes(1);
        }
      }
      for (const row of rows.slice(deleted.length)) {
        expect(row.queryFn).toHaveBeenCalledTimes(1);
      }
    } finally {
      rows.forEach((row) => row.unsubscribe());
      cache.clear();
    }
  },
);
