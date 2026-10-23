import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";
import { useRef, useState, type JSX } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  registryPromotionCandidatesQuery,
  useCurateRegistryPromotionCandidateMutation,
} from "@/lib/gramAdminClient";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { RegistryPromotionDocs } from "./RegistryPromotionDocs";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

import { DataTable } from "@/components/data-table";
import {
  TableHeader,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import {
  RegistryPromotionSheet,
  type PromotionTarget,
} from "./RegistryPromotionSheet";
import { promotionTimestamp, promotionReason } from "./registryPromotionLabels";

const STATUS_FILTERS = [
  ["active", "Active"],
  ["all", "All statuses"],
  ["rejected", "Rejected"],
  ["archived", "Archived"],
  ["promoted", "Published"],
] as const;
type StatusFilter = (typeof STATUS_FILTERS)[number][0];

export function RegistryPromotionCandidates({
  onOpenEntry,
}: {
  onOpenEntry: (id: string) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<StatusFilter>("active");
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [target, setTarget] = useState<PromotionTarget | null>(null);
  const query = useQuery({
    ...registryPromotionCandidatesQuery({
      limit: 25,
      status,
      cursor: cursors[cursors.length - 1],
    }),
    refetchInterval: (state) =>
      state.state.data?.candidates.some(
        (candidate) =>
          candidate.docsStatus === "pending" ||
          candidate.docsStatus === "retrying",
      )
        ? 30_000
        : false,
  });
  return (
    <section className="space-y-3" aria-labelledby="promotion-candidates-title">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id="promotion-candidates-title" className="text-lg font-semibold">
          Remote server promotions
        </h2>
        <Button
          variant="outline"
          size="sm"
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          Refresh candidates
        </Button>
      </div>
      <p className="text-muted-foreground text-sm">
        Servers with observed tool call health are evaluated for the catalog.
        Open a review to check current eligibility and supply public listing
        details.
      </p>
      <div className="flex items-center gap-3">
        <Select
          value={status}
          onValueChange={(value) => {
            const selected = STATUS_FILTERS.find(([key]) => key === value);
            if (selected) {
              setStatus(selected[0]);
              setCursors([undefined]);
            }
          }}
        >
          <SelectTrigger aria-label="Candidate status" className="w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_FILTERS.map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="text-muted-foreground text-sm">Newest first</span>
      </div>
      {query.isError && (
        <p role="alert">
          Unable to load promotion candidates. Refresh to try again.
        </p>
      )}
      <div className="overflow-x-auto rounded-lg border">
        <DataTable cellPadding="condensed">
          <TableHeader>
            <TableRow>
              <TableHead>Source server</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Health evidence</TableHead>
              <TableHead>Promotion</TableHead>
              <TableHead>Setup documentation</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {query.isPending ? (
              <DataTable.NoResultsMessage colSpan={6}>
                Loading promotion candidates…
              </DataTable.NoResultsMessage>
            ) : query.data?.candidates.length === 0 ? (
              <DataTable.NoResultsMessage colSpan={6}>
                No promotion candidates match this status.
              </DataTable.NoResultsMessage>
            ) : (
              query.data?.candidates.map((candidate) => (
                <TableRow key={candidate.id}>
                  <TableCell>
                    <p className="mb-1 max-w-48 truncate font-medium">
                      {candidate.publicTitle ||
                        candidate.sourceName ||
                        "Remote MCP server"}
                    </p>
                    {candidate.projectId && candidate.mcpServerId ? (
                      <Link
                        className="text-muted-foreground text-xs underline"
                        to="/organizations/$idOrSlug/mcp-servers/$serverId"
                        params={{
                          idOrSlug: candidate.organizationId,
                          serverId: candidate.mcpServerId,
                        }}
                        search={{ project: candidate.projectId, window: 14 }}
                      >
                        View source · {candidate.mcpServerId.slice(0, 8)}
                      </Link>
                    ) : (
                      <span className="text-muted-foreground">
                        Source no longer available
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {promotionTimestamp(candidate.createdAt) || "—"}
                  </TableCell>
                  <TableCell>
                    {candidate.observedSuccesses} successful ·{" "}
                    {candidate.observedFailures} failed
                    <br />
                    {candidate.observedDays} active days
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={
                        candidate.status === "promoted" ||
                        (candidate.status === "candidate" && candidate.eligible)
                          ? badgeTone.success
                          : badgeTone.neutral
                      }
                    >
                      {promotionStatusLabel(candidate)}
                    </Badge>
                    <p className="mt-1 max-w-60 text-xs text-muted-foreground">
                      {candidate.status === "promoted"
                        ? `Published ${promotionTimestamp(candidate.promotedAt) || "previously"}`
                        : candidate.status === "rejected" ||
                            candidate.status === "archived"
                          ? "Restore to review this candidate."
                          : promotionReason(candidate.reason)}
                    </p>
                    {candidate.status !== "promoted" && candidate.checkedAt && (
                      <p className="mt-1 text-xs text-muted-foreground">
                        Checked {promotionTimestamp(candidate.checkedAt)}
                      </p>
                    )}
                  </TableCell>
                  <TableCell className="max-w-72 whitespace-normal">
                    {candidate.status === "promoted" ? (
                      <RegistryPromotionDocs candidate={candidate} compact />
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell>
                    {candidate.registryEntryId ? (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          if (candidate.registryEntryId)
                            onOpenEntry(candidate.registryEntryId);
                        }}
                      >
                        View entry
                      </Button>
                    ) : candidate.status === "promoted" ? (
                      <span className="text-muted-foreground">
                        Entry no longer available
                      </span>
                    ) : (
                      <div className="flex flex-wrap items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={
                            candidate.status !== "candidate" ||
                            !candidate.projectId ||
                            !candidate.mcpServerId
                          }
                          onClick={() =>
                            setTarget({
                              organizationId: candidate.organizationId,
                              projectId: candidate.projectId,
                              mcpServerId: candidate.mcpServerId,
                            })
                          }
                        >
                          Review
                        </Button>
                        <CandidateCurationActions
                          candidate={candidate}
                          onUpdated={async () => {
                            setCursors([undefined]);
                            await queryClient.invalidateQueries({
                              predicate: (entry) =>
                                entry.queryKey.some(
                                  (part) =>
                                    typeof part === "string" &&
                                    /registry/i.test(part),
                                ),
                            });
                          }}
                        />
                      </div>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </DataTable.Body>
        </DataTable>
      </div>
      <div className="flex justify-end gap-2">
        <Button
          variant="ghost"
          size="sm"
          disabled={cursors.length === 1 || query.isFetching}
          onClick={() => setCursors((previous) => previous.slice(0, -1))}
        >
          Previous candidates
        </Button>
        <Button
          variant="ghost"
          size="sm"
          disabled={!query.data?.nextCursor || query.isFetching}
          onClick={() => {
            if (query.data?.nextCursor)
              setCursors((previous) => [...previous, query.data.nextCursor]);
          }}
        >
          Next candidates
        </Button>
      </div>
      {target && (
        <RegistryPromotionSheet
          key={`${target.organizationId}/${target.projectId}/${target.mcpServerId}`}
          target={target}
          onClose={() => setTarget(null)}
        />
      )}
    </section>
  );
}

function CandidateCurationActions({
  candidate,
  onUpdated,
}: {
  candidate: AdminRegistryPromotionCandidate;
  onUpdated: () => Promise<void>;
}): JSX.Element {
  const mutation = useCurateRegistryPromotionCandidateMutation();
  const inFlight = useRef(false);
  const retry = useRef<{
    revision: string;
    action: string;
    key: string;
  } | null>(null);
  const submitted =
    mutation.isSuccess &&
    mutation.variables?.request.curationRevision === candidate.curationRevision;
  const apply = async (action: CurationAction): Promise<void> => {
    if (inFlight.current || submitted) return;
    inFlight.current = true;
    if (
      retry.current?.revision !== candidate.curationRevision ||
      retry.current.action !== action
    )
      retry.current = {
        revision: candidate.curationRevision,
        action,
        key: crypto.randomUUID(),
      };
    try {
      await mutation.mutateAsync({
        request: {
          organizationId: candidate.organizationId,
          promotionId: candidate.id,
          curationRevision: candidate.curationRevision,
          action,
          retryKey: retry.current.key,
        },
      });
      await onUpdated();
    } catch {
      // The mutation exposes a bounded message below; retain the exact retry key.
    } finally {
      inFlight.current = false;
    }
  };
  const actions = CURATION_ACTIONS.get(candidate.status) ?? [];
  return (
    <div className="space-y-1">
      <div className="flex gap-1">
        {actions.map((action) => (
          <Button
            key={action}
            variant="ghost"
            size="sm"
            disabled={mutation.isPending || submitted}
            onClick={() => void apply(action)}
          >
            {CURATION_LABELS[action]}
          </Button>
        ))}
      </div>
      {mutation.isError && (
        <p role="alert" className="max-w-52 text-xs">
          Could not update this candidate. Refresh to check its current status,
          then retry.
        </p>
      )}
    </div>
  );
}

type CurationAction = "reject" | "archive" | "restore";
const CURATION_ACTIONS = new Map<string, readonly CurationAction[]>([
  ["candidate", ["reject", "archive"]],
  ["rejected", ["restore"]],
  ["archived", ["restore"]],
]);
const CURATION_LABELS: Record<CurationAction, string> = {
  reject: "Reject",
  archive: "Archive",
  restore: "Restore",
};

function promotionStatusLabel(
  candidate: AdminRegistryPromotionCandidate,
): string {
  switch (candidate.status) {
    case "promoted":
      return "Published";
    case "rejected":
      return "Rejected";
    case "archived":
      return "Archived";
    default:
      return candidate.eligible ? "Ready for review" : "Not ready";
  }
}
