import { describe, expect, it } from "vitest";
import type { EnvironmentVariable } from "./environmentVariableUtils";
import { getSystemProvidedVariables } from "./environmentVariableUtils";

const mkVar = (
  overrides: Partial<EnvironmentVariable> &
    Pick<EnvironmentVariable, "key" | "state">,
): EnvironmentVariable => ({
  id: overrides.id ?? `id-${overrides.key}`,
  key: overrides.key,
  state: overrides.state,
  isRequired: overrides.isRequired ?? true,
  valueGroups: overrides.valueGroups ?? [],
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
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["prod"] },
        ],
      }),
      mkVar({
        key: "DATABASE_URL",
        state: "system",
        valueGroups: [
          { valueHash: "h2", value: "***", environments: ["prod"] },
        ],
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
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["staging"] },
        ],
      }),
      mkVar({
        key: "IN_PROD",
        state: "system",
        valueGroups: [
          { valueHash: "h2", value: "***", environments: ["prod"] },
        ],
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
        valueGroups: [
          { valueHash: "h1", value: "***", environments: ["prod"] },
        ],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["CUSTOM_SECRET"]);
  });
});
