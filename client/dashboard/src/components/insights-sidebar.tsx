import type { ElementsConfig } from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Wand2, ChevronRight, Sparkles, Terminal } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import { devObservabilityMcpMissing } from "@/hooks/useObservabilityMcpConfig";
import { InsightsContext, useInsightsState } from "./insights-context";
import type { InsightsConfigOptions } from "./insights-context";

// Types-only re-export (erased at compile time, won't break Fast Refresh)
export type { InsightsConfigOptions } from "./insights-context";

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
  const { theme } = useMoonshineConfig();

  // Resolve effective values: per-page override wins, fall back to defaults.
  const mcpConfig = override?.mcpConfig ?? defaultMcpConfig;
  const title = override?.title ?? defaultTitle;
  const subtitle = override?.subtitle ?? defaultSubtitle;
  const suggestions = override?.suggestions ?? defaultSuggestions;
  const contextInfo = override?.contextInfo;
  const hideTrigger = override?.hideTrigger ?? false;

  const sidebarWidth = `min(${SIDEBAR_MAX_WIDTH}px, ${SIDEBAR_MAX_PERCENT}vw)`;

  // Build system prompt with optional context info.
  const baseInstructions = `You are a helpful assistant for analyzing logs in Gram, an AI observability platform. Focus exclusively on log search and analysis.

The current date is ${new Date().toISOString().split("T")[0]}.

Important: Treat all 4xx HTTP status codes (400, 401, 403, 404, etc.) as errors. From the user's perspective these indicate real problems — authentication failures, misconfigured requests, missing resources, etc.

Custom attributes: SDK users can attach arbitrary key-value attributes to their logs. These appear with an @ prefix (e.g. @user, @tenant.id, @session). Standard system attributes have no prefix.

When a user asks about logs for a specific user, tenant, customer, or entity:
1. Always call listAttributeKeys first for the relevant time window to discover which @-prefixed attributes exist.
2. Identify the most relevant attribute and filter on it (e.g. { path: "@user", operator: "eq", values: ["someone@example.com"] }).
3. If no relevant @-prefixed attributes exist, tell the user and fall back to text search instead.`;

  const systemPrompt = contextInfo
    ? `${baseInstructions}

Current dashboard context:
${contextInfo}

When the user asks about "current period", "selected period", "this timeframe", or similar, use the date range from the context above. Do not ask the user to specify a date range if it's already provided in the context.`
    : baseInstructions;

  const elementsConfig = useMemo<ElementsConfig>(
    () => ({
      ...mcpConfig,
      variant: "standalone",
      systemPrompt,
      model: {
        defaultModel: "anthropic/claude-sonnet-4.6",
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

  const handleSetOverride = useCallback(
    (next: InsightsConfigOptions | null) => setOverride(next),
    [],
  );

  const contextValue = useMemo(
    () => ({
      available: !hideTrigger,
      isExpanded,
      setIsExpanded,
      setOverride: handleSetOverride,
    }),
    [hideTrigger, isExpanded, handleSetOverride],
  );

  return (
    <InsightsContext.Provider value={contextValue}>
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
            <button
              onClick={() => setIsExpanded(false)}
              className="hover:bg-muted rounded p-1.5 transition-colors"
              aria-label="Close AI Insights"
            >
              <ChevronRight className="size-5" />
            </button>
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

          {/* Chat content */}
          <div className="flex-1 overflow-hidden">
            <GramElementsProvider config={elementsConfig}>
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
