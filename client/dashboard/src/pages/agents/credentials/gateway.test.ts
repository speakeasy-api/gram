import { afterEach, describe, expect, it, vi } from "vitest";
import {
  agentGatewayBase,
  agentGatewayURL,
  mintInstallCommand,
} from "./gateway";

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://app.example.com",
}));

const agentID = "0e3b6b1a-0000-4000-8000-000000000001";

describe("agentGatewayBase", () => {
  it("uses the custom domain when it has an IP allowlist", () => {
    expect(
      agentGatewayBase({
        domain: "mcp.example.com",
        ipAllowlist: ["203.0.113.0/24"],
      }),
    ).toBe("https://mcp.example.com");
  });

  it("keeps the platform host for an open custom domain", () => {
    expect(
      agentGatewayBase({ domain: "mcp.example.com", ipAllowlist: [] }),
    ).toBeUndefined();
  });

  it("keeps the platform host without a custom domain", () => {
    expect(agentGatewayBase(undefined)).toBeUndefined();
  });
});

describe("agentGatewayURL", () => {
  it("defaults to the platform host", () => {
    expect(agentGatewayURL(agentID)).toBe(
      `https://app.example.com/agent-mcp/${agentID}`,
    );
  });

  it("uses the given base", () => {
    expect(agentGatewayURL(agentID, "https://mcp.example.com")).toBe(
      `https://mcp.example.com/agent-mcp/${agentID}`,
    );
  });
});

describe("mintInstallCommand", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("mints on the platform host when the gateway is on a custom domain", async () => {
    const fetchMock = vi.fn(
      async () =>
        new Response(JSON.stringify({ code: "abc_123" }), { status: 201 }),
    );
    vi.stubGlobal("fetch", fetchMock);

    const command = await mintInstallCommand(
      `https://mcp.example.com/agent-mcp/${agentID}`,
      "secret",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      `https://app.example.com/agent-mcp/${agentID}/install-code`,
      expect.objectContaining({ method: "POST" }),
    );
    expect(command).toBe(
      "curl -fsSL https://app.example.com/agent-mcp/install/abc_123 | sh",
    );
  });
});
