import { describe, expect, it } from "vitest";
import {
  assistantResponded,
  permissionAsked,
  promptSubmitted,
  sessionEnded,
  sessionStarted,
  toolCompleted,
  toolFailed,
  toolRequested,
  type Ctx,
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
});
