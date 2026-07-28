import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { SkillFeedbackCounts } from "@gram/client/models/components/skillfeedbackcounts.js";
import { useSkillFeedback } from "@gram/client/react-query/skillFeedback.js";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import { useState } from "react";

const COUNT_LABELS: Array<[keyof SkillFeedbackCounts, string]> = [
  ["helped", "Helped"],
  ["partiallyHelped", "Partially helped"],
  ["didNotHelp", "Did not help"],
  ["misleading", "Misleading"],
  ["harmful", "Harmful"],
];

export function SkillFeedbackSection({
  skillId,
}: {
  skillId: string;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const query = useSkillFeedback({ id: skillId, limit: 20 }, undefined, {
    enabled: open,
    throwOnError: false,
  });
  const notes = query.data?.result.feedback.filter((item) => item.note) ?? [];

  return (
    <SettingsSection>
      <Collapsible open={open} onOpenChange={setOpen}>
        <SettingsSection.Panel>
          <CollapsibleTrigger className="hover:bg-muted/30 flex w-full items-center justify-between gap-4 p-5 text-left">
            <span className="block">
              <Type as="span" variant="subheading" className="block">
                All agent reviews
              </Type>
              <Type as="span" small muted className="block">
                Every report agents filed against this skill. Suggested edits
                are built from these, so review them here only when you want the
                unfiltered pool.
              </Type>
            </span>
            <Icon
              name="chevron-right"
              className={cn(
                "text-muted-foreground h-4 w-4 transition-transform",
                open && "rotate-90",
              )}
            />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="space-y-5 border-t p-5">
              {query.isPending && (
                <div className="space-y-3" aria-label="Loading agent feedback">
                  <Skeleton className="h-8 w-full" />
                  <Skeleton className="h-24 w-full" />
                </div>
              )}
              {query.error && !query.data && (
                <div className="space-y-2">
                  <ErrorAlert
                    title="Unable to load agent feedback"
                    error={query.error}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => void query.refetch()}
                  >
                    Retry
                  </Button>
                </div>
              )}
              {query.data && (
                <>
                  <div className="space-y-2">
                    <Type small muted>
                      All-time resolved outcomes (
                      {query.data.result.counts.total.toLocaleString()})
                    </Type>
                    <div className="flex flex-wrap gap-2">
                      {COUNT_LABELS.map(([key, label]) => (
                        <Badge key={key} variant="neutral">
                          {label}:{" "}
                          {query.data.result.counts[key].toLocaleString()}
                        </Badge>
                      ))}
                    </div>
                  </div>
                  <div className="space-y-3">
                    <Type variant="subheading">Recent notes</Type>
                    {notes.length === 0 ? (
                      <Type small muted>
                        No notes among recent feedback.
                      </Type>
                    ) : (
                      <ul className="divide-y rounded-lg border">
                        {notes.map((feedback) => (
                          <li key={feedback.id} className="space-y-1 p-3">
                            <div className="flex flex-wrap items-center gap-2">
                              <Badge variant="neutral">
                                {feedback.outcome.replaceAll("_", " ")}
                              </Badge>
                              <Type
                                small
                                muted
                                title={dateTimeFormatters.full.format(
                                  feedback.createdAt,
                                )}
                              >
                                <HumanizeDateTime date={feedback.createdAt} />
                              </Type>
                            </div>
                            <Type small>{feedback.note}</Type>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                </>
              )}
            </div>
          </CollapsibleContent>
        </SettingsSection.Panel>
      </Collapsible>
    </SettingsSection>
  );
}
