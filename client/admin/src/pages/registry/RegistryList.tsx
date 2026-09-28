import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
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
import { RegistryEntrySheet, STAGE_A_NOTICE } from "./RegistryEntrySheet";

export function RegistryList(): JSX.Element {
  const [query, setQuery] = useState("");
  const [publication, setPublication] = useState("all");
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [editor, setEditor] = useState<{ id: string | null } | null>(null);
  const page = useQuery(
    registryEntriesQuery({
      query: query || undefined,
      published:
        publication === "all" ? undefined : publication === "published",
      cursor: cursors[cursors.length - 1],
      limit: 25,
    }),
  );
  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Registry</h1>
        <Button onClick={() => setEditor({ id: null })}>New entry</Button>
      </div>
      <p className="text-muted-foreground text-sm">{STAGE_A_NOTICE}</p>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Search registry"
          placeholder="Search registry"
          className="w-full sm:w-80"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setCursors([undefined]);
          }}
        />
        <Select
          value={publication}
          onValueChange={(value) => {
            setPublication(value);
            setCursors([undefined]);
          }}
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
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setEditor({ id: entry.id })}
                      aria-label={`Edit ${entry.name}`}
                    >
                      Edit
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
      {editor && (
        <RegistryEntrySheet
          id={editor.id}
          open
          onOpenChange={(open) => {
            if (!open) setEditor(null);
          }}
        />
      )}
    </div>
  );
}
