import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useApproveSkillSuggestionMutation } from "@gram/client/react-query/approveSkillSuggestion.js";
import { useDismissSkillSuggestionMutation } from "@gram/client/react-query/dismissSkillSuggestion.js";
import { useSkillSuggestions } from "@gram/client/react-query/skillSuggestions.js";
import { Badge } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { SkillManifestDialog } from "./SkillManifestDialog";

const SkillTextDiff = lazy(() => import("./SkillTextDiff"));

export function SuggestedSkillEditSection({
  skillId,
  latestVersion,
}: {
  skillId: string;
  latestVersion: SkillVersion;
}): JSX.Element | null {
  const project = useProject();
  const queryClient = useQueryClient();
  const query = useSkillSuggestions({ skillId, limit: 20 }, undefined, {
    throwOnError: false,
  });
  const approve = useApproveSkillSuggestionMutation();
  const dismiss = useDismissSkillSuggestionMutation();
  const [editOpen, setEditOpen] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [reconciling, setReconciling] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [hiddenSuggestionId, setHiddenSuggestionId] = useState<string | null>(
    null,
  );
  const loadedSuggestion = query.data?.result.suggestions[0];
  const suggestionHidden = loadedSuggestion?.id === hiddenSuggestionId;
  const suggestion = suggestionHidden ? undefined : loadedSuggestion;
  const actionsDisabled =
    reconciling ||
    uncertain ||
    approve.isPending ||
    dismiss.isPending ||
    !!query.error;

  const approveSuggestion = async (): Promise<void> => {
    if (!suggestion) return;
    setActionError(null);
    setReconciling(true);
    try {
      const result = await approve.mutateAsync({
        request: {
          approveSkillSuggestionRequestBody: { id: suggestion.id },
        },
      });
      setHiddenSuggestionId(suggestion.id);
      if (result.outcome === "applied") {
        toast.success("Suggested edit approved");
      } else {
        toast.info("Suggestion was superseded because the skill changed");
      }
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Unable to approve suggestion.";
      setActionError(
        `${message} Approval status may be unknown. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
  };

  const dismissSuggestion = async (): Promise<void> => {
    if (!suggestion) return;
    setActionError(null);
    setReconciling(true);
    try {
      await dismiss.mutateAsync({
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
        `${message} Dismissal status may be unknown. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
  };

  const refreshSuggestion = async (): Promise<void> => {
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
      // Keep the uncertain state until a refresh establishes the suggestion state.
    } finally {
      setReconciling(false);
    }
  };

  if (suggestionHidden) return null;
  if (!query.isPending && !query.error && !suggestion) return null;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Suggested edit</SettingsSection.Title>
        <SettingsSection.Description>
          Agent feedback analysis proposed an update for this skill.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {query.isPending && <Skeleton className="h-48 w-full rounded-lg" />}
          {query.error && (
            <div className="space-y-2">
              <ErrorAlert
                title={
                  suggestion
                    ? "Suggested edit may be stale"
                    : "Unable to load suggested edit"
                }
                error={query.error}
              />
              {suggestion && (
                <Type small muted>
                  Refresh before reviewing or acting on this suggestion.
                </Type>
              )}
              <Button
                size="sm"
                variant="outline"
                onClick={() => void query.refetch()}
              >
                Retry
              </Button>
            </div>
          )}
          {suggestion && (
            <div className="space-y-4">
              <div className="space-y-2">
                <Type variant="subheading">Why this edit was proposed</Type>
                <Type small>{suggestion.rationale}</Type>
                <div className="flex flex-wrap gap-2">
                  <Badge variant="neutral">
                    {suggestion.feedbackCount.toLocaleString()} feedback signals
                  </Badge>
                  <Badge variant="neutral">
                    {suggestion.scoredSessionCount.toLocaleString()} scored
                    sessions
                  </Badge>
                </div>
              </div>
              <Suspense
                fallback={<Skeleton className="h-80 w-full rounded-lg" />}
              >
                <SkillTextDiff
                  oldContent={latestVersion.content}
                  newContent={suggestion.proposedContent}
                  oldLabel="Current SKILL.md"
                  newLabel="Proposed SKILL.md"
                />
              </Suspense>
              {actionError && (
                <div className="space-y-2">
                  <ErrorAlert
                    title="Unable to update suggestion"
                    error={actionError}
                  />
                  {uncertain && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={reconciling}
                      onClick={() => void refreshSuggestion()}
                    >
                      Refresh suggestion
                    </Button>
                  )}
                </div>
              )}
            </div>
          )}
        </SettingsSection.Body>
        {suggestion && (
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Approval creates an immutable version. If the skill changed since
              analysis, the suggestion is superseded instead.
            </SettingsSection.FooterHint>
            <SettingsSection.FooterActions>
              <RequireScope
                scope="skill:write"
                resourceId={project.id}
                level="component"
                reason="You need write access to review suggested edits."
              >
                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={actionsDisabled}
                    onClick={() => void dismissSuggestion()}
                  >
                    {dismiss.isPending ? "Dismissing..." : "Dismiss"}
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={actionsDisabled}
                    onClick={() => setEditOpen(true)}
                  >
                    Edit & approve
                  </Button>
                  <Button
                    size="sm"
                    disabled={actionsDisabled}
                    onClick={() => void approveSuggestion()}
                  >
                    {approve.isPending ? "Approving..." : "Approve"}
                  </Button>
                </div>
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        )}
      </SettingsSection.Panel>
      {suggestion && (
        <SkillManifestDialog
          key={editOpen ? suggestion.id : "closed"}
          mode="approve-suggestion"
          open={editOpen}
          onOpenChange={setEditOpen}
          suggestionId={suggestion.id}
          initialContent={suggestion.proposedContent}
          onSuggestionApproved={() => setHiddenSuggestionId(suggestion.id)}
        />
      )}
    </SettingsSection>
  );
}
