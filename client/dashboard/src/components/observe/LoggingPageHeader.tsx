import { SimpleTooltip } from "@/components/ui/Tooltip";
import { PageEyebrow } from "@/components/page-eyebrow";
import { Alert } from "@/components/ui/Alert";
import { Icon } from "@/components/ui/Icon";
import { useState } from "react";

export const LOG_DATA_RETENTION_MESSAGE =
  "Tool logs and agent sessions are retained for 90 days.";

function LogDataRetentionTooltip(): JSX.Element {
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

export function LogDataRetentionBanner(): JSX.Element | null {
  const [isVisible, setIsVisible] = useState(true);

  if (!isVisible) return null;

  return (
    <Alert
      variant="info"
      dismissible
      onDismiss={() => setIsVisible(false)}
      className="mb-6 text-sm"
    >
      <span className="font-medium">Data retention:</span>{" "}
      {LOG_DATA_RETENTION_MESSAGE}
    </Alert>
  );
}

export function LoggingPageHeader({
  title,
  description,
  eyebrow,
}: {
  title: string;
  description: string;
  eyebrow?: string;
}): JSX.Element {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <PageEyebrow area={eyebrow} />
      <h1 className="text-display-sm font-thin">{title}</h1>
      <p className="text-muted-foreground text-sm">
        {description}{" "}
        <span className="inline-flex align-middle">
          <LogDataRetentionTooltip />
        </span>
      </p>
    </div>
  );
}
