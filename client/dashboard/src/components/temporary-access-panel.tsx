import { useUser } from "@/contexts/Auth";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { useScopeCatalogue } from "@/hooks/useScopeCatalogue";
import { handleError } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { scopeLabel } from "@/pages/access/scopeCatalogue";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import type { TemporaryGrant } from "@gram/client/models/components/temporarygrant.js";
import { useRevokeTemporaryGrantMutation } from "@gram/client/react-query/revokeTemporaryGrant.js";
import { useTemporaryGrants } from "@gram/client/react-query/temporaryGrants.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Clock } from "lucide-react";
import React from "react";
import {
  activeTemporaryGrants,
  formatDeadline,
  formatTimeLeft,
  invalidateTemporaryAccess,
  temporaryGrantTarget,
} from "./temporary-access-helpers";

// The remaining time is shown to the minute; a 15-second tick keeps it within
// a minute without re-rendering more than needed.
const TICK_MS = 15_000;
// Grants given or revoked elsewhere (another tab, an admin) show up within this.
const REFETCH_MS = 60_000;

function useNow(intervalMs: number): Date {
  const [now, setNow] = React.useState(() => new Date());
  React.useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}

/**
 * The viewer's temporary access, floating bottom-right on every page while any
 * of it is active: what each grant covers, when it ends and how long is left,
 * with a revoke that asks once. Renders nothing without an active grant.
 */
export function TemporaryAccessPanel(): React.JSX.Element | null {
  const user = useUser();
  const queryClient = useQueryClient();
  const now = useNow(TICK_MS);
  const [collapsed, setCollapsed] = useLocalStorageState(
    "temporary-access-panel-collapsed",
    false,
  );
  const { data } = useTemporaryGrants(
    { principalUrn: `user:${user.id}` },
    undefined,
    { throwOnError: false, refetchInterval: REFETCH_MS },
  );

  const fetched = data?.grants;
  const grants = React.useMemo(
    () => activeTemporaryGrants(fetched ?? [], now),
    [fetched, now],
  );

  // A grant that runs out while the page is open stops authorizing at once;
  // refresh the viewer's access so gated controls lock again.
  const fetchedActive = fetched?.filter((g) => g.status === "active").length;
  React.useEffect(() => {
    if (fetchedActive !== undefined && grants.length < fetchedActive) {
      invalidateTemporaryAccess(queryClient);
    }
  }, [grants.length, fetchedActive, queryClient]);

  if (grants.length === 0) return null;
  const soonest = grants[0]!;

  return (
    <section
      aria-label="Temporary access"
      className={cn(
        "bg-card fixed right-6 bottom-6 z-[60] border shadow-lg",
        collapsed ? "w-auto" : "w-[22rem] max-w-[calc(100vw-3rem)]",
      )}
    >
      <button
        type="button"
        className={cn(
          "hover:bg-muted/50 flex w-full items-center justify-between gap-3 px-4 py-2.5 text-left text-sm font-medium",
          !collapsed && "bg-muted/30 border-b",
        )}
        aria-expanded={!collapsed}
        onClick={() => setCollapsed(!collapsed)}
      >
        <span className="flex items-center gap-2">
          <Clock className="text-muted-foreground size-3.5" aria-hidden />
          Temporary access
          <Badge variant="warning">{grants.length}</Badge>
        </span>
        <span className="text-muted-foreground flex items-center gap-1.5 text-xs font-normal">
          ends {formatDeadline(soonest.expiresAt, now)}
          {collapsed ? (
            <ChevronUp className="size-3.5" aria-hidden />
          ) : (
            <ChevronDown className="size-3.5" aria-hidden />
          )}
        </span>
      </button>
      {!collapsed && (
        <>
          <ul className="max-h-72 divide-y overflow-y-auto">
            {grants.map((grant) => (
              <TemporaryGrantRow key={grant.id} grant={grant} now={now} />
            ))}
          </ul>
          <p className="text-muted-foreground border-t px-4 py-2 text-xs">
            Revoking takes effect at once.
          </p>
        </>
      )}
    </section>
  );
}

function TemporaryGrantRow({
  grant,
  now,
}: {
  grant: TemporaryGrant;
  now: Date;
}): React.JSX.Element {
  const queryClient = useQueryClient();
  const { catalogue } = useScopeCatalogue();
  const revoke = useRevokeTemporaryGrantMutation();
  const [confirming, setConfirming] = React.useState(false);

  const doRevoke = () =>
    revoke.mutate(
      { request: { riskIDRequestBody: { id: grant.id } } },
      {
        onSuccess: () => invalidateTemporaryAccess(queryClient),
        onError: (error) => {
          setConfirming(false);
          handleError(error, { title: "Could not revoke access" });
        },
      },
    );

  return (
    <li className="flex items-start justify-between gap-3 px-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm">
          {scopeLabel(catalogue, grant.scope)}{" "}
          <span className="text-muted-foreground font-mono text-xs">
            {grant.scope}
          </span>
        </p>
        <p className="text-muted-foreground mt-0.5 text-xs">
          {temporaryGrantTarget(grant)} · ends{" "}
          {formatDeadline(grant.expiresAt, now)} ·{" "}
          <span className="text-warning font-mono whitespace-nowrap">
            {formatTimeLeft(grant.expiresAt, now)} left
          </span>
        </p>
      </div>
      {confirming ? (
        <span className="flex shrink-0 gap-1">
          <Button
            variant="destructive-primary"
            size="xs"
            disabled={revoke.isPending}
            onClick={doRevoke}
          >
            <Button.Text>
              {revoke.isPending ? "Revoking…" : "Revoke"}
            </Button.Text>
          </Button>
          <Button
            variant="tertiary"
            size="xs"
            disabled={revoke.isPending}
            onClick={() => setConfirming(false)}
          >
            <Button.Text>Keep</Button.Text>
          </Button>
        </span>
      ) : (
        <Button
          variant="secondary"
          size="xs"
          className="shrink-0"
          aria-label={`Revoke ${grant.scope} on ${temporaryGrantTarget(grant)}`}
          onClick={() => setConfirming(true)}
        >
          <Button.Text>Revoke</Button.Text>
        </Button>
      )}
    </li>
  );
}
