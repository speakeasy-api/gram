import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { ENTRY_TYPE_META, type TraceEntryType } from "./traceEntries";

export function TraceEntryIcon({
  entryType,
  className,
  iconClassName,
  disabled = false,
}: {
  entryType: TraceEntryType;
  className?: string;
  iconClassName?: string;
  disabled?: boolean;
}) {
  const entryMeta = ENTRY_TYPE_META[entryType];

  return (
    <div
      className={cn(
        "flex size-6 shrink-0 items-center justify-center rounded-full",
        disabled ? "bg-muted" : entryMeta.avatarClassName,
        className,
      )}
    >
      <Icon
        name={entryMeta.icon}
        className={cn(
          "size-4",
          disabled ? "text-muted-foreground" : entryMeta.iconClassName,
          iconClassName,
        )}
      />
    </div>
  );
}
