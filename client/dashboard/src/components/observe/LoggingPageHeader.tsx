import { SimpleTooltip } from "@/components/ui/tooltip";
import { Icon } from "@speakeasy-api/moonshine";

export const LOG_DATA_RETENTION_MESSAGE =
  "Tool logs and agent sessions are retained for 90 days.";

export function LogDataRetentionTooltip() {
  return (
    <SimpleTooltip tooltip={LOG_DATA_RETENTION_MESSAGE}>
      <button
        type="button"
        aria-label="About data retention"
        className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
      >
        <Icon name="info" className="size-3.5" />
      </button>
    </SimpleTooltip>
  );
}

export function LoggingPageHeader({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-center gap-1.5">
        <h1 className="text-xl font-semibold">{title}</h1>
        <LogDataRetentionTooltip />
      </div>
      <p className="text-muted-foreground text-sm">{description}</p>
    </div>
  );
}
