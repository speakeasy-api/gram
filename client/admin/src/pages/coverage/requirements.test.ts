import seed from "../../../../../server/internal/admin/supportmatrix/catalog.json";
import { describe, expect, it } from "vitest";
import { integrationRequirements } from "./requirements";
import {
  mappingKey,
  catalogSchema,
  type Draft,
  type Fact,
  type Status,
} from "./model";

const { methods } = catalogSchema.parse(seed);
const candidates = methods.slice(0, 3);
const targets = [
  { platformId: "p1", capabilityId: "c1" },
  { platformId: "p2", capabilityId: "c1" },
];
function fixture(entries: [number, string, Status, boolean?][]): Draft {
  const draft: Draft = { mappings: {}, references: {}, accounts: {} };
  for (const [index, platform, status, verify = false] of entries) {
    const fact: Fact = { status, verify, note: "" };
    draft.mappings[mappingKey(candidates[index]!.id, platform)] = {
      applicability: "applicable",
      conditions: "",
      accounts: {},
      facts: { c1: fact },
    };
  }
  return draft;
}
describe("integration requirements", () => {
  it("returns standalone alternatives and necessary multi-method combinations, without redundant supersets", () => {
    const draft = fixture([
      [0, "p1", "supported"],
      [0, "p2", "supported"],
      [1, "p1", "supported"],
      [2, "p2", "supported"],
    ]);
    expect(
      integrationRequirements(draft, targets, candidates).combinations,
    ).toEqual([[candidates[0]!.id], [candidates[1]!.id, candidates[2]!.id]]);
  });
  it("recomputes alternatives for a narrowed selection", () => {
    const draft = fixture([
      [0, "p1", "supported"],
      [1, "p2", "supported"],
    ]);
    expect(
      integrationRequirements(draft, targets.slice(0, 1), candidates)
        .combinations,
    ).toEqual([[candidates[0]!.id]]);
    expect(
      integrationRequirements(draft, targets, candidates).combinations,
    ).toEqual([[candidates[0]!.id, candidates[1]!.id]]);
  });
  it("reports gaps for partial and unknown support", () => {
    const result = integrationRequirements(
      fixture([[0, "p1", "partial"]]),
      targets,
      candidates,
    );
    expect(result.combinations).toEqual([]);
    expect(result.gaps).toEqual(targets);
    expect(result.partialCount).toBe(1);
    expect(result.unknownCount).toBe(2);
  });
  it("requires applicable mappings and honors restricted candidate methods", () => {
    const draft = fixture([
      [0, "p1", "supported"],
      [0, "p2", "supported"],
    ]);
    expect(
      integrationRequirements(draft, targets, candidates.slice(1)).combinations,
    ).toEqual([]);
    draft.mappings[mappingKey(candidates[0]!.id, "p1")]!.applicability = "na";
    expect(integrationRequirements(draft, targets, candidates).gaps).toEqual([
      targets[0],
    ]);
  });
  it("flags combinations that rely on unverified coverage", () => {
    const result = integrationRequirements(
      fixture([
        [0, "p1", "supported", true],
        [0, "p2", "supported"],
      ]),
      targets,
      candidates,
    );
    expect(result.provisional).toBe(true);
    expect(result.combinations).toHaveLength(1);
  });
  it("keeps required integrations and alternatives when other targets have gaps", () => {
    const draft = fixture([
      [0, "p1", "supported"],
      [0, "p2", "supported"],
      [1, "p1", "supported"],
      [2, "p2", "supported"],
      [1, "p3", "partial"],
    ]);
    const gaps = [
      { platformId: "p3", capabilityId: "c1" },
      { platformId: "p4", capabilityId: "c1" },
    ];
    const result = integrationRequirements(
      draft,
      [...targets, ...gaps],
      candidates,
    );
    expect(result.combinations).toEqual([
      [candidates[0]!.id],
      [candidates[1]!.id, candidates[2]!.id],
    ]);
    expect(result.gaps).toEqual(gaps);
    expect(result.partialCount).toBe(1);
    expect(result.unknownCount).toBe(2);
    expect(result.provisional).toBe(false);
  });
  it("retains verification flags for known coverage even with remaining gaps", () => {
    const result = integrationRequirements(
      fixture([[0, "p1", "supported", true]]),
      targets,
      candidates,
    );
    expect(result.combinations).toEqual([[candidates[0]!.id]]);
    expect(result.gaps).toEqual([targets[1]]);
    expect(result.provisional).toBe(true);
  });
  it("does not suggest an empty integration set when all coverage is unknown", () => {
    const result = integrationRequirements(fixture([]), targets, candidates);
    expect(result.combinations).toEqual([]);
    expect(result.gaps).toEqual(targets);
  });
  it("does not recommend an empty combination for an empty selection", () => {
    expect(
      integrationRequirements(fixture([]), [], candidates).combinations,
    ).toEqual([]);
  });
});
