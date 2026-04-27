import type { ExternalMCPServer } from "@gram/client/models/components";
import { describe, expect, it } from "vitest";
import { buildCollectionMcpJson, formatMcpJson } from "./mcp-json";

function makeServer(
  overrides: Partial<ExternalMCPServer> = {},
): ExternalMCPServer {
  return {
    description: "Test server",
    registrySpecifier: "test/server",
    version: "1.0.0",
    ...overrides,
  };
}

describe("buildCollectionMcpJson", () => {
  it("serializes collection servers with usable remote endpoints", () => {
    const result = buildCollectionMcpJson([
      makeServer({
        title: "CRM",
        remotes: [
          {
            transportType: "streamable-http",
            url: "https://app.getgram.ai/mcp/crm",
          },
        ],
      }),
    ]);

    expect(result).toEqual({
      config: {
        mcpServers: {
          CRM: {
            type: "http",
            url: "https://app.getgram.ai/mcp/crm",
            headers: {
              Authorization: "Bearer ${GRAM_API_KEY}",
            },
          },
        },
      },
      includedCount: 1,
      excludedCount: 0,
      excludedServers: [],
    });
  });

  it("excludes servers without a usable remote URL", () => {
    const excluded = makeServer({
      title: "Missing endpoint",
      remotes: [{ transportType: "streamable-http", url: " " }],
    });

    const result = buildCollectionMcpJson([
      makeServer({
        title: "Included",
        remotes: [
          {
            transportType: "streamable-http",
            url: "https://app.getgram.ai/mcp/included",
          },
        ],
      }),
      excluded,
      makeServer({ title: "No remotes" }),
    ]);

    expect(result.includedCount).toBe(1);
    expect(result.excludedCount).toBe(2);
    expect(result.excludedServers).toEqual([excluded, expect.any(Object)]);
    expect(result.config.mcpServers).toHaveProperty("Included");
    expect(result.config.mcpServers).not.toHaveProperty("Missing endpoint");
    expect(result.config.mcpServers).not.toHaveProperty("No remotes");
  });

  it("prefers streamable-http remotes and preserves duplicate display names", () => {
    const result = buildCollectionMcpJson([
      makeServer({
        title: "Duplicate",
        registrySpecifier: "test/one",
        remotes: [
          {
            transportType: "sse",
            url: "https://app.getgram.ai/mcp/sse",
          },
          {
            transportType: "streamable-http",
            url: "https://app.getgram.ai/mcp/http",
          },
        ],
      }),
      makeServer({
        title: "Duplicate",
        registrySpecifier: "test/two",
        remotes: [
          {
            transportType: "streamable-http",
            url: "https://app.getgram.ai/mcp/second",
          },
        ],
      }),
    ]);

    expect(result.config.mcpServers.Duplicate.url).toBe(
      "https://app.getgram.ai/mcp/http",
    );
    expect(result.config.mcpServers["Duplicate (2)"].url).toBe(
      "https://app.getgram.ai/mcp/second",
    );
  });

  it("includes remote header metadata when generating configs", () => {
    const result = buildCollectionMcpJson([
      makeServer({
        title: "Livestorm",
        remotes: [
          {
            transportType: "streamable-http",
            url: "https://app.getgram.ai/mcp/livestorm",
            headers: [
              {
                name: "MCP-Livestorm-API-Key",
                placeholder: "${MCP_LIVESTORM_API_KEY}",
                isRequired: true,
                isSecret: true,
              },
            ],
          },
        ],
      }),
    ]);

    expect(result.config.mcpServers.Livestorm.headers).toEqual({
      Authorization: "Bearer ${GRAM_API_KEY}",
      "MCP-Livestorm-API-Key": "${MCP_LIVESTORM_API_KEY}",
    });
  });
});

describe("formatMcpJson", () => {
  it("formats generated configs as pretty JSON", () => {
    expect(
      formatMcpJson({
        mcpServers: {
          CRM: {
            type: "http",
            url: "https://app.getgram.ai/mcp/crm",
            headers: { Authorization: "Bearer ${GRAM_API_KEY}" },
          },
        },
      }),
    ).toContain('\n  "mcpServers": {');
  });
});
