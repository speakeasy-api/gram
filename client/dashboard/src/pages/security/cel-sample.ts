import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { CelMessage } from "./cel-wasm";

/** An editable sample message the CEL testers evaluate against. Tool server
 *  and function are derived from the tool name at eval time, mirroring how
 *  production derives them (server/internal/toolref). */
export type CelSample = {
  kind: string;
  content: string;
  tools: CelSampleTool[];
};

type CelSampleTool = { name: string; args: string };

// Mirrors toolref.MCPServerOf / MCPFunctionOf: "mcp__<server>__<function>"
// (Claude Code) and "MCP:<function>" (Cursor); bare names are native tools.
function toolServerOf(name: string): string {
  if (name.startsWith("mcp__")) {
    const rest = name.slice("mcp__".length);
    const idx = rest.indexOf("__");
    return idx >= 0 ? rest.slice(0, idx) : "";
  }
  if (name.startsWith("MCP:")) return name.slice("MCP:".length);
  return "";
}

function toolFunctionOf(name: string): string {
  if (name.startsWith("mcp__")) {
    const rest = name.slice("mcp__".length);
    const idx = rest.indexOf("__");
    return idx >= 0 ? rest.slice(idx + 2) : rest;
  }
  if (name.startsWith("MCP:")) return name.slice("MCP:".length);
  return name;
}

export function celMessageFromSample(sample: CelSample): CelMessage {
  return {
    type: sample.kind,
    content: sample.content,
    tools:
      sample.kind === "tool_request"
        ? sample.tools.map((t) => ({
            name: t.name,
            server: toolServerOf(t.name),
            function: toolFunctionOf(t.name),
            args: t.args,
          }))
        : undefined,
  };
}

function textContent(content: unknown): string {
  if (typeof content === "string") {
    // Some rows store the multimodal part list serialized as a string; unwrap
    // it so samples show the text rather than the JSON envelope.
    if (content.startsWith("[") || content.startsWith("{")) {
      try {
        const parsed = JSON.parse(content) as unknown;
        if (Array.isArray(parsed)) return textContent(parsed);
      } catch {
        // Plain text that happens to start with a bracket.
      }
    }
    return content;
  }
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === "string") return part;
        if (
          part &&
          typeof part === "object" &&
          typeof (part as { text?: unknown }).text === "string"
        ) {
          return (part as { text: string }).text;
        }
        return "";
      })
      .filter(Boolean)
      .join("\n");
  }
  return "";
}

type RecordedToolCall = { function?: { name?: string; arguments?: string } };

// Mirrors the server's role-to-kind mapping (batch_messages.go): an assistant
// message with tool calls is a tool_request; other roles are not scanned.
export function sampleFromChatMessage(msg: ChatMessage): CelSample | null {
  const content = textContent(msg.content);
  switch (msg.role) {
    case "user":
      return { kind: "user_message", content, tools: [] };
    case "tool":
      return { kind: "tool_response", content, tools: [] };
    case "assistant": {
      const rawCalls = (msg.toolCalls ?? "").trim();
      if (rawCalls === "") {
        return { kind: "assistant_message", content, tools: [] };
      }
      let calls: RecordedToolCall[] = [];
      try {
        const parsed = JSON.parse(rawCalls) as unknown;
        if (Array.isArray(parsed)) calls = parsed as RecordedToolCall[];
      } catch {
        // Malformed tool calls: treat as plain assistant text.
      }
      const tools = calls
        .filter(
          (c) =>
            (c.function?.name ?? "") !== "" ||
            (c.function?.arguments ?? "").trim() !== "",
        )
        .map((c) => ({
          name: c.function?.name ?? "",
          args: c.function?.arguments ?? "",
        }));
      if (tools.length === 0) {
        return { kind: "assistant_message", content, tools: [] };
      }
      return { kind: "tool_request", content, tools };
    }
    default:
      return null;
  }
}
