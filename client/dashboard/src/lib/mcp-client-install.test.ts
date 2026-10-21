import { describe, expect, it } from "vitest";
import {
  claudeCodeAddCommand,
  codexAddCommand,
  codexLoginCommand,
  cursorInstallLink,
  openCodeConfig,
} from "./mcp-client-install";

const URL = "https://app.example.com/mcp/gateway/acme";

describe("mcp client install", () => {
  it("builds the Claude Code command", () => {
    expect(claudeCodeAddCommand("acme", URL)).toBe(
      `claude mcp add --transport http --scope user 'acme' '${URL}'`,
    );
  });

  it("breaks commands over continued lines for display", () => {
    expect(claudeCodeAddCommand("acme", URL, { multiline: true })).toBe(
      `claude mcp add --transport http --scope user \\\n  'acme' \\\n  '${URL}'`,
    );
    expect(codexAddCommand("acme", URL, { multiline: true })).toBe(
      `codex mcp add 'acme' \\\n  --url '${URL}'`,
    );
  });

  it("builds the Codex commands", () => {
    expect(codexAddCommand("acme", URL)).toBe(
      `codex mcp add 'acme' --url '${URL}'`,
    );
    expect(codexLoginCommand("acme")).toBe("codex mcp login 'acme'");
  });

  it("encodes the Cursor config as base64 JSON", () => {
    const link = new globalThis.URL(cursorInstallLink("acme", URL));
    expect(link.protocol).toBe("cursor:");
    expect(link.searchParams.get("name")).toBe("acme");
    expect(JSON.parse(atob(link.searchParams.get("config")!))).toEqual({
      url: URL,
    });
  });

  it("builds the OpenCode block", () => {
    expect(JSON.parse(openCodeConfig("acme", URL))).toEqual({
      mcp: { acme: { type: "remote", url: URL, enabled: true } },
    });
  });
});
