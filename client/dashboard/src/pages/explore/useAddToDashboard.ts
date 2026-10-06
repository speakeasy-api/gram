import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import { useDashboards } from "@gram/client/react-query/dashboards.js";
import { toast } from "sonner";
import { useCanEditDashboard } from "./useCanEditDashboard";
import { useDashboardMutations } from "./useDashboardMutations";
import type { Details } from "./WidgetDialogs";

/**
 * Placing a widget on a dashboard from wherever a widget is: the dashboards
 * the viewer may change, and the two ways on — an existing one, or a new
 * one made for it. Either way the toast offers to open the dashboard.
 */
export function useAddToDashboard(onOpenDashboard: (id: string) => void): {
  /** The project's dashboards the viewer may add to, most recent first. */
  dashboards: Dashboard[];
  pending: boolean;
  /** Place the widget on the dashboard, as a new card at the bottom. */
  add: (dashboard: Dashboard, widgetId: string, then?: () => void) => void;
  /** Make a dashboard with the widget as its first card. */
  create: (details: Details, widgetId: string, then?: () => void) => void;
} {
  const list = useDashboards();
  const canEdit = useCanEditDashboard();
  const mutations = useDashboardMutations();
  const dashboards = (list.data?.dashboards ?? []).filter(canEdit);
  const added = (dashboard: Dashboard, then?: () => void) => {
    then?.();
    toast.success(`Added to “${dashboard.name}”`, {
      action: { label: "Open", onClick: () => onOpenDashboard(dashboard.id) },
    });
  };
  return {
    dashboards,
    pending: mutations.pending,
    add: (dashboard, widgetId, then) =>
      mutations.addWidget(dashboard.id, widgetId, () => added(dashboard, then)),
    create: (details, widgetId, then) =>
      mutations.create(details, (created) =>
        mutations.addWidget(created.id, widgetId, () => added(created, then)),
      ),
  };
}
