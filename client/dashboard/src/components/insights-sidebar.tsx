import type { ElementsConfig } from "@gram-ai/elements";
import { Chat, GramElementsProvider } from "@gram-ai/elements";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { Wand2, ChevronRight, Sparkles } from "lucide-react";
import { useState, useMemo, createContext, useContext } from "react";
import { cn } from "@/lib/utils";

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
  const baseInstructions = `You are a helpful assistant for analyzing observability data.

Important: When analyzing logs and errors, treat all 4xx HTTP status codes (400, 401, 403, 404, etc.) as errors, even though they are technically client errors rather than server errors. From the user's perspective, these responses often indicate problems that need attention (authentication failures, misconfigured requests, missing resources, etc.).`;

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
    [isExpanded, setIsExpanded],
  );

  return (
    <InsightsContext.Provider value={contextValue}>
      <div className="flex h-full w-full">
        {/* Main content area - shrinks when sidebar opens */}
        <div
          className="flex-1 min-w-0 transition-all duration-300 ease-in-out overflow-y-auto overflow-x-hidden"
          style={{
            marginRight: isExpanded ? sidebarWidth : 0,
          }}
        >
          {children}
        </div>

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
              <span className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-500">
                Beta
              </span>
            </div>
            <button
              onClick={() => setIsExpanded(false)}
              className="p-1.5 rounded hover:bg-muted transition-colors"
              aria-label="Close AI Insights"
            >
              <ChevronRight className="size-5" />
            </button>
          </div>

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
