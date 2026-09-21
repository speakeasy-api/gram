import { cleanup, render } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useHideInsightsDock } from "./insights-context";
import { InsightsProvider } from "./insights-dock";
import { GramElementsProvider } from "@/elements";

const mocks = vi.hoisted(() => ({
  activeRoute: "detail" as "detail" | "new",
  hasScope: vi.fn((_scope: string, _resourceId?: string) => false),
  members: vi.fn(() => ({ data: undefined })),
}));

vi.mock("@/elements", async () => {
  const { createContext, createElement, useContext } = await import("react");
  const RuntimeContext = createContext(false);

  return {
    ActiveChatTitle: () => null,
    Chat: () => null,
    ChatComposer: () => null,
    ChatHistory: () => null,
    focusChatComposer: vi.fn(),
    GramElementsProvider: ({ children }: { children: ReactNode }) => {
      if (useContext(RuntimeContext)) {
        throw new Error(
          "useRemoteThreadListRuntime cannot be nested inside another RemoteThreadListRuntime",
        );
      }
      return createElement(RuntimeContext.Provider, { value: true }, children);
    },
    useThreadId: () => ({ threadId: null }),
  };
});

vi.mock("@assistant-ui/react", () => ({
  useAui: () => ({
    composer: () => ({ stopDictation: vi.fn() }),
    thread: () => ({ append: vi.fn() }),
  }),
  useAuiState: () => false,
}));

vi.mock("@/hooks/useObservabilityMcpConfig", () => ({
  useNoToolsetsConfigured: () => false,
}));
vi.mock("@/hooks/useServerAssistantTransport", () => ({
  useServerAssistantTransport: () => ({
    transport: undefined,
    assistantId: "managed-assistant",
    ready: true,
    error: undefined,
    needsAdmin: false,
  }),
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: vi.fn(),
}));
vi.mock("@gram/client/react-query/listChats.js", () => ({
  useListChats: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.members,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: () => ({
    data: undefined,
    isPending: false,
    isFetchingNextPage: false,
    error: undefined,
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: "organization",
    projects: [{ id: "project-a", slug: "project" }],
  }),
  useSession: () => ({ user: { id: "user", email: "user@example.com" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
}));
vi.mock("@/lib/assistantEntityLinks", () => ({
  useAssistantLinkResolver: () => undefined,
}));
vi.mock("@/hooks/useInsightsDockCta", () => ({
  INSIGHTS_DOCK_CONTENT_VT_CLASS: "",
  INSIGHTS_DOCK_VT_CLASS: "",
  useInsightsDockCta: () => ({ dismissed: false, dismiss: vi.fn() }),
}));
vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));
vi.mock("./command-palette/askAiBridge", () => ({
  useAskAiListener: vi.fn(),
}));
vi.mock("react-router", () => ({
  useLocation: () => ({
    pathname:
      mocks.activeRoute === "new"
        ? "/org/projects/project/assistants/new"
        : "/org/projects/project/assistants/assistant-id",
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    playground: { active: false },
    assistants: {
      newAssistant: { active: mocks.activeRoute === "new" },
      detail: { active: mocks.activeRoute === "detail" },
    },
    chat: { conversation: { goTo: vi.fn() } },
  }),
}));

afterEach(cleanup);

function AssistantEditor(): JSX.Element {
  useHideInsightsDock();
  return (
    <GramElementsProvider config={{} as never}>Editor</GramElementsProvider>
  );
}

describe("InsightsProvider", () => {
  it.each([
    ["org:read", "organization", true],
    ["project:read", "project-a", true],
    ["project:read", "unrelated-project", false],
    ["skill:read", "project-a", false],
  ] as const)(
    "gates member lookup for %s on %s",
    (scope, resourceId, enabled) => {
      mocks.hasScope.mockImplementation(
        (s, id) => s === scope && id === resourceId,
      );
      render(
        <InsightsProvider
          mcpConfig={{ projectSlug: "project" } as never}
          title="Assistant"
          subtitle=""
        >
          <AssistantEditor />
        </InsightsProvider>,
      );
      expect(mocks.members).toHaveBeenLastCalledWith(undefined, undefined, {
        enabled,
      });
    },
  );
  it.each(["new", "detail"] as const)(
    "does not wrap the assistant %s route in the shared runtime",
    (activeRoute) => {
      mocks.activeRoute = activeRoute;

      expect(() =>
        render(
          <InsightsProvider
            mcpConfig={{ projectSlug: "project" } as never}
            title="Assistant"
            subtitle=""
          >
            <AssistantEditor />
          </InsightsProvider>,
        ),
      ).not.toThrow();
    },
  );
});
