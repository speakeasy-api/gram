import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { SkillEditSuggestionChange } from "@gram/client/models/components/skilleditsuggestionchange.js";
import { useSkillSuggestionFeedback } from "@gram/client/react-query/skillSuggestionFeedback.js";
import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";
import { useState } from "react";

export type SkillSuggestionActions = {
  disabled: boolean;
  approving: boolean;
  /** Applies just the change this comment is attached to. */
  onApply: () => void;
};

/** Collapsed marker for one proposed change in the diff. */
export function SkillSuggestionMarker({
  open,
  onToggle,
}: {
  open: boolean;
  onToggle: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      aria-expanded={open}
      aria-label="Suggested change"
      onClick={onToggle}
      className={cn(
        "border-border bg-background text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 font-sans text-xs transition-colors",
        open && "text-foreground border-foreground/30",
      )}
    >
      <Icon name="message-square" className="h-3.5 w-3.5" />
    </button>
  );
}

export function SkillSuggestionComment({
  change,
  actions,
}: {
  change: SkillEditSuggestionChange;
  actions: SkillSuggestionActions;
}): JSX.Element {
  const project = useProject();
  const [sourcesOpen, setSourcesOpen] = useState(false);

  return (
    // The card sits inside the diff's <pre>, so the prose font is reset here.
    <div className="border-border bg-card my-2 overflow-hidden border font-sans shadow-sm">
      <div className="space-y-3 px-4 py-3">
        <Text small>
          {change.feedbackSessionCount > 0 && (
            <span className="font-medium">
              {`Requested in ${change.feedbackSessionCount.toLocaleString()} ${
                change.feedbackSessionCount === 1 ? "session" : "sessions"
              }. `}
            </span>
          )}
          {change.rationale}
        </Text>

        <SuggestionSources
          changeId={change.id}
          feedbackCount={change.feedbackCount}
          open={sourcesOpen}
          onToggle={() => setSourcesOpen((current) => !current)}
        />
      </div>

      <div className="border-border flex flex-wrap items-center justify-between gap-3 border-t px-4 py-3">
        <Text small muted>
          Applying records a new version of this skill and makes it the latest
          one agents load.
        </Text>
        <RequireScope
          scope="skill:write"
          resourceId={project.id}
          level="component"
          reason="You need write access to review suggested edits."
        >
          <Button
            size="sm"
            disabled={actions.disabled}
            onClick={actions.onApply}
          >
            {actions.approving ? "Applying..." : "Apply"}
          </Button>
        </RequireScope>
      </div>
    </div>
  );
}

function SuggestionSources({
  changeId,
  feedbackCount,
  open,
  onToggle,
}: {
  changeId: string;
  feedbackCount: number;
  open: boolean;
  onToggle: () => void;
}): JSX.Element | null {
  const query = useSkillSuggestionFeedback(
    { id: changeId, limit: 50 },
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
          {query.isPending && <Skeleton className="h-16 w-full" />}
          {query.error && (
            <ErrorAlert
              title="Unable to load agent reports"
              error={query.error}
            />
          )}
          {query.data && query.data.feedback.length === 0 && (
            <Text small muted>
              The linked reports are no longer available.
            </Text>
          )}
          {query.data && query.data.feedback.length > 0 && (
            <ul className="divide-y border">
              {query.data.feedback.map((feedback) => (
                <li key={feedback.id} className="space-y-1 p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant="neutral">
                      {feedback.outcome.replaceAll("_", " ")}
                    </Badge>
                    <Text
                      small
                      muted
                      title={dateTimeFormatters.full.format(feedback.createdAt)}
                    >
                      <HumanizeDateTime date={feedback.createdAt} />
                    </Text>
                  </div>
                  {feedback.note && <Text small>{feedback.note}</Text>}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
