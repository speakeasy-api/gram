import { parseGramUrn } from "./utils";

interface StatusBadgeProps {
  isSuccess: boolean;
  httpStatusCode?: number;
  severity?: string;
}

export function StatusBadge({
  isSuccess,
  httpStatusCode,
  severity,
}: StatusBadgeProps) {
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
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-destructive-softest text-destructive-default">
          {httpStatusCode}
        </span>
      );
    }
    if (is4xx) {
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-warning-softest text-warning-default">
          {httpStatusCode}
        </span>
      );
    }
    return (
      <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-success-softest text-success-default">
        {httpStatusCode}
      </span>
    );
  }

  // Default OK/ERROR badge
  if (isSuccess) {
    return (
      <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-success-softest text-success-default">
        OK
      </span>
    );
  }

  return (
    <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-destructive-softest text-destructive-default">
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
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-destructive-softest text-destructive-default">
          {upper}
        </span>
      );
    case "WARN":
    case "WARNING":
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-warning-softest text-warning-default">
          WARN
        </span>
      );
    case "DEBUG":
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-surface-secondary-default text-muted-foreground">
          DEBUG
        </span>
      );
    case "INFO":
    default:
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-primary-softest text-primary-default">
          INFO
        </span>
      );
  }
}

interface SpanTypeBadgeProps {
  urn: string;
}

export function SpanTypeBadge({ urn }: SpanTypeBadgeProps) {
  const { kind } = parseGramUrn(urn);

  switch (kind) {
    case "http":
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-cyan-500/20 text-cyan-400 uppercase">
          HTTP
        </span>
      );
    case "function":
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-purple-500/20 text-purple-400 uppercase">
          FN
        </span>
      );
    case "prompt":
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-amber-500/20 text-amber-400 uppercase">
          PROMPT
        </span>
      );
    default:
      return (
        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-surface-secondary-default text-muted-foreground uppercase">
          {kind || "SPAN"}
        </span>
      );
  }
}
