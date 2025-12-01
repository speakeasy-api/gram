import { HumanizeDateTime } from "@/lib/dates";

import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { isAfter } from "date-fns";
import { Type } from "./ui/type";

export function UpdatedAt({
  date,
  italic = true,
  showRecentness = false,
  recentnessThreshold = new Date(Date.now() - 2 * 60 * 60 * 1000), // 2 hours
  className,
}: {
  date: Date;
  italic?: boolean;
  showRecentness?: boolean;
  recentnessThreshold?: Date;
  className?: string;
}) {
  const isRecent = showRecentness && isAfter(date, recentnessThreshold);
  const recentnessClassName = isRecent
    ? "font-normal! text-default-success!"
    : "";

  return (
    <Type
      as="span"
      variant="body"
      muted
      className={cn(
        "text-sm flex items-center gap-1",
        italic && "italic",
        recentnessClassName,
        className,
      )}
    >
      {isRecent && <Icon name="badge-alert" size="small" />}
      Updated <HumanizeDateTime date={date} includeTime={false} />
    </Type>
  );
}
