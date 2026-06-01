import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@gram/client/models/components";
import {
  getRiskEntryCount,
  getVisibleMessages,
  type FilterableTraceEntryType,
} from "./traceEntries";

function makeMessage({
  id,
  role,
  toolCalls,
}: {
  id: string;
  role: ChatMessage["role"];
  toolCalls?: string;
}): ChatMessage {
  return {
    id,
    role,
    toolCalls,
    generation: 0,
  } as ChatMessage;
}

const allTypes: FilterableTraceEntryType[] = [
  "user",
  "assistant",
  "tool_call",
  "tool_result",
];

describe("getVisibleMessages", () => {
  it("preserves existing entry type behavior when risk only is disabled", () => {
    const user = makeMessage({ id: "user-1", role: "user" });
    const assistant = makeMessage({ id: "assistant-1", role: "assistant" });
    const riskResultsByMessage = new Map<string, readonly unknown[]>([
      ["assistant-1", [{ action: "flag" }]],
    ]);

    const visible = getVisibleMessages({
      messages: [user, assistant],
      enabledEntryTypes: ["user"],
      riskOnly: false,
      riskResultsByMessage,
    });

    expect(visible.map((message) => message.id)).toEqual(["user-1"]);
  });

  it("includes flagged and blocked risk entries when risk only is enabled", () => {
    const flagged = makeMessage({ id: "flagged", role: "user" });
    const blocked = makeMessage({ id: "blocked", role: "assistant" });
    const normal = makeMessage({ id: "normal", role: "assistant" });
    const riskResultsByMessage = new Map<string, readonly unknown[]>([
      ["flagged", [{ action: "flag" }]],
      ["blocked", [{ action: "block" }]],
    ]);

    const visible = getVisibleMessages({
      messages: [flagged, blocked, normal],
      enabledEntryTypes: allTypes,
      riskOnly: true,
      riskResultsByMessage,
    });

    expect(visible.map((message) => message.id)).toEqual([
      "flagged",
      "blocked",
    ]);
  });

  it("composes risk only with entry type filters", () => {
    const riskyUser = makeMessage({ id: "risky-user", role: "user" });
    const riskyToolCall = makeMessage({
      id: "risky-tool-call",
      role: "assistant",
      toolCalls: JSON.stringify([{ function: { name: "search" } }]),
    });
    const riskResultsByMessage = new Map<string, readonly unknown[]>([
      ["risky-user", [{ action: "flag" }]],
      ["risky-tool-call", [{ action: "flag" }]],
    ]);

    const visible = getVisibleMessages({
      messages: [riskyUser, riskyToolCall],
      enabledEntryTypes: ["tool_call"],
      riskOnly: true,
      riskResultsByMessage,
    });

    expect(visible.map((message) => message.id)).toEqual(["risky-tool-call"]);
  });
});

describe("getRiskEntryCount", () => {
  it("counts messages that have one or more risk results", () => {
    const messages = [
      makeMessage({ id: "one", role: "user" }),
      makeMessage({ id: "two", role: "assistant" }),
      makeMessage({ id: "three", role: "assistant" }),
    ];
    const riskResultsByMessage = new Map<string, readonly unknown[]>([
      ["one", [{ action: "flag" }, { action: "flag" }]],
      ["three", [{ action: "block" }]],
    ]);

    expect(getRiskEntryCount(messages, riskResultsByMessage)).toBe(2);
  });
});
