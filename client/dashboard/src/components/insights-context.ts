import type { InsightsSuggestion } from "@/lib/insights-suggestions";
import type { ElementsConfig } from "@gram-ai/elements";
import { createContext, useContext, useLayoutEffect } from "react";

/**
 * Per-page overrides for the global AI Insights panel. Pages mount
 * <InsightsConfig {...} /> to register custom title/subtitle/suggestions
 * /context/mcpConfig; on unmount, the global defaults take over again.
 */
export interface InsightsConfigOptions {
  mcpConfig?: Omit<ElementsConfig, "variant" | "welcome" | "theme">;
  title?: string;
  subtitle?: string;
  suggestions?: InsightsSuggestion[];
  contextInfo?: string;
  /** Hide the trigger button (e.g., when logs are disabled on this page). */
  hideTrigger?: boolean;
}

export interface InsightsContextValue {
  available: boolean;
  isExpanded: boolean;
  setIsExpanded: (expanded: boolean) => void;
  /** Pages call this to register a per-page config override. Pass null to
   *  clear (typically on unmount of <InsightsConfig />). */
  setOverride: (override: InsightsConfigOptions | null) => void;
  /** Queue a prompt to be auto-appended to the Insights chat thread.
   *  Fires once per call — intended for "Explore with AI" CTAs that should
   *  drop the user straight into a running conversation. */
  sendPrompt: (prompt: string) => void;
  /** True once the shared Project Assistant runtime is mounted. Surfaces
   *  (e.g. the full-page chat) gate on this before rendering chat UI that
   *  needs the runtime. */
  assistantReady: boolean;
  /** Switch the shared runtime to a fresh empty conversation. */
  newConversation: () => void;
  /** Hide the floating dock while a caller is mounted (ref-counted). Returns
   *  an unregister fn. Independent of `setOverride`, so it survives consumers
   *  that reset the per-page override (e.g. the project dashboard). Prefer the
   *  `useHideInsightsDock` hook over calling this directly. */
  registerDockHide: () => () => void;
}

export const InsightsContext = createContext<InsightsContextValue>({
  available: false,
  isExpanded: false,
  setIsExpanded: () => {},
  setOverride: () => {},
  sendPrompt: () => {},
  assistantReady: false,
  newConversation: () => {},
  registerDockHide: () => () => {},
});

/**
 * Hook to access the insights sidebar state. `available` is false when no
 * InsightsProvider ancestor exists.
 */
export function useInsightsState(): InsightsContextValue {
  return useContext(InsightsContext);
}

/**
 * Hide the floating Project Assistant dock for as long as the calling
 * component is mounted. Use on pages that provide their own chat entry point
 * (e.g. the full-page chat, the home page widget). Ref-counted and independent
 * of the per-page `override`, so it survives consumers that reset the override.
 */
export function useHideInsightsDock(): void {
  const { registerDockHide } = useInsightsState();
  // Layout-timed so the dock is hidden before paint — a post-paint effect would
  // flash the floating dock for one frame when arriving from a dock-visible page.
  useLayoutEffect(() => registerDockHide(), [registerDockHide]);
}
