import { describe, expect, it } from "vitest";
import type {
  ChatMessage,
  ClaudeToolUsage,
  ClaudeTurnUsage,
} from "@gram/client/models/components";
import {
  buildClaudeToolUsageByToolUseId,
  buildClaudeUsageByMessageId,
  formatByteCount,
  formatDurationFromNanos,
} from "./claudeUsage";

let seqCounter = 0;
function message(
  id: string,
  role: ChatMessage["role"],
  promptId?: string,
): ChatMessage {
  return {
    id,
    seq: seqCounter++,
    role,
    model: "",
    createdAt: new Date("2026-06-16T00:00:00Z"),
    generation: 0,
    promptId,
  };
}

function turn(promptId: string): ClaudeTurnUsage {
  return {
    promptId,
    startTimeUnixNano: "1000000000",
    endTimeUnixNano: "2000000000",
    requestCount: 1,
    inputTokens: 10,
    outputTokens: 5,
    cacheReadTokens: 2,
    cacheCreationTokens: 3,
    totalTokens: 20,
    costUsd: 0.01,
    costMicros: 10000,
    models: ["claude-sonnet-4-6"],
    querySources: ["sdk"],
  };
}

function toolUsage(toolUseId: string): ClaudeToolUsage {
  return {
    toolUseId,
    promptId: "prompt-1",
    toolName: "Bash",
    inputSizeBytes: 256,
    resultSizeBytes: 1024,
  };
}

describe("buildClaudeUsageByMessageId", () => {
  it("matches turns to user messages by promptId", () => {
    const firstTurn = turn("prompt-1");
    const usage = buildClaudeUsageByMessageId({
      messages: [
        message("user-1", "user", "prompt-1"),
        message("assistant-1", "assistant"),
      ],
      turns: [firstTurn],
    });

    expect(usage.get("user-1")).toEqual({
      turn: firstTurn,
      match: "exact",
    });
    expect(usage.has("assistant-1")).toBe(false);
  });

  it("falls back to user message order for messages without promptId", () => {
    const firstTurn = turn("prompt-1");
    const secondTurn = turn("prompt-2");
    const usage = buildClaudeUsageByMessageId({
      messages: [
        message("user-1", "user", "prompt-1"),
        message("assistant-1", "assistant"),
        message("user-2", "user"),
      ],
      turns: [firstTurn, secondTurn],
    });

    expect(usage.get("user-1")).toEqual({
      turn: firstTurn,
      match: "exact",
    });
    expect(usage.get("user-2")).toEqual({
      turn: secondTurn,
      match: "ordered",
    });
  });
});

describe("buildClaudeToolUsageByToolUseId", () => {
  it("indexes tool usage by toolUseId", () => {
    const first = toolUsage("toolu_1");
    const second = toolUsage("toolu_2");

    const usage = buildClaudeToolUsageByToolUseId([first, second]);

    expect(usage.get("toolu_1")).toBe(first);
    expect(usage.get("toolu_2")).toBe(second);
  });
});

describe("formatByteCount", () => {
  it("formats bytes with human-readable units", () => {
    expect(formatByteCount(256)).toBe("256 BYTES");
    expect(formatByteCount(4096)).toBe("4 KB");
    expect(formatByteCount(1_572_864)).toBe("1.5 MB");
  });
});

describe("formatDurationFromNanos", () => {
  it("carries rounded seconds into minutes", () => {
    expect(formatDurationFromNanos("0", "59999000000")).toBe("1m 0s");
  });
});
