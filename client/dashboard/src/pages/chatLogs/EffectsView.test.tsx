import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { EffectsView } from "./EffectsView";
import { buildTranscript } from "./transcript";

// Isolate the condensed rows from risk actions and the Elements tool UI.
vi.mock("@/pages/security/useDismissFinding", () => ({
  useDismissFinding: () => ({ dismiss: vi.fn() }),
}));
vi.mock("@gram/client/react-query/summarizeToolCall.js", () => ({
  useSummarizeToolCallMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@/elements", () => ({
  ToolUIGroup: () => null,
  ToolUISection: () => null,
}));

afterEach(cleanup);

const wrapped =
  'Conversation info: ⟦openclaw:ctx⟧\n```json\n{"requester_profile":{"id":"gateway-owner"}}\n```\n\nwhat is on my calendar?';

function showMessage(role: "user" | "assistant") {
  const message: ChatMessage = {
    id: "turn",
    role,
    content: wrapped,
    seq: 1,
    generation: 0,
    model: "",
    createdAt: new Date(0),
  };
  return render(
    <EffectsView
      chatId="chat"
      rows={buildTranscript([message])}
      riskResultsByMessage={new Map()}
      claudeToolUsageByToolUseId={new Map()}
      claudeTurnByPromptId={new Map()}
    />,
  );
}

describe("EffectsView OpenClaw preview", () => {
  it("previews the request of a wrapped user turn", () => {
    showMessage("user");
    expect(screen.getByText("what is on my calendar?")).toBeTruthy();
    expect(screen.queryByText(/openclaw:ctx/)).toBeNull();
  });

  it("leaves a non-user message that matches the wrapper unchanged", () => {
    showMessage("assistant");
    expect(screen.getByText(/openclaw:ctx/)).toBeTruthy();
  });
});
