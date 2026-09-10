import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ConfirmInferenceTrafficSection } from "./confirm-inference-traffic-section";

const mocks = vi.hoisted(() => ({
  request: undefined as undefined | Record<string, unknown>,
  query: {
    data: undefined as undefined | { chats: Array<Record<string, unknown>> },
    isError: false,
    refetch: vi.fn(),
  },
  burst: vi.fn(),
  onScreen: true,
}));

vi.mock("@gram/client/react-query/listChats.js", () => ({
  useListChats: (request: Record<string, unknown>) => {
    mocks.request = request;
    return mocks.query;
  },
}));

// Sections stay mounted while hidden, so the celebration has to know whether
// this one is the step on screen.
vi.mock("./journey-steps", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./journey-steps")>()),
  useIsActiveJourneyStep: () => mocks.onScreen,
}));

vi.mock("@/components/icon-confetti", () => ({
  useConfettiBurst: () => ({
    canvasRef: { current: null },
    burst: mocks.burst,
  }),
}));

vi.mock("motion/react", () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => children,
  motion: { div: "div" },
}));

function chat(overrides: Record<string, unknown> = {}) {
  return {
    id: "chat-1",
    title: "Summarize the release checklist",
    source: "claude-chat-web",
    accountEmail: "someone@example.com",
    lastMessageTimestamp: new Date(),
    numMessages: 2,
    ...overrides,
  };
}

afterEach(cleanup);
beforeEach(() => {
  mocks.request = undefined;
  mocks.query.data = undefined;
  mocks.query.isError = false;
  mocks.query.refetch.mockReset();
  mocks.burst.mockReset();
  mocks.onScreen = true;
});

const section = () => (
  <ConfirmInferenceTrafficSection index={6} description="Send a message." />
);

describe("ConfirmInferenceTrafficSection", () => {
  it("lists only the surfaces an inference hook reports under, since the card opened", () => {
    render(section());

    expect(mocks.request).toMatchObject({
      source: "claude-chat-web,claude-code-web",
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
    });
    expect(mocks.request?.from).toBeInstanceOf(Date);
  });

  it("flips from waiting to confirmed once a conversation arrives", () => {
    const view = render(section());
    expect(screen.getByText("Waiting")).toBeTruthy();

    mocks.query.data = { chats: [chat()] };
    view.rerender(section());

    expect(screen.getByText("Confirmed")).toBeTruthy();
    expect(mocks.burst).toHaveBeenCalledOnce();
  });

  it("does not replay a conversation the next poll lists again", () => {
    const listed = chat();
    const view = render(section());

    mocks.query.data = { chats: [listed] };
    view.rerender(section());
    expect(mocks.burst).toHaveBeenCalledOnce();

    // The feed is a listing, not a cursor: the same rows come back every tick.
    mocks.query.data = { chats: [{ ...listed }] };
    view.rerender(section());

    expect(mocks.burst).toHaveBeenCalledOnce();
  });

  it("counts another turn of the same conversation as fresh traffic", () => {
    const view = render(section());

    mocks.query.data = { chats: [chat()] };
    view.rerender(section());

    mocks.query.data = {
      chats: [
        chat({
          lastMessageTimestamp: new Date(Date.now() + 1000),
        }),
      ],
    };
    view.rerender(section());

    // One burst still — the celebration is for the first arrival only — but
    // the step stays confirmed rather than the turn being dropped as a repeat.
    expect(mocks.burst).toHaveBeenCalledOnce();
    expect(screen.getByText("Confirmed")).toBeTruthy();
  });

  it("offers a retry when the listing fails", () => {
    mocks.query.isError = true;
    render(section());

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(mocks.query.refetch).toHaveBeenCalledOnce();
  });
});
