import type { ElementsConfig } from "@gram-ai/elements";
import { createContext, useContext } from "react";

/**
 * Per-page overrides for the global AI Insights panel. Pages mount
 * <InsightsConfig {...} /> to register custom title/subtitle/suggestions
 * /context/mcpConfig; on unmount, the global defaults take over again.
 */
export interface InsightsConfigOptions {
  mcpConfig?: Omit<ElementsConfig, "variant" | "welcome" | "theme">;
  title?: string;
  subtitle?: string;
  suggestions?: Array<{
    title: string;
    label: string;
    prompt: string;
  }>;
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
}

export const InsightsContext = createContext<InsightsContextValue>({
  available: false,
  isExpanded: false,
  setIsExpanded: () => {},
  setOverride: () => {},
  sendPrompt: () => {},
});

/**
 * Hook to access the insights sidebar state. `available` is false when no
 * InsightsProvider ancestor exists.
 */
export function useInsightsState() {
  return useContext(InsightsContext);
}
