import type { Widget } from "@gram/client/models/components/widget.js";
import { useState, type JSX } from "react";
import {
  AddToDashboardDialog,
  DashboardDetailsDialog,
} from "./DashboardDialogs";
import { useAddToDashboard } from "./useAddToDashboard";

/**
 * The "Add to dashboard" flow for one widget: pick one of the dashboards
 * the viewer may change, or make a new one with the widget on it. Mount it
 * wherever a widget's actions are, with the widget being placed or null.
 */
export function AddToDashboard({
  widget,
  onClose,
  onOpenDashboard,
}: {
  /** The widget being placed, or null when the flow is closed. */
  widget: Widget | null;
  onClose: () => void;
  /** Open a dashboard: the toast's Open, once the widget is on it. */
  onOpenDashboard: (dashboardId: string) => void;
}): JSX.Element {
  const placing = useAddToDashboard(onOpenDashboard);
  const [creating, setCreating] = useState(false);
  const done = () => {
    setCreating(false);
    onClose();
  };
  return (
    <>
      {widget ? (
        <AddToDashboardDialog
          key={widget.id}
          widget={widget}
          dashboards={placing.dashboards}
          loading={placing.loading}
          failed={placing.failed}
          onRetry={placing.retry}
          open={!creating}
          pending={placing.pending}
          onCancel={onClose}
          onAdd={(dashboard) => placing.add(dashboard, widget.id, done)}
          onNew={() => setCreating(true)}
        />
      ) : null}
      <DashboardDetailsDialog
        key={creating ? "creating" : "not-creating"}
        open={creating && widget !== null}
        title="New dashboard"
        confirm="Create and add"
        initial={{ name: "" }}
        pending={placing.pending}
        onCancel={() => setCreating(false)}
        onSubmit={(details) => {
          if (widget) placing.create(details, widget.id, done);
        }}
      />
    </>
  );
}
