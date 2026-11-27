/** Data and terminal hooks shared by the project homepage widgets. */
import { useInsightsState } from "@/components/insights-context";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useCallback, useMemo } from "react";

/** The terminal's element id, so asks from below the fold can scroll to it. */
export const HOME_TERMINAL_ID = "home-terminal";

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;

/**
 * The last 168 whole hours. Exactly 7 days keeps the overview's buckets
 * hourly (the server switches to 6h buckets past 168h), and an hour-aligned
 * window keeps the query key stable within the hour.
 */
export function useWeekWindow(): { from: Date; to: Date; prevFrom: Date } {
  const hour = Math.floor(Date.now() / HOUR_MS) * HOUR_MS;
  return useMemo(
    () => ({
      from: new Date(hour - 7 * DAY_MS),
      to: new Date(hour),
      prevFrom: new Date(hour - 14 * DAY_MS),
    }),
    [hour],
  );
}

export function useLogsEnabled(): {
  enabled: boolean;
  pending: boolean;
  error: boolean;
  /** Confirmed off, not just unknown after a failed request. */
  off: boolean;
} {
  const organization = useOrganization();
  const { data, isPending, isError } = useProductFeatures({
    organizationId: organization.id,
  });
  const enabled = data?.logsEnabled === true;
  return {
    enabled,
    pending: isPending,
    error: isError,
    off: !isPending && !isError && !enabled,
  };
}

/**
 * Asks the terminal beside the widgets, continuing its conversation. Asks from
 * the sections below the fold scroll the terminal back into view.
 */
export function useAskTerminal(): (prompt: string) => void {
  const { sendPrompt } = useInsightsState();
  return useCallback(
    (prompt: string) => {
      sendPrompt(prompt, { newConversation: false });
      document
        .getElementById(HOME_TERMINAL_ID)
        ?.scrollIntoView({ behavior: "smooth", block: "nearest" });
    },
    [sendPrompt],
  );
}
