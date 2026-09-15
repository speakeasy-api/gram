// DNO-1090 spike. Throwaway: this is not the shipping implementation.
//
// Same bridge as gram-bridge.ts, but built on @modelcontextprotocol/sdk
// instead of hand-rolled fetch. Kept alongside the dependency-free variant to
// show the cost difference: this one is shorter and gets transport
// negotiation for free, but obliges the generated Pi package to carry an npm
// dependency that Pi installs on the user's machine.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { appendFileSync, existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

type GramServer = {
  type?: string;
  url?: string;
  headers?: Record<string, string>;
  http_headers?: Record<string, string>;
};

// Gram emits one config per host dialect but the payload is the same three
// fields. Accept every key spelling Gram already generates today
// (Claude/Cursor/Agent Plugins use "mcpServers", OpenCode uses "mcp";
// Codex spells headers "http_headers") so the spike consumes real Gram output
// rather than a Pi-only format.
function readGramConfig(path: string): Record<string, GramServer> {
  if (!existsSync(path)) return {};
  const doc = JSON.parse(readFileSync(path, "utf8")) as {
    mcpServers?: Record<string, GramServer>;
    mcp?: Record<string, GramServer>;
  };
  return doc.mcpServers ?? doc.mcp ?? {};
}

const ENV_REF = /\$\{(?:env:)?([A-Z0-9_]+)\}/g;

function resolveHeaders(server: GramServer): Record<string, string> {
  const raw = { ...(server.headers ?? {}), ...(server.http_headers ?? {}) };
  const resolved: Record<string, string> = {};
  for (const [name, value] of Object.entries(raw)) {
    let missing = false;
    const expanded = value.replace(ENV_REF, (_match, variable: string) => {
      const fromEnv = process.env[variable];
      if (!fromEnv) missing = true;
      return fromEnv ?? "";
    });
    // Drop rather than send an empty credential, matching the rule Gram's
    // OpenCode loader already applies: fail at the server, not on the wire.
    if (!missing) resolved[name] = expanded;
  }
  return resolved;
}

const auditPath = process.env.GRAM_SPIKE_AUDIT ?? "";

function audit(record: unknown): void {
  if (!auditPath) return;
  appendFileSync(auditPath, JSON.stringify(record) + "\n");
}

export default function gramBridge(pi: ExtensionAPI) {
  const configPath =
    process.env.GRAM_MCP_CONFIG ?? join(process.cwd(), "mcp.json");
  const clients: Client[] = [];

  // Long-lived resources belong in session_start, not the factory: Pi loads
  // extensions in invocations that never open a session.
  pi.on("session_start", async (_event, ctx) => {
    const startedAt = Date.now();
    for (const [serverName, server] of Object.entries(
      readGramConfig(configPath),
    )) {
      if (!server.url) continue;

      const client = new Client({ name: "gram-pi-bridge", version: "0.0.1" });
      const headers = resolveHeaders(server);
      try {
        await client.connect(
          new StreamableHTTPClientTransport(new URL(server.url), {
            requestInit: { headers },
          }),
        );
      } catch (error) {
        // Gram servers that authenticate over OAuth land here: this spike
        // carries no authorization code flow.
        ctx.ui.notify(
          `Gram: ${serverName} unreachable (${String(error)})`,
          "error",
        );
        audit({
          kind: "connect_failed",
          server: serverName,
          error: String(error),
        });
        continue;
      }
      clients.push(client);

      const { tools } = await client.listTools();
      for (const tool of tools) {
        pi.registerTool({
          name: tool.name,
          label: `Gram · ${tool.name}`,
          description:
            tool.description ?? `${serverName} tool proxied from Gram`,
          // MCP advertises JSON Schema and Pi's typebox parameters are JSON
          // Schema at runtime, so the discovered schema passes straight
          // through with no translation layer.
          parameters: tool.inputSchema as never,
          async execute(_toolCallId, params) {
            const result = await client.callTool({
              name: tool.name,
              arguments: params as Record<string, unknown>,
            });
            return {
              content: result.content as never,
              details: { server: serverName, isError: result.isError ?? false },
            };
          },
        });
      }
      audit({
        kind: "registered",
        server: serverName,
        tools: tools.map((t) => t.name),
        elapsedMs: Date.now() - startedAt,
      });
      ctx.ui.notify(`Gram: ${tools.length} tools from ${serverName}`, "info");
    }
  });

  // The observability half rides the same module. Gram's other hosts spend a
  // whole second package on this; on Pi it is a handler on an event bus.
  //
  // GRAM_SPIKE_DENY stands in for a Gram policy verdict (RBAC, spend gate,
  // quarantine) to check that a deny can actually stop a call on Pi, and that
  // it covers Pi's own built-in tools, not just the bridged ones.
  const denied = (process.env.GRAM_SPIKE_DENY ?? "").split(",").filter(Boolean);

  pi.on("tool_call", async (event) => {
    audit({ kind: "tool_call", tool: event.toolName, input: event.input });
    if (denied.includes(event.toolName)) {
      audit({ kind: "blocked", tool: event.toolName });
      return { block: true, reason: "Blocked by Gram policy" };
    }
  });
  pi.on("tool_result", async (event) => {
    audit({
      kind: "tool_result",
      tool: event.toolName,
      isError: event.isError ?? false,
    });
  });

  pi.on("session_shutdown", async () => {
    for (const client of clients.splice(0)) await client.close();
  });
}
