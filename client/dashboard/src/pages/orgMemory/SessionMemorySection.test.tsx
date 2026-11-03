import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SessionMemorySection } from "./SessionMemorySection";

const state = vi.hoisted(() => ({
  memory: {
    state: "remembered",
    summary: "Rotated the staging deploy key.",
    summarizedAt: undefined as Date | undefined,
    memos: [
      {
        id: "memo-1",
        content: "Staging key rotates every 30 days.",
        createdAt: new Date("2026-10-28T09:20:00Z"),
      },
    ],
    dayDigestId: "digest-1",
    dayDigestPeriodStart: "2026-10-28",
    canExclude: true,
    canInclude: false,
  },
  mutate: vi.fn(),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    agentSessions: {
      memory: { href: () => "/org/projects/p/agent-sessions/memory" },
    },
  }),
}));

vi.mock("@gram/client/react-query/chatMemory.js", () => ({
  useChatMemory: () => ({
    data: state.memory,
    isError: false,
    isPending: false,
  }),
  setChatMemoryData: vi.fn(),
}));

vi.mock("@gram/client/react-query/chatSetMemoryExcluded.js", () => ({
  useChatSetMemoryExcludedMutation: () => ({
    mutate: state.mutate,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/listChats.js", () => ({
  invalidateAllListChats: vi.fn(),
}));
vi.mock("@gram/client/react-query/orgMemoryDigests.js", () => ({
  invalidateAllOrgMemoryDigests: vi.fn(),
}));
vi.mock("@gram/client/react-query/orgMemoryRecent.js", () => ({
  invalidateAllOrgMemoryRecent: vi.fn(),
}));
vi.mock("@gram/client/react-query/orgMemoryOverview.js", () => ({
  invalidateAllOrgMemoryOverview: vi.fn(),
}));

function renderSection() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter>
          <SessionMemorySection chatId="chat-1" />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.mutate.mockReset();
});

describe("SessionMemorySection", () => {
  it("shows what agents read once expanded", () => {
    renderSection();
    expect(screen.getByText("In memory")).toBeTruthy();
    expect(screen.queryByText("Rotated the staging deploy key.")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: /Organizational Memory/ }),
    );

    expect(screen.getByText("Rotated the staging deploy key.")).toBeTruthy();
    expect(screen.getByText("Staging key rotates every 30 days.")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: /In the day digest for/ }),
    ).toBeTruthy();
  });

  it("excludes the session from memory", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Exclude" }));
    expect(state.mutate).toHaveBeenCalledWith(
      {
        request: {
          setMemoryExcludedRequestBody: { id: "chat-1", excluded: true },
        },
      },
      expect.anything(),
    );
  });
});
