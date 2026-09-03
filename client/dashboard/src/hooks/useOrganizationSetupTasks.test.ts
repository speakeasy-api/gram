import type { GramCore } from "@gram/client/core.js";
import { describe, expect, it } from "vitest";
import { buildOrganizationSetupTasksQuery } from "./useOrganizationSetupTasks";

describe("buildOrganizationSetupTasksQuery", () => {
  it("isolates setup task state by organization and hidden mode", () => {
    const client = {} as GramCore;
    const first = buildOrganizationSetupTasksQuery(client, "org-first", false);
    const second = buildOrganizationSetupTasksQuery(
      client,
      "org-second",
      false,
    );
    const hidden = buildOrganizationSetupTasksQuery(client, "org-first", true);

    expect(first.queryKey).not.toEqual(second.queryKey);
    expect(first.queryKey).not.toEqual(hidden.queryKey);
    expect(first.throwOnError).toBe(false);
    expect(
      buildOrganizationSetupTasksQuery(client, "org-first", false, {
        throwOnError: true,
      }).throwOnError,
    ).toBe(true);
    expect(first.queryKey).toEqual([
      "@gram/client",
      "organizations",
      "listSetupTasks",
      { includeHidden: false, gramSession: "" },
      { organizationId: "org-first" },
    ]);
  });
});
