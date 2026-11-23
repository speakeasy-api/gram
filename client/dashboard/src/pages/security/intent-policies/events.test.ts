// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { IntentCallAssessment } from "@gram/client/models/components/intentcallassessment.js";
import {
  argsSummary,
  buildSessionEvents,
  defaultSelection,
  describeGap,
  groupTurns,
  stepEvent,
  transcriptWindow,
  turnNumbers,
} from "./events";

function message(
  m: Partial<ChatMessage> & { id: string; seq: number },
): ChatMessage {
  return {
    createdAt: new Date(m.seq * 1000),
    generation: 0,
    model: "",
    role: "user",
    ...m,
  };
}

function call(overrides: Partial<IntentCallAssessment>): IntentCallAssessment {
  return {
    arguments: "{}",
    decidedBy: [],
    decision: "permit",
    labels: [],
    messageId: "a1",
    occurredAt: new Date(0),
    outcomes: [],
    seq: 2,
    toolCallId: "t1",
    toolName: "Bash",
    usage: { costUsd: 0, inputTokens: 0, latencyMs: 0, outputTokens: 0 },
    ...overrides,
  };
}

const toolCalls = (...ids: string[]) =>
  JSON.stringify(
    ids.map((id) => ({
      id,
      type: "function",
      function: { name: "Bash", arguments: "{}" },
    })),
  );

const messages = [
  message({ id: "u1", seq: 1, role: "user", content: "clean up the repo" }),
  message({
    id: "a1",
    seq: 2,
    role: "assistant",
    content: "Looking.",
    toolCalls: toolCalls("t1", "t2"),
  }),
  message({
    id: "r1",
    seq: 3,
    role: "tool",
    toolCallId: "t1",
    content: "a\nb",
  }),
  message({ id: "r2", seq: 4, role: "tool", toolCallId: "t2", content: "ok" }),
  message({ id: "a2", seq: 5, role: "assistant", content: "Done." }),
];

const calls = [
  call({ toolCallId: "t1" }),
  call({ toolCallId: "t2", decision: "block", decidedBy: ["c"] }),
];

describe("buildSessionEvents", () => {
  it("interleaves messages, calls and results in sequence order", () => {
    const events = buildSessionEvents(messages, calls);
    expect(events.map((e) => e.kind)).toEqual([
      "user",
      "assistant",
      "tool_call",
      "tool_call",
      "tool_result",
      "tool_result",
      "assistant",
    ]);
    const result = events[4];
    expect(result?.kind === "tool_result" && result).toMatchObject({
      callIndex: 0,
      lines: 2,
      bytes: 3,
    });
  });

  it("shows the transcript's calls before any analysis", () => {
    const events = buildSessionEvents(messages, []);
    const callEvents = events.filter((e) => e.kind === "tool_call");
    expect(callEvents).toHaveLength(2);
    expect(callEvents.map((e) => e.call)).toEqual([undefined, undefined]);
    expect(callEvents.map((e) => e.toolName)).toEqual(["Bash", "Bash"]);
    expect(defaultSelection(events)).toBe(2);
  });

  it("falls back to calls only without a transcript", () => {
    const events = buildSessionEvents([], calls);
    expect(events.map((e) => e.kind)).toEqual(["tool_call", "tool_call"]);
  });
});

describe("groupTurns", () => {
  it("opens a turn at each user prompt and keeps the agent's work in it", () => {
    const followUp = [
      ...messages,
      message({ id: "u2", seq: 6, role: "user", content: "thanks" }),
      message({ id: "a3", seq: 7, role: "assistant", content: "Anytime." }),
    ];
    const events = buildSessionEvents(followUp, calls);
    const groups = groupTurns(events);
    expect(groups.map((g) => g.label)).toEqual([
      "Turn 1 · 2 calls",
      "Turn 2 · no calls",
    ]);
    expect(turnNumbers(events, groups)).toEqual([1, 1, 1, 1, 1, 1, 1, 2, 2]);
  });

  it("puts events before the first prompt in a leading turn", () => {
    const events = buildSessionEvents(messages.slice(1), calls);
    expect(groupTurns(events).map((g) => g.label)).toEqual([
      "Turn 1 · 2 calls",
    ]);
  });
});

describe("selection", () => {
  const events = buildSessionEvents(messages, calls);

  it("starts on the first call needing review", () => {
    expect(defaultSelection(events)).toBe(3);
  });

  it("steps only through events the filter keeps", () => {
    expect(stepEvent(events, 0, 1, "calls")).toBe(2);
    expect(stepEvent(events, 2, 1, "review")).toBe(3);
    expect(stepEvent(events, 3, 1, "review")).toBeUndefined();
    expect(stepEvent(events, 3, -1, "all")).toBe(2);
  });
});

describe("transcript window", () => {
  it("clamps to the session and widens by the extensions", () => {
    expect(transcriptWindow(17, 1, 3, 0, 0)).toEqual({ from: 0, to: 4 });
    expect(transcriptWindow(17, 10, 3, 5, 0)).toEqual({ from: 2, to: 13 });
    expect(transcriptWindow(17, 16, 3, 0, 5)).toEqual({ from: 13, to: 16 });
  });

  it("describes a hidden range", () => {
    const events = buildSessionEvents(messages, calls);
    expect(describeGap(events, 0, 3, "earlier")).toBe(
      "4 earlier events · 2 calls",
    );
    expect(describeGap(events, 6, 6, "later")).toBe("1 later event");
  });
});

describe("argsSummary", () => {
  it("prefers a shell command, else key: value pairs", () => {
    expect(argsSummary('{"command":"ls\\n -la"}')).toBe("ls -la");
    expect(argsSummary('{"path":"/tmp","n":2}')).toBe("path: /tmp n: 2");
    expect(argsSummary("not json")).toBe("not json");
  });
});
