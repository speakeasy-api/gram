import type { ManagedDevice } from "@gram/client/models/components/manageddevice.js";
import { describe, expect, it } from "vitest";
import {
  coverageDisplay,
  deviceCoverageKey,
  toolLabel,
} from "./coverage-states";

function device(overrides: Partial<ManagedDevice>): ManagedDevice {
  return {
    id: "d1",
    provider: "jamf",
    externalId: "ext-1",
    coverageBucket: "agent_active",
    ...overrides,
  } as ManagedDevice;
}

describe("deviceCoverageKey", () => {
  it("shows an active device by its policy state", () => {
    expect(deviceCoverageKey(device({ policyState: "outdated" }))).toBe(
      "outdated",
    );
  });

  it("keeps the bucket when the server sent no policy state", () => {
    expect(deviceCoverageKey(device({}))).toBe("agent_active");
  });

  it("ignores a policy state outside agent_active", () => {
    expect(
      deviceCoverageKey(
        device({ coverageBucket: "agent_stale", policyState: "attested" }),
      ),
    ).toBe("agent_stale");
  });
});

describe("coverageDisplay", () => {
  it("degrades an unrecognized state to a neutral badge", () => {
    expect(coverageDisplay("from_a_newer_server")).toMatchObject({
      label: "Unknown",
      variant: "neutral",
    });
  });

  it("ignores inherited keys", () => {
    expect(coverageDisplay("constructor").label).toBe("Unknown");
    expect(coverageDisplay("__proto__").label).toBe("Unknown");
  });
});

describe("toolLabel", () => {
  it("names known tools and spells out unknown ids", () => {
    expect(toolLabel("claude_code")).toBe("Claude Code");
    expect(toolLabel("new_tool")).toBe("New tool");
  });
});
