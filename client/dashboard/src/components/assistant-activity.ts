import { useInferAssistantIntentMutation } from "@gram/client/react-query/inferAssistantIntent.js";
import type { AssistantIntent } from "@gram/client/models/components/assistantintent.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useLocation } from "react-router";
import {
  clearActivity,
  describeMutationKey,
  getActivity,
  getActivityRevision,
  projectRelativePath,
  recordActionOutcome,
  recordActivity,
} from "@/lib/activity-journal";

/**
 * Records the pages the user opens and the outcome of every API call they
 * make, for the Project Assistant's activity log. The log is per project:
 * switching project starts a fresh one.
 */
export function useRecordDashboardActivity(
  projectSlug: string | undefined,
  enabled: boolean,
): void {
  const { pathname } = useLocation();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (enabled) clearActivity();
  }, [projectSlug, enabled]);

  useEffect(() => {
    if (enabled) recordActivity("page", projectRelativePath(pathname));
  }, [pathname, enabled]);

  useEffect(() => {
    if (!enabled) return undefined;
    return queryClient.getMutationCache().subscribe((event) => {
      if (event.type !== "updated") return;
      const outcome = event.action.type;
      if (outcome !== "success" && outcome !== "error") return;
      const action = describeMutationKey(event.mutation.options.mutationKey);
      if (action) recordActionOutcome(action, outcome === "error");
    });
  }, [queryClient, enabled]);
}

/**
 * Asks the server what the user seems to be doing each time `active` turns
 * on (the dock composer opens). It asks again only when the activity log has
 * changed since the last answer, so opening and closing the dock is free.
 */
export function useAssistantIntent({
  assistantId,
  projectSlug,
  enabled,
  active,
}: {
  assistantId: string | undefined;
  projectSlug: string | undefined;
  enabled: boolean;
  active: boolean;
}): AssistantIntent | null {
  const { pathname } = useLocation();
  const [intent, setIntent] = useState<AssistantIntent | null>(null);
  const askedAtRevision = useRef<number | null>(null);
  // Overriding onError also turns off the default "Request failed" toast: a
  // failed inference is not something the user asked for or can act on.
  const { mutate } = useInferAssistantIntentMutation({
    onError: () => undefined,
  });

  useEffect(() => {
    setIntent(null);
    askedAtRevision.current = null;
  }, [projectSlug]);

  useEffect(() => {
    if (!enabled || !active || !assistantId) return;
    const revision = getActivityRevision();
    if (askedAtRevision.current === revision) return;
    askedAtRevision.current = revision;

    const activity = getActivity();
    if (activity.length === 0) {
      setIntent(null);
      return;
    }
    mutate(
      {
        request: {
          gramProject: projectSlug,
          inferIntentRequestBody: {
            assistantId,
            currentPath: projectRelativePath(pathname),
            activity: activity.map((entry) => ({
              at: new Date(entry.at),
              kind: entry.kind,
              detail: entry.detail,
            })),
          },
        },
      },
      {
        onSuccess: (result) => {
          if (askedAtRevision.current === revision) setIntent(result);
        },
        // The intent only sharpens the suggestions; on failure the dock keeps
        // its page suggestions and the user is not told.
        onError: () => {
          if (askedAtRevision.current === revision) setIntent(null);
        },
      },
    );
  }, [enabled, active, assistantId, projectSlug, pathname, mutate]);

  return intent;
}
