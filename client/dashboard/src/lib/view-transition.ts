import { flushSync } from "react-dom";

/**
 * Swallows the AbortError that both `ViewTransition.ready` and `.finished`
 * reject with when the browser skips a transition (for example when a second
 * transition starts before this one ends). A skip is expected and applies the
 * DOM update anyway, so there is nothing to report. Real errors still reject.
 */
export function ignoreSkippedTransition(error: unknown): void {
  if (error instanceof DOMException && error.name === "AbortError") return;
  throw error;
}

/**
 * Runs a state update inside a View Transition so the browser morphs matching
 * view-transition-name pairs (the "genie" effect). flushSync forces React to
 * apply the change synchronously inside the transition callback so the browser
 * captures the post-update DOM; without it React 19 batches the update until
 * after the snapshot and no transition plays. Falls back to a plain update
 * where the View Transitions API is unavailable.
 */
export function withViewTransition(update: () => void): void {
  if (
    typeof document !== "undefined" &&
    typeof document.startViewTransition === "function"
  ) {
    const transition = document.startViewTransition(() => {
      flushSync(update);
    });
    // Both promises reject with AbortError on a skip; neither is otherwise
    // awaited, so a skip would surface as an unhandled rejection.
    transition.ready.catch(ignoreSkippedTransition);
    transition.finished.catch(ignoreSkippedTransition);
    return;
  }
  update();
}
