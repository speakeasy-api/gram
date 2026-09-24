import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { buildDisplayItems, buildTranscript } from "./transcript";
import { parseClaudeTagWake, projectClaudeTagRows } from "./claudeTag";

const wake =
  '<wake reason="channel-activity"><channel id="DEMO_CHANNEL" name="demo-team"><message from="human" author="Demo User" id="1" trigger="true">&lt;@DEMO_BOT|Claude&gt; hello &amp; welcome</message></channel></wake>';
function message(overrides: Partial<ChatMessage>): ChatMessage {
  return {
    id: "user",
    seq: 1,
    role: "user",
    content: wake,
    createdAt: new Date(),
    generation: 0,
    model: "",
    ...overrides,
  };
}

describe("Claude Tag projection", () => {
  it("keeps distinct author headers for humans in the same wake", () => {
    const content =
      '<wake><channel id="DEMO_CHANNEL"><message from="human" author="First User">Hi</message><message from="human" author="Second User">Hello</message></channel></wake>';
    const rows = projectClaudeTagRows(buildTranscript([message({ content })]));
    const headers = buildDisplayItems({ rows }).filter(
      (item) => item.type === "turnHeader",
    );
    expect(headers.map((item) => item.userId)).toEqual([
      "First User",
      "Second User",
    ]);
  });
  it("keeps failed reply tools visible", () => {
    const rows = buildTranscript([
      message({
        role: "assistant",
        content: "",
        toolCalls: JSON.stringify([
          {
            id: "call",
            function: {
              name: "mcp__slackbot__reply",
              arguments: { text: "Hello" },
            },
          },
        ]),
      }),
      message({
        id: "result",
        role: "tool",
        toolCallId: "call",
        content: '{"isError":true,"text":"Unable to reply"}',
      }),
    ]);
    expect(projectClaudeTagRows(rows)).toEqual(rows);
  });
  it("decodes human text and extracts channel metadata", () => {
    expect(parseClaudeTagWake(wake)).toMatchObject({
      title: "Claude Tag in #demo-team",
      messages: [{ author: "Demo User", channel: "demo-team" }],
    });
  });
  it("keeps the channel title when topics change and normalizes the hash", () => {
    expect(
      parseClaudeTagWake(
        wake
          .replace("hello &amp; welcome", "Plan lunch")
          .replace('name="demo-team"', 'name="#demo-team"'),
      )?.title,
    ).toBe("Claude Tag in #demo-team");
  });
  it("rejects malformed and unrelated markup", () => {
    expect(parseClaudeTagWake("<wake><channel")).toBeNull();
    expect(parseClaudeTagWake("hello")).toBeNull();
  });
  it("shows the human and Slack reply, drops repeated context and acknowledgments, and preserves raw input", () => {
    const input = [
      message({}),
      message({ id: "repeat", seq: 2 }),
      message({
        id: "reply",
        seq: 3,
        role: "assistant",
        content: "",
        toolCalls: JSON.stringify([
          {
            id: "call",
            function: {
              name: "mcp__slackbot__reply",
              arguments: JSON.stringify({
                text: "I can help with the release notes.",
              }),
            },
          },
        ]),
      }),
      message({
        id: "ack",
        seq: 4,
        role: "assistant",
        content: "Replied in the thread.",
      }),
    ];
    const rows = projectClaudeTagRows(buildTranscript(input));
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({
      message: {
        id: "user",
        userId: "Demo User",
        content: "@Claude hello & welcome",
      },
    });
    expect(rows[1]).toMatchObject({
      message: { id: "reply", content: "I can help with the release notes." },
    });
    expect(input[0]?.content).toBe(wake);
  });
  it("retains unrecognized reply inputs", () => {
    const rows = buildTranscript([
      message({
        role: "assistant",
        content: "",
        toolCalls: JSON.stringify([
          { function: { name: "mcp__slackbot__reply", arguments: "invalid" } },
        ]),
      }),
    ]);
    expect(projectClaudeTagRows(rows)).toEqual(rows);
  });
});
