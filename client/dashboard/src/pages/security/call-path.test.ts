import { describe, expect, it } from "vitest";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { callOutcome, callPathFor } from "./call-path";

describe("callPathFor", () => {
  it("blocks the gateway → server link for a denied request", () => {
    const path = callPathFor("request", "denied", "Datadog");
    expect(path.left).toEqual({ blocked: false, label: "request →" });
    expect(path.right).toEqual({ blocked: true, label: "denied" });
    expect(path.serverReached).toBe(false);
    expect(path.caption).toBe(
      "The gateway stopped this tools/call before it reached Datadog. The client received a policy error.",
    );
  });

  it("keeps both links open for a logged request", () => {
    const path = callPathFor("request", "logged", "Linear");
    expect(path.left.blocked).toBe(false);
    expect(path.right).toEqual({ blocked: false, label: "forwarded →" });
    expect(path.serverReached).toBe(true);
    expect(path.caption).toBe(
      "Logged only. The call was forwarded to Linear unchanged.",
    );
  });

  it("blocks the client ← gateway link for a withheld response", () => {
    const path = callPathFor("response", "withheld", "GitHub");
    expect(path.left).toEqual({ blocked: true, label: "withheld" });
    expect(path.right).toEqual({ blocked: false, label: "← response" });
    expect(path.serverReached).toBe(true);
    expect(path.caption).toBe(
      "GitHub returned a result; the gateway withheld it from the client.",
    );
  });

  it.each([
    ["request", "warned_pending", "right", true],
    ["request", "warned_abandoned", "right", true],
    ["request", "warned_acknowledged", "right", false],
    ["request", "quarantined", "right", true],
    ["response", "warned_pending", "left", true],
    ["response", "warned_acknowledged", "left", false],
    ["response", "quarantined", "left", true],
    ["response", "logged", "left", false],
  ] as const)(
    "%s + %s blocks the %s link: %s",
    (phase, outcome, side, blocked) => {
      const path = callPathFor(phase, outcome, "S");
      expect(path[side].blocked).toBe(blocked);
    },
  );

  it("reaches the server only when a request is not held", () => {
    expect(
      callPathFor("request", "warned_acknowledged", "S").serverReached,
    ).toBe(true);
    expect(callPathFor("request", "warned_pending", "S").serverReached).toBe(
      false,
    );
  });

  it("treats a missing outcome as passed through", () => {
    const path = callPathFor(undefined, undefined, "S");
    expect(path.right.blocked).toBe(false);
    expect(path.serverReached).toBe(true);
  });
});

describe("callOutcome", () => {
  const finding = (id: string, overrides: Partial<RiskResult>) =>
    ({
      id,
      executionId: "exec-1",
      phase: "request",
      ...overrides,
    }) as RiskResult;

  it("takes a blocking outcome from a sibling in the same phase", () => {
    const logged = finding("a", { enforcementOutcome: "logged" });
    const denied = finding("b", { enforcementOutcome: "denied" });
    expect(callOutcome(logged, [logged, denied])).toBe("denied");
  });

  it("ignores siblings from another phase", () => {
    const logged = finding("a", { enforcementOutcome: "logged" });
    const withheld = finding("b", {
      phase: "response",
      enforcementOutcome: "withheld",
    });
    expect(callOutcome(logged, [logged, withheld])).toBe("logged");
  });
});
