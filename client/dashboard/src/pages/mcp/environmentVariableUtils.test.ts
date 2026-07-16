import { describe, expect, it } from "vitest";
import type { EnvironmentVariable } from "./environmentVariableUtils";
import {
  getSystemProvidedVariables,
  getValueForEnvironment,
  hasEntryInEnvironment,
  isSecretInEnvironment,
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
        environmentValues: [
          { environmentSlug: "prod", value: "***", isSecret: true },
        ],
      }),
      mkVar({
        key: "DATABASE_URL",
        state: "system",
        environmentValues: [
          { environmentSlug: "prod", value: "***", isSecret: true },
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
        environmentValues: [
          { environmentSlug: "staging", value: "***", isSecret: true },
        ],
      }),
      mkVar({
        key: "IN_PROD",
        state: "system",
        environmentValues: [
          { environmentSlug: "prod", value: "***", isSecret: true },
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
        environmentValues: [
          { environmentSlug: "prod", value: "***", isSecret: true },
        ],
      }),
    ];
    expect(getSystemProvidedVariables(vars, "prod")).toEqual(["CUSTOM_SECRET"]);
  });
});

describe("hasEntryInEnvironment", () => {
  it("distinguishes environments that store the variable from those that do not", () => {
    const envVar = mkVar({
      key: "ONLY_IN_STAGING",
      state: "system",
      environmentValues: [
        { environmentSlug: "staging", value: "***", isSecret: true },
      ],
    });
    expect(hasEntryInEnvironment(envVar, "staging")).toBe(true);
    expect(hasEntryInEnvironment(envVar, "prod")).toBe(false);
  });

  it("reports an entry stored as an empty value", () => {
    // Placeholder entries are stored empty, and they are still entries.
    const envVar = mkVar({
      key: "PLACEHOLDER",
      state: "system",
      environmentValues: [
        { environmentSlug: "prod", value: "", isSecret: true },
      ],
    });
    expect(hasEntryInEnvironment(envVar, "prod")).toBe(true);
  });
});

describe("isSecretInEnvironment", () => {
  it("reports the secrecy the entry has in the selected environment", () => {
    const envVar = mkVar({
      key: "BASE_URL",
      state: "system",
      environmentValues: [
        { environmentSlug: "prod", value: "sup*****", isSecret: true },
        {
          environmentSlug: "staging",
          value: "https://x.test",
          isSecret: false,
        },
      ],
    });
    expect(isSecretInEnvironment(envVar, "prod")).toBe(true);
    expect(isSecretInEnvironment(envVar, "staging")).toBe(false);
  });

  it("treats a variable the environment does not define yet as secret", () => {
    // The row masks the input off the back of this, so an unknown entry must
    // not default to showing whatever gets typed into it.
    const envVar = mkVar({ key: "NEW_VAR", state: "system" });
    expect(isSecretInEnvironment(envVar, "prod")).toBe(true);
  });
});

describe("getValueForEnvironment", () => {
  it("returns the value stored in the selected environment", () => {
    const envVar = mkVar({
      key: "BASE_URL",
      state: "system",
      environmentValues: [
        {
          environmentSlug: "prod",
          value: "https://prod.test",
          isSecret: false,
        },
        {
          environmentSlug: "staging",
          value: "https://stg.test",
          isSecret: false,
        },
      ],
    });
    expect(getValueForEnvironment(envVar, "staging")).toBe("https://stg.test");
  });

  it("returns an empty string when the environment stores nothing", () => {
    const envVar = mkVar({ key: "BASE_URL", state: "system" });
    expect(getValueForEnvironment(envVar, "prod")).toBe("");
  });
});
