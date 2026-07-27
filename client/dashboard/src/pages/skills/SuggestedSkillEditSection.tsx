import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { lazy, Suspense, useMemo, useState } from "react";
import {
  groupHunksByAnchor,
  parseSkillDiffHunks,
  type SkillDiffAnchor,
} from "./skill-diff-anchors";
import { SkillManifestDialog } from "./SkillManifestDialog";
import type { SkillTextDiffProps } from "./SkillTextDiff";
import {
  SkillSuggestionComment,
  SkillSuggestionMarker,
} from "./SkillSuggestionComment";
import { useSkillSuggestionReview } from "./use-skill-suggestion";

// lazy() erases the component's generic, so the annotation type is pinned here.
const SkillTextDiff = lazy(() => import("./SkillTextDiff")) as (
  props: SkillTextDiffProps<SkillDiffAnchor>,
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
  const [openAnchor, setOpenAnchor] = useState<string | null>(null);

  const suggestion = review.suggestion;
  const anchors = useMemo(() => {
    if (!suggestion?.appliesCleanly) return [];
    return groupHunksByAnchor(parseSkillDiffHunks(suggestion.proposedDiff));
  }, [suggestion]);

  if (!review.isPending && !review.loadError && !suggestion) return null;

  const renderAnchor = (anchor: SkillDiffAnchor): JSX.Element | null => {
    if (!suggestion) return null;
    const key = `${anchor.side}:${anchor.line}`;
    if (openAnchor !== key) {
      return (
        <div className="px-4 py-1.5">
          <SkillSuggestionMarker
            count={anchor.hunks.length}
            open={false}
            onToggle={() => setOpenAnchor(key)}
          />
        </div>
      );
    }
    return (
      <div className="px-4">
        <SkillSuggestionComment
          suggestion={suggestion}
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
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Suggested edit</SettingsSection.Title>
        <SettingsSection.Description>
          Analysis of agent feedback proposed this change. Open a comment beside
          a changed line to see why it was proposed and act on it.
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
                lineAnnotations={anchors.map((anchor) => ({
                  side: anchor.side,
                  lineNumber: anchor.line,
                  metadata: anchor,
                }))}
                renderAnnotation={(annotation) =>
                  renderAnchor(annotation.metadata)
                }
              />
            </Suspense>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
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
    </SettingsSection>
  );
}
