import { describe, expect, it } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { buildDisplayItems, buildTranscript } from "./transcript";
import {
  claudeTagMetadata,
  parseClaudeTagWake,
  projectClaudeTagRows,
} from "./claudeTag";

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
  it("finds deliveries after reminder content blocks and preserves the raw input", () => {
    const content = [
      {
        type: "text",
        text: '<system-reminder source="memory">Instructions & <example></system-reminder>',
      },
      {
        type: "text",
        text: '<system-reminder>More context.</system-reminder>\n<standing_owner_message sender="U_DEMO" ts="1.2">hello & welcome</standing_owner_message>Trailing <not-xml',
      },
    ];
    const input = [
      message({ content }),
      message({ id: "repeat", seq: 2, content }),
    ];
    expect(claudeTagMetadata(input).detected).toBe(true);
    const rows = projectClaudeTagRows(buildTranscript(input));
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      message: {
        content: "hello & welcome",
        participants: [{ providerUserId: "U_DEMO" }],
      },
    });
    expect(input[0]?.content).toBe(content);
  });
  it("reads nested thread deliveries through new grouping elements and ignores reference subtrees", () => {
    const content = `<system-reminder><standing_owner_message sender='U_DEMO_QUOTED'>quoted</standing_owner_message></system-reminder>
<session-context future='value' nonce = 'demo_nonce'>
Channel: #demo-team (id: \`C_DEMO\`)
Workspace: \`T_DEMO\`
You: @Claude (bot user id \`U_DEMO_BOT\`)
</session-context nonce='wrong_nonce'>
Still opaque.
</session-context nonce = 'demo_nonce'>
<system-reminder>More context.</system-reminder>
<wake future='value'><channel id='C_DEMO'><participants><person slack-id='U_DEMO_OTHER'/></participants><message from='human' author-id='U_DEMO_ONE'>first</message><thread><messages><message trigger='true' from='human' sender='U_DEMO_TWO' author='Demo Person' ts='2'>Hi <@U_DEMO_BOT|Claude> & welcome</message><message from='human' slack-id='U_DEMO_BOT'>bot</message><message from='sibling'>sibling</message></messages></thread><history><message from='human' author-id='U_DEMO_HISTORY'>history</message></history><system-note><message from='human' author-id='U_DEMO_NOTE'>note</message></system-note></channel></wake>Trailing <not-xml`;
    expect(parseClaudeTagWake(content)).toMatchObject({
      title: "Claude Tag in #demo-team",
      messages: [
        { sender: "U_DEMO_ONE", text: "first" },
        { sender: "U_DEMO_TWO", text: "Hi @Claude & welcome", id: "2" },
      ],
    });
    const rows = projectClaudeTagRows(buildTranscript([message({ content })]));
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([{ userId: "U_DEMO_ONE" }, { userId: "Demo Person" }]);
  });
  it.each([
    ["a < b & c > d", "a < b & c > d"],
    [
      "<![CDATA[example </wake> </standing_owner_message> & raw text]]>",
      "example </wake> </standing_owner_message> & raw text",
    ],
    ["<![CDATA[a < b & <@U_DEMO_BOT|Claude> &amp;]]>", "a < b & @Claude &amp;"],
  ])("preserves literal comparisons and CDATA text: %s", (body, expected) => {
    expect(
      parseClaudeTagWake(
        `<wake><channel id="C_DEMO"><message from="human" author-id="U_DEMO">${body}</message></channel></wake>`,
      )?.messages[0]?.text,
    ).toBe(expected);
    expect(
      parseClaudeTagWake(
        `<standing_owner_message sender="U_DEMO">${body}</standing_owner_message>`,
      )?.messages[0]?.text,
    ).toBe(expected);
  });
  it("reads the actual nonce attribute among future attributes", () => {
    expect(
      parseClaudeTagWake(
        '<session-context data-nonce="other" nonce="demo">opaque</session-context nonce="demo"><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>',
      )?.messages[0]?.text,
    ).toBe("hello");
  });
  it("normalizes standing owner titles", () => {
    expect(
      parseClaudeTagWake(
        '<standing_owner_message sender="U_DEMO">Hi <@U_DEMO_BOT|Claude></standing_owner_message>',
      )?.title,
    ).toBe("Hi @Claude");
  });
  it("trims the bot ID before filtering deliveries", () => {
    expect(
      parseClaudeTagWake(
        '<session-context>You: @Claude (bot user id ` U_DEMO_BOT `)</session-context><wake><channel id="C_DEMO"><message from="human" author-id="U_DEMO_BOT">bot</message></channel></wake>',
      ),
    ).toBeNull();
  });
  it.each([undefined, "Directory Name"])(
    "fills a missing participant display name without replacing directory names: %s",
    (displayName) => {
      const input = message({
        content:
          '<wake><channel id="C_DEMO"><message from="human" author="Observed Name" author-id="U_DEMO">hello</message></channel></wake>',
        participants: [
          { provider: "slack", providerUserId: "U_DEMO", displayName },
        ],
      });
      const rows = projectClaudeTagRows(buildTranscript([input]));
      expect(
        buildDisplayItems({ rows }).filter(
          (item) => item.type === "turnHeader",
        ),
      ).toMatchObject([{ userId: displayName ?? "Observed Name" }]);
      expect(input.participants?.[0]?.displayName).toBe(displayName);
    },
  );
  it("accepts context without a nonce and alternate Slack ID attributes", () => {
    expect(
      parseClaudeTagWake(
        "<session-context>Context</session-context><wake><channel channel-id='C_DEMO'><thread><message from='human' slack-id='U_DEMO'>hello</message></thread></channel></wake>",
      ),
    ).toMatchObject({
      messages: [{ sender: "U_DEMO", channel: "C_DEMO", text: "hello" }],
    });
  });
  it.each([
    '<session-context nonce=broken>opaque</session-context><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>',
    '<system-reminder-extra>opaque</system-reminder><wake><channel id="C_DEMO"><message from="human">hello</message></channel></wake>',
    "<system-reminder><standing_owner_message sender='U_DEMO'>quoted</standing_owner_message></system-reminder>",
    "<system-reminder><standing_owner_message sender='U_DEMO'>quoted</standing_owner_message>",
    "<system-reminder>instructions</system-reminder>Example: <standing_owner_message sender='U_DEMO'>quoted</standing_owner_message>",
    "<wake><channel id='C_DEMO'><thread><message from='human' author-id='U_DEMO'>hello</message></channel></wake>",
  ])("retains unknown or incomplete deliveries: %s", (content) => {
    expect(parseClaudeTagWake(content)).toBeNull();
    const rows = buildTranscript([message({ content })]);
    expect(projectClaudeTagRows(rows)).toEqual(rows);
  });
  it("reads a nonce-delimited context followed by a wake as one message", () => {
    const content =
      '<session-context nonce="demo_nonce">\nChannel: #demo-team (id: `C_DEMO`)\nWorkspace: `T_DEMO`\nYou: `@Claude` (bot user id `U_DEMO_BOT`)\n## Memory\nRaw <@U_DEMO_BOT> & markdown\n</session-context nonce="wrong_nonce">\n</session-context nonce="demo_nonce">\n<wake reason="channel-activity"><channel id="C_DEMO" type="group"><message from="human" author="First Person" author-id="U_DEMO_ONE" id="1">hello &amp; welcome</message><message from="human" author-id="U_DEMO_BOT">joined</message><message from="sibling" author-id="B_DEMO_BOT">reply</message><message trigger="true" from="human" trust="principal" author="Second Person" author-id="U_DEMO_TWO" id="2">summarize the demo rollout</message></channel></wake>';
    expect(parseClaudeTagWake(content)).toMatchObject({
      title: "Claude Tag in #demo-team",
      messages: [
        { sender: "U_DEMO_ONE", text: "hello & welcome" },
        { sender: "U_DEMO_TWO", text: "summarize the demo rollout" },
      ],
    });
    const rows = projectClaudeTagRows(
      buildTranscript([
        message({
          content,
          participants: [
            {
              provider: "slack",
              providerUserId: "U_DEMO_TWO",
              displayName: "Second Person",
            },
          ],
        }),
      ]),
    );
    expect(rows).toHaveLength(2);
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([{ userId: "First Person" }, { userId: "Second Person" }]);
  });
  it("does not extract quoted wakes or unmatched context nonces", () => {
    expect(
      parseClaudeTagWake(
        '<session-context nonce="demo"><wake><channel id="DEMO_CHANNEL"><message from="human">quoted</message></channel></wake></session-context nonce="other">',
      ),
    ).toBeNull();
  });
  it("ignores a quoted wake within a matching context nonce", () => {
    expect(
      parseClaudeTagWake(
        `<session-context nonce="demo">${wake}</session-context nonce="demo">`,
      ),
    ).toBeNull();
  });
  it("uses context channel names for standing owners", () => {
    expect(
      parseClaudeTagWake(
        '<session-context nonce="demo">\nChannel: #demo-team (id: `C_DEMO`)\n</session-context nonce="demo"><standing_owner_message sender="U_DEMO" channel-id="C_DEMO">hello</standing_owner_message>',
      )?.messages[0]?.channel,
    ).toBe("demo-team");
  });
  it("keeps raw envelopes attributed to their captured owner", () => {
    const rows = buildTranscript([
      message({
        userId: "owner",
        participants: [
          { provider: "slack", providerUserId: "first" },
          { provider: "slack", providerUserId: "second" },
        ],
      }),
    ]);
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([{ userId: "owner", participant: undefined }]);
  });
  it("starts a fallback owner turn after a projected participant", () => {
    const rows = projectClaudeTagRows(
      buildTranscript([
        message({
          content:
            '<standing_owner_message sender="U_DEMO">hello</standing_owner_message>',
        }),
        message({
          id: "plain",
          seq: 2,
          content: "ordinary message",
          userId: "owner",
        }),
      ]),
    );
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([
      { userId: "U_DEMO", messageIds: ["user"] },
      { userId: "owner", messageIds: ["plain"] },
    ]);
  });
  it("reads a standing owner independently of trailing delivery prose and history", () => {
    const content =
      '<standing_owner_message sender="U_DEMO_ONE" ts="1.2" sent-at="2026-01-01T12:00:00Z" originating-ask="true">hello &amp; welcome</standing_owner_message>\nDelivery prose <thread_activity><message author="U_DEMO_OTHER">history</message></thread_activity>';
    expect(parseClaudeTagWake(content)?.messages).toEqual([
      expect.objectContaining({
        text: "hello & welcome",
        sender: "U_DEMO_ONE",
        id: "1.2",
      }),
    ]);
    const rows = projectClaudeTagRows(
      buildTranscript([
        message({
          content,
          participants: [
            {
              provider: "slack",
              providerUserId: "U_DEMO_ONE",
              displayName: "Demo Person",
            },
          ],
        }),
      ]),
    );
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([{ userId: "Demo Person" }]);
  });
  it("shows an observed sender for historical messages without persisted attribution", () => {
    const content =
      '<standing_owner_message sender="U_DEMO_ONE">hello</standing_owner_message>';
    const rows = projectClaudeTagRows(
      buildTranscript([
        message({
          content,
          userId: "device_owner",
          externalUserId: "device_identity",
        }),
      ]),
    );
    expect(
      buildDisplayItems({ rows }).filter((item) => item.type === "turnHeader"),
    ).toMatchObject([
      {
        userId: "U_DEMO_ONE",
        participant: { provider: "slack", providerUserId: "U_DEMO_ONE" },
      },
    ]);
  });
  it("does not extract an owner from reference history", () => {
    expect(
      parseClaudeTagWake(
        '<thread_activity><standing_owner_message sender="U_DEMO_ONE">hello</standing_owner_message></thread_activity>',
      ),
    ).toBeNull();
  });
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
