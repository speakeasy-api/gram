interface StatusBadgeProps {
  isSuccess: boolean;
  httpStatusCode?: number;
  severity?: string;
}

export function StatusBadge({
  isSuccess,
  httpStatusCode,
  severity,
}: StatusBadgeProps): JSX.Element {
  // For log entries with severity
  if (severity) {
    return <SeverityBadge severity={severity} />;
  }

  // For trace entries with HTTP status
  if (httpStatusCode) {
    const is4xx = httpStatusCode >= 400 && httpStatusCode < 500;
    const is5xx = httpStatusCode >= 500;

    if (is5xx) {
      return (
        <span className="bg-destructive-softest text-destructive-default rounded px-1.5 py-0.5 text-[10px] font-medium">
          {httpStatusCode}
        </span>
      );
    }
    if (is4xx) {
      return (
        <span className="bg-warning-softest text-warning-default rounded px-1.5 py-0.5 text-[10px] font-medium">
          {httpStatusCode}
        </span>
      );
    }
    return (
      <span className="bg-success-softest text-success-default rounded px-1.5 py-0.5 text-[10px] font-medium">
        {httpStatusCode}
      </span>
    );
  }

  // Default OK/ERROR badge
  if (isSuccess) {
    return (
      <span className="bg-success-softest text-success-default rounded px-1.5 py-0.5 text-[10px] font-medium">
        OK
      </span>
    );
  }

  return (
    <span className="bg-destructive-softest text-destructive-default rounded px-1.5 py-0.5 text-[10px] font-medium">
      ERROR
    </span>
  );
}

function SeverityBadge({ severity }: { severity: string }) {
  const upper = severity.toUpperCase();

  switch (upper) {
    case "ERROR":
    case "FATAL":
      return (
        <span className="bg-destructive-softest text-destructive-default rounded px-1.5 py-0.5 text-[10px] font-medium">
          {upper}
        </span>
      );
    case "WARN":
    case "WARNING":
      return (
        <span className="bg-warning-softest text-warning-default rounded px-1.5 py-0.5 text-[10px] font-medium">
          WARN
        </span>
      );
    case "DEBUG":
      return (
        <span className="bg-surface-secondary-default text-muted-foreground rounded px-1.5 py-0.5 text-[10px] font-medium">
          DEBUG
        </span>
      );
    case "INFO":
    default:
      return (
        <span className="bg-primary-softest text-primary-default rounded px-1.5 py-0.5 text-[10px] font-medium">
          INFO
        </span>
      );
  }
}
