import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EvidenceTitle } from "./EvidenceTitle";

const hasScope = vi.fn<(scope: string, resourceId?: string) => boolean>();
const loadChat = vi.fn<(req: unknown) => Promise<unknown>>();
const listFindings = vi.fn<(req: { cursor?: string }) => Promise<unknown>>();

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    risk: { results: { list: listFindings } },
  }),
}));

vi.mock("@gram/client/react-query/loadChat.js", () => ({
  useLoadChat: (
    request: unknown,
    _security: unknown,
    options: { enabled: boolean },
  ) =>
    useQuery({
      queryKey: ["loadChat", request],
      queryFn: () => loadChat(request),
      enabled: options.enabled,
    }),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({ agentSessions: { href: () => "/agent-sessions" } }),
}));

const TITLE = "Please refund order #4023. Customer card";
const FULL = "Please refund order #4023. Customer card 4111 1111 1111 1111.";

function renderTitle(
  chatId: string | undefined,
  onOpenChat = vi.fn<(chatId: string, chatMessageId?: string) => void>(),
) {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
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
    expect(onOpenChat).toHaveBeenCalledWith("chat-1", "msg-flagged");
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

  it("shows a flagged tool call's arguments beside its text, masking an escaped secret", async () => {
    hasScope.mockReturnValue(true);
    const secret = 'hun"ter\\2';
    loadChat.mockResolvedValue({
      messages: [
        {
          id: "msg-flagged",
          role: "assistant",
          content: "Logging in now.",
          toolCalls: JSON.stringify([
            {
              id: "call-1",
              function: {
                name: "login",
                arguments: JSON.stringify({ password: secret }),
              },
            },
          ]),
        },
      ],
    });
    listFindings.mockResolvedValue({
      results: [{ source: "gitleaks", match: secret }],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const mark = await screen.findByText(/^•+$/);
    expect(mark.tagName).toBe("MARK");
    // Beside the title button, not inside it, so it can be selected and copied.
    const message = screen.getByText(/Logging in now/);
    expect(message.closest("button")).toBeNull();
    expect(message.textContent).toContain("login");
    expect(message.textContent).not.toContain("hun");
  });

  it("masks a secret the provider escaped differently, flagged on another message", async () => {
    hasScope.mockReturnValue(true);
    loadChat.mockResolvedValue({
      messages: [
        {
          id: "msg-flagged",
          role: "assistant",
          content: "Logging in now.",
          toolCalls: JSON.stringify([
            {
              id: "call-1",
              function: {
                name: "login",
                arguments: '{"password":"caf\\u00e9\\/pw"}',
              },
            },
          ]),
        },
      ],
    });
    listFindings.mockResolvedValue({
      results: [
        { source: "gitleaks", match: "café/pw", chatMessageId: "msg-other" },
      ],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    expect(await screen.findByText(/Logging in now/)).toBeTruthy();
    expect(screen.queryByText(/u00e9/)).toBeNull();
    expect(screen.queryByText(/café/)).toBeNull();
  });

  it("refuses to show a message whose flagged secret can't be found in its text", async () => {
    hasScope.mockReturnValue(true);
    loadChat.mockResolvedValue({
      messages: [
        {
          id: "msg-flagged",
          role: "assistant",
          content: "Logging in now.",
          toolCalls: JSON.stringify([
            {
              id: "call-1",
              function: {
                name: "login",
                // Truncated, so the provider's escaping can't be normalized
                // and neither the match nor its escaped form lines up.
                arguments: '{"password":"caf\\u00e9/pw',
              },
            },
          ]),
        },
      ],
    });
    listFindings.mockResolvedValue({
      results: [
        { source: "gitleaks", match: "café/pw", chatMessageId: "msg-flagged" },
      ],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("can't be masked here");
    expect(screen.queryByText(/Logging in now/)).toBeNull();
    expect(screen.queryByText(/u00e9/)).toBeNull();
  });

  it("warns that the message is from an earlier generation when the chat has moved on", async () => {
    hasScope.mockReturnValue(true);
    loadChat.mockResolvedValue({
      maxGeneration: 1,
      messages: [{ id: "msg-other", role: "user", content: "Elsewhere." }],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("earlier version of the session");
    expect(screen.queryByText(/Elsewhere/)).toBeNull();
    expect(screen.getByRole("button", { name: TITLE })).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Collapse message" }),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Open session in Agent Sessions" }),
    ).toBeTruthy();
  });

  it("says the message is no longer flagged when the chat has a single generation", async () => {
    hasScope.mockReturnValue(true);
    loadChat.mockResolvedValue({
      maxGeneration: 0,
      messages: [{ id: "msg-other", role: "user", content: "Elsewhere." }],
    });
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("no longer flagged");
  });

  it("shows an error, and never the message, when the findings fail to load", async () => {
    hasScope.mockReturnValue(true);
    listFindings.mockRejectedValue(new Error("boom"));
    renderTitle("chat-1");

    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Show flagged message" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("Couldn't load this message");
    expect(screen.queryByText(/4111/)).toBeNull();
  });

  it("stays plain text for findings with no chat", () => {
    hasScope.mockReturnValue(true);
    renderTitle(undefined);

    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();
  });
});
