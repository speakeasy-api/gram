import type { FleetDevice } from "@gram/client/models/components/fleetdevice.js";
import { describe, expect, it } from "vitest";
import {
  coverageDisplay,
  deviceCoverageKey,
  matchesCoverageFilter,
  toolLabel,
} from "./coverage-states";

function device(overrides: Partial<FleetDevice>): FleetDevice {
  return {
    id: "mdm:d1",
    inInventory: true,
    provider: "jamf",
    externalId: "ext-1",
    coverageBucket: "agent_active",
    ...overrides,
  } as FleetDevice;
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

describe("matchesCoverageFilter", () => {
  const outdated = device({ policyState: "outdated" });
  const noPolicyState = device({});
  const stale = device({ coverageBucket: "agent_stale" });

  it("matches everything with no selection", () => {
    expect(matchesCoverageFilter(stale, undefined)).toBe(true);
    expect(matchesCoverageFilter(stale, [])).toBe(true);
  });

  it("matches an active device by its policy state", () => {
    expect(matchesCoverageFilter(outdated, ["outdated"])).toBe(true);
    expect(matchesCoverageFilter(outdated, ["attested"])).toBe(false);
  });

  it("matches every active device under agent_active", () => {
    expect(matchesCoverageFilter(outdated, ["agent_active"])).toBe(true);
    expect(matchesCoverageFilter(noPolicyState, ["agent_active"])).toBe(true);
    expect(matchesCoverageFilter(stale, ["agent_active"])).toBe(false);
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

  it("ignores inherited keys", () => {
    expect(toolLabel("constructor")).toBe("Constructor");
  });
});
