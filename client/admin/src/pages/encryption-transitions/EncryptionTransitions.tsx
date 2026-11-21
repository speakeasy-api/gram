import { useRef, useState, type JSX } from "react";
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { toast } from "sonner";
import type { AdminOrganizationEncryptionTransitionRow } from "@gram/admin-client/models/components/adminorganizationencryptiontransitionrow";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  enrollOrganizationEncryption,
  organizationEncryptionTransitionsKeyPrefix,
  organizationEncryptionTransitionsQuery,
} from "@/lib/gramAdminClient";
import {
  formatTimestamp,
  pluralize,
  TRANSITION_PHASE_LABELS,
  TRANSITION_PHASE_TONES,
  TRANSITION_STATE_LABELS,
  TRANSITION_STATES,
  transitionPhase,
  transitionStateLabel,
  labelForUseCase,
} from "@/lib/organizationEncryption";

import type { EncryptionTransitionsSearch } from "./encryptionTransitionsSearch";

const ALL = "all";

/** Organizations per page. */
const PAGE_SIZE = 50;

/**
 * The most organizations one "select next" pass considers: the server's page
 * limit, which is far more than one rollout batch.
 */
const NEXT_CANDIDATE_LIMIT = 200;

/** The default size of a "select next" batch. */
const DEFAULT_BATCH = 10;

/**
 * The pause between enrollments in a bulk run. Each enroll can mint a KMS key,
 * so a batch runs one at a time at about one per second.
 */
export const ENROLL_INTERVAL_MS = 1000;

function enrollable(row: AdminOrganizationEncryptionTransitionRow): boolean {
  return row.state === "platform_key" && !row.disabled;
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

export function EncryptionTransitions(): JSX.Element {
  const search = useSearch({ from: "/encryption-transitions" });
  const navigate = useNavigate({ from: "/encryption-transitions" });
  const client = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const [selected, setSelected] = useState<
    Map<string, AdminOrganizationEncryptionTransitionRow>
  >(new Map());
  const [batchSize, setBatchSize] = useState(String(DEFAULT_BATCH));
  const [bulk, setBulk] = useState<{ done: number; total: number } | null>(
    null,
  );
  const [failures, setFailures] = useState<
    Map<string, { name: string; message: string }>
  >(new Map());
  // Checked between enrollments, so Stop ends the batch after the enrollment
  // in flight.
  const stopRequested = useRef(false);

  const page = search.page ?? 0;
  const query = useQuery({
    ...organizationEncryptionTransitionsQuery({
      // Absent lists the server's first use case.
      useCase: search.useCase,
      state: search.state,
      limit: PAGE_SIZE,
      offset: page * PAGE_SIZE,
    }),
    placeholderData: keepPreviousData,
  });
  const useCase = query.data?.useCase ?? search.useCase ?? "";
  const rows = query.data?.organizations ?? [];
  const counts = new Map(
    (query.data?.stateCounts ?? []).map((count) => [
      count.state,
      count.organizations,
    ]),
  );

  function filterBy(next: EncryptionTransitionsSearch) {
    setSelected(new Map());
    void navigate({ search: next, replace: true });
  }

  function toggle(row: AdminOrganizationEncryptionTransitionRow) {
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(row.organizationId)) next.delete(row.organizationId);
      else next.set(row.organizationId, row);
      return next;
    });
  }

  async function selectNext() {
    const size = Number.parseInt(batchSize, 10);
    if (!Number.isFinite(size) || size < 1) {
      toast.error("Enter how many organizations to select.");
      return;
    }
    try {
      const candidates = await client.fetchQuery(
        organizationEncryptionTransitionsQuery({
          useCase,
          state: "platform_key",
          limit: NEXT_CANDIDATE_LIMIT,
          offset: 0,
        }),
      );
      const next = candidates.organizations.filter(enrollable).slice(0, size);
      setSelected(new Map(next.map((row) => [row.organizationId, row])));
      if (next.length < size) {
        toast.info(
          `Only ${pluralize(next.length, "organization")} can be enrolled.`,
        );
      }
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function enrollSelected() {
    const batch = [...selected.values()];
    if (batch.length === 0) return;
    const ok = await confirm({
      title: `Enroll ${pluralize(batch.length, "organization")} for ${labelForUseCase(useCase)}?`,
      description:
        "Provisions each organization's key, then writes both copies of every new value. They are enrolled one at a time, about one a second, because each can mint a KMS key. A failure doesn't stop the rest. Keep this tab open until it finishes; enrollments already done stay done if it is interrupted.",
      details: (
        <ul className="list-disc pl-5">
          {batch.map((row) => (
            <li key={row.organizationId}>{row.name}</li>
          ))}
        </ul>
      ),
      confirmLabel: "Enroll",
    });
    if (!ok) return;

    setFailures(new Map());
    setBulk({ done: 0, total: batch.length });
    stopRequested.current = false;
    const failed = new Map<string, { name: string; message: string }>();
    let attempted = 0;
    for (const [index, row] of batch.entries()) {
      if (index > 0) await wait(ENROLL_INTERVAL_MS);
      if (stopRequested.current) break;
      attempted++;
      try {
        await enrollOrganizationEncryption(row.organizationId, useCase);
      } catch (e) {
        failed.set(row.organizationId, {
          name: row.name,
          message: errorMessage(e),
        });
      }
      setBulk({ done: index + 1, total: batch.length });
    }
    setBulk(null);
    setFailures(failed);
    setSelected(new Map());
    await client.invalidateQueries({
      queryKey: organizationEncryptionTransitionsKeyPrefix,
    });

    const enrolled = attempted - failed.size;
    if (attempted < batch.length) {
      toast.info(
        `Stopped after ${pluralize(attempted, "organization")}; enrolled ${enrolled}`,
      );
    } else if (failed.size === 0) {
      toast.success(`Enrolled ${pluralize(enrolled, "organization")}`);
    } else {
      toast.warning(
        `Enrolled ${pluralize(enrolled, "organization")}; ${failed.size} failed`,
      );
    }
  }

  const running = bulk !== null;

  return (
    <div className="space-y-6 p-6">
      {confirmDialog}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">Encryption transitions</h1>
          <p className="text-muted-foreground max-w-3xl text-sm">
            Where each organization stands in moving a use case from the
            platform key to its own key. New organizations that could not start
            on their own key appear in platform key. Enroll them here in
            batches; roll back and finalize one organization at a time from its
            Encryption page.
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={useCase}
          onValueChange={(value) =>
            filterBy({ ...search, useCase: value, page: undefined })
          }
        >
          <SelectTrigger size="sm" aria-label="Use case" className="bg-card">
            <SelectValue placeholder="Use case" />
          </SelectTrigger>
          <SelectContent position="popper" align="start">
            {(query.data?.useCases ?? (useCase ? [useCase] : [])).map(
              (name) => (
                <SelectItem key={name} value={name}>
                  {labelForUseCase(name)}
                </SelectItem>
              ),
            )}
          </SelectContent>
        </Select>
        <Select
          value={search.state ?? ALL}
          onValueChange={(value) =>
            filterBy({
              ...search,
              state: TRANSITION_STATES.find((state) => state === value),
              page: undefined,
            })
          }
        >
          <SelectTrigger size="sm" aria-label="State" className="bg-card">
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" align="start">
            <SelectItem value={ALL}>All states</SelectItem>
            {TRANSITION_STATES.map((state) => (
              <SelectItem key={state} value={state}>
                {TRANSITION_STATE_LABELS[state]} ({counts.get(state) ?? 0})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {query.isSuccess && (
          <span className="text-muted-foreground text-sm" aria-live="polite">
            {TRANSITION_STATES.map(
              (state) =>
                `${transitionStateLabel(state)}: ${(counts.get(state) ?? 0).toLocaleString()}`,
            ).join(" · ")}
          </span>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <label
          htmlFor="encryption-batch-size"
          className="text-muted-foreground text-sm"
        >
          Batch size
        </label>
        <Input
          id="encryption-batch-size"
          type="number"
          min={1}
          max={NEXT_CANDIDATE_LIMIT}
          value={batchSize}
          onChange={(event) => setBatchSize(event.target.value)}
          className="w-24"
          disabled={running}
        />
        <Button
          variant="outline"
          size="sm"
          disabled={running || !useCase}
          onClick={() => void selectNext()}
        >
          Select next in platform key
        </Button>
        <Button
          size="sm"
          disabled={running || selected.size === 0}
          onClick={() => void enrollSelected()}
        >
          {bulk
            ? `Enrolling ${bulk.done} of ${bulk.total}`
            : `Enroll selected (${selected.size})`}
        </Button>
        {running && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              stopRequested.current = true;
            }}
          >
            Stop
          </Button>
        )}
        {selected.size > 0 && !running && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setSelected(new Map())}
          >
            Clear selection
          </Button>
        )}
      </div>

      {failures.size > 0 && (
        <div
          role="alert"
          className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm"
        >
          <p className="font-medium text-destructive">
            {pluralize(failures.size, "organization")} could not be enrolled
          </p>
          <ul className="list-disc pl-5">
            {[...failures].map(([id, failure]) => (
              <li key={id}>
                {failure.name}: {failure.message}
              </li>
            ))}
          </ul>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setFailures(new Map())}
          >
            Dismiss
          </Button>
        </div>
      )}

      {query.isError && (
        <p role="alert" className="text-sm">
          Unable to load encryption transitions. {errorMessage(query.error)}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </p>
      )}

      <div className="overflow-clip rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-0">
                <span className="sr-only">Select</span>
              </TableHead>
              <TableHead>Organization</TableHead>
              <TableHead>State</TableHead>
              <TableHead>State changed</TableHead>
              <TableHead className="text-right">Waiting (last run)</TableHead>
              <TableHead className="text-right">
                Unreadable (last run)
              </TableHead>
              <TableHead>Last verified</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-muted-foreground">
                  {query.isPending
                    ? "Loading..."
                    : query.isError
                      ? "Unable to load encryption transitions"
                      : "No organization is in this state."}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => {
                const phase = transitionPhase(
                  row.state,
                  row.platformCopiesClearedAt,
                );
                return (
                  <TableRow key={row.organizationId}>
                    <TableCell>
                      {enrollable(row) && (
                        <Checkbox
                          aria-label={`Select ${row.name}`}
                          checked={selected.has(row.organizationId)}
                          disabled={running}
                          onCheckedChange={() => toggle(row)}
                        />
                      )}
                    </TableCell>
                    <TableCell>
                      <Link
                        to="/organizations/$idOrSlug/encryption"
                        params={{ idOrSlug: row.slug }}
                        className="font-medium hover:underline"
                      >
                        {row.name}
                      </Link>
                      <div className="text-muted-foreground font-mono text-xs">
                        {row.slug}
                        {row.disabled ? " · disabled" : ""}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant="outline"
                        className={TRANSITION_PHASE_TONES[phase]}
                      >
                        {TRANSITION_PHASE_LABELS[phase]}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm">
                      {formatTimestamp(row.stateChangedAt)}
                    </TableCell>
                    <TableCell className="text-right text-sm">
                      {row.lastBackfillRemaining?.toLocaleString() ?? "-"}
                    </TableCell>
                    <TableCell className="text-right text-sm">
                      {row.lastBackfillUnreadable?.toLocaleString() ?? "-"}
                    </TableCell>
                    <TableCell className="text-sm">
                      {formatTimestamp(row.lastVerifiedAt)}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={page === 0}
          onClick={() =>
            filterBy({ ...search, page: page > 1 ? page - 1 : undefined })
          }
        >
          Previous
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={!query.data?.hasMore}
          onClick={() => filterBy({ ...search, page: page + 1 })}
        >
          Next
        </Button>
      </div>
    </div>
  );
}
