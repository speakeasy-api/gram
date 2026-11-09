// @vitest-environment node
import { describe, expect, it } from "vitest";
import { wizardStepIndex, wizardStepParam } from "./wizard-steps";

describe("wizardStepIndex", () => {
  it("opens a step by its id", () => {
    expect(wizardStepIndex("servers", 0)).toBe(1);
    expect(wizardStepIndex("mcp-sign-in", 0)).toBe(2);
    expect(wizardStepIndex("verify", 0)).toBe(5);
  });

  it("still opens a step by the index older links carry", () => {
    expect(wizardStepIndex("2", 0)).toBe(2);
    expect(wizardStepIndex("0", 1)).toBe(0);
  });

  it("opens the default step for no step or an unknown one", () => {
    expect(wizardStepIndex(null, 1)).toBe(1);
    expect(wizardStepIndex("", 1)).toBe(1);
    expect(wizardStepIndex("credential", 1)).toBe(1);
    expect(wizardStepIndex("-1", 1)).toBe(1);
    expect(wizardStepIndex("1.5", 1)).toBe(1);
  });

  it("leaves an index past the fixed steps for the wizard to correct", () => {
    expect(wizardStepIndex("9", 0)).toBe(9);
  });
});

describe("wizardStepParam", () => {
  it("writes the id of a fixed step and the index of a platform step", () => {
    expect(wizardStepParam(2)).toBe("mcp-sign-in");
    expect(wizardStepParam(7)).toBe("7");
  });

  it("round-trips every step", () => {
    for (let index = 0; index < 8; index++) {
      expect(wizardStepIndex(wizardStepParam(index), 0)).toBe(index);
    }
  });
});
