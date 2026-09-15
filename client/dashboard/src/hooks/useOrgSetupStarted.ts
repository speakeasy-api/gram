import { useCallback } from "react";

import { createDismissedCtaStore } from "@/hooks/useDismissedCtaStore";

const store = createDismissedCtaStore("gram-org-welcome-rollout-started");

export function useOrgSetupStarted(orgSlug: string | undefined): {
  setupStarted: boolean;
  markSetupStarted: () => void;
} {
  const setupStarted = store.useDismissed(orgSlug);
  const markSetupStarted = useCallback(() => {
    if (orgSlug) store.write(orgSlug, true);
  }, [orgSlug]);

  return { setupStarted, markSetupStarted };
}
