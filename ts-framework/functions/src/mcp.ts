import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type {
  Gram,
  Manifest,
  ManifestResource,
  ManifestTool,
  ManifestVariables,
} from "./framework.ts";
import {
  McpError,
  ErrorCode,
  ListToolsRequestSchema,
  type ListToolsResult,
  CallToolRequestSchema,
  type CallToolResult,
} from "@modelcontextprotocol/sdk/types.js";
import { Server } from "@modelcontextprotocol/sdk/server";

export interface WrappedMCPServer {
  handleToolCall(call: {
    name: string;
    input?: Record<string, unknown>;
    _meta?: Record<string, unknown>;
  }): Promise<Response>;

  handleResources(call: {
    uri: string;
    input: string;
    _meta?: Record<string, unknown>;
  }): Promise<Response>;

  manifest(): Manifest;
}

/**
 * Wraps an MCP server and exposes it as a Gram Function.
 */
export async function withGram(
  server: McpServer | Server,
  options?: {
    /**
     * Lists the environment variables that can be be passed by Gram when
     * calling tools and resources from the provided server. These will be
     * presented on the dashboard to be filled in by users and presented in the
     * generated MCP bundles and installation instructions.
     */
    variables?: ManifestVariables;
  },
): Promise<WrappedMCPServer> {
  const [serverTransport, clientTransport] =
    InMemoryTransport.createLinkedPair();

  await server.connect(serverTransport);

  const client = new Client({ name: "gram-functions-mcp", version: "0.0.0" });
  await client.connect(clientTransport);

  let tools = await collectTools(client, options?.variables);
  let resources = await collectResources(client, options?.variables);

  async function handleToolCall(call: {
    name: string;
    input?: Record<string, unknown>;
    _meta?: Record<string, unknown>;
  }): Promise<Response> {
    const response = await client.callTool({
      name: call.name,
      arguments: call.input,
      _meta: call._meta,
    });

    const body = JSON.stringify(response);
    return new Response(body, {
      status: 200,
      headers: { "Content-Type": "application/json; mcp=tools_call" },
    });
  }

  async function handleResources(call: {
    uri: string;
    input: string;
    _meta?: Record<string, unknown>;
  }): Promise<Response> {
    const response = await client.readResource({
      uri: call.uri,
      _meta: call._meta,
    });

    const body = JSON.stringify(response);
    return new Response(body, {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }

  function manifest(): Manifest {
    return {
      version: "0.0.0",
      tools,
      resources,
    };
  }

  return {
    handleToolCall,
    handleResources,
    manifest,
  };
}

async function collectTools(
  client: Client,
  variables?: ManifestVariables,
): Promise<Manifest["tools"]> {
  try {
    const res = await client.listTools();
    return res.tools.map((tool) => {
      let gramTool: ManifestTool = {
        name: tool.name,
        description: tool.description,
        inputSchema: tool.inputSchema,
        annotations: tool.annotations
          ? {
              title: tool.annotations.title,
              readOnlyHint: tool.annotations.readOnlyHint,
              destructiveHint: tool.annotations.destructiveHint,
              idempotentHint: tool.annotations.idempotentHint,
              openWorldHint: tool.annotations.openWorldHint,
            }
          : undefined,
        variables: variables,
        meta: {
          ...tool._meta,
          "gram.ai/kind": "mcp-passthrough",
        },
      };
      return gramTool;
    });
  } catch (err) {
    if (err instanceof McpError && err.code === ErrorCode.MethodNotFound) {
      console.warn("No tools registered");
    } else {
      throw err;
    }
    return [];
  }
}

async function collectResources(
  client: Client,
  variables?: ManifestVariables,
): Promise<ManifestResource[]> {
  try {
    const resourcesResponse = await client.listResources();
    return resourcesResponse.resources.map((resource) => {
      let gramResource: ManifestResource = {
        name: resource.name,
        description: resource.description,
        uri: resource.uri,
        mimeType: resource.mimeType,
        title: resource.title,
        variables,
        meta: {
          ...resource._meta,
          "gram.ai/kind": "mcp-passthrough",
        },
      };

      return gramResource;
    });
  } catch (err) {
    if (err instanceof McpError && err.code === ErrorCode.MethodNotFound) {
      console.warn("No tools registered");
    } else {
      throw err;
    }
  }

  return [];
}

/**
 * Creates a low-level MCP server from a Gram instance.
 */
export function fromGram(
  g: Gram,
  options: { name: string; version: string },
): Server {
  const { name, version } = options;

  const structuredLike = /\b(yaml|yml|json|toml|xml|xhtml)\b/i;
  const textLike = /^text\//i;
  const imageLike = /^image\//i;
  const audioLike = /^audio\//i;

  const server = new Server(
    { name, version },
    {
      capabilities: {
        tools: {},
      },
    },
  );

  server.setRequestHandler(
    ListToolsRequestSchema,
    async (): Promise<ListToolsResult> => {
      const tools = (g.manifest().tools || []).map((t) => {
        return {
          name: t.name,
          description: t.description,
          inputSchema: t.inputSchema,
          annotations: t.annotations
            ? {
                title: t.annotations.title,
                readOnlyHint: t.annotations.readOnlyHint,
                destructiveHint: t.annotations.destructiveHint,
                idempotentHint: t.annotations.idempotentHint,
                openWorldHint: t.annotations.openWorldHint,
              }
            : undefined,
        };
      }) as ListToolsResult["tools"];

      return {
        tools,
      };
    },
  );

  server.setRequestHandler(
    CallToolRequestSchema,
    async (req, extra): Promise<CallToolResult> => {
      const { name, arguments: args } = req.params;

      const resp = (await g.handleToolCall({ name, input: args } as any, {
        signal: extra.signal,
      })) as Response;

      let ctype = resp.headers.get("Content-Type") || "";
      ctype = ctype.split(";")[0]?.trim() || "";

      switch (true) {
        case textLike.test(ctype) || structuredLike.test(ctype): {
          const text = await resp.text();
          return {
            content: [{ type: "text", text }],
          };
        }
        case imageLike.test(ctype): {
          return {
            content: [
              {
                type: "image",
                mimeType: ctype,
                data: await responseToBase64(resp),
              },
            ],
          };
        }
        case audioLike.test(ctype): {
          return {
            content: [
              {
                type: "audio",
                mimeType: ctype,
                data: await responseToBase64(resp),
              },
            ],
          };
        }
        default: {
          return {
            isError: true,
            content: [
              {
                type: "text",
                text: `Unhandled content type: ${ctype}. Create a handler for this type in the MCP server.`,
              },
            ],
          };
        }
      }
    },
  );

  return server;
}

async function responseToBase64(resp: Response): Promise<string> {
  const blob = await resp.arrayBuffer();
  const buffer = Buffer.from(blob);
  return buffer.toString("base64");
}
