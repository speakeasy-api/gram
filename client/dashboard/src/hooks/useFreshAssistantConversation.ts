import { useAui } from "@assistant-ui/react";
import { useEffect, useRef, useState } from "react";
import { useInsightsState } from "@/components/insights-context";

/** Start a landing thread without replacing the shared, potentially streaming runtime. */
export function useFreshAssistantConversation(): boolean {
  const aui = useAui();
  const { assistantReady } = useInsightsState();
  const requested = useRef(false);
  const [settled, setSettled] = useState(false);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<unknown>();

  useEffect(() => {
    if (!assistantReady) return;
    const attempt = () => {
      const threads = aui.threads();
      const state = threads.getState();
      if (state.isLoading) return;
      const boundNewThread =
        state.newThreadId != null &&
        state.mainThreadId === state.newThreadId &&
        !aui.thread().getState().isLoading;
      if (!requested.current) {
        requested.current = true;
        if (boundNewThread) {
          setSettled(true);
        } else {
          // Runtime-backed clients return the actual promise despite the void
          // declaration. Also observe binding for clients returning void.
          void Promise.resolve(threads.switchToNewThread()).then(
            () => setSettled(true),
            (err: unknown) => setError(err),
          );
        }
      }
      if (settled && boundNewThread) setReady(true);
    };
    const unsubscribe = aui.subscribe(attempt);
    attempt();
    return unsubscribe;
  }, [aui, assistantReady, settled]);

  if (error) throw error;
  return assistantReady && ready;
}
