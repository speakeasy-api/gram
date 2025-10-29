import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type CallToolResult,
  type ListToolsResult,
} from "@modelcontextprotocol/sdk/types.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import pkg from "../package.json" with { type: "json" };
import gram from "./gram.ts";

const structuredLike = /\b(yaml|yml|json|toml|xml|xhtml)\b/i;
const textLike = /^text\//i;
const imageLike = /^image\//i;
const audioLike = /^audio\//i;

export const server = new Server(
  {
    name: pkg.name,
    version: pkg.version,
  },
  {
    capabilities: {
      tools: {},
    },
  },
);

server.setRequestHandler(
  ListToolsRequestSchema,
  async (): Promise<ListToolsResult> => {
    const tools = gram.manifest().tools.map((t) => {
      return {
        name: t.name,
        description: t.description,
        inputSchema: t.inputSchema,
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

    const resp = await gram.handleToolCall({ name, input: args } as any, {
      signal: extra.signal,
    });

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

async function responseToBase64(resp: Response): Promise<string> {
  const blob = await resp.arrayBuffer();
  const buffer = Buffer.from(blob);
  return buffer.toString("base64");
}

async function run() {
  console.error("Starting MCP server with stdio...");
  const stdio = new StdioServerTransport();
  await server.connect(stdio);

  const quit = async () => {
    console.error("\nShutting down MCP server...");
    await server.close();
    process.exit(0);
  };
  process.once("SIGINT", quit);
  process.once("SIGTERM", quit);
}

if (import.meta.main) {
  run();
}
