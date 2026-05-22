import { afterEach, describe, expect, it, vi } from "vitest";

import {
  isMcpOAuthRequired,
  mcpOAuthProtectedResourceMetadataUrl,
} from "./mcpOAuth";

describe("MCP OAuth discovery", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("builds the protected-resource metadata URL from the MCP URL", () => {
    expect(
      mcpOAuthProtectedResourceMetadataUrl(
        "https://app.getgram.ai/mcp/acme/default",
      ),
    ).toBe(
      "https://app.getgram.ai/.well-known/oauth-protected-resource/mcp/acme/default",
    );
  });

  it("reports OAuth required when metadata advertises authorization servers", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          authorization_servers: ["https://auth.example.com"],
        }),
      ),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      isMcpOAuthRequired("https://app.getgram.ai/mcp/acme"),
    ).resolves.toBe(true);
    expect(fetchMock).toHaveBeenCalledWith(
      "https://app.getgram.ai/.well-known/oauth-protected-resource/mcp/acme",
      { headers: { Accept: "application/json" } },
    );
  });

  it("treats 404 and invalid metadata as OAuth not required", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(new Response("", { status: 404 }))
        .mockResolvedValueOnce(new Response(JSON.stringify({}))),
    );

    await expect(
      isMcpOAuthRequired("https://app.getgram.ai/mcp/missing"),
    ).resolves.toBe(false);
    await expect(
      isMcpOAuthRequired("https://app.getgram.ai/mcp/public"),
    ).resolves.toBe(false);
  });

  it("treats invalid MCP URLs as OAuth not required", async () => {
    await expect(isMcpOAuthRequired("not a URL")).resolves.toBe(false);
  });
});
