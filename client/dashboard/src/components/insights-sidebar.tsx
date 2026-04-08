import type { ElementsConfig } from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Wand2, ChevronRight, Sparkles, Terminal } from "lucide-react";
import { useState, useMemo, createContext, useContext } from "react";
import { cn } from "@/lib/utils";
import { devObservabilityMcpMissing } from "@/hooks/useObservabilityMcpConfig";

// Context for sidebar state
const InsightsContext = createContext<{
  isExpanded: boolean;
  setIsExpanded: (expanded: boolean) => void;
}>({
  isExpanded: false,
  setIsExpanded: () => {},
});

/**
 * Hook to access the insights sidebar state.
 * Returns { isExpanded, setIsExpanded } to allow pages to adapt their layout
 * and control the sidebar.
 */
export function useInsightsState() {
  return useContext(InsightsContext);
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
    () => ({ isExpanded, setIsExpanded }),
    [isExpanded],
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <div className="flex h-full w-full">
        {/* Main content area - shrinks when sidebar opens */}
        <div
          className="flex-1 min-w-0 transition-all duration-300 ease-in-out overflow-hidden"
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

        {/* Toggle button - shows when collapsed and not hidden */}
        {!hideTrigger && (
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className={cn(
              "fixed right-0 top-1/2 -translate-y-1/2 z-40 flex items-center gap-1.5 bg-primary text-primary-foreground px-3 py-2.5 rounded-l-lg shadow-lg hover:bg-primary/90 transition-all duration-300 group",
              isExpanded && "opacity-0 pointer-events-none",
            )}
            aria-label="Open AI Insights"
          >
            <Wand2 className="size-4" />
            <span className="text-sm font-medium">Ask AI</span>
          </button>
        )}

        {/* Sidebar panel - fixed position that slides in */}
        <div
          className={cn(
            "fixed right-0 top-0 bottom-0 z-30 flex flex-col bg-background border-l border-border shadow-xl transition-transform duration-300 ease-in-out",
            isExpanded ? "translate-x-0" : "translate-x-full",
          )}
          style={{ width: sidebarWidth }}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-muted/30">
            <div className="flex items-center gap-2">
              <Sparkles className="size-5 text-primary" />
              <span className="font-semibold">AI Insights</span>
            </div>
            <button
              onClick={() => setIsExpanded(false)}
              className="p-1.5 rounded hover:bg-muted transition-colors"
              aria-label="Close AI Insights"
            >
              <ChevronRight className="size-5" />
            </button>
          </div>

          {/* Dev notice when MCP is not configured */}
          {devObservabilityMcpMissing && !("mcp" in mcpConfig) && (
            <div className="mx-4 mt-3 flex items-start gap-2 rounded-md border border-border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
              <Terminal className="mt-0.5 size-3.5 shrink-0" />
              <span>
                AI tools are unavailable. Run{" "}
                <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
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
