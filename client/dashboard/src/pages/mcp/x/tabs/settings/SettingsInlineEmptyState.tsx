import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import type { ReactNode } from "react";

export function SettingsInlineEmptyState({
  title,
  description,
  action,
  actionLabel,
  onAction,
}: {
  title: string;
  description: string;
  action?: ReactNode;
  actionLabel?: string;
  onAction?: () => void;
}): JSX.Element {
  const renderedAction =
    action ??
    (actionLabel && onAction ? (
      <Button
        size="md"
        variant="secondary"
        className="shrink-0"
        onClick={onAction}
      >
        <Button.LeftIcon>
          <Plus className="size-4" />
        </Button.LeftIcon>
        <Button.Text>{actionLabel}</Button.Text>
      </Button>
    ) : null);

  return (
    <div className="bg-muted/20 flex min-h-[88px] flex-col items-start justify-between gap-3 rounded-md border border-dashed px-4 py-3 sm:flex-row sm:items-center">
      <div className="min-w-0 space-y-1">
        <Type small className="font-medium">
          {title}
        </Type>
        <Type muted small className="max-w-xl">
          {description}
        </Type>
      </div>
      {renderedAction && <div className="shrink-0">{renderedAction}</div>}
    </div>
  );
}
