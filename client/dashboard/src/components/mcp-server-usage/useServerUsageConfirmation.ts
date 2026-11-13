import type { McpServerUsageImpact } from "@gram/client/models/components/mcpserverusageimpact.js";
import { useMcpServerUsageImpact } from "@gram/client/react-query/mcpServerUsageImpact.js";
import type { UseQueryResult } from "@tanstack/react-query";
import { useEffect, useState } from "react";

/** What a confirmation asks the operator to type when the server's name is unknown. */
const FALLBACK_CONFIRM_VALUE = "confirm";

export type ServerUsageConfirmationState = {
  impact: UseQueryResult<McpServerUsageImpact, unknown>;
  /** Whether the operator must type confirmValue before confirming. */
  requireTyping: boolean;
  confirmValue: string;
  typed: string;
  setTyped: (value: string) => void;
  /**
   * Whether the confirm action may run: the usage preview has settled and,
   * when typing is required, the typed value matches.
   */
  canConfirm: boolean;
};

/**
 * Loads who a breaking change to an MCP server affects and tracks the typed
 * confirmation. Pair with `ServerUsageConfirmation`, and gate the dialog's
 * confirm button on `canConfirm`.
 *
 * `requireTyping` defaults to true. Turn it off only for dialogs that already
 * ask for a typed confirmation of their own.
 */
export function useServerUsageConfirmation({
  mcpServerId,
  serverName,
  open,
  requireTyping = true,
}: {
  mcpServerId: string | undefined;
  /** Used when the preview, which carries the server's name, fails to load. */
  serverName?: string;
  open: boolean;
  requireTyping?: boolean;
}): ServerUsageConfirmationState {
  const impact = useMcpServerUsageImpact({ id: mcpServerId ?? "" }, undefined, {
    enabled: open && !!mcpServerId,
    throwOnError: false,
    retry: false,
    // Who is connected changes minute to minute; every opening re-checks.
    staleTime: 0,
  });
  const [typed, setTyped] = useState("");
  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);

  const confirmValue =
    impact.data?.serverName || serverName || FALLBACK_CONFIRM_VALUE;
  const settled = !mcpServerId || !impact.isFetching;
  return {
    impact,
    requireTyping,
    confirmValue,
    typed,
    setTyped,
    canConfirm: settled && (!requireTyping || typed.trim() === confirmValue),
  };
}
