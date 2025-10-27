import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";

export const server = new McpServer({
  name: "demo-server",
  version: "1.0.0",
});

server.registerTool(
  "add",
  {
    title: "Addition Tool",
    description: "Add two numbers",
    inputSchema: { a: z.number(), b: z.number() },
  },
  async ({ a, b }) => {
    const output = { result: a + b };
    return {
      content: [{ type: "text", text: JSON.stringify(output) }],
    };
  },
);

server.registerResource(
  "a-cool-photo",
  "resources://a-cool-photo",
  {
    mimeType: "image/jpg",
    description: "This photo is really something",
    title: "A Cool Photo",
  },
  async (uri) => {
    let res = await fetch("https://picsum.photos/200/300.jpg");
    return {
      contents: [
        {
          uri: uri.href,
          blob: Buffer.from(await res.arrayBuffer()).toString("base64"),
        },
      ],
    };
  },
);
