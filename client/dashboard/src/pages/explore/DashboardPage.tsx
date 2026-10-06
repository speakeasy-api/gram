import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Skeleton } from "@/components/ui/Skeleton";
import { formatRelativeTime } from "@/lib/dates";
import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useDashboard } from "@gram/client/react-query/dashboard.js";
import { useState, type JSX } from "react";
import { Link } from "react-router";
import {
  AddWidgetDialog,
  DashboardDetailsDialog,
  DeleteDashboardDialog,
} from "./DashboardDialogs";
import { DashboardGrid } from "./DashboardGrid";
import { useCanEditDashboard } from "./useCanEditDashboard";
import { useCreatorName } from "./useCreatorName";
import { useDashboardMutations } from "./useDashboardMutations";
import type { OpenInExplore } from "./WidgetView";

/**
 * One dashboard, open: its name and who made it, then its cards on the
 * grid. Someone who may edit it adds widgets, moves cards, and renames or
 * deletes it here; anyone else reads it, or duplicates it to get their own.
 */
export function DashboardPage({
  id,
  widgets,
  backHref,
  backState,
  onOpen,
  onDeleted,
  onOpenQuery,
}: {
  id: string;
  /** The project's widgets, which the cards link to. */
  widgets: Widget[];
  /** Where the list of dashboards is. */
  backHref: string;
  backState: unknown;
  /** Open another dashboard: the copy, after duplicating. */
  onOpen: (dashboard: Dashboard) => void;
  /** This dashboard was deleted. */
  onDeleted: () => void;
  /** Open a card's question in the Explore tab. */
  onOpenQuery: OpenInExplore;
}): JSX.Element {
  const query = useDashboard({ id });
  const creator = useCreatorName();
  const canEdit = useCanEditDashboard();
  const mutations = useDashboardMutations();
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [adding, setAdding] = useState(false);

  const back = (
    <Link
      to={backHref}
      state={backState}
      className="text-muted-foreground hover:text-foreground inline-flex w-max items-center gap-1 text-xs no-underline hover:underline"
    >
      <Icon name="arrow-left" className="size-3" aria-hidden />
      All dashboards
    </Link>
  );

  if (query.isPending) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        {back}
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  // A failed background refetch keeps the cached dashboard on screen.
  if (query.data === undefined) {
    return (
      <div className="flex flex-col gap-4">
        {back}
        <InlineEmptyState
          icon="triangle-alert"
          heading="This dashboard did not load"
          description="It may have been deleted, or the request did not go through."
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void query.refetch()}
            >
              Try again
            </Button>
          }
        />
      </div>
    );
  }

  const dashboard = query.data;
  const editable = canEdit(dashboard);
  const actions: Action[] = [
    ...(editable
      ? [
          {
            label: "Rename",
            icon: "pencil" as const,
            onClick: () => setRenaming(true),
          },
        ]
      : []),
    {
      label: "Duplicate",
      icon: "copy",
      disabled: mutations.pending,
      onClick: () => mutations.duplicate(dashboard.id, onOpen),
    },
    ...(editable
      ? [
          {
            label: "Delete",
            icon: "trash" as const,
            destructive: true,
            separatorBefore: true,
            onClick: () => setDeleting(true),
          },
        ]
      : []),
  ];
  const addWidget = editable ? (
    <Button
      variant="secondary"
      size="sm"
      icon="plus"
      disabled={mutations.pending}
      onClick={() => setAdding(true)}
    >
      Add widget
    </Button>
  ) : null;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3">
        {back}
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <h2 className="text-heading-lg truncate" title={dashboard.name}>
              {dashboard.name}
            </h2>
            {dashboard.description ? (
              <p className="text-muted-foreground text-sm">
                {dashboard.description}
              </p>
            ) : null}
            <p className="text-muted-foreground text-xs">
              Created by {creator(dashboard.createdByUserId)} · Updated{" "}
              {formatRelativeTime(dashboard.updatedAt)}
              {mutations.saving ? " · Saving…" : ""}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {addWidget}
            <MoreActions
              triggerAriaLabel={`Actions for ${dashboard.name}`}
              actions={actions}
            />
          </div>
        </div>
      </div>

      {dashboard.widgets.length === 0 ? (
        <InlineEmptyState
          icon="layout-dashboard"
          heading="Nothing on this dashboard yet"
          description={
            editable
              ? "Add a saved widget to start laying it out. Each card can then be dragged and resized."
              : "Its creator has not placed any widgets on it yet."
          }
        />
      ) : (
        <DashboardGrid
          dashboard={dashboard}
          widgets={widgets}
          canEdit={editable}
          saving={mutations.saving}
          onSave={(placements) =>
            mutations.saveLayout(dashboard.id, placements)
          }
          onRemove={(placementId) =>
            mutations.removeWidget(dashboard.id, placementId)
          }
          onOpen={onOpenQuery}
        />
      )}

      <AddWidgetDialog
        key={adding ? "adding" : "not-adding"}
        open={adding}
        widgets={widgets}
        pending={mutations.pending}
        onCancel={() => setAdding(false)}
        onAdd={(widget) =>
          mutations.addWidget(dashboard.id, widget.id, () => setAdding(false))
        }
      />
      <DashboardDetailsDialog
        key={renaming ? "renaming" : "not-renaming"}
        open={renaming}
        title="Rename dashboard"
        confirm="Rename"
        initial={{ name: dashboard.name, description: dashboard.description }}
        pending={mutations.pending}
        onCancel={() => setRenaming(false)}
        onSubmit={(details) =>
          mutations.update(dashboard.id, details, () => setRenaming(false))
        }
      />
      <DeleteDashboardDialog
        name={dashboard.name}
        open={deleting}
        pending={mutations.pending}
        onCancel={() => setDeleting(false)}
        onConfirm={() =>
          mutations.remove(dashboard.id, () => {
            setDeleting(false);
            onDeleted();
          })
        }
      />
    </div>
  );
}
