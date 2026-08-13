import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { organizationQuery, organizationsListQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";

function org(id: string): AdminOrganization {
  return {
    id,
    name: id,
    slug: id,
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
});
