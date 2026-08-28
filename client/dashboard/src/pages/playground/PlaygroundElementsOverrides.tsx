import { Text } from "@/components/ui/Text";
import { ThreadPrimitive } from "@assistant-ui/react";
import { useGramElements } from "@/elements";
import {
  mcpToolsAvailability,
  mcpToolsWelcomeSubtitle,
  NO_MCP_TOOLS_MESSAGE,
} from "@/elements/lib/mcpToolsAvailability";
import { AlertCircle } from "lucide-react";
import type { FC } from "react";

/**
 * Custom ThreadWelcome component using Gram design system.
 * Displays centered empty state with title, subtitle, and optional suggestions.
 */
export const GramThreadWelcome: FC = () => {
  const { config, mcpTools, mcpToolsLoading, mcpToolsError } =
    useGramElements();
  const { title, subtitle, suggestions } = config.welcome ?? {};
  const availability = mcpToolsAvailability(
    mcpToolsLoading,
    mcpTools,
    mcpToolsError,
  );
  const resolvedSubtitle = mcpToolsWelcomeSubtitle(availability, subtitle);
  const visibleSuggestions =
    availability === "ready" ? (suggestions ?? []) : [];

  return (
    <div className="flex size-full flex-col items-center justify-center gap-3 p-8 text-center">
      <div className="space-y-1">
        <Text variant="subheading" className="font-medium">
          {title}
        </Text>
        <Text variant="small" muted>
          {resolvedSubtitle}
        </Text>
      </div>
      {visibleSuggestions.length > 0 && (
        <div className="mt-4 flex flex-wrap justify-center gap-2">
          {visibleSuggestions.map((suggestion, index) => (
            <ThreadPrimitive.Suggestion
              key={index}
              prompt={suggestion.prompt}
              send
              asChild
            >
              <button
                type="button"
                className="border-input bg-background hover:bg-accent hover:text-accent-foreground inline-flex cursor-pointer items-center rounded-full border px-3 py-1.5 text-sm transition-colors"
              >
                {suggestion.title}
              </button>
            </ThreadPrimitive.Suggestion>
          ))}
        </div>
      )}
    </div>
  );
};

/** Persistent warning when tools/list settled empty or failed. Hidden while loading. */
export function PlaygroundNoToolsBanner(): JSX.Element | null {
  const { mcpTools, mcpToolsLoading, mcpToolsError } = useGramElements();
  if (
    mcpToolsAvailability(mcpToolsLoading, mcpTools, mcpToolsError) !==
    "unavailable"
  ) {
    return null;
  }

  return (
    <div
      role="alert"
      className="bg-warning/15 border-warning/30 text-warning-foreground flex items-center gap-2 border-b px-4 py-2.5 text-sm font-medium"
    >
      <AlertCircle className="size-4 shrink-0" />
      <span>{NO_MCP_TOOLS_MESSAGE}</span>
    </div>
  );
}
