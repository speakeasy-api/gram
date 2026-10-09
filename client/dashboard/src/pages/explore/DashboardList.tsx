import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column, type SortDescriptor } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { formatRelativeTime } from "@/lib/dates";
import type { BuiltInDashboard } from "@gram/client/models/components/builtindashboard.js";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import { useState, type JSX } from "react";
import {
  DashboardDetailsDialog,
  DeleteDashboardDialog,
} from "./DashboardDialogs";
import { useCanEditDashboard } from "./useCanEditDashboard";
import { useCreatorName } from "./useCreatorName";
import { useDashboardMutations } from "./useDashboardMutations";

// The sort the list opens on: the server's own order, most recently updated
// first.
const DEFAULT_SORT: SortDescriptor = { id: "updated", direction: "desc" };

/**
 * One row of the list: a dashboard Speakeasy ships, or one the project
 * made. The two share the columns; what each cell says depends on which.
 */
type Row =
  | {
      kind: "built-in";
      key: string;
      name: string;
      description: string | undefined;
      cards: number;
      builtIn: BuiltInDashboard;
    }
  | {
      kind: "project";
      key: string;
      name: string;
      description: string | undefined;
      cards: number;
      dashboard: Dashboard;
    };

/**
 * The project's dashboards, as the Dashboards page lists them: the ones
 * Speakeasy ships first, marked as such, then the project's own; searchable
 * by name and opened with a click. Anyone can make one; duplicating a
 * Speakeasy-built dashboard or someone else's makes a copy to change.
 */
export function DashboardList({
  dashboards,
  builtIn,
  isPending,
  isError,
  onOpen,
  onOpenBuiltIn,
  onRetry,
}: {
  dashboards: Dashboard[];
  /** The dashboards Speakeasy ships, the same in every project. */
  builtIn: BuiltInDashboard[];
  isPending: boolean;
  isError: boolean;
  onOpen: (dashboard: Dashboard) => void;
  onOpenBuiltIn: (builtIn: BuiltInDashboard) => void;
  onRetry: () => void;
}): JSX.Element {
  const creator = useCreatorName();
  const canEdit = useCanEditDashboard();
  const mutations = useDashboardMutations();

  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<SortDescriptor | null>(DEFAULT_SORT);
  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState<Dashboard | null>(null);
  const [deleting, setDeleting] = useState<Dashboard | null>(null);

  const open = (row: Row) => {
    if (row.kind === "built-in") onOpenBuiltIn(row.builtIn);
    else onOpen(row.dashboard);
  };

  const actionsFor = (row: Row): Action[] => {
    if (row.kind === "built-in") {
      return [
        {
          label: "Open",
          icon: "square-arrow-out-up-right",
          onClick: () => onOpenBuiltIn(row.builtIn),
        },
        {
          label: "Duplicate",
          icon: "copy",
          disabled: mutations.pending,
          onClick: () => mutations.duplicateBuiltIn(row.builtIn.slug, onOpen),
        },
      ];
    }
    const { dashboard } = row;
    return [
      {
        label: "Open",
        icon: "square-arrow-out-up-right",
        onClick: () => onOpen(dashboard),
      },
      ...(canEdit(dashboard)
        ? [
            {
              label: "Rename",
              icon: "pencil" as const,
              onClick: () => setRenaming(dashboard),
            },
          ]
        : []),
      {
        label: "Duplicate",
        icon: "copy",
        disabled: mutations.pending,
        onClick: () => mutations.duplicate(dashboard.id, onOpen),
      },
      ...(canEdit(dashboard)
        ? [
            {
              label: "Delete",
              icon: "trash" as const,
              destructive: true,
              separatorBefore: true,
              onClick: () => setDeleting(dashboard),
            },
          ]
        : []),
    ];
  };

  const columns: Column<Row>[] = [
    {
      key: "name",
      header: "Name",
      width: "3fr",
      sortable: true,
      sortValue: (row) => row.name,
      render: (row) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate font-medium" title={row.name}>
              {row.name}
            </span>
            {row.kind === "built-in" ? (
              <Badge variant="information" size="sm">
                Speakeasy-built
              </Badge>
            ) : null}
          </span>
          {row.description ? (
            <span
              className="text-muted-foreground truncate text-xs"
              title={row.description}
            >
              {row.description}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      key: "widgets",
      header: "Widgets",
      width: "1fr",
      render: (row) => <span className="tabular-nums">{row.cards}</span>,
    },
    {
      key: "creator",
      header: "Created by",
      width: "1.5fr",
      render: (row) => (
        <span className="truncate">
          {row.kind === "built-in"
            ? "Speakeasy"
            : creator(row.dashboard.createdByUserId)}
        </span>
      ),
    },
    {
      key: "updated",
      id: "updated",
      header: "Updated",
      width: "1fr",
      sortable: true,
      sortValue: (row) =>
        row.kind === "built-in" ? "" : row.dashboard.updatedAt,
      render: (row) =>
        row.kind === "built-in" ? null : (
          <span className="text-muted-foreground">
            {formatRelativeTime(row.dashboard.updatedAt)}
          </span>
        ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (row) => (
        // The row opens the dashboard on click, so the menu keeps its own.
        <span onClick={(event) => event.stopPropagation()}>
          <MoreActions
            triggerAriaLabel={`Actions for ${row.name}`}
            actions={actionsFor(row)}
          />
        </span>
      ),
    },
  ];

  const newDashboard = (
    <Button variant="primary" size="sm" onClick={() => setCreating(true)}>
      New dashboard
    </Button>
  );

  let body: JSX.Element;
  if (isPending) {
    body = (
      <div className="flex flex-col gap-2" aria-busy="true">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-10 w-full" />
      </div>
    );
  } else if (isError && dashboards.length === 0 && builtIn.length === 0) {
    // A failed background refetch keeps the cached list on screen.
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="The dashboards did not load"
        description="Your dashboards are safe; the list could not be fetched."
        action={
          <Button variant="secondary" size="sm" onClick={onRetry}>
            Try again
          </Button>
        }
      />
    );
  } else if (dashboards.length === 0 && builtIn.length === 0) {
    body = (
      <InlineEmptyState
        icon="layout-dashboard"
        heading="No dashboards yet"
        description="Lay saved widgets out on a grid, under one date range and filter bar, for everyone in this project."
        action={newDashboard}
      />
    );
  } else {
    const needle = search.trim().toLowerCase();
    const matches = (row: Row) =>
      needle === "" || row.name.toLowerCase().includes(needle);
    // The Speakeasy-built dashboards stay first, in their own order; the
    // sort is over the project's.
    const shipped = builtIn.map(builtInRow).filter(matches);
    const own = sortTableData(
      dashboards.map(projectRow).filter(matches),
      columns,
      sort,
    ) as Row[];
    body = (
      <>
        <Page.Toolbar>
          <Page.Toolbar.Row>
            <Page.Toolbar.Search
              value={search}
              onChange={setSearch}
              placeholder="Search dashboards"
            />
            <Page.Toolbar.Actions>{newDashboard}</Page.Toolbar.Actions>
          </Page.Toolbar.Row>
        </Page.Toolbar>
        <Table
          columns={columns}
          data={[...shipped, ...own]}
          rowKey={(row) => row.key}
          onRowClick={open}
          sort={sort}
          onSortChange={setSort}
          noResultsMessage="No dashboards match this search."
        />
      </>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {body}
      <DashboardDetailsDialog
        key={creating ? "creating" : "not-creating"}
        open={creating}
        title="New dashboard"
        confirm="Create"
        initial={{ name: "" }}
        pending={mutations.pending}
        onCancel={() => setCreating(false)}
        onSubmit={(details) =>
          mutations.create(details, (created) => {
            setCreating(false);
            onOpen(created);
          })
        }
      />
      <DashboardDetailsDialog
        key={renaming ? `renaming-${renaming.id}` : "not-renaming"}
        open={renaming !== null}
        title="Rename dashboard"
        confirm="Rename"
        initial={{
          name: renaming?.name ?? "",
          description: renaming?.description,
        }}
        pending={mutations.pending}
        onCancel={() => setRenaming(null)}
        onSubmit={(details) => {
          if (!renaming) return;
          mutations.update(renaming.id, details, () => setRenaming(null));
        }}
      />
      <DeleteDashboardDialog
        name={deleting?.name ?? ""}
        open={deleting !== null}
        pending={mutations.pending}
        onCancel={() => setDeleting(null)}
        onConfirm={() => {
          if (!deleting) return;
          mutations.remove(deleting.id, () => setDeleting(null));
        }}
      />
    </div>
  );
}

function builtInRow(builtIn: BuiltInDashboard): Row {
  return {
    kind: "built-in",
    // A slug is never a UUID, so the two kinds of key cannot collide.
    key: `built-in:${builtIn.slug}`,
    name: builtIn.name,
    description: builtIn.description,
    cards: builtIn.cards.length,
    builtIn,
  };
}

function projectRow(dashboard: Dashboard): Row {
  return {
    kind: "project",
    key: dashboard.id,
    name: dashboard.name,
    description: dashboard.description,
    cards: dashboard.widgets.length,
    dashboard,
  };
}
