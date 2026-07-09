import { HumanizeDateTime } from "@/lib/dates";

import { cn } from "@/lib/utils";
import { BadgeAlert } from "lucide-react";
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
}): JSX.Element {
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
        "flex items-center gap-1 text-sm",
        italic && "italic",
        recentnessClassName,
        className,
      )}
    >
      {isRecent && <BadgeAlert className="size-4" />}
      Updated <HumanizeDateTime date={date} includeTime={false} />
    </Type>
  );
}
