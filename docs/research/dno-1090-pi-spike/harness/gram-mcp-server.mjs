// Stand-in for a Gram-hosted MCP server. Mirrors the wire contract Gram's
// generated mcp.json points at: streamable HTTP on one URL, bearer auth in an
// Authorization header, tool names in Gram's <source>_<operation> style.
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { z } from "zod";

const PORT = Number(process.env.PORT ?? 8931);
const EXPECTED_AUTH = "Bearer gram-consumer-key";
const callLog = [];

function buildServer() {
  const server = new McpServer({ name: "gram-crm", version: "1.0.0" });

  server.registerTool(
    "crm_list_projects",
    {
      description: "List CRM projects visible to the caller.",
      inputSchema: { status: z.enum(["active", "archived"]).optional() },
    },
    async ({ status }) => {
      callLog.push({ tool: "crm_list_projects", status });
      const projects =
        status === "archived"
          ? ["Legacy Rollout"]
          : ["Apollo Migration", "Q3 Onboarding"];
      return {
        content: [
          {
            type: "text",
            text: `projects(${status ?? "active"}): ${projects.join(", ")}`,
          },
        ],
      };
    },
  );

  server.registerTool(
    "crm_create_task",
    {
      description: "Create a task inside a CRM project.",
      inputSchema: { project: z.string(), title: z.string() },
    },
    async ({ project, title }) => {
      callLog.push({ tool: "crm_create_task", project, title });
      return {
        content: [
          { type: "text", text: `created TASK-4821 "${title}" in ${project}` },
        ],
      };
    },
  );

  return server;
}

const transports = new Map();

const http = createServer(async (req, res) => {
  if (req.url === "/calls") {
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify(callLog));
    return;
  }

  // Gram's gateway rejects unauthenticated MCP traffic; so does this.
  if (req.headers.authorization !== EXPECTED_AUTH) {
    console.error(
      `[mcp] 401 missing/incorrect Authorization: ${req.headers.authorization ?? "<none>"}`,
    );
    res.writeHead(401, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: "unauthorized" }));
    return;
  }

  const sessionId = req.headers["mcp-session-id"];
  let transport = sessionId ? transports.get(sessionId) : undefined;

  if (!transport) {
    transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => randomUUID(),
      onsessioninitialized: (id) => transports.set(id, transport),
    });
    await buildServer().connect(transport);
  }

  await transport.handleRequest(req, res);
});

http.listen(PORT, () =>
  console.error(`[mcp] gram-shaped MCP server on :${PORT}`),
);
