import { useState, type JSX } from "react";
import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { registryPromotionCandidatesQuery } from "@/lib/gramAdminClient";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { RegistryPromotionDocs } from "./RegistryPromotionDocs";
import { Button } from "@/components/ui/button";
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

export function RegistryPromotionCandidates({
  onOpenEntry,
}: {
  onOpenEntry: (id: string) => void;
}): JSX.Element {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [target, setTarget] = useState<PromotionTarget | null>(null);
  const query = useQuery({
    ...registryPromotionCandidatesQuery({
      limit: 25,
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
              <TableHead>Health evidence</TableHead>
              <TableHead>Promotion</TableHead>
              <TableHead>Setup documentation</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {query.isPending ? (
              <DataTable.NoResultsMessage colSpan={5}>
                Loading promotion candidates…
              </DataTable.NoResultsMessage>
            ) : query.data?.candidates.length === 0 ? (
              <DataTable.NoResultsMessage colSpan={5}>
                No remote servers have been evaluated yet.
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
                        candidate.status === "promoted" || candidate.eligible
                          ? badgeTone.success
                          : badgeTone.neutral
                      }
                    >
                      {candidate.status === "promoted"
                        ? "Published"
                        : candidate.eligible
                          ? "Ready for review"
                          : "Not ready"}
                    </Badge>
                    <p className="mt-1 max-w-60 text-xs text-muted-foreground">
                      {candidate.status === "promoted"
                        ? `Published ${promotionTimestamp(candidate.promotedAt) || "previously"}`
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
                      <Button
                        variant="outline"
                        size="sm"
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
          target={target}
          onClose={() => setTarget(null)}
        />
      )}
    </section>
  );
}
