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
  };
  session: { id: string; cwd?: string };
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
    message?: { text: string; role: string };
  };
};

export interface Ctx {
  directory: string;
  fallbackSession: string;
  adapterVersion: string;
  userEmail?: string;
  // Configured MCP server names (keys of client.mcp.status()), used to
  // normalize opencode MCP tool-call names into Gram's canonical form.
  // Undefined/empty disables normalization (fail-open).
  mcpServers?: readonly string[];
}

// opencode names an MCP tool call "<server>_<tool>" (single underscore, e.g.
// "context7_query-docs"). Gram's shadow-MCP scanner and all MCP attribution
// only recognize Claude Code's "mcp__<server>__<tool>" form
// (toolref.IsMCPToolName in server/internal/toolref), so an un-normalized name
// is treated as a native tool and skipped by shadow-MCP detection. Rewrite
// opencode MCP calls into that form; native tools whose prefix matches no
// configured server are returned unchanged.
export function toGramToolName(
  rawName: string,
  mcpServers: readonly string[] | undefined,
): string {
  if (!mcpServers || mcpServers.length === 0) return rawName;
  // Longest matching server prefix wins so a server name containing "_"
  // (e.g. "my_server") isn't mis-split on the first underscore.
  let best = "";
  for (const server of mcpServers) {
    if (rawName.startsWith(`${server}_`) && server.length > best.length) {
      best = server;
    }
  }
  if (best === "") return rawName;
  const fn = rawName.slice(best.length + 1);
  return fn ? `mcp__${best}__${fn}` : rawName;
}

function base(
  type: CanonicalEventType,
  rawEventName: string,
  session: { id?: string; cwd?: string },
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
    },
    session: {
      id: session.id || ctx.fallbackSession,
      cwd: session.cwd ?? ctx.directory,
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
  return base(
    "tool.requested",
    "tool.execute.before",
    { id: input.sessionID },
    {
      tool_call: {
        id: input.callID,
        name: toGramToolName(input.tool, ctx.mcpServers),
        input: args,
      },
    },
    ctx,
  );
}

export function toolCompleted(
  input: { tool: string; sessionID: string; callID: string; args?: unknown },
  output: { output?: unknown } | undefined,
  ctx: Ctx,
): IngestBody {
  return base(
    "tool.completed",
    "tool.execute.after",
    { id: input.sessionID },
    {
      tool_call: {
        id: input.callID,
        name: toGramToolName(input.tool, ctx.mcpServers),
        input: input.args,
        output: output?.output,
      },
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
  return base(
    "tool.failed",
    "tool.execute.error",
    { id: part.sessionID },
    {
      tool_call: {
        id: part.callID,
        name: toGramToolName(part.tool, ctx.mcpServers),
        input: part.state.input,
        error: part.state.error,
      },
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
  message: { sessionID: string },
  text: string,
  ctx: Ctx,
): IngestBody {
  return base(
    "assistant.responded",
    "message.completed",
    { id: message.sessionID },
    { message: { text, role: "assistant" } },
    ctx,
  );
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
