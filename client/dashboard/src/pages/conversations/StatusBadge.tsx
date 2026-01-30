import { ChatSummaryStatus } from "@gram/client/models/components";

interface StatusBadgeProps {
  status: ChatSummaryStatus;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  if (status === "success") {
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full bg-success-softest text-success-default">
        <span className="size-1.5 rounded-full bg-success-default" />
        Resolved
      </span>
    );
  }

  return (
    <span className="inline-flex items-center gap-1 px-2 py-0.5 text-[10px] font-medium rounded-full bg-destructive-softest text-destructive-default">
      <span className="size-1.5 rounded-full bg-destructive-default" />
      Error
    </span>
  );
}
