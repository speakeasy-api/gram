import type { GramChatMessage } from "@gram-ai/elements";
import { describe, expect, it } from "vitest";
import { stripMessageContextFraming } from "./projectAssistantTranscript";

function userMessage(content: GramChatMessage["content"]): GramChatMessage {
  return {
    id: "m1",
    model: "",
    created_at: "2026-06-05T00:00:00Z",
    role: "user",
    content,
  };
}

describe("stripMessageContextFraming", () => {
  it("strips the leading framing block from string content", () => {
    const result = stripMessageContextFraming(
      userMessage(
        "<message-context>\nEventID: e1\nUserID: u1\n</message-context>\n\nWhich agents call the weather tool most?",
      ),
    );
    expect(result?.content).toBe("Which agents call the weather tool most?");
  });

  it("strips framing from a text content part", () => {
    const result = stripMessageContextFraming(
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
    const result = stripMessageContextFraming(
      userMessage(
        "<message-context>\nEventType: assistant_mcp_auth_required\nAuthURL: https://example.test/oauth\n</message-context>\n",
      ),
    );
    expect(result).toBeNull();
  });

  it("only strips a leading block, leaving mid-text tags intact", () => {
    const text = "why does my agent emit <message-context>x</message-context>?";
    expect(stripMessageContextFraming(userMessage(text))?.content).toBe(text);
  });

  it("keeps a media turn whose text part is only framing", () => {
    const result = stripMessageContextFraming(
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
    expect(stripMessageContextFraming(assistant)).toBe(assistant);
  });
});
