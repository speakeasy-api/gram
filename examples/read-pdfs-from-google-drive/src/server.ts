import * as mcp from "@gram-ai/functions/mcp";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import pkg from "../package.json" with { type: "json" };
import gram from "./gram.ts";

async function run() {
  const server = mcp.fromGram(gram, { name: pkg.name, version: pkg.version });

  // Test the search functionality (will trigger OAuth flow if needed)
  try {
    const response = await gram.handleToolCall({
      name: "search_files",
      input: {
        query: "test",
      },
    });
    if (response.ok) {
      console.log("✅ Search successful:", await response.json());
    } else {
      console.error("❌ Search failed:", await response.json());
    }
  } catch (error) {
    console.error("Error testing tool:", error);
  }

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
