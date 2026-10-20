import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import { ChatTranscript, type TranscriptPagination } from "./ChatTranscript";
import { buildTranscript, type DisplayItem } from "./transcript";

// Layout has no viewport in happy-dom. Keep the real transcript and ToolUI;
// only make every virtual row visible and isolate unrelated risk actions.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 72,
      })),
    getTotalSize: () => count * 72,
    measureElement: vi.fn(),
    scrollToIndex: vi.fn(),
  }),
}));
vi.mock("@/pages/security/useDismissFinding", () => ({
  useDismissFinding: () => ({ dismiss: vi.fn() }),
}));
vi.mock("@/elements", async () => {
  const { ToolUI, ToolUIGroup } =
    await import("@/elements/components/ui/tool-ui");
  return { ToolUI, ToolUIGroup, MessageContent: () => null };
});

const pagination: TranscriptPagination = {
  hasMoreBefore: false,
  hasMoreAfter: false,
  onLoadOlder: vi.fn<() => void>(),
  onLoadNewer: vi.fn<() => void>(),
  isFetchingOlder: false,
  isFetchingNewer: false,
  onLoadAllOlder: vi.fn<() => void>(),
  onLoadAllNewer: vi.fn<() => void>(),
  initialScrollIndex: null,
  scrollToFinding: false,
};

function result(content: unknown): ChatMessage {
  return {
    id: "result",
    role: "tool",
    content,
    toolCallId: "call",
    seq: 2,
    generation: 0,
    model: "",
    createdAt: new Date(0),
  };
}
function patch(status: string) {
  return JSON.stringify({
    sessionUpdate: "tool_call_update",
    toolCallId: "call",
    status,
    rawOutput: "tool execution cancelled",
  });
}
function show(message: ChatMessage, grouped = false) {
  const call: ChatMessage = {
    ...message,
    id: "invocation",
    role: "assistant",
    seq: 1,
    content: null,
    toolCallId: undefined,
    toolCalls: JSON.stringify([
      {
        id: "call",
        type: "function",
        function: { name: "shell", arguments: "{}" },
      },
    ]),
  };
  const rows = buildTranscript([call, message]);
  const items: DisplayItem[] = grouped
    ? [
        {
          type: "toolGroup",
          id: "group",
          rows: rows.filter((row) => row.kind === "tool"),
        },
      ]
    : rows.map((row) => ({ type: "row", id: row.id, row }));
  return render(
    <ChatTranscript items={items} ctx={{}} pagination={pagination} />,
  );
}
afterEach(cleanup);

describe("Kit tool result status", () => {
  it.each(["failed", "cancelled"])(
    "renders %s as a red failure, not a green completion",
    (status) => {
      show(result([{ type: "text", text: patch(status) }]));
      expect(
        screen.getByRole("img", { name: "Tool error" }).className,
      ).toContain("text-destructive");
      expect(screen.queryByRole("img", { name: "Tool complete" })).toBeNull();
    },
  );

  it("shows failed tools even in a collapsed group", () => {
    show(result(patch("failed")), true);
    expect(screen.getByRole("button", { name: /1 failed/ })).toBeTruthy();
    const header = screen.getByRole("button", { name: /1 failed/ });
    expect(header.getAttribute("aria-expanded")).toBe("false");
    expect(
      within(header).getByRole("img", { name: "Tool error" }),
    ).toBeTruthy();
    fireEvent.click(header);
    expect(header.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByRole("img", { name: "Tool error" })).toHaveLength(2);
    expect(screen.queryByRole("img", { name: "Tool complete" })).toBeNull();
  });

  it.each(["completed", "in_progress", "pending"])(
    "renders the %s Kit state",
    (status) => {
      show(result(patch(status)));
      const label =
        status === "completed"
          ? "complete"
          : status === "in_progress"
            ? "running"
            : "pending";
      expect(screen.getByRole("img", { name: `Tool ${label}` })).toBeTruthy();
    },
  );

  it.each([
    "ordinary output",
    "{invalid",
    "null",
    '{"status":"failed"}',
    patch("failed").replace('"call"', '"other-call"'),
  ])("does not interpret unrelated output as a Kit failure: %s", (content) => {
    show(result(content));
    expect(screen.getByRole("img", { name: "Tool complete" })).toBeTruthy();
    expect(screen.queryByRole("img", { name: "Tool error" })).toBeNull();
  });
});
