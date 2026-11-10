import { useProjectSlugForRequests } from "@/contexts/Sdk";
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
  /** The dashboards have not answered yet, so none can be offered. */
  loading: boolean;
  /** The dashboards could not be fetched; retry asks again. */
  failed: boolean;
  retry: () => void;
  pending: boolean;
  /** Place the widget on the dashboard, as a new card at the bottom. */
  add: (dashboard: Dashboard, widgetId: string, then?: () => void) => void;
  /**
   * Make a dashboard with the widget as its first card. `then` runs once
   * the dashboard exists, before the widget is placed: a failed placement
   * then leaves a dashboard to add to, not a form to submit again.
   */
  create: (details: Details, widgetId: string, then?: () => void) => void;
} {
  const gramProject = useProjectSlugForRequests();
  const list = useDashboards({ gramProject });
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
    loading: list.isPending,
    failed: list.isError && list.data === undefined,
    retry: () => void list.refetch(),
    pending: mutations.pending,
    add: (dashboard, widgetId, then) =>
      mutations.addWidget(dashboard.id, widgetId, () => added(dashboard, then)),
    create: (details, widgetId, then) =>
      mutations.create(details, (created) => {
        then?.();
        mutations.addWidget(created.id, widgetId, () => added(created));
      }),
  };
}
