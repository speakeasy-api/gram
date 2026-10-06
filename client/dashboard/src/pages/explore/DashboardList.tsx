import { InlineEmptyState } from "@/components/inline-empty-state";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { Table, type Column, type SortDescriptor } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { formatRelativeTime } from "@/lib/dates";
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
 * The project's dashboards, as the Dashboards tab lists them: searchable by
 * name and opened with a click. Anyone can make one; duplicating someone
 * else's makes a copy to change.
 */
export function DashboardList({
  dashboards,
  isPending,
  isError,
  onOpen,
  onRetry,
}: {
  dashboards: Dashboard[];
  isPending: boolean;
  isError: boolean;
  onOpen: (dashboard: Dashboard) => void;
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

  const actionsFor = (dashboard: Dashboard): Action[] => [
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

  const columns: Column<Dashboard>[] = [
    {
      key: "name",
      header: "Name",
      width: "3fr",
      sortable: true,
      sortValue: (dashboard) => dashboard.name,
      render: (dashboard) => (
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate font-medium" title={dashboard.name}>
            {dashboard.name}
          </span>
          {dashboard.description ? (
            <span
              className="text-muted-foreground truncate text-xs"
              title={dashboard.description}
            >
              {dashboard.description}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      key: "widgets",
      header: "Widgets",
      width: "1fr",
      render: (dashboard) => (
        <span className="tabular-nums">{dashboard.widgets.length}</span>
      ),
    },
    {
      key: "creator",
      header: "Created by",
      width: "1.5fr",
      render: (dashboard) => (
        <span className="truncate">{creator(dashboard.createdByUserId)}</span>
      ),
    },
    {
      key: "updated",
      id: "updated",
      header: "Updated",
      width: "1fr",
      sortable: true,
      sortValue: (dashboard) => dashboard.updatedAt,
      render: (dashboard) => (
        <span className="text-muted-foreground">
          {formatRelativeTime(dashboard.updatedAt)}
        </span>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (dashboard) => (
        // The row opens the dashboard on click, so the menu keeps its own.
        <span onClick={(event) => event.stopPropagation()}>
          <MoreActions
            triggerAriaLabel={`Actions for ${dashboard.name}`}
            actions={actionsFor(dashboard)}
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
  } else if (isError && dashboards.length === 0) {
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
  } else if (dashboards.length === 0) {
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
    const filtered = dashboards.filter(
      (dashboard) =>
        needle === "" || dashboard.name.toLowerCase().includes(needle),
    );
    const rows = sortTableData(filtered, columns, sort) as Dashboard[];
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
          data={rows}
          rowKey={(dashboard) => dashboard.id}
          onRowClick={onOpen}
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
        key={creating ? "creating" : "closed"}
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
        key={renaming?.id ?? "closed"}
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
