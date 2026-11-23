// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isThinkingIndicatorActive } from "./thinkingIndicator";

const running = { type: "running" } as const;
const complete = { type: "complete" } as const;

const text = (value: string, status: { type: string } = complete) => ({
  type: "text",
  text: value,
  status,
});
const toolCall = (status: { type: string }) => ({
  type: "tool-call",
  status,
});

describe("isThinkingIndicatorActive", () => {
  it("shows before the first part arrives", () => {
    expect(isThinkingIndicatorActive({ status: running, parts: [] })).toBe(
      true,
    );
  });

  it("shows while the only part is empty text", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [text("", running)],
      }),
    ).toBe(true);
  });

  it("hides while prose streams, so the trailing dot is the only signal", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [text("Here is what I found in the logs", running)],
      }),
    ).toBe(false);
  });

  it("shows when prose has finished but the turn is still running", () => {
    // The text part is complete, so its trailing dot is gone; without the
    // indicator the chat looks idle while the model plans its next step.
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [text("Let me check the recent failures.")],
      }),
    ).toBe(true);
  });

  it("shows after a tool run once later prose has finished", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [toolCall(complete), text("Two of them failed.")],
      }),
    ).toBe(true);
  });

  it("hides while a trailing tool call runs, so the group heading shimmers alone", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [toolCall(running)],
      }),
    ).toBe(false);
  });

  it("hides after a trailing tool call completes, because the group stays running", () => {
    // ToolGroup keeps a trailing run in its running state while the message
    // streams (isTrailingWhileStreaming), so its heading still shimmers.
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [text("Some prose."), toolCall(complete)],
      }),
    ).toBe(false);
  });

  it("hides while the next batch's annotation streams after a tool run", () => {
    // thread.tsx holds the annotation back from the prose render and ToolGroup
    // keeps shimmering, so the group is still the one signal.
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [toolCall(complete), text("Investigat", running)],
      }),
    ).toBe(false);
  });

  it("shows while an opening annotation streams with no tool run yet", () => {
    // The annotation is held back from the prose render and there is no group
    // to shimmer, so the indicator is the only thing that can show work.
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [text("Inv", running)],
      }),
    ).toBe(true);
  });

  it("shows while an annotation streams after finished prose", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [
          text("Here is the summary so far."),
          text("Checking recent", running),
        ],
      }),
    ).toBe(true);
  });

  it("hides while reasoning ends the message, so its trigger shimmers alone", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [{ type: "reasoning", text: "Hmm", status: running }],
      }),
    ).toBe(false);
  });

  it("shows when reasoning is followed by empty text, which stops its shimmer", () => {
    expect(
      isThinkingIndicatorActive({
        status: running,
        parts: [
          { type: "reasoning", text: "Hmm", status: complete },
          text("", running),
        ],
      }),
    ).toBe(true);
  });

  it("hides once the message is complete", () => {
    expect(
      isThinkingIndicatorActive({
        status: complete,
        parts: [text("All done.")],
      }),
    ).toBe(false);
    expect(isThinkingIndicatorActive({ status: complete, parts: [] })).toBe(
      false,
    );
  });

  it("hides when the message needs the user to act", () => {
    expect(
      isThinkingIndicatorActive({
        status: { type: "requires-action" },
        parts: [toolCall({ type: "requires-action" })],
      }),
    ).toBe(false);
  });
});
