import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Type } from "@/components/ui/Type";
import { useProject } from "@/contexts/Auth";
import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { useApproveAllSkillSuggestionsMutation } from "@gram/client/react-query/approveAllSkillSuggestions.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";

export function ApproveAllSkillSuggestions({
  suggestions,
  total,
  fullyLoaded,
}: {
  suggestions: SkillEditSuggestion[];
  total: number;
  fullyLoaded: boolean;
}): JSX.Element | null {
  const project = useProject();
  const queryClient = useQueryClient();
  const approveAll = useApproveAllSkillSuggestionsMutation();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reconciling, setReconciling] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [confirmedSuggestions, setConfirmedSuggestions] = useState<
    SkillEditSuggestion[]
  >([]);

  if (total === 0 && !open) return null;

  const closeDialog = (): void => {
    if (reconciling) return;
    setOpen(false);
    setError(null);
    setUncertain(false);
    setConfirmedSuggestions([]);
  };

  const openDialog = (): void => {
    setError(null);
    setUncertain(false);
    setConfirmedSuggestions(suggestions);
    setOpen(true);
  };

  const approveSuggestions = async (): Promise<void> => {
    setError(null);
    setReconciling(true);
    let result: Awaited<ReturnType<typeof approveAll.mutateAsync>> | undefined;
    try {
      result = await approveAll.mutateAsync({
        request: {
          approveAllSkillSuggestionsRequestBody: {
            suggestionIds: confirmedSuggestions.map(
              (suggestion) => suggestion.id,
            ),
          },
        },
      });
    } catch (approvalError) {
      const message =
        approvalError instanceof Error
          ? approvalError.message
          : "Unable to approve suggestions.";
      setError(
        `${message} Some edits may have applied. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
    if (!result) return;
    const counts = { applied: 0, superseded: 0, conflict: 0, failed: 0 };
    for (const item of result.items) counts[item.outcome] += 1;
    const skipped = Math.max(
      0,
      confirmedSuggestions.length - result.items.length,
    );
    toast.info(
      `Applied ${counts.applied}, superseded ${counts.superseded}, conflicts ${counts.conflict}, failed ${counts.failed}, skipped ${skipped}.`,
    );
    closeDialog();
  };

  return (
    <>
      <RequireScope
        scope="skill:write"
        resourceId={project.id}
        level="component"
        reason="You need write access to approve suggested edits."
      >
        <Button
          size="lg"
          disabled={!fullyLoaded}
          tooltip={
            fullyLoaded
              ? undefined
              : "Wait for all suggested edits to finish loading."
          }
          onClick={openDialog}
        >
          Approve all ({total.toLocaleString()})
        </Button>
      </RequireScope>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          if (nextOpen) return;
          closeDialog();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Approve all suggested edits?</Dialog.Title>
            <Dialog.Description>
              Each suggestion is processed independently. Stale or conflicting
              edits may not be applied.
            </Dialog.Description>
          </Dialog.Header>
          <div
            role="region"
            aria-label="Skills included in bulk approval"
            tabIndex={0}
            className="max-h-64 overflow-y-auto rounded-lg border p-3"
          >
            <ul className="space-y-2">
              {confirmedSuggestions.map((suggestion) => (
                <li key={suggestion.id}>
                  <Type small>{suggestion.skillDisplayName}</Type>
                </li>
              ))}
            </ul>
          </div>
          {error && <ErrorAlert title="Bulk approval failed" error={error} />}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={reconciling}
              onClick={closeDialog}
            >
              Cancel
            </Button>
            <Button
              disabled={reconciling || uncertain || !fullyLoaded}
              onClick={() => void approveSuggestions()}
            >
              {reconciling
                ? "Approving..."
                : `Approve ${confirmedSuggestions.length.toLocaleString()} edits`}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
