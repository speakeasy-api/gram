import type { ElementsConfig } from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Wand2, ChevronRight, Sparkles, Terminal } from "lucide-react";
import { useState, useMemo, createContext, useContext } from "react";
import { cn } from "@/lib/utils";
import { devObservabilityMcpMissing } from "@/hooks/useObservabilityMcpConfig";

// Context for sidebar state. `available` lets descendants detect whether
// they're inside an InsightsSidebar provider — so a page-header-level
// trigger can self-hide on pages that aren't wrapped.
const InsightsContext = createContext<{
  available: boolean;
  isExpanded: boolean;
  setIsExpanded: (expanded: boolean) => void;
}>({
  available: false,
  isExpanded: false,
  setIsExpanded: () => {},
});

/**
 * Hook to access the insights sidebar state.
 * Returns { available, isExpanded, setIsExpanded } to allow pages to adapt
 * their layout and control the sidebar. `available` is false when no
 * InsightsSidebar ancestor exists.
 */
export function useInsightsState() {
  return useContext(InsightsContext);
}

/**
 * Header-bar trigger for opening the AI Insights sidebar. Renders only
 * when inside an InsightsSidebar provider so it can be slotted globally
 * (e.g. into PageHeaderBreadcrumbs) without appearing on pages that don't
 * use insights.
 */
export function InsightsTrigger({ className }: { className?: string }) {
  const { available, isExpanded, setIsExpanded } = useInsightsState();
  if (!available) return null;
  return (
    <>
      {/* SVG gradient def for the wand icon stroke — referenced by url(#…)
          on the Wand2 below. Uses Gram brand-primary reds so the icon
          reads as "AI/magic" while the surrounding button stays neutral. */}
      <svg
        width="0"
        height="0"
        className="absolute"
        aria-hidden="true"
        focusable="false"
      >
        <defs>
          <linearGradient
            id="gram-brand-gradient"
            x1="0%"
            y1="0%"
            x2="100%"
            y2="100%"
          >
            <stop offset="0%" stopColor="hsl(334, 54%, 13%)" />
            <stop offset="50%" stopColor="hsl(4, 67%, 47%)" />
            <stop offset="100%" stopColor="hsl(23, 96%, 62%)" />
          </linearGradient>
        </defs>
      </svg>
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
        <Wand2
          className="size-3.5"
          style={{ stroke: "url(#gram-brand-gradient)" }}
        />
        <span className="font-medium">AI Insights</span>
      </button>
    </>
  );
}

interface InsightsSidebarProps {
  /** Base MCP config from useObservabilityMcpConfig */
  mcpConfig: Omit<ElementsConfig, "variant" | "welcome" | "theme">;
  /** Title shown in the chat welcome screen */
  title: string;
  /** Subtitle shown in the chat welcome screen */
  subtitle: string;
  /** Suggestion prompts for quick actions */
  suggestions?: Array<{
    title: string;
    label: string;
    prompt: string;
  }>;
  /** Default expanded state */
  defaultExpanded?: boolean;
  /** Context information to pass to the chat (like current date range) */
  contextInfo?: string;
  /** Hide the trigger button (e.g., when logs are disabled) */
  hideTrigger?: boolean;
  /** Main content to render alongside the sidebar */
  children: React.ReactNode;
}

const SIDEBAR_MAX_WIDTH = 670;
const SIDEBAR_MAX_PERCENT = 40; // Never more than 40% of viewport

export function InsightsSidebar({
  mcpConfig,
  title,
  subtitle,
  suggestions = [],
  defaultExpanded = false,
  contextInfo,
  hideTrigger = false,
  children,
}: InsightsSidebarProps) {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);
  const { theme } = useMoonshineConfig();

  // Calculate responsive sidebar width (min of fixed width or 40% of viewport)
  const sidebarWidth = `min(${SIDEBAR_MAX_WIDTH}px, ${SIDEBAR_MAX_PERCENT}vw)`;

  // Build system prompt with context info
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
        defaultModel: "anthropic/claude-sonnet-4.5",
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

  const contextValue = useMemo(
    () => ({ available: !hideTrigger, isExpanded, setIsExpanded }),
    [hideTrigger, isExpanded],
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
            Note: the trigger for opening this panel now lives in the top
            breadcrumb bar via <InsightsTrigger />, rendered by PageHeader. */}
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
