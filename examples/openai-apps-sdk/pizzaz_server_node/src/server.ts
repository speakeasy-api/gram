import crypto from "node:crypto";

import express from "express";
import type { Request, Response } from "express";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { createPizzazServer } from "./mcp-server.ts";

async function main() {
  const PORT = process.env.PORT || 3000;
  const app = express();
  const server = createPizzazServer();

  // Enable CORS
  app.use((req, res, next) => {
    res.setHeader("Access-Control-Allow-Origin", "*");
    res.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    res.setHeader("Access-Control-Allow-Headers", "Content-Type, Mcp-Protocol-Version");
    if (req.method === "OPTIONS") {
      res.sendStatus(200);
      return;
    }
    next();
  });

  // GET endpoint for /mcp - not allowed
  app.get("/mcp", (_req: Request, res: Response) => {
    res.status(405).json({ error: "Method Not Allowed" });
  });

  // POST endpoint for /mcp
  app.post("/mcp", express.json(), async (req: Request, res: Response) => {
    console.error("Received POST to /mcp");

    try {
      // Check if this is an initialization request
      const messages = Array.isArray(req.body) ? req.body : [req.body];
      const isInitRequest = messages.some((msg) => msg?.method === "initialize");

      // Get or generate session ID
      let sessionId = req.headers["mcp-session-id"] as string | undefined;

      if (!sessionId && isInitRequest) {
        // New session - generate ID
        sessionId = crypto.randomUUID();
        console.error(`Generated new session ID: ${sessionId}`);
      } else if (sessionId) {
        console.error(`Reusing session ID: ${sessionId}`);
      }

      // Use stateless mode - no session validation
      const transport = new StreamableHTTPServerTransport({
        sessionIdGenerator: undefined,
        enableJsonResponse: true
      });

      // Manually set the session ID for sticky sessions
      if (sessionId) {
        transport.sessionId = sessionId;
      }

      await server.connect(transport);
      await transport.handleRequest(req, res, req.body);

      res.on("close", () => {
        console.error("Request closed");
        transport.close();
      });
    } catch (error) {
      console.error("Error handling MCP request:", error);
      if (!res.headersSent) {
        res.status(500).json({
          jsonrpc: "2.0",
          error: {
            code: -32603,
            message: "Internal server error",
          },
          id: null,
        });
      }
    }
  });

  app.listen(PORT, () => {
    console.error(`MCP Demo Server running on http://localhost:${PORT}`);
    console.error(`StreamableHTTP endpoint: http://localhost:${PORT}/mcp`);
  });
}

main().catch((error) => {
  console.error("Fatal error:", error);
  process.exit(1);
});
