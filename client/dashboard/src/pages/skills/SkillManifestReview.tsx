import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { lazy, Suspense, useMemo, useState } from "react";
import {
  groupHunksByAnchor,
  parseSkillDiffHunks,
  type SkillDiffAnchor,
} from "./skill-diff-anchors";
import { SkillManifestDialog } from "./SkillManifestDialog";
import {
  SkillSuggestionComment,
  SkillSuggestionMarker,
} from "./SkillSuggestionComment";
import { useSkillSuggestionReview } from "./use-skill-suggestion";

const SkillManifestSource = lazy(() => import("./SkillManifestSource"));

export function SkillManifestReview({
  skillId,
  latestVersion,
}: {
  skillId: string;
  latestVersion: SkillVersion;
}): JSX.Element {
  const review = useSkillSuggestionReview(skillId);
  const [editOpen, setEditOpen] = useState(false);
  const [openLine, setOpenLine] = useState<number | null>(null);

  const suggestion = review.suggestion;
  const anchors = useMemo(() => {
    if (!suggestion?.appliesCleanly) return [];
    return groupHunksByAnchor(parseSkillDiffHunks(suggestion.proposedDiff));
  }, [suggestion]);

  const renderAnchor = (anchor: SkillDiffAnchor): JSX.Element | null => {
    if (!suggestion) return null;
    if (openLine !== anchor.line) {
      return (
        <div className="px-4 py-1">
          <SkillSuggestionMarker
            count={anchor.hunks.length}
            open={false}
            onToggle={() => setOpenLine(anchor.line)}
          />
        </div>
      );
    }
    return (
      <div className="px-4">
        <SkillSuggestionComment
          suggestion={suggestion}
          hunks={anchor.hunks}
          actions={{
            disabled: review.actionsDisabled,
            approving: review.approving,
            dismissing: review.dismissing,
            onApprove: () => void review.approve(),
            onDismiss: () => void review.dismiss(),
            onEdit: () => setEditOpen(true),
          }}
        />
      </div>
    );
  };

  return (
    <div className="space-y-3">
      {review.loadError != null && (
        <div className="space-y-2">
          <ErrorAlert
            title="Unable to load suggested edits"
            error={review.loadError}
          />
          <Button size="sm" variant="outline" onClick={review.retry}>
            Retry
          </Button>
        </div>
      )}
      {suggestion && !suggestion.appliesCleanly && (
        <Type small muted>
          A suggested edit is waiting, but it no longer lines up with this
          version of the manifest. It will be retired on the next analysis pass.
        </Type>
      )}
      {review.actionError && (
        <div className="space-y-2">
          <ErrorAlert
            title="Unable to update suggestion"
            error={review.actionError}
          />
          {review.uncertain && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => void review.refresh()}
            >
              Refresh suggestion
            </Button>
          )}
        </div>
      )}
      <Suspense fallback={<Skeleton className="h-80 w-full rounded-lg" />}>
        <SkillManifestSource
          content={latestVersion.content}
          anchors={anchors}
          renderAnchor={renderAnchor}
        />
      </Suspense>
      {suggestion && (
        <SkillManifestDialog
          key={editOpen ? suggestion.id : "closed"}
          mode="approve-suggestion"
          open={editOpen}
          onOpenChange={setEditOpen}
          suggestionId={suggestion.id}
          initialContent={suggestion.proposedContent}
          onSuggestionApproved={review.hide}
        />
      )}
    </div>
  );
}
