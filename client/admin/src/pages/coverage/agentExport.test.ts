import { describe, expect, it } from "vitest";
import { agentMatrixPrompt } from "./agentExport";
import { type Catalog, type Draft } from "./model";

const catalog: Catalog = {
  methods: [
    {
      id: "device",
      name: "Device Agent",
      vendor: "Example",
      plans: "Team and personal accounts",
      facts: {
        session: { status: "supported", note: "via hooks", verify: false },
      },
    },
    {
      id: "api",
      name: "Compliance API",
      vendor: "Example",
      plans: "Enterprise only",
      facts: {
        session: { status: "partial", note: "Limited events", verify: true },
      },
    },
  ],
  products: [
    {
      id: "cli",
      name: "Example CLI",
      family: "Example",
      vendor: "Example",
      surface: "CLI",
    },
    {
      id: "web",
      name: "Example Web",
      family: "Example",
      vendor: "Example",
      surface: "Web",
    },
  ],
  capabilities: [
    { id: "session", name: "Session tracking", group: "Observability" },
    { id: "cost", name: "Cost tracking", group: "Observability" },
  ],
};
const draft: Draft = {
  references: {
    device: {
      cost: {
        status: "supported",
        note: 'Costs, with "quotes"\nand a newline',
        verify: false,
      },
    },
  },
  mappings: {
    "device/cli": {
      applicability: "applicable",
      conditions:
        "Personal accounts: unsupported; Team plans: supported; Enterprise plans: supported",
      facts: {},
    },
    "api/cli": { applicability: "applicable", conditions: "", facts: {} },
    "device/web": {
      applicability: "applicable",
      conditions: "WIP",
      facts: {
        session: {
          status: "unknown",
          note: "Explicitly unassessed",
          verify: true,
        },
      },
    },
  },
};

type ExportData = {
  catalog: Catalog;
  draft: Draft;
  coverage: {
    platform: string;
    capability: string;
    accounts: Record<
      string,
      {
        status: string;
        needsVerification: boolean;
        supportedBy: { method: string; needsVerification: boolean }[];
        partialSupportBy: { method: string; needsVerification: boolean }[];
      }
    >;
  }[];
};
function exportedData(): ExportData {
  return JSON.parse(
    agentMatrixPrompt(catalog, draft).split("Complete dataset (JSON):\n")[1]!,
  ) as ExportData;
}

describe("agent matrix export", () => {
  it("includes every catalog entry, raw override, and platform-capability combination", () => {
    const data = exportedData();
    expect(data.catalog).toEqual(catalog);
    expect(data.draft).toEqual(draft);
    expect(
      data.coverage.map(
        ({ platform, capability }) => `${platform}/${capability}`,
      ),
    ).toEqual(["cli/session", "cli/cost", "web/session", "web/cost"]);
    for (const cell of data.coverage)
      expect(Object.keys(cell.accounts)).toEqual([
        "personal",
        "team",
        "enterprise",
      ]);
  });
  it("names eligible supporting methods and keeps partial support separate", () => {
    const accounts = exportedData().coverage[0]!.accounts;
    expect(accounts.personal?.supportedBy).toEqual([]);
    expect(accounts.team?.supportedBy).toEqual([
      { method: "device", needsVerification: false },
    ]);
    expect(accounts.enterprise?.partialSupportBy).toEqual([
      { method: "api", needsVerification: true },
    ]);
    expect(accounts.enterprise?.status).toBe("supported");
  });
  it("honors explicit unknown overrides and inherited WIP qualifications", () => {
    const coverage = exportedData().coverage;
    expect(coverage[2]?.accounts.team?.status).toBe("unknown");
    expect(coverage[3]?.accounts.team?.status).toBe("partial");
    expect(coverage[3]?.accounts.team?.needsVerification).toBe(true);
  });
});
