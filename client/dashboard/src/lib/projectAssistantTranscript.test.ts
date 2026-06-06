import type { GramChatMessage } from "@gram-ai/elements";
import { describe, expect, it } from "vitest";
import { stripTranscriptFraming } from "./projectAssistantTranscript";

function userMessage(content: GramChatMessage["content"]): GramChatMessage {
  return {
    id: "m1",
    model: "",
    created_at: "2026-06-05T00:00:00Z",
    role: "user",
    content,
  };
}

describe("stripTranscriptFraming", () => {
  it("strips the leading message-context block from string content", () => {
    const result = stripTranscriptFraming(
      userMessage(
        "<message-context>\nEventID: e1\nUserID: u1\n</message-context>\n\nWhich agents call the weather tool most?",
      ),
    );
    expect(result?.content).toBe("Which agents call the weather tool most?");
  });

  it("strips a leading dashboard_context block", () => {
    const result = stripTranscriptFraming(
      userMessage(
        '<dashboard_context>\nThe user clicked "Explore with AI" on the Top Users chart.\n</dashboard_context>\n\nWho are my top users?',
      ),
    );
    expect(result?.content).toBe("Who are my top users?");
  });

  it("strips both envelopes when an Explore-with-AI turn is double-wrapped", () => {
    const result = stripTranscriptFraming(
      userMessage(
        "<message-context>\nEventID: e1\n</message-context>\n\n<dashboard_context>\nTop Servers chart.\n</dashboard_context>\n\nWhat changed?",
      ),
    );
    expect(result?.content).toBe("What changed?");
  });

  it("strips framing from a text content part", () => {
    const result = stripTranscriptFraming(
      userMessage([
        {
          type: "text",
          text: "<message-context>\nEventID: e1\n</message-context>\n\nhello",
        },
      ]),
    );
    expect(result?.content).toEqual([{ type: "text", text: "hello" }]);
  });

  it("drops a user turn that is only a framing block", () => {
    const result = stripTranscriptFraming(
      userMessage(
        "<message-context>\nEventType: assistant_mcp_auth_required\nAuthURL: https://example.test/oauth\n</message-context>\n",
      ),
    );
    expect(result).toBeNull();
  });

  it("only strips leading blocks, leaving mid-text tags intact", () => {
    const text = "why does my agent emit <message-context>x</message-context>?";
    expect(stripTranscriptFraming(userMessage(text))?.content).toBe(text);
  });

  it("keeps a media turn whose text part is only framing", () => {
    const result = stripTranscriptFraming(
      userMessage([
        {
          type: "text",
          text: "<message-context>\nEventID: e1\n</message-context>\n",
        },
        { type: "image_url", image_url: { url: "https://example.test/a.png" } },
      ]),
    );
    // The whole turn must survive (the image is not lost); the framing text is stripped to empty.
    expect(result).not.toBeNull();
    expect(result?.content).toEqual([
      { type: "text", text: "" },
      { type: "image_url", image_url: { url: "https://example.test/a.png" } },
    ]);
  });

  it("passes assistant messages through untouched", () => {
    const assistant: GramChatMessage = {
      id: "a1",
      model: "",
      created_at: "2026-06-05T00:00:00Z",
      role: "assistant",
      content: "The travel-planner agent leads with 1,204 calls.",
    };
    expect(stripTranscriptFraming(assistant)).toBe(assistant);
  });
});
