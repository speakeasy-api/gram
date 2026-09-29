import { describe, expect, it } from "vitest";
import {
  enforcementOutcomeLabel,
  isBlockingOutcome,
  mcpServerDisplayName,
} from "./risk-outcome";

describe("enforcementOutcomeLabel", () => {
  it("labels every recorded outcome and skips unrecorded ones", () => {
    expect(enforcementOutcomeLabel("denied")).toBe("Denied");
    expect(enforcementOutcomeLabel("warned_acknowledged")).toBe(
      "Warned · acknowledged",
    );
    expect(enforcementOutcomeLabel(undefined)).toBeNull();
  });
});

describe("isBlockingOutcome", () => {
  it("flags denied, withheld and quarantined only", () => {
    expect(isBlockingOutcome("denied")).toBe(true);
    expect(isBlockingOutcome("quarantined")).toBe(true);
    expect(isBlockingOutcome("logged")).toBe(false);
    expect(isBlockingOutcome("warned_pending")).toBe(false);
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
