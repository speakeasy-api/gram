import { useAui } from "@assistant-ui/react";
import { useEffect, useRef } from "react";

function isEmptyThreadError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("empty thread");
}

/** Retain fallback text until the destination composer accepts it. */
export function useComposerDraftHandover(
  value: string,
  onAccepted: () => void,
): void {
  const aui = useAui();
  const accepted = useRef(false);
  const onAcceptedRef = useRef(onAccepted);

  useEffect(() => {
    onAcceptedRef.current = onAccepted;
  }, [onAccepted]);

  useEffect(() => {
    if (accepted.current || !value.trim()) return;
    let active = true;
    let writing = false;
    const attempt = () => {
      if (!active || writing || accepted.current) return;
      if (aui.threads().getState().isLoading) return;
      writing = true;
      try {
        try {
          const composer = aui.composer();
          composer.setText(value);
          // A placeholder can also silently drop setText. List readiness
          // alone does not prove that the real composer has bound yet.
          if (composer.getState().text !== value) return;
        } catch (err) {
          // EMPTY_THREAD_CORE throws until the real thread binds. Keep the
          // draft queued and retry on runtime events, but surface real errors.
          if (isEmptyThreadError(err)) return;
          throw err;
        }
        accepted.current = true;
        onAcceptedRef.current();
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
  }, [aui, value]);
}

/** Transfer a fallback draft without adding a visible control. */
export function ComposerDraftHandover({
  value,
  onAccepted,
}: {
  value: string;
  onAccepted: () => void;
}): null {
  useComposerDraftHandover(value, onAccepted);
  return null;
}
