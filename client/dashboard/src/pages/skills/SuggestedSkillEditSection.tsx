import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { SettingsSection } from "@/components/detail/settings-section";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { lazy, Suspense, useMemo, useState } from "react";
import { changeAnchor, type SkillDiffAnchor } from "./skill-diff-anchors";
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
  props: SkillTextDiffProps<SkillDiffAnchor>,
) => JSX.Element;

function toggled(
  current: ReadonlySet<string>,
  id: string,
): ReadonlySet<string> {
  const next = new Set(current);
  if (!next.delete(id)) next.add(id);
  return next;
}

export function SuggestedSkillEditSection({
  skillId,
  latestVersion,
}: {
  skillId: string;
  latestVersion: SkillVersion;
}): JSX.Element | null {
  const project = useProject();
  const review = useSkillSuggestionReview(skillId);
  const [editOpen, setEditOpen] = useState(false);
  const [reviewOpen, setReviewOpen] = useState(false);
  const [openChanges, setOpenChanges] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const [selectedChanges, setSelectedChanges] = useState<ReadonlySet<string>>(
    () => new Set(),
  );

  const suggestion = review.suggestion;
  // Each change is diffed against what the changes before it produce, so the
  // comment for a change is anchored by replaying them in order.
  const anchors = useMemo(() => {
    if (!suggestion?.appliesCleanly) return [];
    return suggestion.changes.flatMap((change) => {
      const anchor = changeAnchor(
        change,
        latestVersion.content,
        suggestion.proposedContent,
      );
      return anchor ? [anchor] : [];
    });
  }, [suggestion, latestVersion.content]);

  // The suggestion shrinks as changes are taken, so the batch only ever
  // counts changes the diff still shows.
  const selectedIds = anchors
    .map((anchor) => anchor.change.id)
    .filter((id) => selectedChanges.has(id));

  if (!review.isPending && !review.loadError && !suggestion) return null;

  const toggleChange = (changeId: string): void => {
    setOpenChanges((current) => toggled(current, changeId));
  };

  const toggleSelected = (changeId: string): void => {
    setSelectedChanges((current) => toggled(current, changeId));
  };

  // A change is applied on its own, so a comment only ever speaks for the
  // change it is attached to and shows only the reports behind it. Comments
  // open independently: comparing two proposals means reading both at once.
  const renderChange = (anchor: SkillDiffAnchor): JSX.Element => {
    const open = openChanges.has(anchor.change.id);

    return (
      <div className="space-y-1 px-4 py-1.5">
        <div className="flex items-center gap-2">
          {anchors.length > 1 && (
            <Checkbox
              checked={selectedChanges.has(anchor.change.id)}
              onCheckedChange={() => toggleSelected(anchor.change.id)}
              disabled={review.actionsDisabled}
              aria-label={`Select change at line ${anchor.line} to apply as a batch`}
            />
          )}
          <SkillSuggestionMarker
            open={open}
            onToggle={() => toggleChange(anchor.change.id)}
          />
        </div>
        {open && (
          <SkillSuggestionComment
            change={anchor.change}
            actions={{
              disabled: review.actionsDisabled,
              approving: review.approving,
              onApply: () => void review.approve([anchor.change.id]),
            }}
          />
        )}
      </div>
    );
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Suggested edit</SettingsSection.Title>
        <SettingsSection.Description>
          Analysis of agent feedback proposed these changes. Open the comment
          beside a change to see why it was proposed, and select changes to
          apply together as one new version.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {review.isPending && <Skeleton className="h-48 w-full" />}
          {review.loadError != null && (
            <div className="space-y-2">
              <ErrorAlert
                title="Unable to load the suggested edit"
                error={review.loadError}
              />
              <Button size="sm" variant="secondary" onClick={review.retry}>
                Retry
              </Button>
            </div>
          )}
          {suggestion && !suggestion.appliesCleanly && (
            <Text small muted>
              This suggestion no longer lines up with the current manifest. It
              will be retired on the next analysis pass.
            </Text>
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
                  variant="secondary"
                  onClick={() => void review.refresh()}
                >
                  Refresh suggestion
                </Button>
              )}
            </div>
          )}
          {suggestion?.appliesCleanly && anchors.length > 1 && (
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
              reason="You need write access to review suggested edits."
            >
              <div className="border-border bg-card flex flex-wrap items-center justify-between gap-3 border px-4 py-3">
                <Text small muted>
                  {`${selectedIds.length} of ${anchors.length} changes selected`}
                </Text>
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={review.actionsDisabled}
                    onClick={() => setReviewOpen(true)}
                  >
                    Apply all
                  </Button>
                  <Button
                    size="sm"
                    disabled={
                      review.actionsDisabled || selectedIds.length === 0
                    }
                    onClick={() => void review.approve(selectedIds)}
                  >
                    {review.approving
                      ? "Applying..."
                      : `Apply selected (${selectedIds.length})`}
                  </Button>
                </div>
              </div>
            </RequireScope>
          )}
          {suggestion?.appliesCleanly && (
            <Suspense fallback={<Skeleton className="h-80 w-full" />}>
              <SkillTextDiff
                oldContent={latestVersion.content}
                newContent={suggestion.proposedContent}
                oldLabel="Current SKILL.md"
                newLabel="Proposed SKILL.md"
                lineAnnotations={anchors.map((anchor) => ({
                  side: anchor.side,
                  lineNumber: anchor.line,
                  metadata: anchor,
                }))}
                renderAnnotation={(annotation) =>
                  renderChange(annotation.metadata)
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
            changeCount={anchors.length}
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
