import { describe, expect, it } from "vitest";
import seed from "../../../../../server/internal/admin/supportmatrix/catalog.json";
import {
  accountFact,
  accountTypes,
  accountEligibility,
  updateAccountEligibility,
  withoutAccountConditions,
  updateAccountNotes,
} from "./accounts";
import { catalogSchema, emptyMapping, type Draft, type Fact } from "./model";
import { resolveMatrixCell } from "./matrixCell";
import { integrationRequirements } from "./requirements";
import { importCsvHeader, parseMatrixImport } from "./importCsv";

const catalog = catalogSchema.parse(seed);
const supported: Fact = { status: "supported", note: "", verify: false };
const draft: Draft = {
  references: {},
  mappings: {
    "device/claude-code-cli": { ...emptyMapping, applicability: "applicable" },
  },
};

describe("account coverage", () => {
  it.each(accountTypes)(
    "inherits Device Agent session coverage for %s accounts",
    (account) => {
      expect(
        resolveMatrixCell(
          draft,
          catalog,
          {
            platforms: "claude-code-cli",
            capabilities: "session",
          },
          account,
        ).fact.status,
      ).toBe("supported");
    },
  );

  it("excludes enterprise-only methods from personal and team coverage", () => {
    const method = catalog.methods.find(
      (method) => method.id === "openai-api",
    )!;
    expect(accountFact(method, supported, "personal").status).toBe(
      "impossible",
    );
    expect(accountFact(method, supported, "team").status).toBe("impossible");
    expect(accountFact(method, supported, "enterprise").status).toBe(
      "supported",
    );
  });

  it("includes enterprise in team eligibility without interpreting personal exclusions as support", () => {
    const method = catalog.methods.find((method) => method.id === "settings")!;
    expect(accountFact(method, supported, "personal").status).toBe(
      "impossible",
    );
    expect(accountFact(method, supported, "team").status).toBe("supported");
    expect(accountFact(method, supported, "enterprise").status).toBe(
      "supported",
    );
    expect(
      accountFact(method, { ...supported, status: "partial" }, "team").status,
    ).toBe("partial");
  });

  it.each(["hooks", "litellm"])(
    "keeps unassessed %s plan eligibility unknown",
    (id) => {
      const method = catalog.methods.find((method) => method.id === id)!;
      for (const account of accountTypes)
        expect(accountFact(method, supported, account).status).toBe("unknown");
    },
  );

  it("does not recommend an ineligible integration", () => {
    const method = catalog.methods.find(
      (method) => method.id === "openai-api",
    )!;
    const restricted: Draft = {
      references: { [method.id]: { session: supported } },
      mappings: {
        [`${method.id}/example`]: {
          ...emptyMapping,
          applicability: "applicable",
        },
      },
    };
    const targets = [{ platformId: "example", capabilityId: "session" }];
    expect(
      integrationRequirements(restricted, targets, [method], "personal")
        .combinations,
    ).toEqual([]);
    expect(
      integrationRequirements(restricted, targets, [method], "enterprise")
        .combinations,
    ).toEqual([[method.id]]);
    expect(
      resolveMatrixCell(
        restricted,
        {
          ...catalog,
          methods: [method],
          products: [
            {
              id: "example",
              name: "Example",
              vendor: "Example",
              family: "Example",
              surface: "App",
            },
          ],
        },
        { platforms: "example", capabilities: "session" },
        "personal",
      ).fact.status,
    ).toBe("impossible");
  });
});

describe("imported plan eligibility", () => {
  it.each([
    [
      "Team plans: ✅; Personal accounts: ✅",
      "supported",
      "supported",
      "supported",
    ],
    [
      "Team plans: ✅; Personal accounts: ☠️",
      "impossible",
      "supported",
      "supported",
    ],
    [
      "Team plans: Enterprise only; Personal accounts: ☠️",
      "impossible",
      "impossible",
      "supported",
    ],
    ["Team plans: --; Personal accounts: --", "unknown", "unknown", "unknown"],
    ["Team plans: MDM; Personal accounts: --", "unknown", "unknown", "unknown"],
  ])("reads %s", (plans, personal, team, enterprise) => {
    const method = { ...catalog.methods[0]!, plans };
    expect(
      accountTypes.map(
        (account) => accountFact(method, supported, account).status,
      ),
    ).toEqual([personal, team, enterprise]);
  });
});

describe("CSV account coverage", () => {
  it.each([
    [
      "Personal accounts: supported; Team plans: supported; Enterprise plans: supported",
      ["supported", "supported", "supported"],
    ],
    [
      "Personal accounts: unsupported; Team plans: unsupported; Enterprise plans: supported",
      ["impossible", "impossible", "supported"],
    ],
    [
      "Personal accounts: unknown; Team plans: supported; Enterprise plans: unsupported",
      ["unknown", "supported", "impossible"],
    ],
    [
      "Team plans: Enterprise only; Personal accounts: ☠️",
      ["impossible", "impossible", "supported"],
    ],
    [
      "Personal accounts: supported\nTeam plans: supported\nEnterprise plans: supported",
      ["supported", "supported", "supported"],
    ],
  ])("uses imported eligibility: %s", (conditions, expected) => {
    const imported = parseMatrixImport(
      [
        importCsvHeader,
        `mapping,device,claude-code-cli,,,,,applicable,"${conditions}"`,
      ].join("\n"),
      catalog,
      { mappings: {}, references: {} },
    ).draft;
    const scopedCatalog = { ...catalog, methods: [catalog.methods[0]!] };
    const statuses = accountTypes.map(
      (account) =>
        resolveMatrixCell(
          imported,
          scopedCatalog,
          { platforms: "claude-code-cli", capabilities: "session" },
          account,
        ).fact.status,
    );
    expect(statuses).toEqual(expected);
    for (const [index, account] of accountTypes.entries()) {
      const requirements = integrationRequirements(
        imported,
        [{ platformId: "claude-code-cli", capabilityId: "session" }],
        scopedCatalog.methods,
        account,
      );
      expect(requirements.combinations).toEqual(
        expected[index] === "supported" ? [["device"]] : [],
      );
    }
  });
});

describe("editing account eligibility", () => {
  const method = catalog.methods[0]!;
  it("changes one account without changing the other accounts or platform restrictions", () => {
    const original = "OS: macOS; Team plans: ✅; Personal accounts: ✅; VERIFY";
    const conditions = updateAccountEligibility(
      method,
      original,
      "team",
      "unsupported",
    );
    expect(
      accountTypes.map((type) => accountEligibility(method, conditions, type)),
    ).toEqual(["supported", "unsupported", "supported"]);
    expect(withoutAccountConditions(conditions)).toBe("OS: macOS; VERIFY");
    const changed: Draft = {
      ...draft,
      mappings: {
        "device/claude-code-cli": {
          ...draft.mappings["device/claude-code-cli"]!,
          conditions,
        },
      },
    };
    const cell = resolveMatrixCell(
      changed,
      { ...catalog, methods: [method] },
      { platforms: "claude-code-cli", capabilities: "session" },
      "team",
    );
    expect(cell.fact.status).toBe("impossible");
    expect(cell.contributions[0]?.fact.status).toBe("impossible");
  });
  it("retains explicit unknown and avoids duplicate labels after repeated edits", () => {
    const first = updateAccountEligibility(
      method,
      "OS: Linux",
      "personal",
      "unknown",
    );
    const second = updateAccountEligibility(
      method,
      first,
      "enterprise",
      "unsupported",
    );
    expect(
      accountTypes.map((type) => accountEligibility(method, second, type)),
    ).toEqual(["unknown", "supported", "unsupported"]);
    expect(second.match(/Personal accounts:/g)).toHaveLength(1);
  });
  it("preserves account settings when multiline notes change", () => {
    const conditions = updateAccountEligibility(
      method,
      "old notes",
      "personal",
      "unsupported",
    );
    const edited = updateAccountNotes(conditions, "macOS only\nRequires setup");
    expect(withoutAccountConditions(edited)).toBe("macOS only\nRequires setup");
    expect(
      accountTypes.map((type) => accountEligibility(method, edited, type)),
    ).toEqual(["unsupported", "supported", "supported"]);
  });
});
