import { useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { registryEntriesQuery } from "@/lib/gramAdminClient";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
      <div className="flex gap-4">
        <Input
          aria-label="Search registry"
          placeholder="Search registry"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setCursors([undefined]);
          }}
        />
        <select
          aria-label="Publication"
          className="border-input rounded-md border px-3"
          value={publication}
          onChange={(event) => {
            setPublication(event.target.value);
            setCursors([undefined]);
          }}
        >
          <option value="all">All entries</option>
          <option value="published">Published</option>
          <option value="unpublished">Unpublished</option>
        </select>
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
      {page.isPending ? (
        <p>Loading registry…</p>
      ) : (
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b">
              <th className="p-3">Name</th>
              <th>Publication</th>
              <th>Validation</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {page.data?.entries.map((entry) => (
              <tr key={entry.id} className="border-b">
                <td className="p-3">{entry.name}</td>
                <td>{entry.published ? "Published" : "Unpublished"}</td>
                <td>
                  {entry.issues.length > 0 ? (
                    <Badge variant="destructive">
                      {entry.issues.length} issues
                    </Badge>
                  ) : (
                    "Valid"
                  )}
                </td>
                <td>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setEditor({ id: entry.id })}
                    aria-label={`Edit ${entry.name}`}
                  >
                    Edit
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {!page.error && page.data?.entries.length === 0 && (
        <p>No entries found.</p>
      )}
      <div className="flex items-center gap-3">
        <Button
          variant="outline"
          disabled={cursors.length === 1 || page.isFetching}
          onClick={() => setCursors((previous) => previous.slice(0, -1))}
        >
          Previous
        </Button>
        <span>Page {cursors.length}</span>
        <Button
          variant="outline"
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
