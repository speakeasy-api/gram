import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { type ReactNode, useEffect, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useHideInsightsDock, useInsightsState } from "./insights-context";
import { InsightsProvider } from "./insights-dock";
import { useFreshAssistantConversation } from "@/hooks/useFreshAssistantConversation";
import { ComposerDraftHandover } from "@/hooks/useComposerDraftHandover";
import { GramElementsProvider } from "@/elements";

const mocks = vi.hoisted(() => ({
  elementsConfig: vi.fn(),
  skills: vi.fn(() => ({
    data: undefined as
      | {
          pages: {
            result: {
              skills: { id: string; name: string; hasValidVersion: boolean }[];
            };
          }[];
        }
      | undefined,
    isPending: false,
    error: null as Error | null,
  })),
  skillContext: undefined as
    | {
        loading?: boolean;
        error?: boolean;
        skills: { id: string }[];
        selectedSkillIds: string[];
        onSelectedSkillIdsChange: (ids: string[]) => void;
      }
    | undefined,
  grants: [] as { scope: "skill:read"; selectors: Record<string, string>[] }[],
  permissionsLoading: false,
  getSkillIds: undefined as (() => string[]) | undefined,
  activeRoute: "detail" as "detail" | "new" | "home" | "page",
  assistantReady: true,
  composerSetText: vi.fn(),
  composerText: "",
  composerBound: true,
  runtimeMount: vi.fn(),
  switchToNewThread: vi.fn(),
  threadsLoading: false,
  listeners: new Set<() => void>(),
  recentChats: undefined as
    | { chats: { id: string; lastMessageTimestamp: Date }[] }
    | undefined,
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
  const { createContext, createElement, useContext, useEffect } =
    await import("react");
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
      useEffect(() => {
        mocks.runtimeMount();
      }, []);
      mocks.elementsConfig(config);
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

vi.mock("@assistant-ui/react", () => {
  const aui = {
    composer: () => ({
      stopDictation: vi.fn(),
      setText: (text: string) => {
        mocks.composerSetText(text);
        if (mocks.composerBound) mocks.composerText = text;
      },
      getState: () => ({ text: mocks.composerText }),
    }),
    thread: () => ({ append: vi.fn() }),
    threads: () => ({
      getState: () => ({ isLoading: mocks.threadsLoading }),
      switchToNewThread: mocks.switchToNewThread,
    }),
    subscribe: (listener: () => void) => {
      mocks.listeners.add(listener);
      return () => mocks.listeners.delete(listener);
    },
  };
  return {
    useAui: () => aui,
    useAuiState: (
      selector: (state: {
        threads: { isLoading: boolean };
        thread: { isRunning: boolean; isLoading: boolean; isDisabled: boolean };
        composer: { text: string; dictation: null };
      }) => unknown,
    ) =>
      selector({
        threads: { isLoading: mocks.threadsLoading },
        thread: { isRunning: false, isLoading: false, isDisabled: false },
        composer: { text: "", dictation: null },
      }),
    AuiConfig: () => ({}),
    AuiProvider: ({ children }: { children: ReactNode }) => children,
  };
});

vi.mock("@/hooks/useObservabilityMcpConfig", () => ({
  useNoToolsetsConfigured: () => false,
}));
vi.mock("@/hooks/useServerAssistantTransport", () => ({
  useServerAssistantTransport: (
    _projectSlug: string,
    _enabled: boolean,
    options: { getSkillIds: () => string[] },
  ) => {
    mocks.getSkillIds = options.getSkillIds;
    return {
      transport: undefined,
      assistantId: mocks.assistantReady ? "managed-assistant" : "",
      ready: mocks.assistantReady,
      allowed: true,
      error: undefined,
      needsAdmin: false,
    };
  },
}));
vi.mock("@/hooks/useDrainInfiniteQuery", () => ({
  useDrainInfiniteQuery: vi.fn(),
}));
vi.mock("@gram/client/react-query/listChats.js", () => ({
  useListChats: () => ({ data: mocks.recentChats }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: mocks.members,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: mocks.skills,
}));
vi.mock("@/hooks/useRBAC", async (original) => ({
  ...(await original<typeof import("@/hooks/useRBAC")>()),
  useRBAC: () => ({
    hasScope: mocks.hasScope,
    grants: mocks.grants,
    isLoading: mocks.permissionsLoading,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-id", slug: "project" }),
  useOrganization: () => ({
    id: "organization",
    projects: [
      { id: "project-id", slug: "project" },
      { id: "target-id", slug: "target" },
    ],
  }),
  useSession: () => ({ user: { id: "user", email: "user@example.com" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project",
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
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
// The real route helpers derive which assistant route is active from this
// pathname.
vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => vi.fn(),
  useLocation: () => ({
    pathname: {
      new: "/org/projects/project/assistants/new",
      detail: "/org/projects/project/assistants/assistant-id",
      home: "/org/projects/project",
      page: "/org/projects/project/settings",
    }[mocks.activeRoute],
  }),
}));

afterEach(cleanup);
beforeEach(() => {
  mocks.recentChats = undefined;
  mocks.listeners.clear();
  mocks.elementsConfig.mockClear();
  mocks.skills.mockReturnValue({
    data: undefined,
    isPending: false,
    error: null,
  });
  mocks.grants = [];
  mocks.permissionsLoading = false;
  mocks.getSkillIds = undefined;
  mocks.skillContext = undefined;
  mocks.activeRoute = "detail";
  mocks.assistantReady = true;
  mocks.composerSetText.mockClear();
  mocks.composerText = "";
  mocks.composerBound = true;
  mocks.runtimeMount.mockClear();
  mocks.switchToNewThread.mockClear();
  mocks.threadsLoading = false;
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
  it("does not connect project MCP inventory in the shared server-assistant runtime", () => {
    mocks.activeRoute = "home";
    const mcps = Array.from({ length: 1767 }, (_, i) => ({
      url: `https://gram.example/mcp/server-${i}`,
      name: `server-${i}`,
    }));
    render(
      <InsightsProvider
        mcpConfig={{
          projectSlug: "project",
          mcp: "https://gram.example/mcp/legacy",
          mcps,
        }}
        title="Assistant"
        subtitle=""
      >
        <div />
      </InsightsProvider>,
    );
    expect(mocks.elementsConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        projectSlug: "project",
        mcp: undefined,
        mcps: [],
        history: expect.objectContaining({ enabled: true }),
      }),
    );
  });
  it.each([
    ["target-id", true],
    ["project-id", false],
  ] as const)(
    "gates configured project skills with grant on %s",
    (projectId, enabled) => {
      mocks.activeRoute = "home";
      mocks.grants = [
        {
          scope: "skill:read",
          selectors: [{ resourceKind: "skill", resourceId: "*", projectId }],
        },
      ];
      mocks.skills.mockReturnValue({
        data: {
          pages: [
            {
              result: {
                skills: [
                  {
                    id: "target-skill",
                    name: "Target skill",
                    hasValidVersion: true,
                  },
                ],
              },
            },
          ],
        },
        isPending: false,
        error: null,
      });
      render(
        <InsightsProvider
          mcpConfig={{ projectSlug: "target" } as never}
          title="Assistant"
          subtitle=""
        >
          <div />
        </InsightsProvider>,
      );
      expect(mocks.skills).toHaveBeenLastCalledWith(
        { limit: 200, gramProject: "target" },
        undefined,
        expect.objectContaining({ enabled }),
      );
      expect(mocks.skillContext?.skills.map((skill) => skill.id)).toEqual(
        enabled ? ["target-skill"] : [],
      );
    },
  );

  it.each(["revoked", "loading"])(
    "removes selected skills from the transport when permission is %s",
    (permission) => {
      mocks.activeRoute = "home";
      mocks.grants = [
        {
          scope: "skill:read",
          selectors: [
            { resourceKind: "skill", resourceId: "*", projectId: "target-id" },
          ],
        },
      ];
      mocks.skills.mockReturnValue({
        data: {
          pages: [
            {
              result: {
                skills: [
                  {
                    id: "target-skill",
                    name: "Target skill",
                    hasValidVersion: true,
                  },
                ],
              },
            },
          ],
        },
        isPending: false,
        error: null,
      });
      const provider = () => (
        <InsightsProvider
          mcpConfig={{ projectSlug: "target" } as never}
          title="Assistant"
          subtitle=""
        >
          <div />
        </InsightsProvider>
      );
      const { rerender } = render(provider());
      act(() => mocks.skillContext?.onSelectedSkillIdsChange(["target-skill"]));
      // Keep the callback already handed to the long-lived transport, not a new one.
      const getSkillIds = mocks.getSkillIds!;
      expect(getSkillIds()).toEqual(["target-skill"]);
      if (permission === "revoked") mocks.grants = [];
      else mocks.permissionsLoading = true;
      rerender(provider());
      expect(mocks.skillContext?.skills).toEqual([]);
      expect(getSkillIds()).toEqual([]);
      expect(mocks.skillContext?.selectedSkillIds).toEqual([]);
    },
  );

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
    "does not mount the shared runtime on the assistant %s route",
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
      expect(mocks.elementsConfig).not.toHaveBeenCalledWith(
        expect.objectContaining({ history: expect.anything() }),
      );
    },
  );

  describe("page state", () => {
    let mounts = 0;
    let newConversation: () => void = () => {};

    beforeEach(() => {
      mounts = 0;
      mocks.activeRoute = "page";
    });

    function StatefulPage(): JSX.Element {
      const [draft, setDraft] = useState("");
      const insights = useInsightsState();
      newConversation = insights.newConversation;
      useEffect(() => {
        mounts += 1;
      }, []);
      return (
        <input
          aria-label="Project name"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
      );
    }

    function page(): JSX.Element {
      return (
        <InsightsProvider
          mcpConfig={{ projectSlug: "project" } as never}
          title="Assistant"
          subtitle=""
        >
          <StatefulPage />
        </InsightsProvider>
      );
    }

    function typeDraft(): HTMLElement {
      const input = screen.getByLabelText("Project name");
      fireEvent.change(input, { target: { value: "half typed" } });
      return input;
    }

    it("survives the runtime becoming ready", () => {
      mocks.assistantReady = false;
      const { rerender, container } = render(page());
      const input = typeDraft();
      expect(container.querySelector(".gram-elements")).toBeNull();

      mocks.assistantReady = true;
      rerender(page());

      expect(screen.getByLabelText("Project name")).toBe(input);
      expect(input).toHaveProperty("value", "half typed");
      expect(mounts).toBe(1);
      expect(input.closest(".gram-elements")).not.toBeNull();
    });

    it("survives a new conversation restarting the runtime", () => {
      render(page());
      const input = typeDraft();

      act(() => newConversation());

      expect(screen.getByLabelText("Project name")).toBe(input);
      expect(input).toHaveProperty("value", "half typed");
      expect(mounts).toBe(1);
    });

    it("survives the runtime going away for a route that owns its own", () => {
      const { rerender } = render(page());
      const input = typeDraft();

      mocks.activeRoute = "detail";
      rerender(page());
      expect(input.closest(".gram-elements")).toBeNull();

      mocks.activeRoute = "page";
      rerender(page());

      expect(screen.getByLabelText("Project name")).toBe(input);
      expect(input).toHaveProperty("value", "half typed");
      expect(mounts).toBe(1);
    });

    it("exposes the runtime as ready only once it can be read", () => {
      const seen: boolean[] = [];
      function ReadinessProbe(): null {
        seen.push(useInsightsState().assistantReady);
        return null;
      }
      mocks.assistantReady = false;
      const probe = () => (
        <InsightsProvider
          mcpConfig={{ projectSlug: "project" } as never}
          title="Assistant"
          subtitle=""
        >
          <ReadinessProbe />
        </InsightsProvider>
      );
      const { rerender } = render(probe());
      expect(seen.at(-1)).toBe(false);

      mocks.assistantReady = true;
      rerender(probe());

      expect(seen.at(-1)).toBe(true);
    });

    function FreshLanding({
      initialValue = "",
    }: {
      initialValue?: string;
    }): JSX.Element {
      const ready = useFreshAssistantConversation();
      const [value, setValue] = useState(initialValue);
      return (
        <>
          <input
            aria-label="Landing draft"
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
          {ready && (
            <ComposerDraftHandover
              value={value}
              onAccepted={() => setValue("")}
            />
          )}
        </>
      );
    }

    function landing(initialValue = ""): JSX.Element {
      return (
        <InsightsProvider
          mcpConfig={{ projectSlug: "project" } as never}
          title="Assistant"
          subtitle=""
        >
          <FreshLanding initialValue={initialValue} />
        </InsightsProvider>
      );
    }

    it("starts landing with a fresh keyed runtime once, even without a draft", () => {
      const { rerender } = render(landing());
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(2);
      expect(mocks.switchToNewThread).not.toHaveBeenCalled();
      expect(mocks.composerSetText).not.toHaveBeenCalled();
      rerender(landing());
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(2);
    });

    it("does not seed the previous ready runtime before requesting the keyed reset", () => {
      const writesAtMount: number[] = [];
      mocks.composerSetText.mockImplementationOnce(() => {
        writesAtMount.push(mocks.runtimeMount.mock.calls.length);
      });
      render(landing("pending draft"));
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(2);
      expect(writesAtMount).toEqual([2]);
      expect(mocks.composerText).toBe("pending draft");
      expect(mocks.switchToNewThread).not.toHaveBeenCalled();
    });

    it("preserves the landing draft through connection and keyed publication", () => {
      mocks.assistantReady = false;
      const { rerender } = render(landing());
      fireEvent.change(screen.getByLabelText("Landing draft"), {
        target: { value: "pending draft" },
      });
      expect(mocks.composerSetText).not.toHaveBeenCalled();
      mocks.assistantReady = true;
      rerender(landing());
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(1);
      expect(mocks.composerText).toBe("pending draft");
      expect(screen.getByLabelText("Landing draft")).toHaveProperty(
        "value",
        "",
      );
      expect(mocks.switchToNewThread).not.toHaveBeenCalled();
    });

    it("retains landing text when the new runtime drops a write and retries after binding", () => {
      mocks.assistantReady = false;
      mocks.composerBound = false;
      const { rerender } = render(landing());
      fireEvent.change(screen.getByLabelText("Landing draft"), {
        target: { value: "pending draft" },
      });
      mocks.assistantReady = true;
      rerender(landing());
      expect(screen.getByLabelText("Landing draft")).toHaveProperty(
        "value",
        "pending draft",
      );
      mocks.composerBound = true;
      act(() => mocks.listeners.forEach((listener) => listener()));
      rerender(landing());
      expect(mocks.composerText).toBe("pending draft");
      expect(screen.getByLabelText("Landing draft")).toHaveProperty(
        "value",
        "",
      );
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(1);
      expect(mocks.switchToNewThread).not.toHaveBeenCalled();
    });

    it("waits for landing list loading without resetting its keyed runtime again", () => {
      mocks.assistantReady = false;
      mocks.threadsLoading = true;
      const { rerender } = render(landing());
      fireEvent.change(screen.getByLabelText("Landing draft"), {
        target: { value: "pending draft" },
      });
      mocks.assistantReady = true;
      rerender(landing());
      expect(mocks.composerSetText).not.toHaveBeenCalled();
      expect(screen.getByLabelText("Landing draft")).toHaveProperty(
        "value",
        "pending draft",
      );
      mocks.threadsLoading = false;
      act(() => mocks.listeners.forEach((listener) => listener()));
      rerender(landing());
      expect(mocks.composerText).toBe("pending draft");
      expect(mocks.runtimeMount).toHaveBeenCalledTimes(1);
      expect(mocks.switchToNewThread).not.toHaveBeenCalled();
    });

    it("moves a draft from the fallback input to the composer when ready", () => {
      mocks.assistantReady = false;
      const { rerender } = render(page());
      fireEvent.change(screen.getByLabelText("Ask the Project Assistant"), {
        target: { value: "why is usage up" },
      });

      mocks.assistantReady = true;
      rerender(page());

      expect(mocks.composerSetText).toHaveBeenCalledWith("why is usage up");
      expect(screen.queryByLabelText("Ask the Project Assistant")).toBeNull();
    });

    it("keeps a pending draft editable instead of hiding it behind Continue chat", () => {
      mocks.assistantReady = false;
      mocks.threadsLoading = true;
      const { rerender } = render(page());
      const input = screen.getByLabelText("Ask the Project Assistant");
      fireEvent.change(input, { target: { value: "pending draft" } });
      mocks.recentChats = {
        chats: [{ id: "recent-chat", lastMessageTimestamp: new Date() }],
      };
      mocks.assistantReady = true;
      rerender(page());
      expect(screen.getByLabelText("Ask the Project Assistant")).toBe(input);
      expect(
        screen.queryByRole("button", { name: "Continue chat" }),
      ).toBeNull();
      fireEvent.change(input, { target: { value: "pending draft and more" } });
      mocks.threadsLoading = false;
      act(() => mocks.listeners.forEach((listener) => listener()));
      expect(mocks.composerSetText).toHaveBeenLastCalledWith(
        "pending draft and more",
      );
      mocks.recentChats = undefined;
    });

    it("holds a fallback draft until the thread list has loaded", () => {
      mocks.assistantReady = false;
      mocks.threadsLoading = true;
      const { rerender } = render(page());
      fireEvent.change(screen.getByLabelText("Ask the Project Assistant"), {
        target: { value: "why is usage up" },
      });

      mocks.assistantReady = true;
      rerender(page());
      expect(mocks.composerSetText).not.toHaveBeenCalled();

      mocks.threadsLoading = false;
      act(() => mocks.listeners.forEach((listener) => listener()));
      rerender(page());
      expect(mocks.composerSetText).toHaveBeenCalledWith("why is usage up");
    });
  });
});
