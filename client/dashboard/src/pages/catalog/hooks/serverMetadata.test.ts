import { describe, expect, it } from "vitest";
import type { FilterValues } from "../filter-defaults";
import type { CatalogServer } from "../hooks";
import type { FilterState } from "./useFilterState";
import {
  filterAndSortServers,
  requiresManualSetup,
  serverSetupType,
  extractAuthType,
  parseServerMetadata,
  isCatalogServer,
} from "./serverMetadata";

function server(
  registrySpecifier: string,
  supportsDcr: boolean,
): CatalogServer {
  return {
    description: "test",
    registrySpecifier,
    version: "1.0.0",
    meta: {},
    supportsDcr,
  };
}

const NO_FILTERS: FilterValues = {
  authTypes: [],
  updatedRange: "any",
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

  it("ignores legacy auth metadata when native evidence is unknown", () => {
    const legacy = {
      ...server("legacy/server", false),
      authSetup: "unknown" as const,
      meta: {
        "com.pulsemcp/server-version": {
          "remotes[0]": { authOptions: [{ type: "api_key" }] },
        },
      },
    };
    expect(serverSetupType(legacy)).toBe("unknown");
    expect(extractAuthType(legacy)).toBe("unknown");
    legacy.meta["com.pulsemcp/server-version"][
      "remotes[0]"
    ].authOptions[0]!.type = "none";
    expect(serverSetupType(legacy)).toBe("unknown");
  });

  it("does not label unknown authentication as no-auth", () => {
    expect(
      extractAuthType({
        ...server("unknown/server", false),
        authSetup: "unknown",
      }),
    ).toBe("unknown");
  });
});

describe("filterAndSortServers setup filter", () => {
  const servers: CatalogServer[] = [
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

describe("native catalog metadata", () => {
  it("keeps sparse records unknown without fabricated tool or popularity evidence", () => {
    const sparse = { ...server("example/sparse", false), meta: undefined };
    expect(isCatalogServer(sparse)).toBe(true);
    expect(parseServerMetadata(sparse)).toEqual({
      authType: "unknown",
      authTypeDisplay: "Unknown",
      setupType: "unknown",
      isLatest: undefined,
      status: undefined,
      publishedAt: undefined,
      updatedAt: undefined,
    });
    expect(
      filterAndSortServers([sparse], filterState({ updatedRange: "week" })),
    ).toEqual([]);
  });

  it("reads native auth capabilities and standard lifecycle, not vendor verification", () => {
    const native = {
      ...server("example/native", false),
      meta: {
        "com.speakeasy.ai/catalog": {
          supportsCimd: true,
          authSetup: "cimd" as const,
          authDiscoveryStatus: "complete" as const,
        },
        "io.modelcontextprotocol.registry/official": {
          status: "active",
          updatedAt: "2026-10-01T00:00:00Z",
          isLatest: true,
        },
      },
    };
    expect(parseServerMetadata(native)).toMatchObject({
      authType: "oauth",
      setupType: "auto",
      status: "active",
      updatedAt: new Date("2026-10-01T00:00:00Z"),
    });
    expect(parseServerMetadata(native)).not.toHaveProperty("isOfficial");
    expect(parseServerMetadata(native).publishedAt).toBeUndefined();
  });

  it("accepts explicit native no-auth but not unavailable discovery", () => {
    const native = server("example/native", false);
    expect(
      extractAuthType({
        ...native,
        meta: {
          "com.speakeasy.ai/catalog": {
            authSetup: "none",
            authDiscoveryStatus: "not_required",
          },
        },
      }),
    ).toBe("none");
    expect(
      extractAuthType({
        ...native,
        meta: {
          "com.speakeasy.ai/catalog": {
            supportsDcr: false,
            supportsCimd: false,
            authSetup: "unknown",
            authDiscoveryStatus: "unavailable",
          },
        },
      }),
    ).toBe("unknown");
  });
});
