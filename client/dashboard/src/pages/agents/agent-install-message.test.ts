import { describe, expect, it } from "vitest";
import { openClawInstallMessage } from "./agent-install-message";

describe("openClawInstallMessage", () => {
  it("asks for a post-install check without accepting a redeemable command", () => {
    const message = openClawInstallMessage();
    expect(message).toContain("trusted shell");
    expect(message).toContain("restarted");
    expect(message).toContain('"Speakeasy hooks:" PASS or FAIL');
    expect(message).toContain("If it was PASS, also check");
    expect(message).toContain("agents without a project do not get hooks");
    expect(message).toContain("Do not read or print credentials");
    expect(message).not.toContain("curl");
    expect(message).not.toContain("/agent-mcp/install/");
  });
});
