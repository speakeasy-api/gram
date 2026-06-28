import { flushSync } from "react-dom";

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
    document.startViewTransition(() => {
      flushSync(update);
    });
    return;
  }
  update();
}
