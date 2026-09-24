import seed from "../../../../../server/internal/admin/supportmatrix/catalog.json";
import { describe, expect, it } from "vitest";
import {
  emptyMapping,
  getFact,
  catalogSchema,
  summarize,
  unknown,
  type Fact,
  type Mapping,
  type Draft,
} from "./model";
import { resolveMatrixCell } from "./matrixCell";
import { integrationRequirements } from "./requirements";

const { methods } = catalogSchema.parse(seed);
const supported: Fact = { status: "supported", note: "", verify: false };
describe("product coverage", () => {
  it("derives reference claims only for applicable mappings", () => {
    expect(methods[0]?.facts["session"]?.status).toBe("supported");
    expect(getFact(emptyMapping, "session").status).toBe("unknown");
    expect(
      getFact({ ...emptyMapping, applicability: "applicable" }, "session")
        .status,
    ).toBe("unknown");
    expect(getFact(emptyMapping, "session", supported).status).toBe("unknown");
    expect(
      getFact(
        { ...emptyMapping, applicability: "applicable" },
        "session",
        supported,
      ).status,
    ).toBe("supported");
  });
  it("honors applicability even when previous coverage facts exist", () => {
    const mapping = { ...emptyMapping, facts: { session: supported } };
    expect(getFact(mapping, "session").status).toBe("unknown");
    expect(getFact({ ...mapping, applicability: "na" }, "session").status).toBe(
      "na",
    );
    expect(
      getFact({ ...mapping, applicability: "applicable" }, "session").status,
    ).toBe("supported");
  });
  it("keeps negative summaries unknown when any method is unassessed", () => {
    expect(
      summarize([{ ...unknown, status: "impossible" }, unknown]).status,
    ).toBe("unknown");
    expect(summarize([supported, unknown]).status).toBe("supported");
  });
  it("keeps verification uncertainty when every supporting method needs verification", () => {
    expect(summarize([{ ...supported, verify: true }, unknown]).verify).toBe(
      true,
    );
    expect(summarize([{ ...supported, verify: true }, supported]).verify).toBe(
      false,
    );
  });
});

describe("derived platform coverage", () => {
  const mapping: Mapping = {
    applicability: "applicable",
    conditions: "✅; Team plans only; macOS",
    accounts: {},
    facts: {},
  };
  it("carries method and platform qualifiers into the derived note", () => {
    const fact = getFact(mapping, "session", {
      ...supported,
      note: "via hooks",
      verify: true,
    });
    expect(fact).toMatchObject({ status: "supported", verify: true });
    expect(fact.note).toContain("Derived from method reference");
    expect(fact.note).toContain("via hooks");
    expect(fact.note).toContain("Team plans only; macOS");
  });
  it.each(["unknown", "unimplemented", "partial", "supported"] as const)(
    "preserves an explicit %s override",
    (status) => {
      const override: Fact = {
        status,
        note: "Explicit exception",
        verify: false,
      };
      expect(
        getFact(
          {
            ...mapping,
            conditions: "no hooks; cost only; WIP",
            facts: { session: override },
          },
          "session",
          supported,
        ),
      ).toEqual(override);
    },
  );
  it("limits cost-only and session-only mappings", () => {
    expect(
      getFact(
        { ...mapping, conditions: "✅ (cost only)" },
        "session-cost",
        supported,
      ).status,
    ).toBe("na");
    expect(
      getFact({ ...mapping, conditions: "✅ (cost only)" }, "cost", supported)
        .status,
    ).toBe("supported");
    expect(
      getFact(
        { ...mapping, conditions: "session tracking only" },
        "cost",
        supported,
      ).status,
    ).toBe("na");
    expect(
      getFact(
        { ...mapping, conditions: "session tracking only" },
        "session",
        supported,
      ).status,
    ).toBe("supported");
  });
  it("excludes hook-dependent features while retaining MCP distribution", () => {
    const withoutHooks = { ...mapping, conditions: "✅ (mcp, no hooks)" };
    expect(
      getFact(withoutHooks, "session", { ...supported, note: "✅ (via hooks)" })
        .status,
    ).toBe("unimplemented");
    expect(getFact(withoutHooks, "org", supported).status).toBe("supported");
  });
  it("carries platform VERIFY and WIP into derived and aggregate coverage", () => {
    expect(
      getFact({ ...mapping, conditions: "✅ verify" }, "session", supported)
        .verify,
    ).toBe(true);
    const wip = getFact(
      { ...mapping, conditions: "✅ WIP" },
      "session",
      supported,
    );
    expect(wip).toMatchObject({ status: "partial", verify: true });
    expect(summarize([wip, unknown])).toMatchObject({
      status: "partial",
      verify: true,
    });
  });
  it("uses the same derived facts in both matrix pivots and integration recommendations", () => {
    const catalog = catalogSchema.parse(seed);
    const device = catalog.methods.find((method) => method.id === "device")!;
    const draft: Draft = {
      accounts: {},
      references: {
        device: {
          session: { ...supported, note: "Updated reference", verify: true },
        },
      },
      mappings: { "device/claude-code-cli": mapping },
    };
    const aggregate = resolveMatrixCell(draft, catalog, {
      platforms: "claude-code-cli",
      capabilities: "session",
    });
    const specific = resolveMatrixCell(draft, catalog, {
      methods: "device",
      platforms: "claude-code-cli",
      capabilities: "session",
    });
    expect(aggregate.fact).toMatchObject({ status: "supported", verify: true });
    expect(specific.fact.note).toContain("Updated reference");
    const result = integrationRequirements(
      draft,
      [{ platformId: "claude-code-cli", capabilityId: "session" }],
      [device],
    );
    expect(result.combinations).toEqual([["device"]]);
    expect(result.provisional).toBe(true);
    expect(draft.mappings["device/claude-code-cli"]!.facts).toEqual({});
  });
});
