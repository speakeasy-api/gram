import { DEMO_ORG_SLUG } from "@/lib/demo";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LogsAgentsContent } from "./LogsAgents";

const state = vi.hoisted(() => ({
  access: "normal" as "normal" | "support" | "workos" | "demo",
  list: vi.fn(),
  sheet: vi.fn(),
  mutation: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project" }),
  useSession: () => ({
    organization: {
      slug: state.access === "demo" ? DEMO_ORG_SLUG : "test-org",
    },
    organizationOverride: state.access === "support" || state.access === "demo",
    impersonatorEmail: state.access === "workos" ? "support@example.com" : null,
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/routes", () => ({ useOrgRoutes: () => ({}) }));
vi.mock("@/hooks/useObservabilityMcpConfig", () => ({
  useObservabilityMcpConfig: () => ({}),
}));
vi.mock("@/hooks/useLogsEnabled", () => ({
  useLogsEnabledErrorCheck: (result: unknown) => result,
}));
vi.mock("@gram/client/react-query/chatDelete.js", () => ({
  useChatDeleteMutation: () => ({ mutate: state.mutation }),
}));
vi.mock("@gram/client/react-query/assistantsGet.js", () => ({
  useAssistantsGet: () => ({}),
}));
vi.mock("@gram/client/react-query/listChatSources.js", () => ({
  useListChatSources: () => ({ data: { sources: [] } }),
}));
vi.mock("@gram/client/react-query/listChats.js", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useListChats: (args: unknown) => {
    state.list(args);
    return { data: { chats: [], total: 0 }, isLoading: false, error: null };
  },
}));
vi.mock("@/components/insights-dock", () => ({ InsightsConfig: () => null }));
vi.mock("@/components/observe/LoggingPageHeader", () => ({
  LoggingPageHeader: ({ title }: { title: string }) => <h1>{title}</h1>,
}));
vi.mock("@/pages/chatLogs/ChatLogsTable", () => ({
  ChatLogsTable: () => <div>Session metadata</div>,
}));
vi.mock("@/pages/chatLogs/ChatDetailPanel", () => ({
  ChatDetailSheet: (props: unknown) => {
    state.sheet(props);
    return <div>Transcript sheet</div>;
  },
}));
vi.mock("@/components/page-layout", () => {
  const Container = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  const Toolbar = Object.assign(Container, {
    Search: ({
      value,
      onChange,
    }: {
      value: string;
      onChange: (value: string) => void;
    }) => (
      <input
        aria-label="Search sessions"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    ),
    Filters: () => <button>Filters</button>,
    Actions: Container,
    SortBy: () => null,
    Refresh: () => null,
  });
  return { Page: { Toolbar } };
});

function mount(url: string) {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <TooltipProvider>
        <MemoryRouter initialEntries={[url]}>
          <LogsAgentsContent />
        </MemoryRouter>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  state.access = "normal";
  vi.clearAllMocks();
});

describe("Agent Sessions protected state", () => {
  it.each(["support", "workos"] as const)(
    "protects %s access: one notice, no search or deep link, usable filters and metadata",
    (access) => {
      state.access = access;
      mount("/?search=cached-title&chatId=cached-session&has_risk=true");
      expect(screen.getAllByText("Session content is protected")).toHaveLength(
        1,
      );
      expect(
        screen.getByText(
          "Titles and transcripts are hidden during support access. Session metadata remains available.",
        ),
      ).toBeTruthy();
      expect(
        (
          screen.getByRole("textbox", {
            name: "Search sessions",
          }) as HTMLInputElement
        ).disabled,
      ).toBe(true);
      expect(screen.getByPlaceholderText("Search unavailable")).toBeTruthy();
      expect(screen.getByRole("button", { name: "Filters" })).toBeTruthy();
      expect(screen.getByText("Session metadata")).toBeTruthy();
      expect(state.list).toHaveBeenLastCalledWith(
        expect.objectContaining({ search: undefined, hasRisk: "true" }),
      );
      expect(state.sheet).not.toHaveBeenCalled();
    },
  );

  it.each(["normal", "demo"] as const)(
    "retains search and the detail sheet for %s access",
    (access) => {
      state.access = access;
      mount("/?search=normal-title");
      expect(screen.queryByText("Session content is protected")).toBeNull();
      fireEvent.change(
        screen.getByRole("textbox", { name: "Search sessions" }),
        { target: { value: "updated" } },
      );
      expect(state.list).toHaveBeenLastCalledWith(
        expect.objectContaining({ search: "updated" }),
      );
      expect(state.sheet).toHaveBeenCalled();
    },
  );
});
