import { describe, expect, it } from "vitest";
import {
  parsePolicyNewPrefill,
  policyNewPrefillQuery,
} from "./policy-new-prefill";

describe("policy new prefill", () => {
  it("round-trips categories, servers, action, score and name", () => {
    const query = policyNewPrefillQuery({
      categories: new Set(["secrets", "pii"]),
      mcpServerIds: ["srv-1", "srv-2"],
      action: "warn",
      score: 7,
      name: "Linear guardrail",
    });
    const params = new URLSearchParams(query);
    expect(params.get("kind")).toBe("standard");
    const prefill = parsePolicyNewPrefill(params);
    expect([...(prefill.categories ?? [])].sort()).toEqual(["pii", "secrets"]);
    expect(prefill.mcpServerIds).toEqual(["srv-1", "srv-2"]);
    expect(prefill.action).toBe("warn");
    expect(prefill.score).toBe(7);
    expect(prefill.name).toBe("Linear guardrail");
  });

  it("drops unknown values and keeps the single-category form", () => {
    const prefill = parsePolicyNewPrefill(
      new URLSearchParams(
        "category=secrets&categories=bogus&action=nuke&score=99",
      ),
    );
    expect([...(prefill.categories ?? [])]).toEqual(["secrets"]);
    expect(prefill.action).toBeUndefined();
    expect(prefill.score).toBeUndefined();
    expect(prefill.mcpServerIds).toEqual([]);
  });
});
