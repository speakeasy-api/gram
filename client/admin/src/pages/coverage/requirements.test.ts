import { describe, expect, it } from "vitest";
import { integrationRequirements } from "./requirements";
import type { Fact, Method, PlatformSupport, Status } from "./model";

const ids = ["m1", "m2", "m3"];
const targets = [
  { platformId: "p1", capabilityId: "c1" },
  { platformId: "p2", capabilityId: "c1" },
];
/** Methods m1..m3, each applying to the platforms it has an entry for. */
function fixture(
  entries: [number, string, Status, boolean?][],
  personal: PlatformSupport["accounts"]["personal"] = "supported",
): Method[] {
  return ids.map((id, index) => ({
    id,
    name: id.toUpperCase(),
    vendor: "Vendor",
    plans: "",
    claims: {},
    platforms: entries
      .filter(([candidate]) => candidate === index)
      .map(([, platform, status, verify = false]) => {
        const fact: Fact = { status, verify, note: "" };
        return {
          platform,
          applicability: "applicable" as const,
          accounts: {
            personal,
            team: "supported" as const,
            enterprise: "supported" as const,
          },
          note: "",
          cells: { c1: fact },
        };
      }),
  }));
}

describe("integration requirements", () => {
  it("returns standalone alternatives and necessary multi-method combinations, without redundant supersets", () => {
    const candidates = fixture([
      [0, "p1", "supported"],
      [0, "p2", "supported"],
      [1, "p1", "supported"],
      [2, "p2", "supported"],
    ]);
    expect(integrationRequirements(targets, candidates).combinations).toEqual([
      ["m1"],
      ["m2", "m3"],
    ]);
  });
  it("recomputes alternatives for a narrowed selection", () => {
    const candidates = fixture([
      [0, "p1", "supported"],
      [1, "p2", "supported"],
    ]);
    expect(
      integrationRequirements(targets.slice(0, 1), candidates).combinations,
    ).toEqual([["m1"]]);
    expect(integrationRequirements(targets, candidates).combinations).toEqual([
      ["m1", "m2"],
    ]);
  });
  it("reports gaps for partial and unknown support", () => {
    const result = integrationRequirements(
      targets,
      fixture([[0, "p1", "partial"]]),
    );
    expect(result.combinations).toEqual([]);
    expect(result.gaps).toEqual(targets);
    // The other two methods have no entry for either platform, so both
    // targets count as unknown as well as the partial one.
    expect(result.partialCount).toBe(1);
    expect(result.unknownCount).toBe(2);
  });
  it("marks a cover provisional when it leans on a cell that still needs verification", () => {
    const provisional = integrationRequirements(
      targets,
      fixture([
        [0, "p1", "supported", true],
        [0, "p2", "supported"],
      ]),
    );
    expect(provisional.combinations).toEqual([["m1"]]);
    expect(provisional.provisional).toBe(true);
    const verified = integrationRequirements(
      targets,
      fixture([
        [0, "p1", "supported"],
        [0, "p2", "supported"],
      ]),
    );
    expect(verified.provisional).toBe(false);
  });
  it("counts an ineligible account type as a gap", () => {
    const candidates = fixture([[0, "p1", "supported"]], "unsupported");
    expect(
      integrationRequirements(targets.slice(0, 1), candidates, "team")
        .combinations,
    ).toEqual([["m1"]]);
    const personal = integrationRequirements(
      targets.slice(0, 1),
      candidates,
      "personal",
    );
    expect(personal.combinations).toEqual([]);
    expect(personal.gaps).toEqual(targets.slice(0, 1));
  });
});
