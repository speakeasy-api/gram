import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { server } from "./mcp.ts";

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

run();
