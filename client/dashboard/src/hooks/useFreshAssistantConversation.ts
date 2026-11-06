import { useLayoutEffect, useRef, useState } from "react";
import { useInsightsState } from "@/components/insights-context";

/** Landing widgets start a fresh keyed runtime without switching a live one. */
export function useFreshAssistantConversation(): boolean {
  const { newConversation, assistantReady } = useInsightsState();
  const requestedRef = useRef(false);
  const [requested, setRequested] = useState(false);
  useLayoutEffect(() => {
    if (requestedRef.current) return;
    requestedRef.current = true;
    newConversation();
    setRequested(true);
  }, [newConversation]);
  // Never mount the draft handover against the previous runtime, including
  // the initial render before the layout effect requests the keyed reset.
  // InsightsProvider publishes assistantReady only for the current key.
  return requested && assistantReady;
}
