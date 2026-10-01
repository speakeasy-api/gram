import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { MoreActions } from "@/components/ui/MoreActions";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Skeleton } from "@/components/ui/Skeleton";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { formatRelativeTime } from "@/lib/dates";
import type { ExploreQuery } from "@gram/client/models/components/explorequery.js";
import { useCreateExploreQueryMutation } from "@gram/client/react-query/createExploreQuery.js";
import { useDeleteExploreQueryMutation } from "@gram/client/react-query/deleteExploreQuery.js";
import {
  invalidateAllExploreQueries,
  useExploreQueries,
} from "@gram/client/react-query/exploreQueries.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useUpdateExploreQueryMutation } from "@gram/client/react-query/updateExploreQuery.js";
import { useQueryClient } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import { useState, type FormEvent, type JSX } from "react";
import { toast } from "sonner";
import type { ExploreSpec } from "./exploreModel";
import { differsFromSaved, savedSpecFromSpec } from "./savedSpec";

/** At most this long, matching the explore service. */
const MAX_NAME_LENGTH = 200;

/**
 * The project's saved queries: the one the builder has open, the list to
 * open another from, and saving, renaming and deleting. A query is a named,
 * kept question; sharing what is on screen is the URL's job, not this one's.
 */
export function SavedQueryBar({
  spec,
  savedId,
  onOpen,
  onSavedIdChange,
}: {
  spec: ExploreSpec;
  /** The saved query the builder has open, if any. */
  savedId: string | null;
  /** Restore a saved query into the builder. */
  onOpen: (query: ExploreQuery) => void;
  /** The builder now has this saved query open, or none. */
  onSavedIdChange: (savedId: string | null) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const list = useExploreQueries();
  const queries = list.data?.queries ?? [];
  const open = savedId ? queries.find((query) => query.id === savedId) : null;
  const creator = useCreatorName();

  const [naming, setNaming] = useState<"create" | "rename" | null>(null);
  const [deleting, setDeleting] = useState(false);

  const refresh = () => invalidateAllExploreQueries(queryClient);
  const fail = (action: string) => (error: Error) =>
    toast.error(`Could not ${action} the query: ${error.message}`);

  const create = useCreateExploreQueryMutation({
    onSuccess: async (created) => {
      setNaming(null);
      onSavedIdChange(created.id);
      await refresh();
    },
    onError: fail("save"),
  });
  const update = useUpdateExploreQueryMutation({
    onSuccess: async () => {
      setNaming(null);
      await refresh();
    },
    onError: fail("save"),
  });
  const remove = useDeleteExploreQueryMutation({
    onSuccess: async () => {
      setDeleting(false);
      onSavedIdChange(null);
      await refresh();
    },
    onError: fail("delete"),
  });

  const saveNew = (name: string) =>
    create.mutate({
      request: {
        createQueryRequestBody: {
          name,
          dataset: spec.dataset,
          spec: savedSpecFromSpec(spec),
        },
      },
    });
  // Saving in place writes what the builder holds under the query's name.
  const saveInPlace = (query: ExploreQuery) =>
    update.mutate({
      request: {
        updateQueryRequestBody: {
          id: query.id,
          name: query.name,
          dataset: spec.dataset,
          spec: savedSpecFromSpec(spec),
        },
      },
    });
  // Renaming changes the name alone, so edits not yet saved stay unsaved.
  const rename = (query: ExploreQuery, name: string) =>
    update.mutate({
      request: {
        updateQueryRequestBody: {
          id: query.id,
          name,
          dataset: query.dataset,
          spec: query.spec,
        },
      },
    });

  const changed = open ? differsFromSaved(spec, open) : false;
  const saving = create.isPending || update.isPending;

  return (
    <div className="flex flex-wrap items-center gap-3">
      <SavedQueryList
        queries={queries}
        isPending={list.isPending}
        isError={list.isError}
        openId={open?.id ?? null}
        creator={creator}
        onOpen={onOpen}
      />
      <div className="flex min-w-0 flex-1 items-baseline gap-2">
        {open ? (
          <>
            <span className="truncate text-sm font-medium" title={open.name}>
              {open.name}
            </span>
            <span className="text-muted-foreground shrink-0 text-xs">
              by {creator(open.createdByUserId)}
              {changed ? " · Unsaved changes" : ""}
            </span>
          </>
        ) : (
          <span className="text-muted-foreground text-sm">Unsaved query</span>
        )}
      </div>
      {open ? (
        <>
          <Button
            variant="secondary"
            size="sm"
            icon="save"
            disabled={!changed || saving}
            onClick={() => saveInPlace(open)}
          >
            Save
          </Button>
          <MoreActions
            triggerAriaLabel="Query actions"
            actions={[
              {
                label: "Rename",
                icon: "pencil",
                onClick: () => setNaming("rename"),
              },
              {
                label: "Save as new query",
                icon: "copy",
                onClick: () => setNaming("create"),
              },
              {
                label: "Delete",
                icon: "trash",
                destructive: true,
                separatorBefore: true,
                onClick: () => setDeleting(true),
              },
            ]}
          />
        </>
      ) : (
        <Button
          variant="secondary"
          size="sm"
          icon="save"
          onClick={() => setNaming("create")}
        >
          Save query
        </Button>
      )}

      <NameDialog
        // Remounted per opening, so the field starts from the right name.
        key={naming ?? "closed"}
        open={naming !== null}
        title={naming === "rename" ? "Rename query" : "Save query"}
        confirm={naming === "rename" ? "Rename" : "Save"}
        initialName={
          naming === "rename" && open
            ? open.name
            : open
              ? `${open.name} (copy)`
              : ""
        }
        pending={saving}
        onCancel={() => setNaming(null)}
        onSubmit={(name) => {
          if (naming === "rename" && open) rename(open, name);
          else saveNew(name);
        }}
      />
      {open ? (
        <Dialog
          open={deleting}
          onOpenChange={(next) => {
            if (!remove.isPending) setDeleting(next);
          }}
        >
          <Dialog.Content closeable={!remove.isPending}>
            <Dialog.Header>
              <Dialog.Title>Delete “{open.name}”?</Dialog.Title>
              <Dialog.Description>
                It leaves this project's saved queries for everyone. The builder
                keeps what it shows, so the query is still a link until you move
                on.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button
                variant="tertiary"
                onClick={() => setDeleting(false)}
                disabled={remove.isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={() => remove.mutate({ request: { id: open.id } })}
                disabled={remove.isPending}
              >
                Delete
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      ) : null}
    </div>
  );
}

function SavedQueryList({
  queries,
  isPending,
  isError,
  openId,
  creator,
  onOpen,
}: {
  queries: ExploreQuery[];
  isPending: boolean;
  isError: boolean;
  openId: string | null;
  creator: (userId: string | undefined) => string;
  onOpen: (query: ExploreQuery) => void;
}): JSX.Element {
  const [shown, setShown] = useState(false);
  return (
    <Popover open={shown} onOpenChange={setShown}>
      <PopoverTrigger asChild>
        <Button variant="tertiary" size="sm" icon="bookmark">
          Saved queries
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 p-1">
        {isPending ? (
          <div className="flex flex-col gap-2 p-2" aria-busy="true">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : isError ? (
          <p className="text-muted-foreground p-3 text-sm">
            The saved queries did not load.
          </p>
        ) : queries.length === 0 ? (
          <p className="text-muted-foreground p-3 text-sm">
            No saved queries in this project yet. Save one to keep it here.
          </p>
        ) : (
          // The server lists them most recently updated first.
          <ul className="flex max-h-96 flex-col overflow-y-auto">
            {queries.map((query) => (
              <li key={query.id}>
                <button
                  type="button"
                  aria-current={query.id === openId ? "true" : undefined}
                  className="hover:bg-muted focus-visible:ring-ring aria-[current=true]:bg-muted flex w-full flex-col items-start gap-0.5 rounded-sm px-3 py-2 text-left focus-visible:ring-2 focus-visible:outline-none"
                  onClick={() => {
                    setShown(false);
                    onOpen(query);
                  }}
                >
                  <span className="flex w-full items-center gap-1.5 text-sm">
                    <span className="truncate">{query.name}</span>
                    {query.invalidReason ? (
                      <SimpleTooltip tooltip={query.invalidReason}>
                        <TriangleAlert
                          className="text-warning size-3.5 shrink-0"
                          aria-label="No longer runs"
                        />
                      </SimpleTooltip>
                    ) : null}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {query.dataset} · {creator(query.createdByUserId)} ·{" "}
                    {formatRelativeTime(query.updatedAt)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}

function NameDialog({
  open,
  title,
  confirm,
  initialName,
  pending,
  onCancel,
  onSubmit,
}: {
  open: boolean;
  title: string;
  confirm: string;
  initialName: string;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (name: string) => void;
}): JSX.Element {
  const [name, setName] = useState(initialName);
  const trimmed = name.trim();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (trimmed !== "") onSubmit(trimmed);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <Dialog.Content closeable={!pending}>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Dialog.Header>
            <Dialog.Title>{title}</Dialog.Title>
            <Dialog.Description>
              Saved queries are shared with everyone in this project. Names need
              not be unique; each shows who saved it.
            </Dialog.Description>
          </Dialog.Header>
          <Input
            value={name}
            onChange={setName}
            maxLength={MAX_NAME_LENGTH}
            placeholder="Cost by model"
            aria-label="Query name"
            autoFocus
          />
          <Dialog.Footer>
            <Button
              type="button"
              variant="tertiary"
              onClick={onCancel}
              disabled={pending}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              disabled={pending || trimmed === ""}
            >
              {confirm}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

/** Who saved a query, by the project's members, as a display name. */
function useCreatorName(): (userId: string | undefined) => string {
  const { data } = useMembers();
  return (userId) => {
    if (!userId) return "Unknown";
    const member = (data?.members ?? []).find((m) => m.id === userId);
    if (member) return member.name || member.email;
    // Members still loading, or a creator who has since left.
    return data === undefined ? "…" : "A former member";
  };
}
