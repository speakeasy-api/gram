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

// Provides only what useDensity()/useElements() read inside the chart and ui
// renderers — no auth, no MCP, no runtime.
const STUB_CONTEXT: ElementsContextType = {
  config: { projectSlug: "" },
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
 * Standalone renderer for stored chat message content. Recognises the same
 * `chart` and `ui` fenced code blocks that the live `<Chat />` component
 * renders as widgets, but works without an `ElementsProvider`, MCP client,
 * auth session, or assistant-ui runtime.
 *
 * Use in static viewers (agent session detail panel, replay, share) so a
 * stored bar chart appears as a chart instead of as raw JSON. Plain markdown
 * formatting is intentionally not applied — text segments render as
 * preformatted text.
 */
export const MessageContent: FC<MessageContentProps> = ({
  content,
  className,
}) => {
  const segments = useMemo(() => parseSegments(content), [content]);

  return (
    <ElementsContext.Provider value={STUB_CONTEXT}>
      {/* Empty tools so generative-ui's <ActionButton> renders disabled. */}
      <ToolExecutionProvider tools={{}}>
        <div className={className}>
          {segments.map((seg, i) => {
            if (seg.type === "text") {
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
