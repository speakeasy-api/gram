import { useCallback, useState, useSyncExternalStore, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  applyRefresh,
  previewRefresh,
  refreshError,
  refreshStatus,
  type RefreshChange,
  type RefreshPlan,
} from "./catalogRefresh";
const statusKey = ["admin", "catalog-refresh-status"];
const groups = [
  ["create", "Create"],
  ["update", "Update"],
  ["unpublish", "Unpublish (not delete)"],
  ["republish", "Republish"],
  ["adopt", "Adopt"],
  ["preserved", "Preserved"],
  ["conflict", "Conflicts"],
] as const;
function Changes({ changes }: { changes: RefreshChange[] }) {
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {groups.map(([action, label]) => {
        const items = changes.filter((change) => change.action === action);
        return (
          <section key={action} className="min-w-0 rounded-md border p-3">
            <h3 className="font-medium">
              {label} ({items.length})
            </h3>
            <ul className="max-h-48 overflow-auto text-sm">
              {items.map((item) => (
                <li key={item.name} className="break-all py-1">
                  {item.name}
                  {item.reason && (
                    <span className="text-muted-foreground">
                      {" "}
                      — {item.reason}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </div>
  );
}
function subscribeClock(onChange: () => void): () => void {
  const timer = setInterval(onChange, 1000);
  return () => clearInterval(timer);
}

export function CatalogRegistrySync(): JSX.Element {
  const client = useQueryClient();
  const status = useQuery({
    queryKey: statusKey,
    queryFn: refreshStatus,
    retry: false,
  });
  const [plan, setPlan] = useState<RefreshPlan | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const expiresAt = plan?.expiresAt;
  // A primitive boolean stays stable between clock ticks until expiry. Unlike
  // returning Date.now(), this snapshot cannot trigger a render loop.
  const isExpired = useCallback(
    () => expiresAt !== undefined && Date.parse(expiresAt) <= Date.now(),
    [expiresAt],
  );
  const expired = useSyncExternalStore(subscribeClock, isExpired);
  const preview = useMutation({
    mutationFn: previewRefresh,
    retry: false,
    onSuccess: (next) => {
      setPlan(next);
    },
  });
  const apply = useMutation({
    mutationFn: applyRefresh,
    retry: false,
    onSettled: () => {
      setPlan(null);
      setConfirmed(false);
      void client.invalidateQueries({ queryKey: statusKey });
      void client.invalidateQueries({
        predicate: (entry) =>
          entry.queryKey.some(
            (part) => typeof part === "string" && /registry/i.test(part),
          ),
      });
    },
  });
  const busy = preview.isPending || apply.isPending;
  return (
    <section
      aria-label="Catalog Registry Sync"
      className="space-y-4 rounded-lg border p-4"
    >
      <h2 className="text-lg font-semibold">Catalog Registry Sync</h2>
      <p className="text-muted-foreground text-sm">
        Copy the production catalog into this local or dev database. Existing
        preview environments are separate database clones: shared asset bytes do
        not mean a shared database. Unpublish hides entries; it does not delete
        them. Local edits and conflicts are preserved. The server enforces
        destination and source safeguards.
      </p>
      <div className="space-y-1 text-sm" aria-live="polite">
        {status.isPending && <p>Loading sync status…</p>}
        {status.data && (
          <>
            <p className="break-all">Source: {status.data.sourceIdentity}</p>
            <p>Last full success: {status.data.lastFullSuccessAt ?? "Never"}</p>
            <p className="break-all">
              Last full success revision:{" "}
              {status.data.lastFullSuccessRevision ?? "None"}
            </p>
            <p>Last attempt: {status.data.lastAttemptState ?? "None"}</p>
            <p className="break-all">
              Last attempt revision: {status.data.lastAttemptRevision ?? "None"}
            </p>
            <p>Unresolved conflicts: {status.data.conflicts?.length ?? 0}</p>
            {status.data.conflicts?.length ? (
              <ul>
                {status.data.conflicts.map((item) => (
                  <li key={item.name} className="break-all">
                    {item.name} — {item.reason ?? "Conflict"}
                  </li>
                ))}
              </ul>
            ) : null}
            {status.data.failure && (
              <p role="alert">
                Last error: {status.data.failure}. Check source configuration
                and preview again.
              </p>
            )}
          </>
        )}
      </div>
      {status.error && <p role="alert">{refreshError(status.error)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={busy || status.isFetching}
          onClick={() => void status.refetch()}
        >
          Check status
        </Button>
        <Button
          disabled={busy}
          onClick={() => {
            setPlan(null);
            setConfirmed(false);
            apply.reset();
            preview.reset();
            preview.mutate();
          }}
        >
          Preview from production
        </Button>
      </div>
      <p role="status">
        {preview.isPending
          ? "Preparing preview — fetching and comparing the production snapshot…"
          : apply.isPending
            ? "Applying confirmed plan — waiting for the server result…"
            : plan
              ? "Preview ready — review and confirm before applying."
              : "No plan selected."}
      </p>
      {(preview.error || apply.error) && (
        <p role="alert">{refreshError(preview.error ?? apply.error)}</p>
      )}
      {plan && (
        <div className="space-y-4">
          <dl className="space-y-1 break-all text-sm">
            <dt>Plan ID</dt>
            <dd>{plan.id}</dd>
            <dt>Source</dt>
            <dd>{plan.sourceIdentity}</dd>
            <dt>Source revision</dt>
            <dd>{plan.sourceRevision}</dd>
            <dt>Expires at</dt>
            <dd>{plan.expiresAt}</dd>
          </dl>
          <Changes changes={plan.changes ?? []} />
          {expired && (
            <p role="alert">
              Plan expired. Preview again and confirm the new plan.
            </p>
          )}
          <label className="flex items-start gap-2 text-sm">
            <Checkbox
              checked={confirmed}
              disabled={busy || expired}
              onCheckedChange={(value) => setConfirmed(value === true)}
            />
            <span className="break-all">
              I confirm plan {plan.id}, revision {plan.sourceRevision}, expiring{" "}
              {plan.expiresAt}, for this database. Conflicts will remain
              unresolved.
            </span>
          </label>
          <Button
            disabled={!confirmed || busy || expired}
            onClick={() => {
              if (Date.parse(plan.expiresAt) <= Date.now()) {
                return;
              }
              apply.mutate(plan.id);
            }}
          >
            Apply confirmed plan
          </Button>
        </div>
      )}
      {apply.data && (
        <div className="space-y-2" role="status">
          <p>
            {apply.data.state === "completed"
              ? "Full sync completed."
              : apply.data.state === "completed_with_conflicts"
                ? "Applied with conflicts — not a full sync success."
                : `Server result: ${apply.data.state}`}
          </p>
          <p className="break-all text-sm">
            Plan {apply.data.planId} · Revision {apply.data.sourceRevision} ·
            Completed {apply.data.completedAt}
          </p>
          <Changes changes={apply.data.changes ?? []} />
        </div>
      )}
    </section>
  );
}
