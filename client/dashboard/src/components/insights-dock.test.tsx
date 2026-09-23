import { cleanup, render } from "@testing-library/react";
import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useHideInsightsDock } from "./insights-context";
import { InsightsProvider } from "./insights-dock";
import { GramElementsProvider } from "@/elements";

const mocks = vi.hoisted(() => ({
  skills: vi.fn(() => ({
    data: undefined,
    isPending: false,
    error: null as Error | null,
  })),
  skillContext: undefined as { loading?: boolean; error?: boolean } | undefined,
  activeRoute: "detail" as "detail" | "new" | "home",
  resolveCreator: undefined as
    | ((chat: { userId: string }) => unknown)
    | undefined,
  hasScope: vi.fn((_scope: string, _resourceId?: string) => false),
  members: vi.fn(() => ({
    data: {
      members: [
        { id: "member-a", name: "Member", email: "member@example.com" },
      ],
    },
  })),
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
    GramElementsProvider: ({
      children,
      config,
    }: {
      children: ReactNode;
      config: {
        history?: { resolveCreator?: typeof mocks.resolveCreator };
        composer?: { skillContext?: typeof mocks.skillContext };
      };
    }) => {
      const hasRuntime = useContext(RuntimeContext);
      if (config.composer) mocks.skillContext = config.composer.skillContext;
      if (config.history) {
        mocks.resolveCreator = config.history.resolveCreator;
        if (mocks.activeRoute === "home") return null;
      }
      if (hasRuntime) {
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
  useSkillsInfinite: mocks.skills,
}));
vi.mock("@/hooks/useRBAC", async (original) => ({
  ...(await original<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-id", slug: "project" }),
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
beforeEach(() => {
  mocks.skills.mockReturnValue({
    data: undefined,
    isPending: false,
    error: null,
  });
  mocks.skillContext = undefined;
  mocks.activeRoute = "detail";
  mocks.resolveCreator = undefined;
  mocks.hasScope.mockReturnValue(false);
});

function AssistantEditor(): JSX.Element {
  useHideInsightsDock();
  return (
    <GramElementsProvider config={{} as never}>Editor</GramElementsProvider>
  );
}

describe("InsightsProvider", () => {
  it.each([null, new Error("cached failure")])(
    "masks disabled uncached skills state (%s)",
    (error) => {
      mocks.activeRoute = "home";
      mocks.skills.mockReturnValue({ data: undefined, isPending: true, error });
      render(
        <InsightsProvider
          mcpConfig={{ projectSlug: "project" } as never}
          title="Assistant"
          subtitle=""
        >
          <div />
        </InsightsProvider>,
      );
      expect(mocks.skills).toHaveBeenLastCalledWith(
        expect.anything(),
        undefined,
        expect.objectContaining({ enabled: false }),
      );
      expect(mocks.skillContext).toMatchObject({
        loading: false,
        error: false,
      });
    },
  );
  it("ignores cached members after directory permission is revoked", () => {
    mocks.activeRoute = "home";
    mocks.hasScope.mockImplementation((scope) => scope === "org:read");
    // Keep the cached result unchanged across the authorization transition.
    mocks.members.mockReturnValue(mocks.members());
    const provider = () => (
      <InsightsProvider
        mcpConfig={{ projectSlug: "project" } as never}
        title="Assistant"
        subtitle=""
      >
        <div />
      </InsightsProvider>
    );
    const { rerender } = render(provider());
    expect(mocks.resolveCreator).toBeDefined();
    expect(mocks.resolveCreator?.({ userId: "member-a" })).toMatchObject({
      name: "Member",
      email: "member@example.com",
    });

    mocks.hasScope.mockReturnValue(false);
    rerender(provider());
    expect(mocks.members).toHaveBeenLastCalledWith(undefined, undefined, {
      enabled: false,
    });
    expect(mocks.resolveCreator?.({ userId: "member-a" })).toBeUndefined();
  });

  it.each([
    ["org:read", "organization", true],
    ["project:read", "project-a", false],
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
