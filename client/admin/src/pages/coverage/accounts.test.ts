import { describe, expect, it } from "vitest";
import seed from "../../../../../server/internal/admin/supportmatrix/catalog.json";
import {
  accountEligibility,
  accountFact,
  accountTypes,
  withEligibility,
} from "./accounts";
import {
  catalogSchema,
  emptyMapping,
  methodAccounts,
  type Draft,
  type Fact,
  type Method,
} from "./model";
import { resolveMatrixCell } from "./matrixCell";
import { integrationRequirements } from "./requirements";
import { importCsvHeader, parseMatrixImport } from "./importCsv";

const catalog = catalogSchema.parse(seed);
const supported: Fact = { status: "supported", note: "", verify: false };
const method = (id: string): Method =>
  catalog.methods.find((candidate) => candidate.id === id)!;
const emptyDraft: Draft = { mappings: {}, references: {}, accounts: {} };
const draft: Draft = {
  ...emptyDraft,
  mappings: {
    "device/claude-code-cli": { ...emptyMapping, applicability: "applicable" },
  },
};
const statuses = (
  methodId: string,
  fact: Fact = supported,
  current: Draft = emptyDraft,
) =>
  accountTypes.map(
    (account) =>
      accountFact(
        current,
        method(methodId),
        current.mappings[`${methodId}/claude-code-cli`],
        fact,
        account,
      ).status,
  );

describe("account coverage", () => {
  it.each(accountTypes)(
    "inherits Device Agent session coverage for %s accounts",
    (account) => {
      expect(
        resolveMatrixCell(
          draft,
          catalog,
          { platforms: "claude-code-cli", capabilities: "session" },
          account,
        ).fact.status,
      ).toBe("supported");
    },
  );

  it("excludes enterprise-only methods from personal and team coverage", () => {
    expect(statuses("openai-api")).toEqual([
      "impossible",
      "impossible",
      "supported",
    ]);
  });

  it("excludes personal accounts from a method that needs an admin console", () => {
    expect(statuses("settings")).toEqual([
      "impossible",
      "supported",
      "supported",
    ]);
    expect(statuses("settings", { ...supported, status: "partial" })[1]).toBe(
      "partial",
    );
  });

  // These two rows were blank for every account type: their plan notes never
  // described a plan, and eligibility was read from that prose.
  it.each(["hooks", "litellm"])(
    "answers for %s on every account type",
    (id) => {
      expect(statuses(id)).toEqual(["supported", "supported", "supported"]);
    },
  );

  it("leaves an unassessed account type unknown rather than ineligible", () => {
    const unassessed: Draft = {
      ...emptyDraft,
      accounts: { device: { team: "supported" } },
    };
    expect(statuses("device", supported, unassessed)).toEqual([
      "unknown",
      "supported",
      "unknown",
    ]);
  });

  it("keeps a not-applicable claim not applicable for an ineligible account", () => {
    const na: Fact = { status: "na", note: "", verify: false };
    expect(statuses("openai-api", na)).toEqual(["na", "na", "na"]);
  });

  it("takes a platform's answer over the method's, for that account only", () => {
    const overridden: Draft = {
      ...emptyDraft,
      mappings: {
        "settings/claude-code-cli": {
          ...emptyMapping,
          applicability: "applicable",
          accounts: { personal: "supported" },
        },
      },
    };
    expect(statuses("settings", supported, overridden)).toEqual([
      "supported",
      "supported",
      "supported",
    ]);
    expect(
      accountEligibility(
        methodAccounts(emptyDraft, method("settings")),
        {},
        "personal",
      ),
    ).toBe("unsupported");
  });

  it("does not recommend an ineligible integration", () => {
    const openai = method("openai-api");
    const restricted: Draft = {
      ...emptyDraft,
      references: { [openai.id]: { session: supported } },
      mappings: {
        [`${openai.id}/example`]: {
          ...emptyMapping,
          applicability: "applicable",
        },
      },
    };
    const targets = [{ platformId: "example", capabilityId: "session" }];
    expect(
      integrationRequirements(restricted, targets, [openai], "personal")
        .combinations,
    ).toEqual([]);
    expect(
      integrationRequirements(restricted, targets, [openai], "enterprise")
        .combinations,
    ).toEqual([[openai.id]]);
    expect(
      resolveMatrixCell(
        restricted,
        {
          ...catalog,
          methods: [openai],
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

describe("editing account eligibility", () => {
  it("changes one account type without disturbing the others", () => {
    const accounts = withEligibility(
      { personal: "supported", team: "supported", enterprise: "supported" },
      "team",
      "unsupported",
    );
    expect(accounts).toEqual({
      personal: "supported",
      team: "unsupported",
      enterprise: "supported",
    });
  });

  it("restores the method's answer when a platform override is cleared", () => {
    const accounts = withEligibility(
      { personal: "supported" },
      "personal",
      "inherit",
    );
    expect(accounts).toEqual({});
    expect(
      accountEligibility(
        methodAccounts(emptyDraft, method("settings")),
        accounts,
        "personal",
      ),
    ).toBe("unsupported");
  });

  it("uses the edited draft answer ahead of the seeded catalog answer", () => {
    const edited: Draft = {
      ...draft,
      accounts: { device: { personal: "unsupported" } },
    };
    const cell = resolveMatrixCell(
      edited,
      { ...catalog, methods: [method("device")] },
      { platforms: "claude-code-cli", capabilities: "session" },
      "personal",
    );
    expect(cell.fact.status).toBe("impossible");
    expect(cell.contributions[0]?.fact.status).toBe("impossible");
  });
});

describe("imported account eligibility", () => {
  const importRows = (...rows: string[]) =>
    parseMatrixImport([importCsvHeader, ...rows].join("\n"), catalog, {
      ...emptyDraft,
    }).draft;

  it.each([
    [
      "personal:supported;team:supported;enterprise:supported",
      ["supported", "supported", "supported"],
    ],
    [
      "personal:unsupported;team:unsupported;enterprise:supported",
      ["impossible", "impossible", "supported"],
    ],
    [
      "personal:unknown;team:supported;enterprise:unsupported",
      ["unknown", "supported", "impossible"],
    ],
  ])("reads mapping eligibility %s", (accounts, expected) => {
    const imported = importRows(
      `mapping,device,claude-code-cli,,,,,applicable,,"${accounts}"`,
    );
    const scoped = { ...catalog, methods: [method("device")] };
    expect(
      accountTypes.map(
        (account) =>
          resolveMatrixCell(
            imported,
            scoped,
            { platforms: "claude-code-cli", capabilities: "session" },
            account,
          ).fact.status,
      ),
    ).toEqual(expected);
    for (const [index, account] of accountTypes.entries())
      expect(
        integrationRequirements(
          imported,
          [{ platformId: "claude-code-cli", capabilityId: "session" }],
          scoped.methods,
          account,
        ).combinations,
      ).toEqual(expected[index] === "supported" ? [["device"]] : []);
  });

  it("reads method-wide eligibility and lets a mapping differ from it", () => {
    const imported = importRows(
      `method,device,,,,,,,,"personal:unsupported;team:supported;enterprise:supported"`,
      `mapping,device,claude-code-cli,,,,,applicable,,"personal:supported"`,
      `mapping,device,claude-code-web,,,,,applicable,,`,
    );
    expect(imported.accounts["device"]).toEqual({
      personal: "unsupported",
      team: "supported",
      enterprise: "supported",
    });
    const scoped = { ...catalog, methods: [method("device")] };
    const personalOn = (platform: string) =>
      resolveMatrixCell(
        imported,
        scoped,
        { platforms: platform, capabilities: "session" },
        "personal",
      ).fact.status;
    expect(personalOn("claude-code-cli")).toBe("supported");
    expect(personalOn("claude-code-web")).toBe("impossible");
  });

  it.each([
    [`method,device,,,,,,,,"owner:supported"`, 'unknown account type "owner"'],
    [
      `method,device,,,,,,,,"team:maybe"`,
      "account eligibility must be supported, unsupported, or unknown",
    ],
    [
      `method,device,,,,,,,,"team:supported;team:unknown"`,
      'account type "team" appears twice',
    ],
    [
      `method,device,claude-code-cli,,,,,,,"team:supported"`,
      "method rows must fill only method_id and accounts",
    ],
    [
      `reference,device,,session,supported,,false,,,"team:supported"`,
      "must leave applicability, conditions, and accounts empty",
    ],
  ])("rejects %s", (row, message) => {
    expect(() => importRows(row)).toThrow(message);
  });
});
