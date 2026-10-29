import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { registryEntriesQuery } from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { DataTable } from "@/components/data-table";
import {
  TableHeader,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { badgeTone } from "@/lib/badgeTone";
import { Badge } from "@/components/ui/badge";
import { CATALOG_NOTICE } from "./RegistryEntryEditor";
import { registrySearch, type RegistrySearch } from "./registrySearch";
import { RegistryPromotionCandidates } from "./RegistryPromotionCandidates";
import { RegistryOktaUnmapped } from "./RegistryOktaUnmapped";

export function RegistryList(): JSX.Element {
  // Read from the location rather than the route, so the filters and the
  // navigation key below always come from the same update, and the list also
  // renders outside its route in tests.
  const location = useLocation();
  const search = registrySearch(location.search);
  const navigate = useNavigate();
  // The router commits the address in a transition, after the keystroke, so
  // the box keeps its own text; otherwise a mid-string edit sends the caret to
  // the end. It takes the address's term only after a navigation it did not
  // make, which carries no `registryFilter` mark.
  const [draft, setDraft] = useState({
    text: search.q ?? "",
    key: location.state.__TSR_key,
  });
  if (draft.key !== location.state.__TSR_key) {
    setDraft({
      text: "registryFilter" in location.state ? draft.text : (search.q ?? ""),
      key: location.state.__TSR_key,
    });
  }
  const filter = (next: RegistrySearch): void => {
    void navigate({
      to: ".",
      search: (previous) => ({ ...previous, ...next }),
      state: (previous) => ({ ...previous, registryFilter: true }),
      replace: true,
    });
    setCursors([undefined]);
  };
  const open = (entryId: string): void => {
    void navigate({
      to: "/registry/$entryId",
      params: { entryId },
      search: (previous) => previous,
    });
  };
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const page = useQuery(
    registryEntriesQuery({
      query: search.q,
      published: search.publication
        ? search.publication === "published"
        : undefined,
      cursor: cursors[cursors.length - 1],
      limit: 25,
    }),
  );
  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">MCP Registry</h1>
        <Button asChild>
          <Link to="/registry/new" search={(previous) => previous}>
            New entry
          </Link>
        </Button>
      </div>
      <p className="text-muted-foreground text-sm">{CATALOG_NOTICE}</p>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search registry"
          placeholder="Search registry"
          className="w-full sm:w-80"
          value={draft.text}
          onChange={(event) => {
            setDraft({ ...draft, text: event.target.value });
            filter({ q: event.target.value || undefined });
          }}
        />
        <Select
          value={search.publication ?? "all"}
          onValueChange={(value) =>
            filter({
              publication:
                value === "published" || value === "unpublished"
                  ? value
                  : undefined,
            })
          }
        >
          <SelectTrigger aria-label="Publication">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All entries</SelectItem>
            <SelectItem value="published">Published</SelectItem>
            <SelectItem value="unpublished">Unpublished</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {page.error && (
        <div role="alert">
          <p>{page.error.message}</p>
          <Button
            disabled={page.isFetching}
            onClick={() => void page.refetch()}
          >
            Retry
          </Button>
        </div>
      )}
      <div className="overflow-x-auto rounded-lg border">
        <DataTable cellPadding="condensed">
          <TableHeader className="bg-muted">
            <TableRow>
              <TableHead scope="col">Name</TableHead>
              <TableHead scope="col">Publication</TableHead>
              <TableHead scope="col">Validation</TableHead>
              <TableHead scope="col" className="text-right">
                Actions
              </TableHead>
            </TableRow>
          </TableHeader>
          <DataTable.Body>
            {page.isPending ? (
              <DataTable.NoResultsMessage
                colSpan={4}
                className="text-muted-foreground"
              >
                Loading registry…
              </DataTable.NoResultsMessage>
            ) : !page.error && page.data?.entries.length === 0 ? (
              <DataTable.NoResultsMessage
                colSpan={4}
                className="text-muted-foreground"
              >
                No entries found.
              </DataTable.NoResultsMessage>
            ) : (
              page.data?.entries.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell className="font-medium">{entry.name}</TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={
                        entry.published ? badgeTone.success : badgeTone.neutral
                      }
                    >
                      {entry.published ? "Published" : "Unpublished"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={
                        entry.issues.length > 0
                          ? badgeTone.warning
                          : badgeTone.success
                      }
                    >
                      {entry.issues.length > 0
                        ? `${entry.issues.length} issues`
                        : "Valid"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" asChild>
                      <Link
                        to="/registry/$entryId"
                        params={{ entryId: entry.id }}
                        search={(previous) => previous}
                        aria-label={`Edit ${entry.name}`}
                      >
                        Edit
                      </Link>
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </DataTable.Body>
        </DataTable>
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button
          variant="ghost"
          size="xs"
          disabled={cursors.length === 1 || page.isFetching}
          onClick={() => setCursors((previous) => previous.slice(0, -1))}
        >
          Previous
        </Button>
        <span className="text-muted-foreground text-sm">
          Page {cursors.length}
        </span>
        <Button
          variant="ghost"
          size="xs"
          disabled={!page.data?.nextCursor || page.isFetching}
          onClick={() => {
            if (page.data?.nextCursor)
              setCursors((previous) => [...previous, page.data.nextCursor]);
          }}
        >
          Next
        </Button>
      </div>
      <RegistryPromotionCandidates onOpenEntry={open} />
      <RegistryOktaUnmapped onOpen={open} />
    </div>
  );
}
