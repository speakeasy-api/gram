// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  PLATFORM_STEPS_START,
  WIZARD_STEPS,
  wizardStepIndex,
  wizardStepParam,
} from "./wizard-steps";

/** The step's current position, so reordering the steps keeps these tests. */
const at = (id: (typeof WIZARD_STEPS)[number]["id"]) =>
  WIZARD_STEPS.findIndex((step) => step.id === id);

describe("wizardStepIndex", () => {
  it("opens a step by its id", () => {
    for (const step of WIZARD_STEPS) {
      expect(wizardStepIndex(step.id, 0)).toBe(at(step.id));
    }
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
  it("writes the id of a fixed step", () => {
    expect(wizardStepParam(at("mcp-sign-in"), false)).toBe("mcp-sign-in");
    expect(wizardStepParam(at("verify"), false)).toBe("verify");
  });

  it("writes the index of a platform's own setup step", () => {
    expect(wizardStepParam(PLATFORM_STEPS_START, true)).toBe(
      String(PLATFORM_STEPS_START),
    );
    expect(wizardStepParam(PLATFORM_STEPS_START + 3, true)).toBe(
      String(PLATFORM_STEPS_START + 3),
    );
    expect(wizardStepParam(at("servers"), true)).toBe("servers");
  });

  it("round-trips every step, with or without platform steps", () => {
    for (const platformSteps of [false, true]) {
      for (let index = 0; index < 8; index++) {
        expect(wizardStepIndex(wizardStepParam(index, platformSteps), 0)).toBe(
          index,
        );
      }
    }
  });
});
