import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import * as mcp from "@speakeasy-api/functions/mcp";

import pkg from "../package.json" with { type: "json" };
import functions from "./functions.ts";

async function run() {
  const server = mcp.fromFunctions(functions, {
    name: pkg.name,
    version: pkg.version,
  });

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
