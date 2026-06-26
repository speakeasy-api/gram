import { describe, expect, it } from "vitest";
import type { FilterValues } from "../filter-defaults";
import type { PulseMCPServer } from "../hooks";
import type { FilterState } from "./useFilterState";
import { filterAndSortServers, requiresManualSetup } from "./serverMetadata";

function server(
  registrySpecifier: string,
  supportsDcr: boolean,
): PulseMCPServer {
  return {
    description: "test",
    registrySpecifier,
    version: "1.0.0",
    meta: {},
    toolCount: 0,
    isReadOnly: false,
    supportsDcr,
  };
}

const NO_FILTERS: FilterValues = {
  authTypes: [],
  toolBehaviors: [],
  minUsers: 0,
  updatedRange: "any",
  minTools: 0,
  setupTypes: [],
};

function filterState(filters: Partial<FilterValues>): FilterState {
  return {
    category: "all",
    sort: "alphabetical",
    filters: { ...NO_FILTERS, ...filters },
  };
}

describe("requiresManualSetup", () => {
  it("treats DCR-capable servers as automatic", () => {
    expect(requiresManualSetup(server("auto/server", true))).toBe(false);
  });

  it("treats non-DCR servers as manual", () => {
    expect(requiresManualSetup(server("manual/server", false))).toBe(true);
  });
});

describe("filterAndSortServers setup filter", () => {
  const servers = [server("auto/server", true), server("manual/server", false)];

  it("returns all servers when no setup filter is active", () => {
    const result = filterAndSortServers(servers, filterState({}));
    expect(result.map((s) => s.registrySpecifier)).toEqual([
      "auto/server",
      "manual/server",
    ]);
  });

  it("keeps only manual servers when filtering by manual", () => {
    const result = filterAndSortServers(
      servers,
      filterState({ setupTypes: ["manual"] }),
    );
    expect(result.map((s) => s.registrySpecifier)).toEqual(["manual/server"]);
  });

  it("keeps only automatic servers when filtering by auto", () => {
    const result = filterAndSortServers(
      servers,
      filterState({ setupTypes: ["auto"] }),
    );
    expect(result.map((s) => s.registrySpecifier)).toEqual(["auto/server"]);
  });

  it("keeps both when both setup types are selected", () => {
    const result = filterAndSortServers(
      servers,
      filterState({ setupTypes: ["auto", "manual"] }),
    );
    expect(result).toHaveLength(2);
  });
});
