import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { WorkloadOrganizationConnectionDetails } from "@gram/client/models/components/workloadorganizationconnectiondetails.js";
import { TriangleAlert } from "lucide-react";
import { type ReactNode, useId } from "react";
import {
  notReadyWarning,
  RESOURCE_HINT,
  unavailableMessage,
  useOrganizationConnection,
} from "./organizationConnection";

function Note({
  children,
  tooltip,
  warning,
}: {
  children: ReactNode;
  tooltip?: string;
  warning?: boolean;
}): JSX.Element {
  const note = (
    <span
      tabIndex={tooltip ? 0 : undefined}
      className="inline-flex items-center gap-1"
    >
      {warning && (
        <TriangleAlert
          aria-hidden
          className="text-default-warning size-3.5 shrink-0"
        />
      )}
      <Text as="span" small muted={!warning} warning={warning}>
        {children}
      </Text>
    </span>
  );
  if (!tooltip) return note;
  return <SimpleTooltip tooltip={tooltip}>{note}</SimpleTooltip>;
}

function EndpointValue({
  details,
}: {
  details: WorkloadOrganizationConnectionDetails;
}): JSX.Element {
  const unavailable = unavailableMessage(details);
  if (unavailable !== null) return <Note>{unavailable}</Note>;

  const warning = notReadyWarning(details);
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <span className="flex min-w-0 items-center gap-1">
        <code className="bg-muted min-w-0 px-2 py-1 text-xs break-all">
          {details.tokenEndpoint}
        </code>
        <CopyButton
          text={details.tokenEndpoint}
          size="xs"
          tooltip="Copy token endpoint"
          className="shrink-0"
        />
      </span>
      {warning !== null && (
        <Note warning tooltip={warning}>
          Agent authorization is off
        </Note>
      )}
    </span>
  );
}

function hasEndpoint(
  details: WorkloadOrganizationConnectionDetails | undefined,
): boolean {
  return details !== undefined && unavailableMessage(details) === null;
}

/**
 * The organization's one token endpoint, which a platform exchanges its
 * workloads' identity tokens at for any of the organization's MCP servers.
 */
export function OrganizationTokenEndpoint({
  className,
}: {
  className?: string;
}): JSX.Element {
  const labelId = useId();
  const { data, isPending, isError, refetch } = useOrganizationConnection();

  let body: ReactNode;
  if (isPending) {
    body = <Skeleton className="h-6 w-64" />;
  } else if (isError) {
    body = (
      <span className="inline-flex items-center gap-1">
        <Note warning>Couldn&apos;t load the token endpoint</Note>
        <Button
          type="button"
          size="xs"
          variant="tertiary"
          onClick={() => void refetch()}
        >
          <Button.Text>Try again</Button.Text>
        </Button>
      </span>
    );
  } else {
    body = <EndpointValue details={data} />;
  }

  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <div
        role="group"
        aria-labelledby={labelId}
        className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2"
      >
        <span id={labelId} className="text-eyebrow">
          Token endpoint
        </span>
        {body}
      </div>
      {hasEndpoint(data) && (
        <Text muted small>
          {RESOURCE_HINT}
        </Text>
      )}
    </div>
  );
}
