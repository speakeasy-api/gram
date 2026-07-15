import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type {
  Gram,
  Manifest,
  ManifestResource,
  ManifestTool,
  ManifestVariables,
  MCPClientInfo,
} from "./framework.ts";
import {
  McpError,
  ErrorCode,
  ListToolsRequestSchema,
  type ListToolsResult,
  CallToolRequestSchema,
  type CallToolResult,
  ListResourcesRequestSchema,
  type ListResourcesResult,
  ReadResourceRequestSchema,
  type ReadResourceResult,
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
 * Coerce an untrusted, self-reported client identity into an `MCPClientInfo`.
 * Accepts the shape used by both the MCP `initialize` handshake and the
 * `io.modelcontextprotocol/clientInfo` request-`_meta` hint. Returns `undefined`
 * unless a non-empty `name` string is present; `version` defaults to `""` when
 * absent so a client that reports only a name is still usable.
 */
function normalizeClientInfo(value: unknown): MCPClientInfo | undefined {
  if (value == null || typeof value !== "object") {
    return undefined;
  }
  const record = value as Record<string, unknown>;
  const name = record["name"];
  if (typeof name !== "string" || name === "") {
    return undefined;
  }
  const version = typeof record["version"] === "string" ? record["version"] : "";
  return { name, version };
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

  // fromGram snapshots the current manifest once; later Gram mutations are not
  // reflected in the MCP server handlers created here.
  const manifest = g.manifest();
  const hasResources =
    manifest.resources != null && manifest.resources.length > 0;

  const server = new Server(
    { name, version },
    {
      capabilities: {
        tools: {},
        ...(hasResources ? { resources: {} } : {}),
      },
    },
  );

  server.setRequestHandler(
    ListToolsRequestSchema,
    async (): Promise<ListToolsResult> => {
      const tools = (manifest.tools || []).map((t) => {
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
          ...(t.meta != null
            ? { _meta: t.meta as Record<string, unknown> }
            : {}),
        };
      }) as ListToolsResult["tools"];

      return {
        tools,
      };
    },
  );

  // Converts a tool's `Response` into an MCP `CallToolResult`. Shared between
  // the normal return path and the error path so that failures surfaced via
  // `ctx.fail()` (which reject with a `Response`) render identically.
  async function responseToCallToolResult(
    resp: Response,
  ): Promise<CallToolResult> {
    let ctype = resp.headers.get("Content-Type") || "";
    ctype = ctype.split(";")[0]?.trim() || "";

    switch (true) {
      case textLike.test(ctype) || structuredLike.test(ctype): {
        const text = await resp.text();
        return {
          content: [{ type: "text", text }],
          isError: !resp.ok,
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
          isError: !resp.ok,
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
          isError: !resp.ok,
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
  }

  server.setRequestHandler(
    CallToolRequestSchema,
    async (req, extra): Promise<CallToolResult> => {
      const { name, arguments: args } = req.params;

      // Identify the calling MCP client so tools can adapt to known callers via
      // `ctx.clientInfo`. Prefer the per-call hint carried in request `_meta`
      // (`io.modelcontextprotocol/clientInfo`); fall back to the identity
      // captured during the MCP `initialize` handshake. Self-reported metadata:
      // safe for observability/convenience, never for authorization.
      const clientInfo =
        normalizeClientInfo(
          req.params._meta?.["io.modelcontextprotocol/clientInfo"],
        ) ?? normalizeClientInfo(server.getClientVersion());

      let resp: Response;
      try {
        resp = (await g.handleToolCall({ name, input: args } as any, {
          signal: extra.signal,
          clientInfo,
        })) as Response;
      } catch (err) {
        // `ctx.fail()` and input validation failures reject with a `Response`
        // rather than returning one. Render it like any other tool response so
        // the failure surfaces as a normal `isError` result instead of an
        // opaque MCP "Internal Error" in clients like the Inspector (AGE-2779).
        if (err instanceof Response) {
          return responseToCallToolResult(err);
        }
        // Any other thrown value is an unexpected error in user code. Report
        // its message to the client instead of letting the transport surface a
        // generic internal error.
        return {
          isError: true,
          content: [
            {
              type: "text",
              text: err instanceof Error ? err.message : String(err),
            },
          ],
        };
      }

      return responseToCallToolResult(resp);
    },
  );

  if (hasResources) {
    server.setRequestHandler(
      ListResourcesRequestSchema,
      async (): Promise<ListResourcesResult> => {
        return {
          resources: (manifest.resources || []).map((r) => ({
            name: r.name,
            uri: r.uri,
            description: r.description,
            mimeType: r.mimeType,
            title: r.title,
          })),
        };
      },
    );

    server.setRequestHandler(
      ReadResourceRequestSchema,
      async (req): Promise<ReadResourceResult> => {
        const { uri } = req.params;
        const resp = await g.handleResourceRead({ uri });
        const text = await resp.text();
        return {
          contents: [
            {
              uri,
              mimeType: resp.headers.get("Content-Type") || "text/plain",
              text,
            },
          ],
        };
      },
    );
  }

  return server;
}

async function responseToBase64(resp: Response): Promise<string> {
  const blob = await resp.arrayBuffer();
  const buffer = Buffer.from(blob);
  return buffer.toString("base64");
}
