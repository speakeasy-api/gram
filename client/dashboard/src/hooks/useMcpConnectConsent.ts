import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";
import { useCallback } from "react";

// Persisted Connect consent for issuer-gated MCP surfaces, keyed by the
// mcp_server id (server Tools tab) or toolset id (playground). Minting a
// user-session JWT persists a session row server-side, so the first mint always
// waits for an explicit Connect click — but once the user has consented for a
// given server/toolset, return visits re-mint without asking again. The ids are
// UUIDs, so the key needs no extra org/project scoping, and the key is not in
// logout-storage's preserved list, so consent is cleared on logout rather than
// leaking to the next user of the browser.
const store = createDismissedCtaStore("gram-mcp-connect-consent");

export function useMcpConnectConsent(id: string | undefined): {
  /** True once the user has consented to connect for this id. */
  connectRequested: boolean;
  /** Records consent for this id; no-op while the id is still loading. */
  requestConnect: () => void;
} {
  const connectRequested = store.useDismissed(id);

  const requestConnect = useCallback(() => {
    if (id) store.write(id, true);
  }, [id]);

  return { connectRequested, requestConnect };
}
