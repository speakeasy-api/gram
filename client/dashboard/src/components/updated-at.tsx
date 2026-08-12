import { HumanizeDateTime } from "@/lib/dates";

import { cn } from "@/lib/utils";
import { Icon } from "@/components/ui/Icon";
import { isAfter } from "date-fns";
import { Text } from "@/components/ui/Text";

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
    <Text
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
      {isRecent && <Icon name="badge-alert" size="small" />}
      Updated <HumanizeDateTime date={date} includeTime={false} />
    </Text>
  );
}
