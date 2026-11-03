import { describe, expect, it } from "vitest";
import type { FilterValues } from "../filter-defaults";
import type { PulseMCPServer } from "../hooks";
import type { FilterState } from "./useFilterState";
import {
  filterAndSortServers,
  requiresManualSetup,
  serverSetupType,
  extractAuthType,
} from "./serverMetadata";

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

  it("requires evidence before classifying a server as manual", () => {
    expect(serverSetupType(server("unknown/server", false))).toBe("unknown");
    expect(
      requiresManualSetup({
        ...server("manual/server", false),
        authSetup: "manual",
      }),
    ).toBe(true);
  });

  it("recognizes CIMD and explicit no-auth endpoints as automatic", () => {
    const cimd = { ...server("cimd/server", false), supportsCimd: true };
    expect(serverSetupType(cimd)).toBe("auto");
    expect(extractAuthType(cimd)).toBe("oauth");
    expect(
      serverSetupType({ ...server("public/server", false), authSetup: "none" }),
    ).toBe("auto");
  });

  it("uses explicit legacy auth metadata when capability discovery is unknown", () => {
    const legacy = {
      ...server("legacy/server", false),
      authSetup: "unknown" as const,
      meta: {
        "com.pulsemcp/server-version": {
          "remotes[0]": { authOptions: [{ type: "api_key" }] },
        },
      },
    };
    expect(serverSetupType(legacy)).toBe("manual");
    legacy.meta["com.pulsemcp/server-version"][
      "remotes[0]"
    ].authOptions[0]!.type = "none";
    expect(serverSetupType(legacy)).toBe("auto");
  });

  it("does not label unknown authentication as no-auth", () => {
    expect(
      extractAuthType({
        ...server("unknown/server", false),
        authSetup: "unknown",
      }),
    ).toBe("other");
  });
});

describe("filterAndSortServers setup filter", () => {
  const servers: PulseMCPServer[] = [
    server("auto/server", true),
    { ...server("manual/server", false), authSetup: "manual" },
    server("unknown/server", false),
  ];

  it("returns all servers when no setup filter is active", () => {
    const result = filterAndSortServers(servers, filterState({}));
    expect(result.map((s) => s.registrySpecifier)).toEqual([
      "auto/server",
      "manual/server",
      "unknown/server",
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

  it("can show only entries with unknown setup", () => {
    expect(
      filterAndSortServers(
        servers,
        filterState({ setupTypes: ["unknown"] }),
      ).map((s) => s.registrySpecifier),
    ).toEqual(["unknown/server"]);
  });

  it("keeps both when both setup types are selected", () => {
    const result = filterAndSortServers(
      servers,
      filterState({ setupTypes: ["auto", "manual"] }),
    );
    expect(result).toHaveLength(2);
  });
});
