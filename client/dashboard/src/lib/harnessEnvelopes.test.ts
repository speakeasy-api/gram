import { describe, expect, it } from "vitest";
import { stripLeadingEnvelopes } from "./harnessEnvelopes";

const conversationInfo =
  'Conversation info (untrusted metadata):\n```json\n{\n  "chat_id": "channel:42",\n  "sender": {\n    "name": "Example User"\n  },\n  "was_mentioned": true\n}\n```';

describe("stripLeadingEnvelopes", () => {
  it("strips the assistant runtime's message-context framing", () => {
    expect(
      stripLeadingEnvelopes(
        "<message-context>\nEventID: abc\n</message-context>\n\nHow do I reduce token usage?",
      ),
    ).toBe("How do I reduce token usage?");
  });

  it("strips OpenClaw's conversation info block", () => {
    expect(
      stripLeadingEnvelopes(
        `${conversationInfo}\n\n@Bot what does this command do`,
      ),
    ).toBe("@Bot what does this command do");
  });

  it("strips the timestamp and delivery hint only when an envelope follows", () => {
    expect(
      stripLeadingEnvelopes(
        `[Thu 2026-08-27 13:51 EDT] Delivery: to send a message, use the \`message\` tool.\n\n${conversationInfo}\n\nping`,
      ),
    ).toBe("ping");
    expect(
      stripLeadingEnvelopes("[Mon 2024-05-01 09:30] could we move the sync?"),
    ).toBe("[Mon 2024-05-01 09:30] could we move the sync?");
  });

  it("strips stacked blocks including history rows", () => {
    expect(
      stripLeadingEnvelopes(
        `${conversationInfo}\n\nReply target of current user message (untrusted, for context):\n\`\`\`json\n{"body": "earlier"}\n\`\`\`\n\nChat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\n#2 2026-08-27 13:52:10 EDT ->#1 bob: yo\n\nRecent messages (untrusted, chronological, oldest first):\n#3 2026-08-27 13:53:00 EDT carol: hey\n\nsummarize the thread`,
      ),
    ).toBe("summarize the thread");
  });

  it("keeps a human line right after a history block", () => {
    expect(
      stripLeadingEnvelopes(
        "Chat history since last reply (untrusted, for context):\n#1 2026-08-27 13:51:33 EDT alice: hi\nNote: can you continue",
      ),
    ).toBe("Note: can you continue");
  });

  it("leaves mid-message sentinels and unterminated fences alone", () => {
    const mid = "why does the prompt say (untrusted metadata): here";
    expect(stripLeadingEnvelopes(mid)).toBe(mid);
    const open =
      'Conversation info (untrusted metadata):\n```json\n{"chat_id": "x"\n\nping';
    expect(stripLeadingEnvelopes(open)).toBe(open);
  });

  it("returns text without an envelope untouched", () => {
    expect(stripLeadingEnvelopes("  indented paste\n")).toBe(
      "  indented paste\n",
    );
  });
});
