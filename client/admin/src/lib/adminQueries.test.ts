import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  cancelOrganizationFetches,
  invalidateOrganizationStats,
  organizationQuery,
  organizationsListQuery,
  organizationsStatsQuery,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import type {
  AdminOrganization,
  ListOrganizationsResult,
} from "@/lib/gramAdminApi";

// The slug is never the id. A fixture that spells them the same lets a lookup
// by slug pass every assertion written for a lookup by id, and the two are
// separate cache entries with separate consequences.
function org(id: string): AdminOrganization {
  return {
    id,
    name: id,
    slug: `slug-for-${id}`,
    account_type: "free",
    whitelisted: false,
    member_count: 1,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("organizationsListQuery", () => {
  it("invalidates every filtered page from the unfiltered key", () => {
    const qc = new QueryClient();
    const filtered = organizationsListQuery({ q: "x", cursor: "page-2" });
    qc.setQueryData(filtered.queryKey, { organizations: [] });
    qc.setQueryData(["gram-admin-project", "p"], {});

    void qc.invalidateQueries({ queryKey: organizationsListQuery().queryKey });

    expect(qc.getQueryState(filtered.queryKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(["gram-admin-project", "p"])?.isInvalidated).toBe(
      false,
    );
  });

  it("caches a blank filter and an absent one as the same page", () => {
    expect(
      organizationsListQuery({ q: "", cursor: undefined }).queryKey,
    ).toEqual(organizationsListQuery().queryKey);
  });
});

describe("organizationQuery", () => {
  // Asserts the id reaches the cache, not that the key reads any particular
  // way. A key spelled out in the test would fail on a prefix rename, which is
  // a refactor rather than a defect.
  it("gives each organization its own cache entry", () => {
    const qc = new QueryClient();
    qc.setQueryData(organizationQuery("org-a").queryKey, org("org-a"));
    qc.setQueryData(organizationQuery("org-b").queryKey, org("org-b"));

    expect(qc.getQueryData(organizationQuery("org-a").queryKey)?.id).toBe(
      "org-a",
    );
  });

  // A list row seeds this entry so the detail page paints on the first frame.
  // That snapshot is as old as the list fetch behind it, so this query must not
  // take the staleTime adminSessionQuery does, or the seed becomes the record
  // an admin reads and acts on.
  it("leaves a seeded record open to a refetch", () => {
    expect(organizationQuery("org-a").staleTime).toBeUndefined();
  });
});

// What a write puts back. The list and the peek panel repaint from this and
// nothing refetches behind it, so an entry this function misses is a surface
// still showing the state the operator just changed.
describe("writeOrganizationToCache", () => {
  const LIVE: AdminOrganization = { ...org("org-a"), slug: "placeholder-a" };
  const DISABLED: AdminOrganization = {
    ...LIVE,
    disabled_at: "2026-08-01T00:00:00Z",
  };
  // Distinct from anything a cancelled read answers with below.
  const FRESH = {
    total: 2,
    created_last_7_days: 1,
    trials_ending_soon: 1,
    disabled: 1,
    disabled_last_7_days: 1,
  };

  it("answers the detail route by id and by slug", () => {
    const qc = new QueryClient();
    qc.setQueryData(organizationQuery("org-a").queryKey, LIVE);
    // The row links by slug, so this is the entry the operator opens after
    // acting on the row, and it is a different entry from the one above.
    qc.setQueryData(organizationQuery(DISABLED.slug).queryKey, LIVE);

    writeOrganizationToCache(qc, DISABLED);

    expect(
      qc.getQueryData(organizationQuery("org-a").queryKey)?.disabled_at,
    ).toBe(DISABLED.disabled_at);
    expect(
      qc.getQueryData(organizationQuery(DISABLED.slug).queryKey)?.disabled_at,
    ).toBe(DISABLED.disabled_at);
  });

  it("replaces the record on every page that holds it, and only that record", () => {
    const qc = new QueryClient();
    const page = organizationsListQuery({ q: "x" });
    qc.setQueryData(page.queryKey, {
      organizations: [org("org-b"), LIVE],
      next_cursor: "cursor_page_two",
    });

    writeOrganizationToCache(qc, DISABLED);

    const after = qc.getQueryData<ListOrganizationsResult>(page.queryKey);
    expect(after?.organizations.map((row) => row.disabled_at)).toEqual([
      undefined,
      DISABLED.disabled_at,
    ]);
    // The rest of the page is the page, not just its rows. A cursor dropped
    // here strands the operator on whichever page they had reached.
    expect(after?.next_cursor).toBe("cursor_page_two");
  });

  // The row is found by id, and by nothing else. A cached page is as old as the
  // fetch behind it, so it can hold a record whose slug has since been changed
  // from another surface; a writer matching on the slug would miss that row and
  // leave the state the operator just changed on the page.
  it("finds the row whose slug has moved on since the page was fetched", () => {
    const qc = new QueryClient();
    const page = organizationsListQuery({ q: "x" });
    qc.setQueryData(page.queryKey, {
      organizations: [{ ...LIVE, slug: "placeholder-a-as-it-was" }],
    });

    writeOrganizationToCache(qc, DISABLED);

    const after = qc.getQueryData<ListOrganizationsResult>(page.queryKey);
    expect(after?.organizations[0]).toEqual(DISABLED);
  });

  // Every write moves one of these, and a single record cannot move a count.
  it("marks the platform totals for a refetch", () => {
    const qc = new QueryClient();
    qc.setQueryData(organizationsStatsQuery.queryKey, {
      total: 1,
      created_last_7_days: 0,
      trials_ending_soon: 0,
      disabled: 0,
      disabled_last_7_days: 0,
    });

    writeOrganizationToCache(qc, DISABLED);

    expect(
      qc.getQueryState(organizationsStatsQuery.queryKey)?.isInvalidated,
    ).toBe(true);
  });

  // The cold-load window: the aggregate is still open, and an invalidation
  // joins a running fetch rather than replacing it. Its pre-write answer would
  // fill the cache and read as fresh.
  it("survives a first stats read that was still open when it landed", async () => {
    const qc = new QueryClient();

    let land: (stats: unknown) => void = () => {};
    const stale = new Promise((resolve) => {
      land = resolve;
    });
    const inFlight = qc.prefetchQuery({
      ...organizationsStatsQuery,
      queryFn: () => stale as Promise<never>,
    });

    await cancelOrganizationFetches(qc);
    writeOrganizationToCache(qc, DISABLED);

    land({ total: 1, disabled: 0 });
    await inFlight;

    expect(qc.getQueryData(organizationsStatsQuery.queryKey)).toBeUndefined();
  });

  // The same window, on the path where the write never lands. Nothing is put
  // in the cache to replace what the cancel dropped, so the read that was
  // cancelled has to be asked for again or the strip keeps its three dashes.
  it("asks again for a stats read the cancel dropped when no write follows", async () => {
    const qc = new QueryClient();

    let land: (stats: unknown) => void = () => {};
    const stale = new Promise((resolve) => {
      land = resolve;
    });
    let calls = 0;
    const queryFn = (): Promise<never> => {
      calls += 1;
      return (calls === 1 ? stale : Promise.resolve(FRESH)) as Promise<never>;
    };
    // Observed, because an invalidation refetches the queries something is
    // watching. The strip is on screen for every write this covers.
    const observer = new QueryObserver(qc, {
      ...organizationsStatsQuery,
      queryFn,
    });
    const unwatch = observer.subscribe(() => {});
    await vi.waitFor(() => {
      expect(calls).toBe(1);
    });

    await cancelOrganizationFetches(qc);
    invalidateOrganizationStats(qc);

    await vi.waitFor(() => {
      expect(qc.getQueryData(organizationsStatsQuery.queryKey)).toEqual(FRESH);
    });

    // The cancelled read, answering late. It is dropped either way, and this
    // pins that the second answer is not the one overwritten.
    land({ total: 1, disabled: 0 });
    await vi.waitFor(() => {
      expect(qc.getQueryData(organizationsStatsQuery.queryKey)).toEqual(FRESH);
    });
    unwatch();
  });

  it("leaves a page that never held the record exactly as it was", () => {
    const qc = new QueryClient();
    const page = organizationsListQuery({ q: "other" });
    const before: ListOrganizationsResult = { organizations: [org("org-b")] };
    qc.setQueryData(page.queryKey, before);

    writeOrganizationToCache(qc, DISABLED);

    // Untouched down to the reference. React Query keeps the old object where
    // the new one is deeply equal, so this holds whether the page was skipped
    // or rebuilt from rows that had not changed; what it rules out is a write
    // that lands on a page the record is not on.
    expect(qc.getQueryData(page.queryKey)).toBe(before);
  });

  // The write is only as good as the reads it outlives. A list fetch that was
  // already open when the operator acted answers with the row as it was, and
  // React Query commits that answer whenever it arrives, so without the cancel
  // it lands on top of the write and the organization reads as though nothing
  // happened.
  it("survives a read that was already in flight when it landed", async () => {
    const qc = new QueryClient();
    const page = organizationsListQuery();
    qc.setQueryData(page.queryKey, { organizations: [LIVE] });

    let land: (result: ListOrganizationsResult) => void = () => {};
    const stale = new Promise<ListOrganizationsResult>((resolve) => {
      land = resolve;
    });

    // Asked before the write, and still open when the write comes back.
    const inFlight = qc.prefetchQuery({ ...page, queryFn: () => stale });

    await cancelOrganizationFetches(qc);
    writeOrganizationToCache(qc, DISABLED);

    // The stale answer arrives late, carrying the row in its pre-write state.
    land({ organizations: [LIVE] });
    await inFlight;

    const after = qc.getQueryData<ListOrganizationsResult>(page.queryKey);
    expect(after?.organizations[0]).toEqual(DISABLED);
  });

  // The entry the operator actually opened. The row links by slug wherever an
  // organization has one, so the detail read in flight is keyed by slug, and
  // the slug is not known when the write starts. Cancelling the id alone would
  // leave this one free to answer late and put the record back as it was.
  it("survives a detail read in flight under the slug, not the id", async () => {
    const qc = new QueryClient();
    const detail = organizationQuery(DISABLED.slug);

    let land: (result: AdminOrganization) => void = () => {};
    const stale = new Promise<AdminOrganization>((resolve) => {
      land = resolve;
    });

    const inFlight = qc.prefetchQuery({ ...detail, queryFn: () => stale });

    await cancelOrganizationFetches(qc);
    writeOrganizationToCache(qc, DISABLED);

    land(LIVE);
    await inFlight;

    expect(qc.getQueryData(detail.queryKey)?.disabled_at).toBe(
      DISABLED.disabled_at,
    );
  });
});
