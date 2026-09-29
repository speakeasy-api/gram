import { describe, expect, it } from "vitest";
import {
  enforcementOutcomeLabel,
  isBlockingOutcome,
  mcpServerDisplayName,
} from "./risk-outcome";

describe("enforcementOutcomeLabel", () => {
  it.each([
    ["logged", "Logged"],
    ["denied", "Denied"],
    ["withheld", "Withheld"],
    ["warned_pending", "Warned · pending"],
    ["warned_acknowledged", "Warned · acknowledged"],
    ["warned_abandoned", "Warned · abandoned"],
    ["quarantined", "Quarantined"],
  ] as const)("labels %s as %s", (outcome, label) => {
    expect(enforcementOutcomeLabel(outcome)).toBe(label);
  });

  it("skips an unrecorded outcome", () => {
    expect(enforcementOutcomeLabel(undefined)).toBeNull();
  });
});

describe("isBlockingOutcome", () => {
  it("flags denied, withheld and quarantined only", () => {
    expect(isBlockingOutcome("denied")).toBe(true);
    expect(isBlockingOutcome("withheld")).toBe(true);
    expect(isBlockingOutcome("quarantined")).toBe(true);
    expect(isBlockingOutcome("logged")).toBe(false);
    expect(isBlockingOutcome("warned_pending")).toBe(false);
    expect(isBlockingOutcome("warned_acknowledged")).toBe(false);
    expect(isBlockingOutcome("warned_abandoned")).toBe(false);
    expect(isBlockingOutcome(undefined)).toBe(false);
  });
});

describe("mcpServerDisplayName", () => {
  it("prefers name, then slug, then a short id", () => {
    const id = "12345678-aaaa";
    expect(mcpServerDisplayName({ id, name: " Petstore ", slug: "p" })).toBe(
      "Petstore",
    );
    expect(mcpServerDisplayName({ id, name: " ", slug: "petstore" })).toBe(
      "petstore",
    );
    expect(mcpServerDisplayName({ id })).toBe("12345678");
  });
});
