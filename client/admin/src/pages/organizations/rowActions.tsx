import {
  useMutation,
  useQueryClient,
  type UseMutationResult,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";

import {
  cancelOrganizationFetches,
  organizationQuery,
  writeOrganizationToCache,
} from "@/lib/adminQueries";
import {
  disableOrganization,
  enableOrganization,
  extendTrial,
  type AdminOrganization,
  type ExtendTrialRequest,
  type TrialState,
} from "@/lib/gramAdminApi";

// The row's own behavior lives here, so the slices that add a row menu, a
// disable action and a trial extension all land in one file. The peek control
// is a component, and a file that mixes a hook with components loses fast
// refresh, so it sits in `PeekTrigger.tsx` instead. The controls these hooks
// drive sit in `OrganizationActions.tsx` for the same reason.
export function useOpenOrganization(): (org: AdminOrganization) => void {
  const navigate = useNavigate();
  const qc = useQueryClient();

  return useCallback(
    (org: AdminOrganization) => {
      const idOrSlug = org.slug || org.id;
      // The row already holds the whole record, so the detail page paints from
      // it on the first frame instead of showing a spinner. The detail query
      // still refetches behind that: the snapshot is stale the moment it
      // lands, and an admin reading a stale record is worse than one request.
      qc.setQueryData(organizationQuery(idOrSlug).queryKey, org);
      void navigate({ to: "/organizations/$idOrSlug", params: { idOrSlug } });
    },
    [navigate, qc],
  );
}

// The two states the server will extend. Everything else is refused there:
// converted, demoted and expired are all rejected with a conflict, and an
// organization with no trial has no end date to add days to. Offering the
// action for one of those would be offering a request that cannot succeed.
//
// A set rather than a disjunction, so the states are data a test can walk.
const EXTENDABLE_TRIAL_STATES: ReadonlySet<TrialState> = new Set([
  "running",
  "ending_soon",
]);

// Not for a disabled organization, whatever its trial says. The server would
// take the request: nothing there reads disabled_at, and the trial goes on
// running while every member is locked out. That is the reason to leave the
// action off. Buying more of a trial nobody can use is an offer the product
// cannot honour, and re-enabling is one press away for an operator who means
// to make it real.
export function canExtendTrial(org: AdminOrganization): boolean {
  return (
    !org.disabled_at &&
    org.trial_state !== undefined &&
    EXTENDABLE_TRIAL_STATES.has(org.trial_state)
  );
}

type OrganizationWrite<TVariables> = UseMutationResult<
  AdminOrganization,
  Error,
  TVariables
>;

// All three writes answer with the organization in its new state and all three
// put it in the cache the same way, so the list and the peek repaint from the
// response with no refetch behind them.
//
// All three drop the reads already in flight first. React Query awaits
// `onMutate` before it sends the request, so the stale fetch is cancelled
// before the write leaves rather than racing it home.
export function useDisableOrganization(): OrganizationWrite<string> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => disableOrganization({ id }),
    onMutate: (id) => cancelOrganizationFetches(qc, id),
    onSuccess: (org) => writeOrganizationToCache(qc, org),
  });
}

export function useEnableOrganization(): OrganizationWrite<string> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => enableOrganization({ id }),
    onMutate: (id) => cancelOrganizationFetches(qc, id),
    onSuccess: (org) => writeOrganizationToCache(qc, org),
  });
}

export function useExtendTrial(): OrganizationWrite<ExtendTrialRequest> {
  const qc = useQueryClient();
  return useMutation({
    // Wrapped, so the body is the only argument the client is handed: the
    // mutation passes its own context as a second one.
    mutationFn: (body: ExtendTrialRequest) => extendTrial(body),
    onMutate: (body) => cancelOrganizationFetches(qc, body.id),
    onSuccess: (org) => writeOrganizationToCache(qc, org),
  });
}
