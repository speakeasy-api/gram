import { useEffect } from "react";

/**
 * Cross-tree bridge for the command palette's "Ask AI" row.
 *
 * The command palette mounts at the App root (App.tsx), OUTSIDE the
 * InsightsProvider that owns the Project Assistant sidebar. So the palette
 * can't call `useInsightsState().sendPrompt` directly. Instead it dispatches a
 * window CustomEvent; a listener inside InsightsProvider picks it up and opens
 * the sidebar with the prompt. This keeps the two subsystems decoupled — the
 * palette knows nothing about the assistant beyond this event contract.
 */
const ASK_AI_EVENT = "gram:command-palette:ask-ai";

interface AskAiEventDetail {
  /** Free-form prompt typed into the palette. Empty string just opens the
   *  assistant sidebar without seeding a message. */
  prompt: string;
}

/** Fire from the palette when the user selects the "Ask AI" row. */
export function requestAskAi(prompt: string): void {
  window.dispatchEvent(
    new CustomEvent<AskAiEventDetail>(ASK_AI_EVENT, { detail: { prompt } }),
  );
}

/** Subscribe (inside InsightsProvider) to "Ask AI" requests from the palette. */
export function useAskAiListener(handler: (prompt: string) => void): void {
  useEffect(() => {
    const onEvent = (event: Event) => {
      handler((event as CustomEvent<AskAiEventDetail>).detail.prompt);
    };
    window.addEventListener(ASK_AI_EVENT, onEvent);
    return () => window.removeEventListener(ASK_AI_EVENT, onEvent);
  }, [handler]);
}
