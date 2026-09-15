// DNO-1090 spike. Throwaway: this is not the shipping implementation.
//
// Pi ships no MCP client, so the MCP half of a Gram plugin — a generated
// mcp.json — has nothing to attach to. This extension is that missing half.
// It reads a Gram-generated mcp.json, speaks MCP streamable HTTP itself, and
// republishes each discovered tool through pi.registerTool() so Pi's agent
// loop can call it. The observability half rides the same module through
// pi.on("tool_call") / pi.on("tool_result").
//
// Deliberately dependency-free (fetch only, no MCP SDK) to check whether a
// generated Pi package can be self-contained, the way Gram's other generated
// packages are.
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { appendFileSync, existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

type GramServer = {
  url?: string;
  // Gram spells the header bag differently per host dialect: "headers" for
  // Claude/Cursor/OpenCode/Agent Plugins, "http_headers" for Codex.
  headers?: Record<string, string>;
  http_headers?: Record<string, string>;
};

const auditPath = process.env.GRAM_SPIKE_AUDIT ?? "";

function audit(record: unknown): void {
  if (auditPath) appendFileSync(auditPath, JSON.stringify(record) + "\n");
}

// Gram emits one config per host dialect, but the payload is always the same
// three fields. Reading every spelling Gram already generates lets the spike
// consume real generator output instead of a Pi-only format.
function readGramConfig(path: string): Record<string, GramServer> {
  if (!existsSync(path)) return {};
  const doc = JSON.parse(readFileSync(path, "utf8")) as {
    mcpServers?: Record<string, GramServer>;
    mcp?: Record<string, GramServer>;
  };
  return doc.mcpServers ?? doc.mcp ?? {};
}

const ENV_REF = /\$\{(?:env:)?([A-Z0-9_]+)\}/g;

// Per-org packages inline the key; published ones defer to ${env:GRAM_API_KEY}.
// An unresolved reference drops the header rather than sending an empty
// credential, so the request fails at the server instead of on the wire —
// the rule Gram's OpenCode loader already applies.
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
    if (!missing) resolved[name] = expanded;
  }
  return resolved;
}

class StreamableHTTPClient {
  private sessionId?: string;
  private nextId = 1;

  constructor(
    private readonly url: string,
    private readonly headers: Record<string, string>,
  ) {}

  private async rpc(
    method: string,
    params?: unknown,
    notify = false,
  ): Promise<any> {
    const body: Record<string, unknown> = { jsonrpc: "2.0", method };
    if (params !== undefined) body.params = params;
    if (!notify) body.id = this.nextId++;

    const response = await fetch(this.url, {
      method: "POST",
      headers: {
        ...this.headers,
        "content-type": "application/json",
        accept: "application/json, text/event-stream",
        ...(this.sessionId ? { "mcp-session-id": this.sessionId } : {}),
      },
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      throw new Error(
        `${method}: HTTP ${response.status} ${await response.text()}`,
      );
    }

    const issued = response.headers.get("mcp-session-id");
    if (issued) this.sessionId = issued;
    if (notify || response.status === 202) return undefined;

    const raw = await response.text();
    // A streamable-HTTP server may answer with a bare JSON body or with an SSE
    // stream carrying the same JSON-RPC envelope.
    const payload = response.headers
      .get("content-type")
      ?.includes("text/event-stream")
      ? JSON.parse(
          raw
            .split("\n")
            .filter((line) => line.startsWith("data:"))
            .map((line) => line.slice(5).trim())
            .find((data) => data && data !== "[DONE]") ?? "{}",
        )
      : JSON.parse(raw);
    if (payload.error)
      throw new Error(`${method}: ${JSON.stringify(payload.error)}`);
    return payload.result;
  }

  async connect(): Promise<void> {
    await this.rpc("initialize", {
      protocolVersion: "2025-06-18",
      capabilities: {},
      clientInfo: { name: "gram-pi-bridge", version: "0.0.1" },
    });
    await this.rpc("notifications/initialized", undefined, true);
  }

  listTools(): Promise<{
    tools: Array<{ name: string; description?: string; inputSchema: unknown }>;
  }> {
    return this.rpc("tools/list");
  }

  callTool(
    name: string,
    args: unknown,
  ): Promise<{ content: unknown; isError?: boolean }> {
    return this.rpc("tools/call", { name, arguments: args });
  }
}

export default function gramBridge(pi: ExtensionAPI) {
  // Resolve the config relative to this module so an installed package works
  // from any cwd, the way Gram's OpenCode loader does.
  const packageRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
  const configPath =
    process.env.GRAM_MCP_CONFIG ?? join(packageRoot, "mcp.json");

  // Stands in for a Gram policy verdict (RBAC, spend gate, quarantine) to
  // check that a deny can actually stop a call on Pi.
  const denied = (process.env.GRAM_SPIKE_DENY ?? "").split(",").filter(Boolean);

  // Gram attributes MCP tool calls by parsing the host's tool-name convention
  // (server/internal/toolref: "mcp__<server>__<tool>" or "MCP:<tool>"). Because
  // the bridge chooses the name it registers, Pi can be made to emit a
  // convention toolref already understands. "mcp-prefixed" checks that Pi
  // accepts such a name at all.
  const namePrefix = process.env.GRAM_SPIKE_NAME_STYLE === "mcp-prefixed";

  // Long-lived resources belong in session_start, not the factory: Pi loads
  // extensions in invocations that never open a session.
  pi.on("session_start", async (_event, ctx) => {
    const startedAt = Date.now();

    for (const [serverName, server] of Object.entries(
      readGramConfig(configPath),
    )) {
      if (!server.url) continue;

      const client = new StreamableHTTPClient(
        server.url,
        resolveHeaders(server),
      );
      try {
        await client.connect();
      } catch (error) {
        // Gram servers that authenticate over OAuth also land here: this spike
        // implements no authorization code flow.
        audit({
          kind: "connect_failed",
          server: serverName,
          error: String(error),
        });
        ctx.ui.notify(`Gram: ${serverName} unavailable`, "error");
        continue;
      }

      const { tools } = await client.listTools();
      for (const tool of tools) {
        const registeredName = namePrefix
          ? `mcp__${serverName}__${tool.name}`
          : tool.name;
        pi.registerTool({
          name: registeredName,
          label: `Gram · ${tool.name}`,
          description:
            tool.description ?? `${serverName} tool proxied from Gram`,
          // MCP advertises JSON Schema and Pi's typebox parameters are JSON
          // Schema at runtime, so the discovered schema passes straight
          // through with no translation layer.
          parameters: tool.inputSchema as never,
          async execute(_toolCallId, params) {
            const result = await client.callTool(tool.name, params);
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
        tools: tools.map((tool) =>
          namePrefix ? `mcp__${serverName}__${tool.name}` : tool.name,
        ),
        elapsedMs: Date.now() - startedAt,
      });
      ctx.ui.notify(`Gram: ${tools.length} tools from ${serverName}`, "info");
    }
  });

  // Gram's other hosts spend a whole second package on the observability half.
  // On Pi it is a pair of handlers on the same event bus, and the deny covers
  // Pi's built-in tools too, not just the bridged ones.
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
}
