// Pure event -> canonical Gram hook payload mapping. No network, no opencode
// SDK types imported: local structural types keep this decoupled from
// @opencode-ai/plugin's exact (and still-moving) type surface, and match
// only the fields we actually read off opencode's hook inputs/outputs.
//
// Keep `raw_event_name` strings in lockstep with
// server/internal/hooks/events.go's parseOpencodeHookEvent.

export type CanonicalEventType =
  | "session.started"
  | "session.ended"
  | "prompt.submitted"
  | "tool.requested"
  | "tool.completed"
  | "tool.failed"
  | "assistant.responded";

export type IngestBody = {
  schema_version: "hook.ingest.v1";
  idempotency_key: string;
  source: {
    adapter: "opencode";
    adapter_version: string;
    raw_event_name: string;
    user_email?: string;
    // Machine hostname. Drives the "origin" (device) tier of the employee
    // data-flow graph (gram.hook.hostname on the server).
    hostname?: string;
  };
  // model rides here so ingest stamps gen_ai.response.model (Model Usage widget).
  session: { id: string; cwd?: string; model?: string };
  event: { type: CanonicalEventType; occurred_at: string };
  data?: {
    prompt?: { text: string };
    tool_call?: {
      id?: string;
      name: string;
      input?: unknown;
      output?: unknown;
      error?: unknown;
      permission_type?: string;
    };
    // Structured MCP server identity for an MCP tool call. Lets the ingest
    // pipeline (canonicalMCPData in server/internal/hooks) classify the call as
    // an MCP server and resolve gram-hosted vs shadow by URL — the same path
    // Claude Code / Codex / Cursor use — instead of inferring from the name.
    mcp?: McpBlock;
    // Token/cost usage for an assistant turn. Feeds the token totals, cost, and
    // token time-series widgets (gen_ai.usage.* on the server).
    usage?: UsageBlock;
    message?: { text: string; role: string };
  };
};

// Wire shape of the ingest payload's data.usage block (HookUsageData in the Goa
// design). All fields optional; omitted when opencode doesn't report them.
export type UsageBlock = {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  cost?: number;
};

// Identity of a configured MCP server, resolved from opencode's config.mcp.
// Remote servers carry a url; local (stdio) servers carry a command.
export interface McpServer {
  url?: string;
  command?: string;
}

// Wire shape of the ingest payload's data.mcp block (HookMCPData in the Goa
// design). server_name is always set; url/command are set per transport.
export type McpBlock = {
  server_name: string;
  url?: string;
  command?: string;
};

export interface Ctx {
  directory: string;
  fallbackSession: string;
  adapterVersion: string;
  userEmail?: string;
  // Machine hostname (os.hostname()), attached to every event's source.
  hostname?: string;
  // Configured MCP servers keyed by name (from opencode config.mcp), used to
  // normalize MCP tool-call names into Gram's canonical form and to attach the
  // server identity block. Undefined/empty disables both (fail-open).
  mcpServers?: ReadonlyMap<string, McpServer>;
}

// resolveMcpTool matches an opencode tool name against the configured MCP
// servers. opencode names an MCP tool call "<server>_<tool>" (single
// underscore, e.g. "context7_query-docs"); Gram's shadow-MCP scanner and MCP
// attribution key off Claude Code's "mcp__<server>__<tool>" form plus a
// structured data.mcp block (toolref / canonicalMCPData on the server). Returns
// the canonical name and that block, or null for native tools (prefix matches
// no configured server). Longest matching prefix wins so a server name
// containing "_" (e.g. "my_server") isn't mis-split on the first underscore.
export function resolveMcpTool(
  rawName: string,
  mcpServers: ReadonlyMap<string, McpServer> | undefined,
): { name: string; mcp: McpBlock } | null {
  if (!mcpServers || mcpServers.size === 0) return null;
  let best = "";
  for (const server of mcpServers.keys()) {
    if (rawName.startsWith(`${server}_`) && server.length > best.length) {
      best = server;
    }
  }
  if (best === "") return null;
  const fn = rawName.slice(best.length + 1);
  if (!fn) return null;
  const meta = mcpServers.get(best) ?? {};
  return {
    name: `mcp__${best}__${fn}`,
    mcp: {
      server_name: best,
      ...(meta.url ? { url: meta.url } : {}),
      ...(meta.command ? { command: meta.command } : {}),
    },
  };
}

function base(
  type: CanonicalEventType,
  rawEventName: string,
  session: { id?: string; cwd?: string; model?: string },
  data: IngestBody["data"],
  ctx: Ctx,
): IngestBody {
  return {
    schema_version: "hook.ingest.v1",
    idempotency_key: crypto.randomUUID(),
    source: {
      adapter: "opencode",
      adapter_version: ctx.adapterVersion,
      raw_event_name: rawEventName,
      ...(ctx.userEmail ? { user_email: ctx.userEmail } : {}),
      ...(ctx.hostname ? { hostname: ctx.hostname } : {}),
    },
    session: {
      id: session.id || ctx.fallbackSession,
      cwd: session.cwd ?? ctx.directory,
      ...(session.model ? { model: session.model } : {}),
    },
    event: { type, occurred_at: new Date().toISOString() },
    data,
  };
}

export function sessionStarted(
  session: { id: string; directory?: string },
  ctx: Ctx,
): IngestBody {
  return base(
    "session.started",
    "session.created",
    { id: session.id, cwd: session.directory },
    undefined,
    ctx,
  );
}

export function sessionEnded(
  sessionID: string,
  rawEventName: "session.idle" | "session.deleted",
  ctx: Ctx,
): IngestBody {
  return base("session.ended", rawEventName, { id: sessionID }, undefined, ctx);
}

export function toolRequested(
  input: { tool: string; sessionID: string; callID: string },
  args: unknown,
  ctx: Ctx,
): IngestBody {
  const mcp = resolveMcpTool(input.tool, ctx.mcpServers);
  return base(
    "tool.requested",
    "tool.execute.before",
    { id: input.sessionID },
    {
      tool_call: {
        id: input.callID,
        name: mcp?.name ?? input.tool,
        input: args,
      },
      ...(mcp ? { mcp: mcp.mcp } : {}),
    },
    ctx,
  );
}

export function toolCompleted(
  input: { tool: string; sessionID: string; callID: string; args?: unknown },
  output: { output?: unknown } | undefined,
  ctx: Ctx,
): IngestBody {
  const mcp = resolveMcpTool(input.tool, ctx.mcpServers);
  return base(
    "tool.completed",
    "tool.execute.after",
    { id: input.sessionID },
    {
      tool_call: {
        id: input.callID,
        name: mcp?.name ?? input.tool,
        input: input.args,
        output: output?.output,
      },
      ...(mcp ? { mcp: mcp.mcp } : {}),
    },
    ctx,
  );
}

// Synthesized from a message.part.updated event carrying a tool part whose
// state transitioned to "error" — opencode's tool.execute.after hook has no
// error field on its output, so a failed tool call surfaces here instead.
export function toolFailed(
  part: {
    sessionID: string;
    callID: string;
    tool: string;
    state: { input?: unknown; error?: string };
  },
  ctx: Ctx,
): IngestBody {
  const mcp = resolveMcpTool(part.tool, ctx.mcpServers);
  return base(
    "tool.failed",
    "tool.execute.error",
    { id: part.sessionID },
    {
      tool_call: {
        id: part.callID,
        name: mcp?.name ?? part.tool,
        input: part.state.input,
        error: part.state.error,
      },
      ...(mcp ? { mcp: mcp.mcp } : {}),
    },
    ctx,
  );
}

export function promptSubmitted(
  message: { sessionID: string },
  text: string,
  ctx: Ctx,
): IngestBody {
  return base(
    "prompt.submitted",
    "message.submitted",
    { id: message.sessionID },
    { prompt: { text } },
    ctx,
  );
}

export function assistantResponded(
  message: {
    sessionID: string;
    modelID?: string;
    cost?: number;
    tokens?: {
      input?: number;
      output?: number;
      cache?: { read?: number; write?: number };
    };
  },
  text: string,
  ctx: Ctx,
): IngestBody {
  const usage = usageFromMessage(message);
  return base(
    "assistant.responded",
    "message.completed",
    { id: message.sessionID, model: message.modelID },
    {
      message: { text, role: "assistant" },
      ...(usage ? { usage } : {}),
    },
    ctx,
  );
}

// usageFromMessage maps opencode's assistant-message token/cost fields onto the
// ingest data.usage block, dropping fields opencode didn't report. Returns
// undefined when nothing usable is present so the block is omitted entirely.
function usageFromMessage(message: {
  cost?: number;
  tokens?: {
    input?: number;
    output?: number;
    cache?: { read?: number; write?: number };
  };
}): UsageBlock | undefined {
  const t = message.tokens;
  const usage: UsageBlock = {};
  if (t?.input != null) usage.input_tokens = t.input;
  if (t?.output != null) usage.output_tokens = t.output;
  if (t?.cache?.read != null) usage.cache_read_tokens = t.cache.read;
  if (t?.cache?.write != null) usage.cache_write_tokens = t.cache.write;
  if (message.cost != null) usage.cost = message.cost;
  return Object.keys(usage).length > 0 ? usage : undefined;
}

// Mapped as a tool.requested event with permission_type set, matching how
// the ingest pipeline recognizes a permission ask (canonicalPermissionType
// in server/internal/hooks/ingest_hooks.go), not a standalone canonical
// event type.
export function permissionAsked(
  permission: { id: string; sessionID: string; type: string; callID?: string },
  ctx: Ctx,
): IngestBody {
  return base(
    "tool.requested",
    "permission.asked",
    { id: permission.sessionID },
    {
      tool_call: {
        id: permission.callID ?? permission.id,
        name: permission.type,
        permission_type: permission.type,
      },
    },
    ctx,
  );
}
