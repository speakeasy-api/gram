import { useCallback, useMemo } from "react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import type { ElementsConfig } from "@gram-ai/elements";
import speakeasyIcon from "@/assets/speakeasy-icon.svg";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import { stripMessageContextFraming } from "@/lib/projectAssistantTranscript";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";

export interface ChatPageConfig {
  config: ElementsConfig;
  /** True once the managed assistant has resolved and the transport is live. */
  ready: boolean;
  /** Connection error message, if resolving the managed assistant failed. */
  error: string | null;
}

/**
 * Builds an {@link ElementsConfig} for the standalone Chat pages (`/chat`).
 *
 * It resolves the SAME server-side Project Assistant the docked composer uses
 * (provisioning is idempotent, keyed by project slug), so both surfaces read
 * and write the same server-persisted conversations — the page is a second
 * entrance to chat, not a second assistant. Conversation history, the
 * conversation list, and ids all live on the chat service; this hook only
 * builds the send transport and the Elements config that points at it.
 *
 * Pass `initialThreadId` to open a specific conversation when the provider
 * mounts (Elements calls `runtime.threads.switchToThread` for us).
 */
export function useChatPageConfig(options?: {
  initialThreadId?: string;
}): ChatPageConfig {
  const initialThreadId = options?.initialThreadId;
  // The standalone page can answer about anything, like the global dock — no
  // tool filter.
  const includeAll = useCallback(() => true, []);
  const mcpConfig = useObservabilityMcpConfig({ toolsToInclude: includeAll });
  const { theme } = useMoonshineConfig();
  // The page is a dedicated chat surface, so resolve the assistant eagerly
  // (the dock resolves lazily on first open). `assistantId` scopes the
  // conversation list to this assistant's chats.
  const {
    transport,
    assistantId: managedAssistantId,
    ready,
    error,
  } = useServerAssistantTransport(mcpConfig.projectSlug, true);

  const config = useMemo<ElementsConfig>(
    () => ({
      ...mcpConfig,
      variant: "standalone",
      // Route through the persistent server-side Project Assistant; its model
      // and system prompt are owned server-side.
      transport,
      // Edit relies on assistant-ui's local branch rewriting, which the
      // server-side assistant transport can't honour — hide the affordance.
      allowMessageEdit: false,
      history: {
        enabled: true,
        threadListFilters: { assistant_id: managedAssistantId },
        // The assistant mints chat ids server-side, so defer client minting.
        deferThreadIdMinting: true,
        // Strip the backend `<message-context>` framing block (replay noise)
        // before Elements renders the transcript.
        transformChatMessage: stripMessageContextFraming,
        // When set, Elements loads and switches to this conversation on mount.
        initialThreadId,
      },
      api: {
        ...mcpConfig.api,
        headers: {
          ...mcpConfig.api?.headers,
          "X-Gram-Source": "dashboard-chat-page",
        },
      },
      welcome: {
        logo: speakeasyIcon,
        title: "Ask anything",
        subtitle:
          "Your assistant for exploring the platform — logs, traces, MCP servers, and more.",
        suggestions: INSIGHTS_SUGGESTIONS.default,
      },
      composer: {
        placeholder: "Ask anything",
        attachments: false,
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
      },
    }),
    [mcpConfig, transport, managedAssistantId, theme, initialThreadId],
  );

  return { config, ready, error };
}
