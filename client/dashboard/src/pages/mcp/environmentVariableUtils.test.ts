import { describe, expect, it } from "vitest";
import type { EnvironmentVariable } from "./environmentVariableUtils";
import {
  getSystemProvidedVariables,
  getSystemValueSaveOp,
} from "./environmentVariableUtils";

const mkVar = (
  overrides: Partial<EnvironmentVariable> &
    Pick<EnvironmentVariable, "key" | "state">,
): EnvironmentVariable => ({
  id: overrides.id ?? `id-${overrides.key}`,
  key: overrides.key,
  state: overrides.state,
  isRequired: overrides.isRequired ?? true,
  environmentValues: overrides.environmentValues ?? [],
  description: overrides.description,
});

describe("getSystemProvidedVariables", () => {
  it("returns empty array when no vars are in system state", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({ key: "A", state: "user-provided" }),
      mkVar({ key: "B", state: "omitted" }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual([]);
  });

  it("returns keys of system vars that have a value in the attached env", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "STRIPE_API_KEY",
        state: "system",
        environmentValues: [{ environmentSlug: "prod", value: "***" }],
      }),
      mkVar({
        key: "DATABASE_URL",
        state: "system",
        environmentValues: [{ environmentSlug: "prod", value: "***" }],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual([
      "STRIPE_API_KEY",
      "DATABASE_URL",
    ]);
  });

  it("excludes system vars with no value in the attached env", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "ONLY_IN_STAGING",
        state: "system",
        environmentValues: [{ environmentSlug: "staging", value: "***" }],
      }),
      mkVar({
        key: "IN_PROD",
        state: "system",
        environmentValues: [{ environmentSlug: "prod", value: "***" }],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["IN_PROD"]);
  });

  it("handles custom (non-required) system vars the same as required", () => {
    const vars: EnvironmentVariable[] = [
      mkVar({
        key: "CUSTOM_SECRET",
        state: "system",
        isRequired: false,
        environmentValues: [{ environmentSlug: "prod", value: "***" }],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["CUSTOM_SECRET"]);
  });
});

describe("getSystemValueSaveOp", () => {
  it("skips when the variable was never edited", () => {
    expect(getSystemValueSaveOp(undefined, "sup*****")).toEqual({
      kind: "skip",
    });
    expect(getSystemValueSaveOp(undefined, "")).toEqual({ kind: "skip" });
  });

  it("skips when the editing value is the loaded redacted value", () => {
    // State toggles seed editing state with the redacted display value;
    // saving must not write the mask back over the real secret.
    expect(getSystemValueSaveOp("sup*****", "sup*****")).toEqual({
      kind: "skip",
    });
  });

  it("updates when the user typed a new value", () => {
    expect(getSystemValueSaveOp("new-secret", "sup*****")).toEqual({
      kind: "update",
      value: "new-secret",
    });
  });

  it("updates when a value is typed for a variable with no stored value", () => {
    expect(getSystemValueSaveOp("first-value", "")).toEqual({
      kind: "update",
      value: "first-value",
    });
  });

  it("removes when the user cleared a previously stored value", () => {
    expect(getSystemValueSaveOp("", "sup*****")).toEqual({ kind: "remove" });
  });

  it("skips when cleared and no value was stored", () => {
    expect(getSystemValueSaveOp("", "")).toEqual({ kind: "skip" });
  });
});
