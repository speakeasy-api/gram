import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { lazy, Suspense, useMemo, useState } from "react";
import { parseSkillDiffHunks, type SkillDiffHunk } from "./skill-diff-anchors";
import { SkillManifestDialog } from "./SkillManifestDialog";
import type { SkillTextDiffProps } from "./SkillTextDiff";
import {
  SkillSuggestionComment,
  SkillSuggestionMarker,
} from "./SkillSuggestionComment";
import { SkillSuggestionReviewDialog } from "./SkillSuggestionReviewDialog";
import { useSkillSuggestionReview } from "./use-skill-suggestion";

// lazy() erases the component's generic, so the annotation type is pinned here.
const SkillTextDiff = lazy(() => import("./SkillTextDiff")) as (
  props: SkillTextDiffProps<SkillDiffHunk>,
) => JSX.Element;

export function SuggestedSkillEditSection({
  skillId,
  latestVersion,
}: {
  skillId: string;
  latestVersion: SkillVersion;
}): JSX.Element | null {
  const review = useSkillSuggestionReview(skillId);
  const [editOpen, setEditOpen] = useState(false);
  const [reviewOpen, setReviewOpen] = useState(false);
  const [openHunk, setOpenHunk] = useState<number | null>(null);

  const suggestion = review.suggestion;
  const hunks = useMemo(() => {
    if (!suggestion?.appliesCleanly) return [];
    return parseSkillDiffHunks(suggestion.proposedDiff);
  }, [suggestion]);

  if (!review.isPending && !review.loadError && !suggestion) return null;

  // Each hunk is applied on its own, so a comment only ever speaks for the
  // change it is attached to.
  const renderHunk = (index: number): JSX.Element => {
    if (!suggestion || openHunk !== index) {
      return (
        <div className="px-4 py-1.5">
          <SkillSuggestionMarker
            open={false}
            onToggle={() => setOpenHunk(index)}
          />
        </div>
      );
    }
    return (
      <div className="px-4">
        <SkillSuggestionComment
          suggestion={suggestion}
          changeCount={hunks.length}
          actions={{
            disabled: review.actionsDisabled,
            approving: review.approving,
            onApply: () => void review.approve(index),
            onApplyAll: () => setReviewOpen(true),
          }}
        />
      </div>
    );
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Suggested edit</SettingsSection.Title>
        <SettingsSection.Description>
          Analysis of agent feedback proposed these changes. Open the comment
          beside a change to see why it was proposed and apply it on its own.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {review.isPending && <Skeleton className="h-48 w-full rounded-lg" />}
          {review.loadError != null && (
            <div className="space-y-2">
              <ErrorAlert
                title="Unable to load the suggested edit"
                error={review.loadError}
              />
              <Button size="sm" variant="outline" onClick={review.retry}>
                Retry
              </Button>
            </div>
          )}
          {suggestion && !suggestion.appliesCleanly && (
            <Type small muted>
              This suggestion no longer lines up with the current manifest. It
              will be retired on the next analysis pass.
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
          {suggestion?.appliesCleanly && (
            <Suspense
              fallback={<Skeleton className="h-80 w-full rounded-lg" />}
            >
              <SkillTextDiff
                oldContent={latestVersion.content}
                newContent={suggestion.proposedContent}
                oldLabel="Current SKILL.md"
                newLabel="Proposed SKILL.md"
                lineAnnotations={hunks.map((hunk) => ({
                  side: hunk.side,
                  lineNumber: hunk.line,
                  metadata: hunk,
                }))}
                renderAnnotation={(annotation) =>
                  renderHunk(hunks.indexOf(annotation.metadata))
                }
              />
            </Suspense>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
      {suggestion && (
        <>
          <SkillSuggestionReviewDialog
            suggestion={suggestion}
            currentContent={latestVersion.content}
            changeCount={hunks.length}
            open={reviewOpen}
            onOpenChange={setReviewOpen}
            busy={review.actionsDisabled}
            onApplyAll={() => {
              setReviewOpen(false);
              void review.approve();
            }}
            onEdit={() => {
              setReviewOpen(false);
              setEditOpen(true);
            }}
            onDismiss={() => {
              setReviewOpen(false);
              void review.dismiss();
            }}
          />
          <SkillManifestDialog
            key={editOpen ? suggestion.id : "closed"}
            mode="approve-suggestion"
            open={editOpen}
            onOpenChange={setEditOpen}
            suggestionId={suggestion.id}
            initialContent={suggestion.proposedContent}
            onSuggestionApproved={review.hide}
          />
        </>
      )}
    </SettingsSection>
  );
}
