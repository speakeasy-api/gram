import { useAui } from "@assistant-ui/react";
import { useEffect, useRef, useState } from "react";
import { useInsightsState } from "@/components/insights-context";

/** Start a landing thread without replacing the shared, potentially streaming runtime. */
export function useFreshAssistantConversation(): boolean {
  const aui = useAui();
  const { assistantReady } = useInsightsState();
  const request = useRef({ aui, requested: false });
  const [status, setStatus] = useState({
    aui,
    settled: false,
    ready: false,
    error: null as unknown,
  });

  useEffect(() => {
    if (!assistantReady) return;
    if (request.current.aui !== aui) {
      request.current = { aui, requested: false };
    }
    const currentRequest = request.current;
    const current = status.aui === aui;
    if (!current) {
      setStatus({ aui, settled: false, ready: false, error: null });
    }
    const attempt = () => {
      const threads = aui.threads();
      const state = threads.getState();
      if (state.isLoading) return;
      const boundNewThread =
        state.newThreadId != null &&
        state.mainThreadId === state.newThreadId &&
        !aui.thread().getState().isLoading;
      if (!currentRequest.requested) {
        currentRequest.requested = true;
        const settle = (error: unknown = null) => {
          // A pending switch on the previous runtime must not release this one.
          if (request.current !== currentRequest) return;
          setStatus((previous) => ({
            aui,
            ready: previous.aui === aui && previous.ready,
            settled: true,
            error,
          }));
        };
        if (boundNewThread) {
          settle();
        } else {
          // Runtime-backed clients return the actual promise despite the void
          // declaration. Also observe binding for clients returning void.
          void Promise.resolve(threads.switchToNewThread()).then(
            () => settle(),
            settle,
          );
        }
      }
      if (current && status.settled && !status.ready && boundNewThread) {
        setStatus((previous) =>
          previous.aui === aui ? { ...previous, ready: true } : previous,
        );
      }
    };
    const unsubscribe = aui.subscribe(attempt);
    attempt();
    return unsubscribe;
  }, [aui, assistantReady, status]);

  // A new client invalidates readiness during render, before effect cleanup.
  if (status.aui !== aui) return false;
  if (status.error) throw status.error;
  return assistantReady && status.ready;
}
