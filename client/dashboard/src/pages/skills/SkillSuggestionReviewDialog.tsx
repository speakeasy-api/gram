import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Skeleton } from "@/components/ui/Skeleton";
import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { lazy, Suspense } from "react";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

/**
 * Suggestion-level review: every change still proposed, shown together, with
 * the actions that act on the whole suggestion rather than a single change.
 */
export function SkillSuggestionReviewDialog({
  suggestion,
  currentContent,
  changeCount,
  open,
  onOpenChange,
  busy,
  onApplyAll,
  onEdit,
  onDismiss,
}: {
  suggestion: SkillEditSuggestion;
  currentContent: string;
  changeCount: number;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  busy: boolean;
  onApplyAll: () => void;
  onEdit: () => void;
  onDismiss: () => void;
}): JSX.Element {
  const changeLabel = changeCount === 1 ? "1 change" : `${changeCount} changes`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>Apply all changes</Dialog.Title>
          <Dialog.Description>
            {`This suggestion proposes ${changeLabel} to ${suggestion.skillDisplayName}. Applying records one new version carrying all of them.`}
          </Dialog.Description>
        </Dialog.Header>

        <div className="min-h-0 overflow-y-auto pr-1">
          <Suspense fallback={<Skeleton className="h-64 w-full" />}>
            <SkillTextDiff
              oldContent={currentContent}
              newContent={suggestion.proposedContent}
              oldLabel="Current SKILL.md"
              newLabel="Proposed SKILL.md"
            />
          </Suspense>
        </div>

        <Dialog.Footer className="sm:justify-between">
          <Button variant="secondary" disabled={busy} onClick={onDismiss}>
            Dismiss suggestion
          </Button>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" disabled={busy} onClick={onEdit}>
              Edit before applying
            </Button>
            <Button disabled={busy} onClick={onApplyAll}>
              {busy ? "Applying..." : "Apply all changes"}
            </Button>
          </div>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
