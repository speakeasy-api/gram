import { describe, expect, it } from "vitest";
import { openClawInstallMessage } from "./agent-install-message";

describe("openClawInstallMessage", () => {
  it("asks for a post-install check without accepting a redeemable command", () => {
    const message = openClawInstallMessage();
    expect(message).toContain("trusted shell");
    expect(message).toContain("restarted");
    expect(message).toContain("Do not read or print credentials");
    expect(message).not.toContain("curl");
    expect(message).not.toContain("/agent-mcp/install/");
  });
});
