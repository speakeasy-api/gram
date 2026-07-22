import { SimpleTooltip } from "@/components/ui/tooltip";
import { Alert, Icon } from "@speakeasy-api/moonshine";

export const LOG_DATA_RETENTION_MESSAGE =
  "Tool logs and agent sessions are retained for 90 days.";

export function LogDataRetentionTooltip(): JSX.Element {
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

export function LogDataRetentionBanner(): JSX.Element {
  return (
    <Alert variant="info" dismissible className="mb-6 text-sm">
      <span className="font-medium">Data retention:</span>{" "}
      {LOG_DATA_RETENTION_MESSAGE}
    </Alert>
  );
}

export function LoggingPageHeader({
  title,
  description,
}: {
  title: string;
  description: string;
}): JSX.Element {
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
