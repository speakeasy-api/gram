import type { Widget } from "@gram/client/models/components/widget.js";
import { useCreateWidgetMutation } from "@gram/client/react-query/createWidget.js";
import { useDeleteWidgetMutation } from "@gram/client/react-query/deleteWidget.js";
import { useDuplicateWidgetMutation } from "@gram/client/react-query/duplicateWidget.js";
import { useUpdateWidgetMutation } from "@gram/client/react-query/updateWidget.js";
import { invalidateAllWidget } from "@gram/client/react-query/widget.js";
import { invalidateAllWidgets } from "@gram/client/react-query/widgets.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { WidgetState } from "./widgetSpec";

/** What a widget is saved as: its name, description and builder state. */
export interface WidgetDraft extends WidgetState {
  name: string;
  description?: string | undefined;
  dataset: string;
}

/**
 * Every write the Explore page makes to widgets, with the list refreshed
 * after each and a failure reported where the person can see it. Each call
 * takes its own follow-up, so the bar and the list react differently to the
 * same write.
 */
export function useWidgetMutations(): {
  create: (draft: WidgetDraft, then?: (created: Widget) => void) => void;
  update: (id: string, draft: WidgetDraft, then?: () => void) => void;
  duplicate: (id: string, then?: (copy: Widget) => void) => void;
  remove: (id: string, then?: () => void) => void;
  pending: boolean;
} {
  const queryClient = useQueryClient();
  const refresh = async () => {
    await Promise.all([
      invalidateAllWidgets(queryClient),
      invalidateAllWidget(queryClient),
    ]);
  };
  const fail = (action: string) => (error: Error) =>
    toast.error(`Could not ${action} the widget: ${error.message}`);

  const createMutation = useCreateWidgetMutation({ onError: fail("save") });
  const updateMutation = useUpdateWidgetMutation({ onError: fail("save") });
  const duplicateMutation = useDuplicateWidgetMutation({
    onError: fail("duplicate"),
  });
  const deleteMutation = useDeleteWidgetMutation({ onError: fail("delete") });

  return {
    create: (draft, then) =>
      createMutation.mutate(
        { request: { createWidgetRequestBody: draft } },
        {
          onSuccess: (created) => {
            then?.(created);
            void refresh();
          },
        },
      ),
    update: (id, draft, then) =>
      updateMutation.mutate(
        { request: { updateWidgetRequestBody: { id, ...draft } } },
        {
          onSuccess: () => {
            then?.();
            void refresh();
          },
        },
      ),
    duplicate: (id, then) =>
      duplicateMutation.mutate(
        { request: { duplicateWidgetRequestBody: { id } } },
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
      duplicateMutation.isPending ||
      deleteMutation.isPending,
  };
}
