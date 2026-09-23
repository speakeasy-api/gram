import { describe, expect, it } from "vitest";
import { surfaceGuidance } from "./surface-guidance-copy";

describe("surfaceGuidance", () => {
  it("names the next step when this use case was picked", () => {
    const guidance = surfaceGuidance("observability", {
      useCase: "observability",
      verified: false,
      selected: true,
      nextStep: {
        slug: "plugin-distribution:cursor",
        title: "Install the Cursor observability plugin",
        description: "Download it and install it on one machine.",
        techniqueSlug: "cursor-hooks",
        destination: "plugins",
        evidence: "a session from Cursor",
      },
    });
    expect(guidance.action).toBe("step");
    expect(guidance.heading).toBe("Observability is not set up yet");
    expect(guidance.description).toBe(
      "Your next step: Install the Cursor observability plugin. Download it and install it on one machine.",
    );
  });

  it("points into onboarding when another use case was picked", () => {
    const guidance = surfaceGuidance("security", {
      useCase: "security",
      verified: false,
      selected: false,
    });
    expect(guidance.action).toBe("onboarding");
    expect(guidance.heading).toBe("Security & policies are not set up yet");
    expect(guidance.description).toContain("enabled risk policy");
  });

  it("points into onboarding before any answers exist", () => {
    expect(surfaceGuidance("mcp-gateway", undefined).action).toBe("onboarding");
    expect(surfaceGuidance("cost-tracking", undefined).heading).toBe(
      "Spend controls are not set up yet",
    );
  });
});
