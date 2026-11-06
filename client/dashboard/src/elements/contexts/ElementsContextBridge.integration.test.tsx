import { useAui, useAuiState } from "@assistant-ui/react";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { useContext } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ElementsConfig } from "@/elements/types";
import { ChatIdContext } from "./ChatIdContext";
import { ConnectionStatusContext } from "./ConnectionStatusContext";
import { ElementsContext, ToolApprovalContext } from "./contexts";
import {
  createElementsContextBridge,
  ElementsContextSource,
  ElementsContextTarget,
  usePublishedElementsSource,
} from "./ElementsContextBridge";
import { ElementsProvider } from "./ElementsProvider";
import { MarkdownLinkContext } from "./MarkdownLinkContext";
import { ToolExecutionContext } from "./ToolExecutionContext";
import { ThreadMetaContext } from "./ThreadMetaContext";

// Keep the provider and assistant-ui runtime real; only external services are stubbed.
vi.mock("@/elements/hooks/useAuth", () => ({
  useAuth: () => ({
    isLoading: false,
    headers: {},
    ensureValidHeaders: async () => ({}),
  }),
}));
vi.mock("@/elements/hooks/useMCPTools", () => ({
  useMCPTools: () => ({
    data: {
      lookup: { execute: async () => "tool result" },
    },
    mcpHeaders: {},
    isLoading: false,
    error: null,
  }),
}));
vi.mock("@/elements/lib/errorTracking", () => ({
  initErrorTracking: vi.fn(),
  trackError: vi.fn(),
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function useProviderValues() {
  return {
    aui: useAui(),
    chat: useContext(ChatIdContext),
    connection: useContext(ConnectionStatusContext),
    elements: useContext(ElementsContext),
    link: useContext(MarkdownLinkContext),
    approval: useContext(ToolApprovalContext),
    execution: useContext(ToolExecutionContext),
    threadMeta: useContext(ThreadMetaContext),
  };
}

describe("ElementsContextBridge with ElementsProvider", () => {
  it.each([false, true])(
    "shares actual-provider contexts and runtime with a sibling (history=%s)",
    async (historyEnabled) => {
      // The production history adapter still uses this assistant-ui legacy API.
      if (historyEnabled) {
        vi.spyOn(console, "warn").mockImplementation((message: unknown) => {
          expect(message).toBe(
            "[assistant-ui] RemoteThreadListAdapter.unstable_Provider is deprecated and will be removed in a future minor release. Return the same adapters from unstable_useAdapters and drop unstable_Provider so useRemoteThreadListRuntime runs on the RemoteThreadList client.",
          );
        });
      }
      const fetchHistory = vi.fn(async (url: string) => {
        if (!url.endsWith("/rpc/chat.list")) {
          throw new Error(`Unexpected request: ${url}`);
        }
        return new Response(
          JSON.stringify({
            chats: [
              {
                id: "saved-chat",
                title: "Saved chat",
                created_at: "2026-01-01T00:00:00Z",
              },
            ],
          }),
          { status: 200 },
        );
      });
      vi.stubGlobal("fetch", fetchHistory);
      const bridge = createElementsContextBridge();
      const config: ElementsConfig = {
        projectSlug: "bridge-test",
        plugins: [],
        history: { enabled: historyEnabled, isOwnChat: () => false },
        composer: { attachments: false },
        resolveLink: () => ({ href: "/resolved" }),
      };
      let source: ReturnType<typeof useProviderValues> | undefined;
      let target: ReturnType<typeof useProviderValues> | undefined;

      function SourceProbe() {
        source = useProviderValues();
        const text = useAuiState(({ composer }) => composer.text);
        return <output aria-label="source composer">{text}</output>;
      }
      function TargetProbe() {
        target = useProviderValues();
        const text = useAuiState(({ composer }) => composer.text);
        return <output aria-label="target composer">{text}</output>;
      }
      function Target() {
        const published = usePublishedElementsSource(bridge);
        return (
          <ElementsContextTarget bridge={bridge}>
            {published === "actual-provider" ? <TargetProbe /> : null}
          </ElementsContextTarget>
        );
      }

      render(
        <>
          <Target />
          <ElementsProvider config={config}>
            <ElementsContextSource bridge={bridge} sourceId="actual-provider" />
            <SourceProbe />
          </ElementsProvider>
        </>,
      );

      await waitFor(() => expect(target).toBeDefined());
      if (historyEnabled) {
        await waitFor(() =>
          expect(target!.threadMeta["saved-chat"]).toEqual({
            createdAt: "2026-01-01T00:00:00Z",
            readOnly: true,
          }),
        );
        expect(fetchHistory).toHaveBeenCalled();
      } else {
        expect(fetchHistory).not.toHaveBeenCalled();
      }
      // Check each context supplied by the actual provider, not a hand-built
      // provider stack that could drift from the production composition.
      for (const key of [
        "chat",
        "connection",
        "elements",
        "link",
        "approval",
        "execution",
        "threadMeta",
      ] as const) {
        expect(target![key], key).toBe(source![key]);
      }
      expect(target!.chat).toEqual({ chatId: null });
      expect(target!.elements!.config).toBe(config);
      expect(target!.link.resolveLink).toBe(config.resolveLink);
      expect(target!.connection!.state).toBe("connected");
      expect(target!.execution!.isToolAvailable("lookup")).toBe(true);
      expect(await target!.execution!.executeTool("lookup", {})).toEqual({
        success: true,
        result: "tool result",
      });

      act(() => {
        target!.aui.composer().setText("from the sibling");
        target!.elements!.setIsExpanded(true);
        target!.approval!.whitelistTool("lookup");
        target!.connection!.markDisconnected();
      });

      expect(screen.getByLabelText("source composer").textContent).toBe(
        "from the sibling",
      );
      expect(screen.getByLabelText("target composer").textContent).toBe(
        "from the sibling",
      );
      expect(source!.elements!.isExpanded).toBe(true);
      expect(target!.elements).toBe(source!.elements);
      expect(source!.approval!.isToolApproved("lookup")).toBe(true);
      expect(target!.approval).toBe(source!.approval);
      expect(source!.connection!.state).toBe("reconnecting");
      expect(target!.connection).toBe(source!.connection);
    },
  );
});
