import { describe, expect, it } from "vitest";

import { openClawInstallMessage } from "./agent-install-message";

describe("openClawInstallMessage", () => {
  const command =
    "curl -fsSL https://app.example.test/agent-mcp/install/abc123 | sh";

  it("carries the install command on a line of its own", () => {
    const lines = openClawInstallMessage(command).split("\n");
    expect(lines.at(-1)).toBe(command);
  });

  it("asks OpenClaw to report the hooks self-check", () => {
    const message = openClawInstallMessage(command);
    // The install script prints "Speakeasy hooks: PASS" or "...: FAIL: <why>".
    expect(message).toContain('"Speakeasy hooks:"');
    expect(message).toContain("PASS or FAIL");
  });
});
