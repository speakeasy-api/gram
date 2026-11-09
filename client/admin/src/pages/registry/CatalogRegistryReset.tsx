import { useCallback, useState, useSyncExternalStore, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  applyReset,
  previewReset,
  resetStatus,
  resetError,
  type ResetPlan,
} from "./catalogReset";
import type { RefreshChange } from "./catalogRefresh";
const statusKey = ["admin", "catalog-reset-status"];
function Changes({ changes }: { changes: RefreshChange[] }): JSX.Element {
  return (
    <ul className="max-h-96 overflow-auto text-sm">
      {changes.map((change, i) => (
        <li key={`${change.name}-${i}`} className="break-all">
          {change.action}: {change.name}
          {change.reason ? ` — ${change.reason}` : ""}
        </li>
      ))}
    </ul>
  );
}
function subscribeClock(onChange: () => void): () => void {
  const timer = setInterval(onChange, 1000);
  return () => clearInterval(timer);
}

export function CatalogRegistryReset(): JSX.Element {
  const client = useQueryClient();
  const [plan, setPlan] = useState<ResetPlan | null>(null);
  const [confirmation, setConfirmation] = useState("");
  const expiresAt = plan?.expiresAt;
  const isExpired = useCallback(
    (): boolean =>
      expiresAt !== undefined &&
      (!Number.isFinite(Date.parse(expiresAt)) ||
        Date.parse(expiresAt) <= Date.now()),
    [expiresAt],
  );
  const expired = useSyncExternalStore(subscribeClock, isExpired);
  const status = useQuery({
    queryKey: statusKey,
    queryFn: resetStatus,
    retry: false,
  });
  const preview = useMutation({
    mutationFn: previewReset,
    retry: false,
    onSuccess: (next) => {
      setPlan(next);
    },
  });
  const apply = useMutation({
    mutationFn: applyReset,
    retry: false,
    onSettled: () => {
      setPlan(null);
      setConfirmation("");
      void client.invalidateQueries({
        predicate: (query) =>
          query.queryKey.some(
            (key) =>
              typeof key === "string" &&
              /registry|catalog-.*-status/i.test(key),
          ),
      });
    },
  });
  const busy = preview.isPending || apply.isPending;
  return (
    <section
      aria-label="Catalog destination reset"
      className="space-y-4 rounded-lg border p-4"
    >
      <h2 className="text-lg font-semibold">Reset destination catalog</h2>
      <p className="text-sm text-muted-foreground">
        Discard testing edits in this destination only and restore the
        production catalog. Destination-only entries are unpublished, not
        deleted. Existing independent preview databases are unaffected; future
        clones inherit the source database state. Installed connections and
        credentials are untouched.
      </p>
      {status.data && (
        <div className="text-sm">
          <p>
            Last full catalog success (
            {status.data.lastFullSuccessOperation ?? "operation unknown"}):{" "}
            {status.data.lastFullSuccessAt ?? "Never"}
          </p>
          <p>
            Last catalog attempt (
            {status.data.lastAttemptOperation ?? "operation unknown"}):{" "}
            {status.data.lastAttemptState ?? "None"}
          </p>
          {status.data.failure && <p role="alert">{status.data.failure}</p>}
          {!!status.data.conflicts?.length && (
            <>
              <p>Unresolved catalog conflicts — not a full success.</p>
              <Changes changes={status.data.conflicts} />
            </>
          )}
        </div>
      )}
      {status.error && <p role="alert">{resetError(status.error)}</p>}
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={busy || status.isFetching}
          onClick={() => void status.refetch()}
        >
          Check reset status
        </Button>
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            setPlan(null);
            setConfirmation("");
            apply.reset();
            preview.reset();
            preview.mutate();
          }}
        >
          Preview destination reset
        </Button>
      </div>
      {busy && (
        <p role="status">
          {preview.isPending
            ? "Preparing reset preview…"
            : "Resetting destination — waiting for server result…"}
        </p>
      )}
      {(preview.error || apply.error) && (
        <p role="alert">{resetError(preview.error ?? apply.error)}</p>
      )}
      {plan && (
        <div className="space-y-3">
          <dl className="break-all text-sm">
            <dt>Destination</dt>
            <dd>{plan.destination}</dd>
            <dt>Plan ID</dt>
            <dd>{plan.id}</dd>
            <dt>Source</dt>
            <dd>{plan.sourceIdentity}</dd>
            <dt>Source revision</dt>
            <dd>{plan.sourceRevision}</dd>
            <dt>Expires at</dt>
            <dd>{plan.expiresAt}</dd>
          </dl>
          <p>{plan.scopeWarning}</p>
          <h3>Exact planned operations and structural conflicts</h3>
          <Changes changes={plan.changes ?? []} />
          {plan.changes?.some((change) => change.action === "conflict") && (
            <p role="alert">
              Reset will be incomplete: structural conflicts remain; only the
              listed safe operations will apply.
            </p>
          )}
          {expired && (
            <p role="alert">
              Plan expired. Preview again and confirm the new plan.
            </p>
          )}
          <p>Type this exact confirmation:</p>
          <code className="block break-all">{plan.confirmation}</code>
          <label htmlFor="catalog-reset-confirmation">Reset confirmation</label>
          <Input
            id="catalog-reset-confirmation"
            autoComplete="off"
            value={confirmation}
            disabled={busy || expired}
            onChange={(event) => setConfirmation(event.target.value)}
          />
          <Button
            variant="destructive"
            disabled={
              busy ||
              expired ||
              !plan.confirmation ||
              confirmation !== plan.confirmation ||
              plan.operation !== "reset"
            }
            onClick={() => {
              if (
                Date.parse(plan.expiresAt) > Date.now() &&
                confirmation === plan.confirmation
              )
                apply.mutate({ planId: plan.id, confirmation });
            }}
          >
            Reset destination
          </Button>
        </div>
      )}
      {apply.data && (
        <div role="status" className="space-y-2">
          <p>
            {apply.data.state === "completed"
              ? "Destination reset completed."
              : apply.data.state === "completed_with_conflicts"
                ? "Applied with conflicts — not a full reset success."
                : `Reset result: ${apply.data.state}`}
          </p>
          <p className="break-all text-sm">
            Destination {apply.data.destination} · Plan {apply.data.planId} ·
            Revision {apply.data.sourceRevision} · Completed{" "}
            {apply.data.completedAt}
          </p>
          <Changes changes={apply.data.changes ?? []} />
        </div>
      )}
    </section>
  );
}
