import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import {
  invalidateAllAccessRequests,
  useAccessRequests,
} from "@gram/client/react-query/accessRequests.js";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import React from "react";
import { toast } from "sonner";
import { invalidateTemporaryAccess } from "./temporary-access-helpers";

/** How often the viewer's requests are checked while one is pending. */
export const ACCESS_REQUEST_POLL_MS = 15_000;

/**
 * Watches the viewer's own access requests from one place in the app, so a
 * page opens up without a reload once an admin decides a request, from
 * Speakeasy or Slack, wherever the request was made: an inline banner, a
 * popover on a disabled control or a full page. It polls only while one of
 * the viewer's requests is pending; React Query pauses the polling in
 * background tabs. Renders nothing.
 */
export function AccessRequestWatcher(): React.JSX.Element | null {
  const accessRequestsOn =
    useFeatureFlag(FEATURE_FLAGS.accessRequests).status === "enabled";
  if (!accessRequestsOn) return null;
  return <PendingAccessRequestWatcher />;
}

function PendingAccessRequestWatcher(): null {
  const queryClient = useQueryClient();
  // Requests seen pending since the app loaded, and requests made since, are
  // announced when decided; a decision made before the app loaded stays
  // quiet. A request made here can be decided before a poll sees it pending.
  const seenPending = React.useRef(new Set<string>());
  const announced = React.useRef(new Set<string>());
  const [mountedAt] = React.useState(() => Date.now());
  const mine = useAccessRequests({ mine: true }, undefined, {
    refetchInterval: (query) =>
      query.state.data?.requests.some((r) => r.status === "pending")
        ? ACCESS_REQUEST_POLL_MS
        : false,
    throwOnError: false,
  });

  React.useEffect(() => {
    if (!mine.data) return;
    // The list holds only the newest requests, so a request missing from it
    // tells nothing: only a listed, decided request ends the wait.
    const decided: AccessRequest[] = [];
    for (const request of mine.data.requests) {
      if (request.status === "pending") {
        seenPending.current.add(request.id);
        continue;
      }
      const waitedOn =
        seenPending.current.delete(request.id) ||
        request.createdAt.getTime() > mountedAt;
      if (waitedOn && !announced.current.has(request.id)) {
        announced.current.add(request.id);
        decided.push(request);
      }
    }
    if (decided.length === 0) return;
    if (decided.some((request) => request.status === "approved")) {
      refetchWithNewAccess(queryClient);
    } else {
      invalidateTemporaryAccess(queryClient);
      void invalidateAllAccessRequests(queryClient);
    }
    for (const request of decided) announce(request);
  }, [mine.data, mountedAt, queryClient]);

  return null;
}

function announce(request: AccessRequest): void {
  if (request.status === "approved") {
    toast.success(
      request.grantExpiresAt
        ? `Access approved until ${format(request.grantExpiresAt, "HH:mm")}.`
        : "Access approved.",
    );
  } else if (request.status === "denied") {
    toast.error("Your access request was denied.");
  } else if (request.status === "expired") {
    toast.info("Your access request expired before anyone decided it.");
  }
}

/**
 * Refetches everything the page shows once an approval lands. Lists that
 * loaded before it, such as Agent Sessions without chat:read, came back
 * filtered to what the viewer could see then, not refused, so refreshing the
 * grants alone leaves them stale until a reload. Only queries on screen
 * refetch now; the rest refetch when next used.
 */
function refetchWithNewAccess(queryClient: QueryClient): void {
  void queryClient.invalidateQueries();
}
