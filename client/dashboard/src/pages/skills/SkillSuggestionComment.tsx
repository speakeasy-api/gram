import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { SkillEditSuggestion } from "@gram/client/models/components/skilleditsuggestion.js";
import { useSkillSuggestionFeedback } from "@gram/client/react-query/skillSuggestionFeedback.js";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import { useState } from "react";

export type SkillSuggestionActions = {
  disabled: boolean;
  approving: boolean;
  dismissing: boolean;
  onApprove: () => void;
  onEdit: () => void;
  onDismiss: () => void;
};

/**
 * Collapsed gutter marker for a manifest line the suggestion touches. The count
 * only shows when more than one proposed change lands on the same line.
 */
export function SkillSuggestionMarker({
  count,
  open,
  onToggle,
}: {
  count: number;
  open: boolean;
  onToggle: () => void;
}): JSX.Element {
  const label =
    count === 1 ? "1 suggested change" : `${count} suggested changes`;

  return (
    <button
      type="button"
      aria-expanded={open}
      aria-label={label}
      onClick={onToggle}
      className={cn(
        "border-border bg-background text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 font-sans text-xs transition-colors",
        open && "text-foreground border-foreground/30",
      )}
    >
      <Icon name="message-square" className="h-3.5 w-3.5" />
      {count > 1 && <span className="font-medium">{count}</span>}
    </button>
  );
}

export function SkillSuggestionComment({
  suggestion,
  actions,
}: {
  suggestion: SkillEditSuggestion;
  actions: SkillSuggestionActions;
}): JSX.Element {
  const project = useProject();
  const [sourcesOpen, setSourcesOpen] = useState(false);

  return (
    // The card sits inside the diff's <pre>, so the prose font is reset here.
    <div className="border-border bg-card my-2 overflow-hidden rounded-lg border font-sans shadow-sm">
      <div className="space-y-3 px-4 py-3">
        <Type small>
          {suggestion.feedbackSessionCount > 0 && (
            <span className="font-medium">
              {`Requested in ${suggestion.feedbackSessionCount.toLocaleString()} ${
                suggestion.feedbackSessionCount === 1 ? "session" : "sessions"
              }. `}
            </span>
          )}
          {suggestion.rationale}
        </Type>

        <SuggestionSources
          suggestionId={suggestion.id}
          feedbackCount={suggestion.feedbackCount}
          open={sourcesOpen}
          onToggle={() => setSourcesOpen((current) => !current)}
        />
      </div>

      <div className="border-border flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
        <Type small muted>
          Applying updates the working draft only. Changes reach members when
          you promote this skill.
        </Type>
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
              disabled={actions.disabled}
              onClick={actions.onDismiss}
            >
              {actions.dismissing ? "Dismissing..." : "Dismiss"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={actions.disabled}
              onClick={actions.onEdit}
            >
              Apply with edits
            </Button>
            <Button
              size="sm"
              disabled={actions.disabled}
              onClick={actions.onApprove}
            >
              {actions.approving ? "Applying..." : "Apply to draft"}
            </Button>
          </div>
        </RequireScope>
      </div>
    </div>
  );
}

function SuggestionSources({
  suggestionId,
  feedbackCount,
  open,
  onToggle,
}: {
  suggestionId: string;
  feedbackCount: number;
  open: boolean;
  onToggle: () => void;
}): JSX.Element | null {
  const query = useSkillSuggestionFeedback(
    { id: suggestionId, limit: 50 },
    undefined,
    { enabled: open, throwOnError: false },
  );

  if (feedbackCount === 0) return null;

  const label =
    feedbackCount === 1 ? "1 agent report" : `${feedbackCount} agent reports`;

  return (
    <div className="space-y-2">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-xs transition-colors"
      >
        <Icon
          name="chevron-right"
          className={cn(
            "h-3.5 w-3.5 transition-transform",
            open && "rotate-90",
          )}
        />
        {open ? `Hide ${label}` : `Built from ${label}`}
      </button>
      {open && (
        <div className="space-y-2">
          {query.isPending && <Skeleton className="h-16 w-full rounded-md" />}
          {query.error && (
            <ErrorAlert
              title="Unable to load agent reports"
              error={query.error}
            />
          )}
          {query.data && query.data.feedback.length === 0 && (
            <Type small muted>
              The linked reports are no longer available.
            </Type>
          )}
          {query.data && query.data.feedback.length > 0 && (
            <ul className="divide-y rounded-md border">
              {query.data.feedback.map((feedback) => (
                <li key={feedback.id} className="space-y-1 p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="neutral">
                      {feedback.outcome.replaceAll("_", " ")}
                    </Badge>
                    <Type
                      small
                      muted
                      title={dateTimeFormatters.full.format(feedback.createdAt)}
                    >
                      <HumanizeDateTime date={feedback.createdAt} />
                    </Type>
                  </div>
                  {feedback.note && <Type small>{feedback.note}</Type>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
