import { useAui } from "@assistant-ui/react";
import { useEffect, useRef } from "react";

/** Retain fallback text until the destination composer accepts it. */
export function useComposerDraftHandover(
  value: string,
  onAccepted: () => void,
): void {
  const aui = useAui();
  const accepted = useRef(false);

  useEffect(() => {
    if (accepted.current) return;
    let active = true;
    let writing = false;
    const attempt = () => {
      if (!active || writing || accepted.current) return;
      if (aui.threads().getState().isLoading) return;
      writing = true;
      try {
        if (!value.trim()) return;
        const composer = aui.composer();
        composer.setText(value);
        // The placeholder silently drops setText. List readiness alone does
        // not prove that the real thread/composer has bound yet.
        if (composer.getState().text !== value) return;
        accepted.current = true;
        onAccepted();
      } finally {
        writing = false;
      }
    };
    // Binding can change without isLoading changing. Subscribe to runtime
    // events, not only the selected loading flag; guard synchronous writes
    // against reentrant notifications.
    const unsubscribe = aui.subscribe(attempt);
    attempt();
    return () => {
      active = false;
      unsubscribe();
    };
  }, [aui, value, onAccepted]);
}
