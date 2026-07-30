import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { useApproveSkillSuggestionMutation } from "@gram/client/react-query/approveSkillSuggestion.js";
import { useDismissSkillSuggestionMutation } from "@gram/client/react-query/dismissSkillSuggestion.js";
import { useSkillSuggestions } from "@gram/client/react-query/skillSuggestions.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";

export type SkillSuggestionReview = {
  suggestion: SkillEditSuggestion | undefined;
  isPending: boolean;
  loadError: Error | null;
  actionError: string | null;
  /** True while the suggestion's server state is unknown after a failed action. */
  uncertain: boolean;
  actionsDisabled: boolean;
  approving: boolean;
  dismissing: boolean;
  /** Applies the proposed changes with the given ids, or every change when omitted. */
  approve: (changeIds?: string[]) => Promise<void>;
  dismiss: () => Promise<void>;
  refresh: () => Promise<void>;
  retry: () => void;
  hide: () => void;
};

export function useSkillSuggestionReview(
  skillId: string,
): SkillSuggestionReview {
  const queryClient = useQueryClient();
  const query = useSkillSuggestions({ skillId, limit: 20 }, undefined, {
    throwOnError: false,
  });
  const approveMutation = useApproveSkillSuggestionMutation();
  const dismissMutation = useDismissSkillSuggestionMutation();
  const [actionError, setActionError] = useState<string | null>(null);
  const [reconciling, setReconciling] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [hiddenSuggestionId, setHiddenSuggestionId] = useState<string | null>(
    null,
  );

  const loaded = query.data?.result.suggestions[0];
  const suggestion = loaded?.id === hiddenSuggestionId ? undefined : loaded;

  const approve = async (changeIds?: string[]): Promise<void> => {
    if (!suggestion) return;
    setActionError(null);
    setReconciling(true);
    try {
      const result = await approveMutation.mutateAsync({
        request: {
          approveSkillSuggestionRequestBody: { id: suggestion.id, changeIds },
        },
      });
      // A partial apply leaves the suggestion open carrying the rest, so it
      // has to stay on screen.
      if (result.outcome !== "partially_applied") {
        setHiddenSuggestionId(suggestion.id);
      }
      switch (result.outcome) {
        case "applied":
          toast.success("Suggested edit applied as a new version");
          break;
        case "partially_applied":
          toast.success("Selected changes applied as a new version");
          break;
        case "superseded":
          toast.info("Suggestion was superseded because the skill changed");
          break;
      }
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Unable to apply suggestion.";
      setActionError(
        `${message} Its status may be unknown. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
  };

  const dismiss = async (): Promise<void> => {
    if (!suggestion) return;
    setActionError(null);
    setReconciling(true);
    try {
      await dismissMutation.mutateAsync({
        request: {
          dismissSkillSuggestionRequestBody: { id: suggestion.id },
        },
      });
      setHiddenSuggestionId(suggestion.id);
      toast.success("Suggested edit dismissed");
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Unable to dismiss suggestion.";
      setActionError(
        `${message} Its status may be unknown. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
  };

  const refresh = async (): Promise<void> => {
    if (!suggestion) return;
    setReconciling(true);
    try {
      const refreshed = await query.refetch();
      if (!refreshed.isSuccess) return;
      const remainsOpen = refreshed.data?.result.suggestions.some(
        (candidate) =>
          candidate.id === suggestion.id && candidate.status === "open",
      );
      if (remainsOpen) {
        setActionError(null);
        setUncertain(false);
      } else {
        setHiddenSuggestionId(suggestion.id);
      }
    } catch {
      // Keep the uncertain state until a refresh establishes the server state.
    } finally {
      setReconciling(false);
    }
  };

  return {
    suggestion,
    isPending: query.isPending,
    loadError: query.error,
    actionError,
    uncertain,
    actionsDisabled:
      reconciling ||
      uncertain ||
      approveMutation.isPending ||
      dismissMutation.isPending ||
      !!query.error,
    approving: approveMutation.isPending,
    dismissing: dismissMutation.isPending,
    approve,
    dismiss,
    refresh,
    retry: () => void query.refetch(),
    hide: () => setHiddenSuggestionId(suggestion?.id ?? null),
  };
}
