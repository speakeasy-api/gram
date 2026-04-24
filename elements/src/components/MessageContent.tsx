"use client";

import { FC, useMemo } from "react";
import { ElementsContext } from "@/contexts/contexts";
import { ToolExecutionProvider } from "@/contexts/ToolExecutionContext";
import type { ElementsContextType, Model } from "@/types";
import { recommended } from "@/plugins";
import { chart } from "@/plugins/chart";
import { generativeUI } from "@/plugins/generative-ui";
import { parseSegments } from "./MessageContent.parser";

const SUPPORTED_LANGUAGES: Record<string, FC<{ code: string }>> = {
  chart: chart.Component as FC<{ code: string }>,
  ui: generativeUI.Component as FC<{ code: string }>,
};

// Minimal stub ElementsContext value. The chart/ui plugin renderers reach into
// ElementsContext via useDensity()/useElements() to read density classes, so a
// static viewer must provide the context — but it doesn't need any of the
// runtime/auth/MCP machinery that GramElementsProvider sets up.
const STUB_CONTEXT: ElementsContextType = {
  config: {
    projectSlug: "",
  },
  setModel: () => {},
  model: "" as Model,
  isExpanded: false,
  setIsExpanded: () => {},
  isOpen: false,
  setIsOpen: () => {},
  plugins: recommended,
  mcpTools: undefined,
};

export interface MessageContentProps {
  /** Raw assistant message content (markdown text optionally containing
   * ```chart and ```ui fenced code blocks). */
  content: string;
  /** Optional className applied to the root container. */
  className?: string;
}

/**
 * Standalone renderer for stored chat message content. Recognizes the same
 * `chart` and `ui` fenced code blocks that the live `<Chat />` component
 * renders as widgets — but works as a plain component without requiring an
 * `ElementsProvider`, MCP client, auth session, or assistant-ui runtime.
 *
 * Use this in static viewers (agent session detail panels, replay, share
 * pages, etc.) so a `Tool Calls by Source — Last 7 Days` chart appears as
 * the actual bar chart it was rendered as live, instead of as raw JSON text.
 *
 * Plain markdown formatting is intentionally **not** applied — text segments
 * render as preformatted text. If the surrounding viewer needs full markdown,
 * mount this inside its own markdown component and let the markdown renderer
 * delegate `chart`/`ui` code blocks to this component instead.
 */
export const MessageContent: FC<MessageContentProps> = ({
  content,
  className,
}) => {
  const segments = useMemo(() => parseSegments(content), [content]);

  return (
    <ElementsContext.Provider value={STUB_CONTEXT}>
      {/* Empty ToolExecutionProvider so generative-ui's <ActionButton> sees
          isToolAvailable() === false and renders disabled instead of a
          live-looking button that no-ops on click. Static viewers (session
          detail panel, replay) intentionally don't run tool calls; the
          ActionButton's defensive fallback would also catch this, but
          mounting the provider explicitly makes the intent visible in code. */}
      <ToolExecutionProvider tools={{}}>
        <div className={className}>
          {segments.map((seg, i) => {
            if (seg.type === "text") {
              // Skip purely-whitespace text segments between adjacent widgets
              // so the layout doesn't get blank line-height runs.
              if (seg.text.trim() === "") return null;
              return (
                <div key={i} className="whitespace-pre-wrap">
                  {seg.text}
                </div>
              );
            }
            const Component = SUPPORTED_LANGUAGES[seg.lang];
            if (!Component) return null;
            return (
              <div key={i} className="my-2">
                <Component code={seg.code} />
              </div>
            );
          })}
        </div>
      </ToolExecutionProvider>
    </ElementsContext.Provider>
  );
};
