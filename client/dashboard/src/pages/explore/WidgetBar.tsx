import { Button } from "@/components/ui/Button";
import { MoreActions } from "@/components/ui/MoreActions";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { Widget } from "@gram/client/models/components/widget.js";
import { useId, useState, type JSX } from "react";
import { AddToDashboard } from "./AddToDashboard";
import type { ExploreSpec } from "./exploreModel";
import { useAddToDashboard } from "./useAddToDashboard";
import { useCanEditWidget } from "./useCanEditWidget";
import { useWidgetMutations } from "./useWidgetMutations";
import {
  DeleteWidgetDialog,
  UnsavedDot,
  WidgetDetailsDialog,
  type Details,
} from "./WidgetDialogs";
import { copyName } from "./widgetNames";
import { describeDashboards } from "./widgetUsage";
import {
  differsFromWidget,
  specFromStoredWidget,
  widgetFromSpec,
} from "./widgetSpec";

type Naming = "create" | "copy" | "rename";

/** The Advanced pick's value for placing the new widget nowhere. */
const NO_DASHBOARD = "__none__";

const RANGED_REASON = "Pick a window to save: a widget keeps a relative one";

/**
 * The widget the builder has open, above the builder: its name, whether the
 * builder has moved on from it, and saving, renaming, duplicating and
 * deleting it. With none open it offers to save the builder as a widget.
 * Sharing what is on screen is the URL's job; sharing a widget is
 * duplicating it.
 */
export function WidgetBar({
  spec,
  widgetId,
  widgets,
  listResolving,
  onOpen,
  confirmLeave,
  onWidgetIdChange,
  onOpenDashboard,
}: {
  spec: ExploreSpec;
  /** The widget the builder has open, if any. */
  widgetId: string | null;
  /** The project's widgets, as listed. */
  widgets: Widget[];
  /** Whether the list is still catching up with the open widget. */
  listResolving: boolean;
  /** Restore a widget into the builder. */
  onOpen: (widget: Widget) => void;
  /** Run this once leaving the open widget's unsaved edits is confirmed. */
  confirmLeave: (proceed: () => void) => void;
  /** The builder now has this widget open, or none. */
  onWidgetIdChange: (widgetId: string | null) => void;
  /** Open a dashboard, once a widget is placed on it. */
  onOpenDashboard: (dashboardId: string) => void;
}): JSX.Element {
  const open = widgetId
    ? widgets.find((widget) => widget.id === widgetId)
    : undefined;
  // A widget the list has not caught up with yet — just created, or linked
  // while the list loads — is not an unsaved one, so it cannot be saved
  // again as new until it resolves.
  const resolving = widgetId !== null && !open && listResolving;
  const canEdit = useCanEditWidget();
  const editable = open ? canEdit(open) : false;
  const mutations = useWidgetMutations();

  const [naming, setNaming] = useState<Naming | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [placing, setPlacing] = useState(false);
  // Saving as new can put the widget straight onto a dashboard.
  const placement = useAddToDashboard(onOpenDashboard);
  const [dashboardId, setDashboardId] = useState<string | null>(null);
  const openNaming = (kind: Naming) => {
    setDashboardId(null);
    setNaming(kind);
  };

  const draft = (details: Details) => ({
    ...details,
    dataset: spec.dataset,
    ...widgetFromSpec(spec),
  });
  const submit = (details: Details) => {
    if (naming === "rename" && open) {
      // Renaming changes the name and description alone, so edits not yet
      // saved stay unsaved.
      mutations.update(
        open.id,
        {
          ...details,
          dataset: open.dataset,
          query: open.query,
          visualization: open.visualization,
        },
        () => setNaming(null),
      );
      return;
    }
    const target = placement.dashboards.find((d) => d.id === dashboardId);
    mutations.create(draft(details), (created) => {
      setNaming(null);
      onWidgetIdChange(created.id);
      if (target) placement.add(target, created.id);
    });
  };

  // A widget the builder cannot read is not what the builder shows, so it
  // has no edits to mark and saving would overwrite it with something else.
  const readable = open ? specFromStoredWidget(open) !== null : false;
  const changed = open && readable ? differsFromWidget(spec, open) : false;
  // A widget keeps a relative window and follows you forward in time, so a
  // query over an absolute range is shared by its link, not saved.
  const ranged = spec.range !== undefined;
  // A disabled button takes no focus and shows no tooltip, so the reason it
  // is disabled is shown beside it and named as its description.
  const rangedHintId = useId();
  const rangedHint = ranged ? (
    <span id={rangedHintId} className="text-muted-foreground text-xs">
      {RANGED_REASON}
    </span>
  ) : null;
  // A widget is linked onto its dashboards, so saving edits changes the
  // card on each of them: said beside Save, once there is something to save.
  const usageHintId = useId();
  const usage = open?.dashboards ?? [];
  const usageHint =
    changed && !ranged && usage.length > 0 ? (
      <span id={usageHintId} className="text-muted-foreground text-xs">
        Saving changes its card on {describeDashboards(usage)}
      </span>
    ) : null;

  return (
    <div className="flex min-w-0 items-center gap-2">
      {open ? (
        <>
          {changed ? (
            <UnsavedDot label="Unsaved changes" tooltip="Unsaved changes" />
          ) : null}
          {/* Someone else's widget without project write cannot be
              changed, only copied. */}
          {editable ? rangedHint : null}
          {editable ? usageHint : null}
          {editable ? (
            <Button
              variant="secondary"
              size="sm"
              icon="save"
              disabled={!readable || !changed || ranged || mutations.pending}
              aria-describedby={
                ranged ? rangedHintId : usageHint ? usageHintId : undefined
              }
              onClick={() =>
                mutations.update(
                  open.id,
                  draft({ name: open.name, description: open.description }),
                )
              }
            >
              Save
            </Button>
          ) : null}
          <MoreActions
            triggerAriaLabel="Widget actions"
            actions={[
              ...(editable
                ? [
                    {
                      label: "Rename",
                      icon: "pencil" as const,
                      onClick: () => openNaming("rename"),
                    },
                  ]
                : []),
              {
                label: "Save as new widget",
                icon: "file-plus",
                description: ranged
                  ? RANGED_REASON
                  : "Keeps the builder's unsaved edits",
                disabled: ranged,
                onClick: () => openNaming("copy"),
              },
              {
                label: "Add to dashboard",
                icon: "layout-dashboard",
                description: "Places the widget as it was saved",
                onClick: () => setPlacing(true),
              },
              {
                label: "Duplicate",
                icon: "copy",
                description: "Copies the widget as it was saved",
                onClick: () =>
                  confirmLeave(() => mutations.duplicate(open.id, onOpen)),
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
            ]}
          />
        </>
      ) : (
        <>
          <UnsavedDot label="Unsaved widget" tooltip="Not saved yet" />
          {rangedHint}
          <Button
            variant="secondary"
            size="sm"
            icon="save"
            disabled={resolving || ranged}
            aria-describedby={ranged ? rangedHintId : undefined}
            onClick={() => openNaming("create")}
          >
            Save widget
          </Button>
        </>
      )}

      <WidgetDetailsDialog
        key={naming ?? "closed"}
        open={naming !== null}
        title={
          naming === "rename"
            ? "Rename widget"
            : naming === "copy"
              ? "Save as new widget"
              : "Save widget"
        }
        confirm={naming === "rename" ? "Rename" : "Save"}
        initial={
          naming === "rename" && open
            ? { name: open.name, description: open.description }
            : naming === "copy" && open
              ? { name: copyName(open.name), description: open.description }
              : { name: "" }
        }
        pending={mutations.pending || placement.pending}
        advanced={
          naming !== "rename" && placement.dashboards.length > 0 ? (
            <label className="flex flex-col gap-1.5 text-sm">
              Add to dashboard
              <Select
                value={dashboardId ?? NO_DASHBOARD}
                onValueChange={(next) =>
                  setDashboardId(next === NO_DASHBOARD ? null : next)
                }
              >
                <SelectTrigger className="h-10 w-full" aria-label="Dashboard">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NO_DASHBOARD}>No dashboard</SelectItem>
                  {placement.dashboards.map((dashboard) => (
                    <SelectItem key={dashboard.id} value={dashboard.id}>
                      {dashboard.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>
          ) : undefined
        }
        onCancel={() => setNaming(null)}
        onSubmit={submit}
      />
      <AddToDashboard
        widget={placing && open ? open : null}
        onClose={() => setPlacing(false)}
        onOpenDashboard={onOpenDashboard}
      />
      {open ? (
        <DeleteWidgetDialog
          name={open.name}
          dashboards={open.dashboards}
          open={deleting}
          pending={mutations.pending}
          onCancel={() => setDeleting(false)}
          onConfirm={() =>
            mutations.remove(open.id, () => {
              setDeleting(false);
              onWidgetIdChange(null);
            })
          }
        />
      ) : null}
    </div>
  );
}
