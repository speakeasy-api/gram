import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ChatLogsTable } from "./ChatLogsTable";

vi.mock("@/components/ui/button", () => ({
  Button: ({
    children,
    onClick,
    "aria-label": ariaLabel,
  }: {
    children: ReactNode;
    onClick?: () => void;
    "aria-label"?: string;
  }) => (
    <button onClick={onClick} aria-label={ariaLabel}>
      {children}
    </button>
  ),
}));

vi.mock("@/components/ui/tooltip", () => ({
  SimpleTooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

function makeChat(id: string): ChatOverview {
  const createdAt = new Date("2026-01-01T12:00:00Z");

  return {
    createdAt,
    id,
    lastMessageTimestamp: new Date("2026-01-01T12:03:00Z"),
    numMessages: 4,
    title: "Investigate session",
    updatedAt: new Date("2026-01-01T12:03:00Z"),
  };
}

describe("ChatLogsTable", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("copies the raw chat id without a label prefix", () => {
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const chatId = "chat_01HXQ1P84WV3S9J7Z52DKVE7NE";

    render(
      <ChatLogsTable
        chats={[makeChat(chatId)]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Copy Chat ID" }));

    expect(writeText).toHaveBeenCalledWith(chatId);
  });

  it("shows created and last activity timestamps", () => {
    render(
      <ChatLogsTable
        chats={[makeChat("chat_01HXQ1P84WV3S9J7Z52DKVE7NE")]}
        onDeleteChat={() => {
          /* test stub */
        }}
        onSelectChat={() => {
          /* test stub */
        }}
        isLoading={false}
        error={null}
      />,
    );

    expect(screen.getByText(/^Created Jan 1, \d{2}:00$/)).toBeTruthy();
    expect(screen.getByText(/^Last activity Jan 1, \d{2}:03$/)).toBeTruthy();
  });
});
