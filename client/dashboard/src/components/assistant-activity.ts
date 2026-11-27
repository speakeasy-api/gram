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
 * switching project starts a fresh one. `scope` names the organization and
 * project, since project slugs repeat across organizations.
 */
export function useRecordDashboardActivity(
  scope: string,
  enabled: boolean,
): void {
  const { pathname } = useLocation();
  const queryClient = useQueryClient();

  useEffect(() => {
    if (enabled) clearActivity();
  }, [scope, enabled]);

  useEffect(() => {
    if (enabled) recordActivity("page", projectRelativePath(pathname));
  }, [pathname, enabled]);

  useEffect(() => {
    if (!enabled) return undefined;
    // A call started in the previous project can settle after the switch;
    // only calls submitted under this scope belong in its log.
    const scopeStartedAt = Date.now();
    return queryClient.getMutationCache().subscribe((event) => {
      if (event.type !== "updated") return;
      if (event.mutation.state.submittedAt < scopeStartedAt) return;
      const outcome = event.action.type;
      if (outcome !== "success" && outcome !== "error") return;
      const action = describeMutationKey(event.mutation.options.mutationKey);
      if (action) recordActionOutcome(action, outcome === "error");
    });
  }, [queryClient, enabled, scope]);
}

/**
 * Asks the server what the user seems to be doing each time `active` turns
 * on (the dock composer opens). It asks again only when the activity log has
 * changed since the last answer, so opening and closing the dock is free.
 */
export function useAssistantIntent({
  assistantId,
  projectSlug,
  scope,
  enabled,
  active,
}: {
  assistantId: string | undefined;
  projectSlug: string | undefined;
  /** Organization and project; a change discards the last answer. */
  scope: string;
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

  // A new scope, or turning the feature off, drops the last answer. Clearing
  // askedAtRevision also makes any request still in flight land nowhere.
  useEffect(() => {
    setIntent(null);
    askedAtRevision.current = null;
  }, [scope, enabled]);

  useEffect(() => {
    if (!enabled || !active || !assistantId) return;
    const revision = getActivityRevision();
    if (askedAtRevision.current === revision) return;
    askedAtRevision.current = revision;

    // The last answer describes older activity, so it must not ride along
    // with a message sent before the new one arrives.
    setIntent(null);
    const activity = getActivity();
    if (activity.length === 0) return;
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
