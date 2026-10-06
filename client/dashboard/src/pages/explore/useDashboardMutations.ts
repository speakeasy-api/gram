import type { Dashboard } from "@gram/client/models/components/dashboard.js";
import type { PlacementInput } from "@gram/client/models/components/placementinput.js";
import { useAddDashboardWidgetMutation } from "@gram/client/react-query/addDashboardWidget.js";
import { useCreateDashboardMutation } from "@gram/client/react-query/createDashboard.js";
import { invalidateAllDashboard } from "@gram/client/react-query/dashboard.js";
import { invalidateAllDashboards } from "@gram/client/react-query/dashboards.js";
import { useDeleteDashboardMutation } from "@gram/client/react-query/deleteDashboard.js";
import { useDuplicateDashboardMutation } from "@gram/client/react-query/duplicateDashboard.js";
import { useRemoveDashboardWidgetMutation } from "@gram/client/react-query/removeDashboardWidget.js";
import { useSaveDashboardLayoutMutation } from "@gram/client/react-query/saveDashboardLayout.js";
import { useUpdateDashboardMutation } from "@gram/client/react-query/updateDashboard.js";
import { invalidateAllWidget } from "@gram/client/react-query/widget.js";
import { invalidateAllWidgets } from "@gram/client/react-query/widgets.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Details } from "./WidgetDialogs";

/**
 * Every write the Explore page makes to dashboards, with the lists refreshed
 * after each and a failure reported where the person can see it. Widgets are
 * refreshed too: each says which dashboards it is on, and duplicating a
 * dashboard copies its widgets.
 */
export function useDashboardMutations(): {
  create: (details: Details, then?: (created: Dashboard) => void) => void;
  update: (id: string, details: Details, then?: () => void) => void;
  /** Replace the layout; the grid autosaves, so there is no follow-up. */
  saveLayout: (id: string, placements: PlacementInput[]) => void;
  addWidget: (id: string, widgetId: string, then?: () => void) => void;
  removeWidget: (id: string, placementId: string, then?: () => void) => void;
  duplicate: (id: string, then?: (copy: Dashboard) => void) => void;
  remove: (id: string, then?: () => void) => void;
  /** Any write is in flight, a layout save included, so dialogs and menus wait. */
  pending: boolean;
  /** A layout save is in flight. */
  saving: boolean;
} {
  const queryClient = useQueryClient();
  const refresh = async () => {
    await Promise.all([
      invalidateAllDashboards(queryClient),
      invalidateAllDashboard(queryClient),
      invalidateAllWidgets(queryClient),
      invalidateAllWidget(queryClient),
    ]);
  };
  const fail = (action: string) => (error: Error) =>
    toast.error(`Could not ${action} the dashboard: ${error.message}`);

  const createMutation = useCreateDashboardMutation({
    onError: fail("create"),
  });
  const updateMutation = useUpdateDashboardMutation({ onError: fail("save") });
  const layoutMutation = useSaveDashboardLayoutMutation({
    onError: fail("save the layout of"),
  });
  const addMutation = useAddDashboardWidgetMutation({
    onError: fail("add the widget to"),
  });
  const removeMutation = useRemoveDashboardWidgetMutation({
    onError: fail("remove the widget from"),
  });
  const duplicateMutation = useDuplicateDashboardMutation({
    onError: fail("duplicate"),
  });
  const deleteMutation = useDeleteDashboardMutation({
    onError: fail("delete"),
  });

  return {
    create: (details, then) =>
      createMutation.mutate(
        { request: { createDashboardRequestBody: details } },
        {
          onSuccess: (created) => {
            then?.(created);
            void refresh();
          },
        },
      ),
    update: (id, details, then) =>
      updateMutation.mutate(
        { request: { updateDashboardRequestBody: { id, ...details } } },
        {
          onSuccess: () => {
            then?.();
            void refresh();
          },
        },
      ),
    saveLayout: (id, placements) =>
      layoutMutation.mutate(
        { request: { saveDashboardLayoutRequestBody: { id, placements } } },
        // A failed save is reported, and the grid snaps back to what is
        // saved once the refetch answers.
        { onSettled: () => void refresh() },
      ),
    addWidget: (id, widgetId, then) =>
      addMutation.mutate(
        { request: { addDashboardWidgetRequestBody: { id, widgetId } } },
        {
          onSuccess: () => {
            then?.();
            void refresh();
          },
        },
      ),
    removeWidget: (id, placementId, then) =>
      removeMutation.mutate(
        {
          request: { removeDashboardWidgetRequestBody: { id, placementId } },
        },
        {
          onSuccess: () => {
            then?.();
            void refresh();
          },
        },
      ),
    duplicate: (id, then) =>
      duplicateMutation.mutate(
        { request: { duplicateDashboardRequestBody: { id } } },
        {
          onSuccess: (copy) => {
            then?.(copy);
            void refresh();
          },
        },
      ),
    remove: (id, then) =>
      deleteMutation.mutate(
        { request: { id } },
        {
          onSuccess: () => {
            then?.();
            void refresh();
          },
        },
      ),
    pending:
      createMutation.isPending ||
      updateMutation.isPending ||
      addMutation.isPending ||
      removeMutation.isPending ||
      duplicateMutation.isPending ||
      deleteMutation.isPending ||
      layoutMutation.isPending,
    saving: layoutMutation.isPending,
  };
}
