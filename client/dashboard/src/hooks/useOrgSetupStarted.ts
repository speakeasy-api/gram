import { useCallback } from "react";

import { createPersistedFlagStore } from "@/hooks/usePersistedFlagStore";

const store = createPersistedFlagStore("gram-org-welcome-rollout-started");

export function useOrgSetupStarted(orgSlug: string | undefined): {
  setupStarted: boolean;
  markSetupStarted: () => void;
} {
  const setupStarted = store.useFlag(orgSlug);
  const markSetupStarted = useCallback(() => {
    if (orgSlug) store.write(orgSlug, true);
  }, [orgSlug]);

  return { setupStarted, markSetupStarted };
}
