import { HumanizeDateTime } from "@/lib/dates";

import { cn } from "@/lib/utils";
import { Type } from "./ui/type";

export function UpdatedAt({
  date,
  italic = true,
  className,
}: {
  date: Date;
  italic?: boolean;
  className?: string;
}) {
  return (
    <Type
      as="span"
      variant="body"
      muted
      className={cn("text-sm inline", italic && "italic", className)}
    >
      Updated <HumanizeDateTime date={date} includeTime={false} />
    </Type>
  );
}
