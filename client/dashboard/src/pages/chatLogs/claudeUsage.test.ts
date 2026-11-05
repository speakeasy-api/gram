// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { ClaudeToolUsage } from "@gram/client/models/components/claudetoolusage.js";
import type { ClaudeTurnUsage } from "@gram/client/models/components/claudeturnusage.js";
import {
  buildClaudeToolUsageByToolUseId,
  buildClaudeUsageByMessageId,
  formatByteCount,
  formatDurationFromNanos,
  isCostUnknown,
  totalTokensFor,
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
  it("carries seconds into minutes", () => {
    expect(formatDurationFromNanos("0", "59999000000")).toBe("1m 0s");
  });
});

describe("isCostUnknown", () => {
  it("treats tokens with no cost as an unknown cost", () => {
    expect(isCostUnknown({ totalCost: 0, totalTokens: 10 })).toBe(true);
    expect(isCostUnknown({ totalInputTokens: 4, totalOutputTokens: 6 })).toBe(
      true,
    );
  });

  it("keeps a reported cost", () => {
    expect(isCostUnknown({ totalCost: 0.5, totalTokens: 10 })).toBe(false);
  });

  it("does not flag a session with no usage at all", () => {
    expect(isCostUnknown({ totalCost: 0, totalTokens: 0 })).toBe(false);
    expect(isCostUnknown({})).toBe(false);
  });
});

describe("totalTokensFor", () => {
  it("prefers the reported total and falls back to input plus output", () => {
    expect(totalTokensFor({ totalTokens: 12, totalInputTokens: 1 })).toBe(12);
    expect(totalTokensFor({ totalInputTokens: 3, totalOutputTokens: 4 })).toBe(
      7,
    );
  });
});
