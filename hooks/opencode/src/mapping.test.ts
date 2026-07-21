import { describe, expect, it } from "vitest";
import {
  assistantResponded,
  permissionAsked,
  promptSubmitted,
  resolveMcpTool,
  sessionEnded,
  sessionStarted,
  toolCompleted,
  toolFailed,
  toolRequested,
  type Ctx,
  type McpServer,
} from "./mapping.js";

const ctx: Ctx = {
  directory: "/repo",
  fallbackSession: "fallback-session",
  adapterVersion: "0.1.0",
};

describe("mapping", () => {
  it("sessionStarted -> session.started / session.created", () => {
    const body = sessionStarted({ id: "s1", directory: "/repo" }, ctx);
    expect(body.event.type).toBe("session.started");
    expect(body.source.raw_event_name).toBe("session.created");
    expect(body.source.adapter).toBe("opencode");
    expect(body.session).toEqual({ id: "s1", cwd: "/repo" });
    expect(body.schema_version).toBe("hook.ingest.v1");
    expect(body.idempotency_key).toBeTruthy();
  });

  it("sessionEnded(session.idle) -> session.ended / session.idle", () => {
    const body = sessionEnded("s1", "session.idle", ctx);
    expect(body.event.type).toBe("session.ended");
    expect(body.source.raw_event_name).toBe("session.idle");
    expect(body.session.id).toBe("s1");
  });

  it("sessionEnded(session.deleted) -> session.ended / session.deleted", () => {
    const body = sessionEnded("s1", "session.deleted", ctx);
    expect(body.event.type).toBe("session.ended");
    expect(body.source.raw_event_name).toBe("session.deleted");
  });

  it("toolRequested -> tool.requested / tool.execute.before", () => {
    const body = toolRequested(
      { tool: "bash", sessionID: "s1", callID: "c1" },
      { cmd: "ls" },
      ctx,
    );
    expect(body.event.type).toBe("tool.requested");
    expect(body.source.raw_event_name).toBe("tool.execute.before");
    expect(body.data?.tool_call).toEqual({
      id: "c1",
      name: "bash",
      input: { cmd: "ls" },
    });
  });

  it("toolCompleted -> tool.completed / tool.execute.after", () => {
    const body = toolCompleted(
      { tool: "bash", sessionID: "s1", callID: "c1", args: { cmd: "ls" } },
      { output: "file1\nfile2" },
      ctx,
    );
    expect(body.event.type).toBe("tool.completed");
    expect(body.source.raw_event_name).toBe("tool.execute.after");
    expect(body.data?.tool_call).toEqual({
      id: "c1",
      name: "bash",
      input: { cmd: "ls" },
      output: "file1\nfile2",
    });
  });

  it("toolFailed -> tool.failed / tool.execute.error", () => {
    const body = toolFailed(
      {
        sessionID: "s1",
        callID: "c1",
        tool: "bash",
        state: { input: { cmd: "rm -rf /" }, error: "permission denied" },
      },
      ctx,
    );
    expect(body.event.type).toBe("tool.failed");
    expect(body.source.raw_event_name).toBe("tool.execute.error");
    expect(body.data?.tool_call?.error).toBe("permission denied");
  });

  it("promptSubmitted -> prompt.submitted / message.submitted", () => {
    const body = promptSubmitted({ sessionID: "s1" }, "hello", ctx);
    expect(body.event.type).toBe("prompt.submitted");
    expect(body.source.raw_event_name).toBe("message.submitted");
    expect(body.data?.prompt).toEqual({ text: "hello" });
  });

  it("assistantResponded -> assistant.responded / message.completed", () => {
    const body = assistantResponded({ sessionID: "s1" }, "hi there", ctx);
    expect(body.event.type).toBe("assistant.responded");
    expect(body.source.raw_event_name).toBe("message.completed");
    expect(body.data?.message).toEqual({ text: "hi there", role: "assistant" });
  });

  it("assistantResponded carries model and token/cost usage", () => {
    const body = assistantResponded(
      {
        sessionID: "s1",
        modelID: "claude-opus-4-8",
        cost: 0.0123,
        tokens: { input: 100, output: 50, cache: { read: 10, write: 5 } },
      },
      "hi there",
      ctx,
    );
    expect(body.session.model).toBe("claude-opus-4-8");
    expect(body.data?.usage).toEqual({
      input_tokens: 100,
      output_tokens: 50,
      cache_read_tokens: 10,
      cache_write_tokens: 5,
      cost: 0.0123,
    });
  });

  it("assistantResponded omits usage/model when the message reports none", () => {
    const body = assistantResponded({ sessionID: "s1" }, "hi there", ctx);
    expect(body.session.model).toBeUndefined();
    expect(body.data?.usage).toBeUndefined();
  });

  it("attaches source.hostname from ctx", () => {
    const withHost: Ctx = { ...ctx, hostname: "dev-box.local" };
    const body = sessionStarted({ id: "s1" }, withHost);
    expect(body.source.hostname).toBe("dev-box.local");
  });

  it("omits source.hostname when absent from ctx", () => {
    const body = sessionStarted({ id: "s1" }, ctx);
    expect(body.source.hostname).toBeUndefined();
  });

  it("permissionAsked -> tool.requested / permission.asked with permission_type", () => {
    const body = permissionAsked(
      { id: "p1", sessionID: "s1", type: "bash", callID: "c1" },
      ctx,
    );
    expect(body.event.type).toBe("tool.requested");
    expect(body.source.raw_event_name).toBe("permission.asked");
    expect(body.data?.tool_call).toEqual({
      id: "c1",
      name: "bash",
      permission_type: "bash",
    });
  });

  it("attributes user_email when present in ctx", () => {
    const withEmail: Ctx = { ...ctx, userEmail: "dev@example.com" };
    const body = sessionStarted({ id: "s1" }, withEmail);
    expect(body.source.user_email).toBe("dev@example.com");
  });

  it("omits user_email when absent from ctx", () => {
    const body = sessionStarted({ id: "s1" }, ctx);
    expect(body.source.user_email).toBeUndefined();
  });

  it("falls back to ctx.fallbackSession when no session id is given", () => {
    const body = sessionStarted({ id: "" }, ctx);
    expect(body.session.id).toBe(ctx.fallbackSession);
  });

  it("normalizes MCP tool-call names and attaches the server identity", () => {
    const mcpCtx: Ctx = {
      ...ctx,
      mcpServers: new Map<string, McpServer>([
        ["context7", { url: "https://mcp.context7.com/mcp" }],
        ["github", {}],
      ]),
    };
    const body = toolRequested(
      { tool: "context7_query-docs", sessionID: "s1", callID: "c1" },
      {},
      mcpCtx,
    );
    expect(body.data?.tool_call?.name).toBe("mcp__context7__query-docs");
    expect(body.data?.mcp).toEqual({
      server_name: "context7",
      url: "https://mcp.context7.com/mcp",
    });
  });

  it("omits the mcp block for native tools", () => {
    const mcpCtx: Ctx = {
      ...ctx,
      mcpServers: new Map<string, McpServer>([["context7", {}]]),
    };
    const body = toolRequested(
      { tool: "bash", sessionID: "s1", callID: "c1" },
      { cmd: "ls" },
      mcpCtx,
    );
    expect(body.data?.tool_call?.name).toBe("bash");
    expect(body.data?.mcp).toBeUndefined();
  });
});

describe("resolveMcpTool", () => {
  const servers = new Map<string, McpServer>([
    ["context7", { url: "https://mcp.context7.com/mcp" }],
    ["my_server", { command: "node server.js" }],
  ]);

  it("rewrites <server>_<tool> and carries the server url", () => {
    expect(resolveMcpTool("context7_query-docs", servers)).toEqual({
      name: "mcp__context7__query-docs",
      mcp: { server_name: "context7", url: "https://mcp.context7.com/mcp" },
    });
  });

  it("carries the command for local (stdio) servers", () => {
    expect(resolveMcpTool("my_server_run", servers)).toEqual({
      name: "mcp__my_server__run",
      mcp: { server_name: "my_server", command: "node server.js" },
    });
  });

  it("prefers the longest matching server prefix (server names with _)", () => {
    // "my_server" must win over a hypothetical "my" so the tool isn't
    // mis-split on the first underscore.
    const withShort = new Map<string, McpServer>([
      ["my", {}],
      ["my_server", {}],
    ]);
    expect(resolveMcpTool("my_server_run", withShort)?.name).toBe(
      "mcp__my_server__run",
    );
  });

  it("returns null for native tools (no matching server)", () => {
    expect(resolveMcpTool("bash", servers)).toBeNull();
    expect(resolveMcpTool("edit", servers)).toBeNull();
  });

  it("returns null when no MCP servers are configured", () => {
    expect(resolveMcpTool("context7_query-docs", new Map())).toBeNull();
    expect(resolveMcpTool("context7_query-docs", undefined)).toBeNull();
  });
});
