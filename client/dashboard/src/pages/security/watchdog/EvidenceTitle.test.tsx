import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EvidenceTitle } from "./EvidenceTitle";

const hasScope = vi.fn<(scope: string, resourceId?: string) => boolean>();
const loadChat = vi.fn<() => Promise<unknown>>();
const listFindings = vi.fn<(req: { cursor?: string }) => Promise<unknown>>();

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    chat: { load: loadChat },
    risk: { results: { list: listFindings } },
  }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ agentSessions: { href: () => "/agent-sessions" } }),
}));

const TITLE = "Please refund order #4023. Customer card";
const FULL = "Please refund order #4023. Customer card 4111 1111 1111 1111.";

function renderTitle(
  chatId: string | undefined,
  onOpenChat = vi.fn<(chatId: string) => void>(),
) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <EvidenceTitle
          title={TITLE}
          createdAt={new Date()}
          chatId={chatId}
          chatMessageId="msg-flagged"
          onOpenChat={onOpenChat}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return onOpenChat;
}

afterEach(cleanup);
beforeEach(() => {
  hasScope.mockReset();
  loadChat.mockReset();
  listFindings.mockReset();
  // The card number's finding sits on the second page, so masking only works
  // if every page of the chat's findings is collected.
  listFindings.mockImplementation(({ cursor }) =>
    Promise.resolve(
      cursor
        ? { results: [{ source: "presidio", match: "4111 1111 1111 1111" }] }
        : { results: [], nextCursor: "page-2" },
    ),
  );
  loadChat.mockResolvedValue({
    messages: [
      { id: "msg-opening", role: "user", content: "Hi, I need help." },
      { id: "msg-flagged", role: "user", content: FULL },
    ],
  });
});

describe("EvidenceTitle", () => {
  it("offers no chat-backed controls without chat:read, and never loads the chat", () => {
    hasScope.mockReturnValue(false);
    renderTitle("chat-1");

    expect(screen.getByText(TITLE)).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
    expect(hasScope).toHaveBeenCalledWith("chat:read", "chat-1");
    expect(loadChat).not.toHaveBeenCalled();
    expect(listFindings).not.toHaveBeenCalled();
  });

  it("opens the session from the title and links to Agent Sessions", async () => {
    hasScope.mockReturnValue(true);
    const onOpenChat = renderTitle("chat-1");

    await userEvent.setup().click(screen.getByRole("button", { name: TITLE }));
    expect(onOpenChat).toHaveBeenCalledWith("chat-1");
    expect(
      screen
        .getByRole("link", { name: "Open session in Agent Sessions" })
        .getAttribute("href"),
    ).toBe("/agent-sessions?chatId=chat-1");
  });

  it("expands to the flagged message, not the opening one, with matches masked", async () => {
    hasScope.mockReturnValue(true);
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const mark = await screen.findByText("•".repeat(19));
    expect(mark.tagName).toBe("MARK");
    expect(screen.queryByText(/4111/)).toBeNull();
    expect(screen.queryByText(/I need help/)).toBeNull();
    expect(listFindings).toHaveBeenCalledTimes(2);
    expect(loadChat).toHaveBeenCalledWith(
      expect.objectContaining({ id: "chat-1", riskOnly: true }),
    );
  });

  it("highlights a non-secret match without masking it", async () => {
    hasScope.mockReturnValue(true);
    loadChat.mockResolvedValue({
      messages: [
        { id: "msg-flagged", role: "tool", content: "bash cmd=printenv" },
      ],
    });
    listFindings.mockResolvedValue({
      results: [{ source: "custom", match: "printenv" }],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const mark = await screen.findByText("printenv");
    expect(mark.tagName).toBe("MARK");
  });

  it("stays plain text for findings with no chat", () => {
    hasScope.mockReturnValue(true);
    renderTitle(undefined);

    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });
});
