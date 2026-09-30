import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { registryOktaUnmappedQuery } from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { DataTable } from "@/components/data-table";
import {
  TableHeader,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";

type Props = {
  onOpen: (entryId: string) => void;
};

/**
 * Okta application names seen in synced tenants that no catalog entry
 * claims yet, with the entry the heuristic proposes. Opening the entry shows
 * the proposal in its editor.
 */
export function RegistryOktaUnmapped({ onOpen }: Props): JSX.Element {
  const unmapped = useQuery(registryOktaUnmappedQuery());
  return (
    <section aria-labelledby="registry-okta-unmapped" className="space-y-2">
      <h2 id="registry-okta-unmapped" className="text-lg font-semibold">
        Unmapped Okta applications
      </h2>
      <p className="text-muted-foreground text-sm">
        Applications customers have in Okta that no catalog entry maps yet, by
        how many organizations run them. A proposed entry is a guess from the
        vendor domain, title or tenant label; confirm it in the editor.
      </p>
      {unmapped.error && (
        <div role="alert">
          <p>{unmapped.error.message}</p>
          <Button
            disabled={unmapped.isFetching}
            onClick={() => void unmapped.refetch()}
          >
            Retry
          </Button>
        </div>
      )}
      <div className="overflow-x-auto rounded-lg border">
        <DataTable cellPadding="condensed">
          <TableHeader className="bg-muted">
            <TableRow>
              <TableHead scope="col">Okta name</TableHead>
              <TableHead scope="col">Organizations</TableHead>
              <TableHead scope="col">Sign-on modes</TableHead>
              <TableHead scope="col">Proposed entry</TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {unmapped.isPending ? (
              <DataTable.NoResultsMessage
                colSpan={4}
                className="text-muted-foreground"
              >
                Loading Okta applications…
              </DataTable.NoResultsMessage>
            ) : !unmapped.error && unmapped.data?.names.length === 0 ? (
              <DataTable.NoResultsMessage
                colSpan={4}
                className="text-muted-foreground"
              >
                Every observed application is mapped.
              </DataTable.NoResultsMessage>
            ) : (
              unmapped.data?.names.map((item) => (
                <TableRow key={item.oinName}>
                  <TableCell className="font-medium">
                    <code>{item.oinName}</code>
                  </TableCell>
                  <TableCell>{item.organizations}</TableCell>
                  <TableCell className="space-x-1">
                    {item.signOnModes.map((mode) => (
                      <Badge
                        key={mode}
                        variant="outline"
                        className={badgeTone.neutral}
                      >
                        {mode}
                      </Badge>
                    ))}
                  </TableCell>
                  <TableCell>
                    {item.suggestedEntryId && item.suggestedEntryName ? (
                      <Button
                        variant="link"
                        size="sm"
                        className="h-auto p-0"
                        onClick={() => onOpen(item.suggestedEntryId as string)}
                      >
                        {item.suggestedEntryName}
                        {item.reason ? ` (${item.reason})` : ""}
                      </Button>
                    ) : (
                      <span className="text-muted-foreground">none</span>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </DataTable.Body>
        </DataTable>
      </div>
    </section>
  );
}
