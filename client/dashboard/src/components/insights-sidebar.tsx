import { useAiInsightsMcpConfig } from "@/hooks/useAiInsightsMcpConfig";
import { devObservabilityMcpMissing } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { useAssistantRuntime } from "@assistant-ui/react";
import type { ElementsConfig } from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import {
  invalidateAllInsightsListMemories,
  invalidateAllInsightsListProposals,
  useInsightsListMemories,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import {
  ChevronRight,
  RotateCw,
  Sparkles,
  Terminal,
  Wand2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { InsightsConfigOptions } from "./insights-context";
import { InsightsContext, useInsightsState } from "./insights-context";
import { MemoryPill } from "./insights/MemoryPill";
import { ProposalsPanel } from "./insights/ProposalsPanel";

// Types-only re-export (erased at compile time, won't break Fast Refresh)
export type { InsightsConfigOptions } from "./insights-context";

/**
 * Cycles an element's `color` through the Speakeasy brand rainbow on hover —
 * used for small icon-only "Explore with AI" CTAs where a border treatment
 * would be invisible. Requires <InsightsRainbowStyles /> in the tree.
 */
export const INSIGHTS_AI_RAINBOW_CLASS = "insights-ai-rainbow";

/**
 * Reveals a full-spectrum Speakeasy brand gradient border on hover — same
 * 9-stop palette as the login page's BrandGradientBar. Used for the nav-bar
 * AI Insights trigger where the button shape can host a real border.
 * Requires <InsightsRainbowStyles /> in the tree. Works best on elements
 * with a 1px border and a border-radius.
 */
export const INSIGHTS_AI_RAINBOW_BORDER_CLASS = "insights-ai-rainbow-border";

function InsightsRainbowStyles() {
  return (
    <style>{`
      @keyframes insights-ai-rainbow {
        0%   { color: #C83228; }
        16%  { color: #FB873F; }
        33%  { color: #D2DC91; }
        50%  { color: #5A8250; }
        66%  { color: #2873D7; }
        83%  { color: #9BC3FF; }
        100% { color: #C83228; }
      }
      .${INSIGHTS_AI_RAINBOW_CLASS}:hover {
        animation: insights-ai-rainbow 2.5s linear infinite;
      }

      /* Gradient border via a masked pseudo-element. Mask cuts the interior
         so only the 1px ring shows; border-radius is inherited so rounded
         corners stay intact. Fades in on hover; the underlying border goes
         transparent so we don't double up. */
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS} {
        position: relative;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}::before {
        content: "";
        position: absolute;
        inset: 0;
        padding: 1px;
        border-radius: inherit;
        background: linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%);
        -webkit-mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
        -webkit-mask-composite: xor;
        mask: linear-gradient(#fff 0 0) content-box, linear-gradient(#fff 0 0);
        mask-composite: exclude;
        opacity: 0;
        transition: opacity 200ms ease;
        pointer-events: none;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}:hover::before {
        opacity: 1;
      }
      .${INSIGHTS_AI_RAINBOW_BORDER_CLASS}:hover {
        border-color: transparent;
      }
    `}</style>
  );
}

/**
 * Header-bar trigger for opening the AI Insights sidebar. Renders only
 * when inside an InsightsProvider so it can be slotted globally (e.g. into
 * PageHeaderBreadcrumbs) without appearing on pages that opt out via
 * hideTrigger.
 */
export function InsightsTrigger({ className }: { className?: string }) {
  const { available, isExpanded, setIsExpanded } = useInsightsState();
  if (!available) return null;
  return (
    <button
      type="button"
      onClick={() => setIsExpanded(!isExpanded)}
      aria-label={isExpanded ? "Close AI Insights" : "Open AI Insights"}
      aria-pressed={isExpanded}
      className={cn(
        "border-border hover:bg-accent hover:text-accent-foreground inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-sm transition-colors",
        isExpanded && "bg-accent text-accent-foreground",
        INSIGHTS_AI_RAINBOW_BORDER_CLASS,
        className,
      )}
    >
      <Wand2 className="size-3.5" />
      <span className="font-medium">AI Insights</span>
    </button>
  );
}

/**
 * Page-level config override. Mount this anywhere inside an InsightsProvider
 * to swap in a custom prompt/suggestions/MCP filter. Cleans up on unmount,
 * restoring the provider's defaults.
 */
export function InsightsConfig(options: InsightsConfigOptions) {
  const { setOverride } = useInsightsState();
  // Stringify the options so the effect re-fires only when content changes,
  // not on every parent render that creates a fresh object identity.
  const key = JSON.stringify(options);
  useEffect(() => {
    setOverride(options);
    return () => setOverride(null);
  }, [key, setOverride, options]);
  return null;
}

interface InsightsProviderProps {
  /** Default MCP config used when no <InsightsConfig> override is mounted. */
  mcpConfig: Omit<ElementsConfig, "variant" | "welcome" | "theme">;
  /** Default welcome title. */
  title: string;
  /** Default welcome subtitle. */
  subtitle: string;
  /** Default suggestion prompts. */
  suggestions?: Array<{
    title: string;
    label: string;
    prompt: string;
  }>;
  /** Default expanded state. */
  defaultExpanded?: boolean;
  /** Children rendered alongside the sidebar (page content). */
  children: React.ReactNode;
}

const SIDEBAR_MAX_WIDTH = 670;
const SIDEBAR_MAX_PERCENT = 40; // Never more than 40% of viewport

export function InsightsProvider({
  mcpConfig: defaultMcpConfig,
  title: defaultTitle,
  subtitle: defaultSubtitle,
  suggestions: defaultSuggestions = [],
  defaultExpanded = false,
  children,
}: InsightsProviderProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const [override, setOverride] = useState<InsightsConfigOptions | null>(null);
  const [pendingPrompt, setPendingPrompt] = useState<{
    text: string;
    nonce: number;
  } | null>(null);
  // Bumping this remounts <GramElementsProvider> via the `key` prop, which
  // drops the chat thread + forces a fresh chatSessions.create call. Use when
  // the chat session goes stale (token expired, MCP env decryption failure
  // since fixed, etc.) and the user wants a clean slate without a full reload.
  const [sessionEpoch, setSessionEpoch] = useState(0);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const queryClient = useQueryClient();
  const refreshSession = useCallback(() => {
    if (isRefreshing) return;
    setIsRefreshing(true);
    // (1) Remount the chat provider so the chat session token + thread reset.
    setSessionEpoch((e) => e + 1);
    // (2) Invalidate the proposals + memories caches so the panels pick up
    // anything the agent created since the last fetch (the agent's MCP
    // mutations don't tickle React Query on the dashboard side).
    void invalidateAllInsightsListProposals(queryClient);
    void invalidateAllInsightsListMemories(queryClient);
    // 700ms gives the spin enough visual presence to register as a deliberate
    // action without feeling laggy. The provider remounts immediately on epoch
    // change; the timeout is purely UX feedback.
    window.setTimeout(() => setIsRefreshing(false), 700);
  }, [isRefreshing, queryClient]);
  const { theme } = useMoonshineConfig();

  // Ai-insights MCP lives at the same Gram origin as the rest of the
  // dashboard. We reuse the observability MCP config's transport/auth (api
  // url, session, environment headers) because both MCPs share the same
  // server — different URLs on the same origin don't need different auth.
  // If that assumption ever breaks (e.g. the ai-insights MCP moves to a
  // different host), this is the place to split transport per MCP.
  const aiInsightsMcpConfig = useAiInsightsMcpConfig();

  // Resolve effective values: per-page override wins, fall back to defaults.
  const rawMcpConfig = override?.mcpConfig ?? defaultMcpConfig;
  const mcpConfig = useMemo<typeof rawMcpConfig>(() => {
    const observabilityMcp = rawMcpConfig.mcp;
    const aiInsightsMcp = aiInsightsMcpConfig.mcp;
    const urls: string[] = [];
    if (typeof observabilityMcp === "string" && observabilityMcp) {
      urls.push(observabilityMcp);
    } else if (Array.isArray(observabilityMcp)) {
      urls.push(...observabilityMcp);
    }
    if (typeof aiInsightsMcp === "string" && aiInsightsMcp) {
      urls.push(aiInsightsMcp);
    } else if (Array.isArray(aiInsightsMcp)) {
      urls.push(...aiInsightsMcp);
    }
    if (urls.length === 0) {
      return rawMcpConfig;
    }
    // Pages narrow the observability tool list via `tools.toolsToInclude`
    // (e.g. ChatLogs.tsx whitelists just gram_search_logs/gram_search_chats).
    // Elements applies that filter to the *merged* tool list across all MCPs,
    // which would silently drop every insights_* tool from the ai-insights
    // MCP. Wrap the page's filter so insights_* tools are always allowed
    // through regardless of how the observability surface is narrowed.
    const rawFilter = rawMcpConfig.tools?.toolsToInclude;
    const wrappedFilter = ({ toolName }: { toolName: string }) => {
      if (toolName.startsWith("insights_")) return true;
      if (typeof rawFilter === "function") return rawFilter({ toolName });
      if (Array.isArray(rawFilter)) return rawFilter.includes(toolName);
      return true; // No filter → include everything.
    };
    return {
      ...rawMcpConfig,
      mcp: urls,
      tools: { ...rawMcpConfig.tools, toolsToInclude: wrappedFilter },
    };
  }, [rawMcpConfig, aiInsightsMcpConfig]);
  const title = override?.title ?? defaultTitle;
  const subtitle = override?.subtitle ?? defaultSubtitle;
  const suggestions = override?.suggestions ?? defaultSuggestions;
  const contextInfo = override?.contextInfo;
  const hideTrigger = override?.hideTrigger ?? false;

  const sidebarWidth = `min(${SIDEBAR_MAX_WIDTH}px, ${SIDEBAR_MAX_PERCENT}vw)`;

  // Memory slice — only fetch when the sidebar is open. The chat agent
  // sees the top-20 most-recently-used memories injected as
  // <workspace_memory> bullets at the top of its system prompt.
  // throwOnError: false — a 401/5xx must degrade to "no memories" so the
  // entire dashboard isn't replaced by the global error boundary.
  const { data: memoriesData } = useInsightsListMemories(
    { limit: 20 },
    undefined,
    {
      enabled: isExpanded,
      throwOnError: false,
      // Poll while the sidebar is open — picks up memories the agent creates
      // during the same conversation without requiring a manual refresh.
      refetchInterval: 10_000,
    },
  );
  const memories = memoriesData?.memories ?? [];
  const workspaceMemoryBlock = useMemo(() => {
    if (memories.length === 0) return "";
    const bullets = memories
      .map((m) => {
        const tagPart = m.tags.length > 0 ? ` [${m.tags.join(", ")}]` : "";
        return `- (${m.kind})${tagPart} ${m.content}`;
      })
      .join("\n");
    return `<workspace_memory>
Relevant facts, playbooks, and findings from prior sessions in this project. Consult these before asking the user for context they may have already provided.
${bullets}
</workspace_memory>

`;
  }, [memories]);

  // Investigation protocol. The imperative voice on step 3 + the standalone
  // "memory creation is non-optional" paragraph below are deliberate: in
  // practice, models systematically under-invoke state-creating actions
  // (memory writes) in favor of immediate-visible-feedback ones (proposals
  // the user will see). Phrasing memory writes as MUST + tying them to a
  // concrete consequence ("future sessions lose this context") is what
  // converts the model from "memory exists" to "memory used."
  const investigationProtocol = `
When asked to diagnose an issue, follow this loop:
(1) Form a single hypothesis.
(2) Gather evidence with read tools.
(3) You MUST call \`insights_record_finding\` with at least one one-line bullet summarizing what you learned, BEFORE proposing any fix. Findings auto-expire after 7 days and are how investigations leave a trail; skipping this step loses the value of the investigation between sessions.
(4) If evidence points to a tool or toolset fix, call \`insights_propose_variation\` or \`insights_propose_toolset_change\` with a clear \`reasoning\`.
(5) After completing the investigation, if the finding generalizes (e.g. "users across our API consistently confuse slugs with UUIDs", "auth failures cluster on a specific tool family"), you MUST also call \`insights_remember\` with kind=fact or kind=playbook so the next session inherits this knowledge instead of rediscovering it. Do not skip this for repeating patterns — that is the entire point of workspace memory.

Do NOT apply proposals yourself; the human reviews and applies.`;

  // Build system prompt with optional context info.
  const baseInstructions = `${workspaceMemoryBlock}You are a helpful assistant for analyzing logs in Gram, an AI observability platform. Focus exclusively on log search and analysis.

The current date is ${new Date().toISOString().split("T")[0]}.

Important: Treat all 4xx HTTP status codes (400, 401, 403, 404, etc.) as errors. From the user's perspective these indicate real problems — authentication failures, misconfigured requests, missing resources, etc.

Custom attributes: SDK users can attach arbitrary key-value attributes to their logs. These appear with an @ prefix (e.g. @user, @tenant.id, @session). Standard system attributes have no prefix.

When a user asks about logs for a specific user, tenant, customer, or entity:
1. Always call listAttributeKeys first for the relevant time window to discover which @-prefixed attributes exist.
2. Identify the most relevant attribute and filter on it (e.g. { path: "@user", operator: "eq", values: ["someone@example.com"] }).
3. If no relevant @-prefixed attributes exist, tell the user and fall back to text search instead.
${investigationProtocol}`;

  const systemPrompt = contextInfo
    ? `${baseInstructions}

Current dashboard context:
${contextInfo}

When the user asks about "current period", "selected period", "this timeframe", or similar, use the date range from the context above. Do not ask the user to specify a date range if it's already provided in the context.`
    : baseInstructions;

  // New config identity on every override change is intentional: clicking
  // "Explore with AI" on a different chart should drop the user into a fresh,
  // focused conversation with the new contextInfo, not splice a new system
  // prompt into an in-flight thread from a different chart. If we ever want
  // to preserve threads across Explore clicks, split transport config
  // (mcpConfig/model/theme) from presentation config (systemPrompt/welcome)
  // inside GramElementsProvider so only the transport piece is stable.
  const elementsConfig = useMemo<ElementsConfig>(
    () => ({
      ...mcpConfig,
      variant: "standalone",
      systemPrompt,
      model: {
        defaultModel: "anthropic/claude-sonnet-4.6",
      },
      api: {
        ...mcpConfig.api,
        headers: {
          ...mcpConfig.api?.headers,
          "X-Gram-Source": "dashboard-ai-insights",
        },
      },
      tools: {
        ...mcpConfig.tools,
        // Cap individual MCP tool outputs to ~12.5K tokens. Observability
        // queries (gram_search_logs, gram_get_deployment_logs) can return
        // hundreds of KB; without this cap, one wide search fills the
        // context window.
        maxOutputBytes: 50_000,
      },
      contextCompaction: {
        // Start compacting at 60% of the model ceiling — Insights runs long
        // tool-heavy conversations and benefits from a tighter margin than
        // the library default of 70%.
        compactAtFraction: 0.6,
      },
      welcome: {
        title,
        subtitle,
        suggestions,
      },
      theme: {
        colorScheme: theme === "dark" ? "dark" : "light",
      },
    }),
    [mcpConfig, title, subtitle, suggestions, theme, systemPrompt],
  );
  // workspaceMemoryBlock + investigationProtocol flow into systemPrompt, so
  // the memo above picks them up through that dep. Keeping them out of the
  // dep list avoids triggering extra config-identity churn when React Query
  // rehydrates equivalent memory data.

  const handleSetOverride = useCallback(
    (next: InsightsConfigOptions | null) => setOverride(next),
    [],
  );

  const handleSendPrompt = useCallback((text: string) => {
    // Nonce lets the bridge detect repeat clicks on the same prompt (same
    // chart twice in a row); reference-equal objects would otherwise be
    // skipped by the bridge's useEffect.
    setPendingPrompt({ text, nonce: Date.now() });
  }, []);

  const consumePendingPrompt = useCallback(() => setPendingPrompt(null), []);

  const contextValue = useMemo(
    () => ({
      available: !hideTrigger,
      isExpanded,
      setIsExpanded,
      setOverride: handleSetOverride,
      sendPrompt: handleSendPrompt,
    }),
    [hideTrigger, isExpanded, handleSetOverride, handleSendPrompt],
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <InsightsRainbowStyles />
      <div className="flex h-full w-full">
        {/* Main content area - shrinks when sidebar opens */}
        <div
          className="min-w-0 flex-1 overflow-hidden transition-all duration-300 ease-in-out"
          style={{
            marginRight: isExpanded ? sidebarWidth : 0,
          }}
        >
          {children}
        </div>

        {/* Backdrop overlay - closes sidebar when clicked */}
        {isExpanded && (
          <div
            className="fixed inset-0 z-20"
            onClick={() => setIsExpanded(false)}
            aria-hidden="true"
          />
        )}

        {/* Sidebar panel - fixed position that slides in.
            Trigger lives in the top breadcrumb bar via <InsightsTrigger />. */}
        <div
          className={cn(
            "bg-background border-border fixed top-0 right-0 bottom-0 z-30 flex flex-col border-l shadow-xl transition-transform duration-300 ease-in-out",
            isExpanded ? "translate-x-0" : "translate-x-full",
          )}
          style={{ width: sidebarWidth }}
        >
          {/* Header */}
          <div className="border-border bg-muted/30 flex items-center justify-between border-b px-4 py-3">
            <div className="flex items-center gap-2">
              <Sparkles className="text-primary size-5" />
              <span className="font-semibold">AI Insights</span>
            </div>
            <div className="flex items-center gap-1">
              <button
                onClick={refreshSession}
                disabled={isRefreshing}
                className={cn(
                  "hover:bg-muted text-muted-foreground rounded p-1.5 transition-colors",
                  isRefreshing && "cursor-not-allowed opacity-70",
                )}
                aria-label="Refresh AI Insights session"
                aria-busy={isRefreshing}
                title="Start a new chat session"
              >
                <RotateCw
                  className={cn(
                    "size-4 transition-transform",
                    isRefreshing && "animate-spin",
                  )}
                />
              </button>
              <button
                onClick={() => setIsExpanded(false)}
                className="hover:bg-muted rounded p-1.5 transition-colors"
                aria-label="Close AI Insights"
              >
                <ChevronRight className="size-5" />
              </button>
            </div>
          </div>

          {/* Dev notice when MCP is not configured */}
          {devObservabilityMcpMissing && !("mcp" in mcpConfig) && (
            <div className="border-border bg-muted/50 text-muted-foreground mx-4 mt-3 flex items-start gap-2 rounded-md border px-3 py-2 text-xs">
              <Terminal className="mt-0.5 size-3.5 shrink-0" />
              <span>
                AI tools are unavailable. Run{" "}
                <code className="bg-muted text-foreground rounded px-1 py-0.5 font-mono">
                  mise seed
                </code>{" "}
                to enable the observability MCP server.
              </span>
            </div>
          )}

          {/* Proposals + memory (only mount when expanded so queries don't
              fire for closed sidebars). */}
          {isExpanded && (
            <>
              <ProposalsPanel />
              <MemoryPill />
            </>
          )}

          {/* Chat content */}
          <div className="flex-1 overflow-hidden">
            <GramElementsProvider key={sessionEpoch} config={elementsConfig}>
              <PendingPromptBridge
                pending={pendingPrompt}
                onConsume={consumePendingPrompt}
              />
              <Chat />
            </GramElementsProvider>
          </div>
        </div>
      </div>
    </InsightsContext.Provider>
  );
}

/**
 * @deprecated Use <InsightsProvider> at the app shell level + <InsightsConfig>
 * on individual pages that need custom prompts. This alias is kept temporarily
 * to avoid breaking out-of-tree consumers; remove after migration.
 */
export const InsightsSidebar = InsightsProvider;

/**
 * Lives inside GramElementsProvider so it can access useThreadRuntime().
 * When a pending prompt is queued via InsightsContext.sendPrompt, this bridge
 * appends it to the current thread as a user message, triggering the
 * assistant to respond. It fires once per nonce so repeat clicks on the
 * same CTA still work.
 */
function PendingPromptBridge({
  pending,
  onConsume,
}: {
  pending: { text: string; nonce: number } | null;
  onConsume: () => void;
}) {
  const assistantRuntime = useAssistantRuntime();
  const firedNonceRef = useRef<number | null>(null);

  useEffect(() => {
    if (!pending || !assistantRuntime) return;
    if (firedNonceRef.current === pending.nonce) return;
    firedNonceRef.current = pending.nonce;

    const { text } = pending;
    // Switch to a brand-new thread before appending. This sidesteps the
    // assistant-ui MessageRepository id-collision error
    // ("A message with the same id already exists in the parent tree")
    // that triggers when a second Explore click tries to append into a
    // thread that still holds messages from the previous chart's
    // conversation. It also matches the intended product UX: each Explore
    // CTA starts a fresh focused chat with the new contextInfo.
    assistantRuntime.threads
      .switchToNewThread()
      .then(() => {
        assistantRuntime.thread.append(text);
      })
      .catch((err: unknown) => {
        // eslint-disable-next-line no-console
        console.error("Failed to send Explore prompt:", err);
      });

    onConsume();
  }, [pending, assistantRuntime, onConsume]);

  return null;
}
