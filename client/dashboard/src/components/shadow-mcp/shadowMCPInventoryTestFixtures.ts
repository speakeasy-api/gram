import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/**
 * Test-only: the access summary the server would compute for a row whose
 * legacy access field carries the given value, so fixtures keep one knob.
 * Kept beside the component rather than copied into every test file.
 */
export function testAccessSummary(
  access: ShadowMCPInventoryServer["access"],
): ShadowMCPInventoryServer["accessSummary"] {
  switch (access) {
    case "allowed":
      return {
        state: "allowed",
        allowedFor: "everyone",
        blockedFor: "none",
        blockingDefault: "deny",
        decision: undefined,
        decisionCoverage: "none",
      };
    case "blocked":
      return {
        state: "blocked",
        allowedFor: "none",
        blockedFor: "none",
        blockingDefault: "deny",
        decision: undefined,
        decisionCoverage: "none",
      };
    case "restricted":
      return {
        state: "restricted",
        allowedFor: "none",
        blockedFor: "some",
        blockingDefault: "allow",
        decision: undefined,
        decisionCoverage: "none",
      };
    default:
      return {
        state: "unenforced",
        allowedFor: "none",
        blockedFor: "none",
        blockingDefault: "none",
        decision: undefined,
        decisionCoverage: "none",
      };
  }
}
