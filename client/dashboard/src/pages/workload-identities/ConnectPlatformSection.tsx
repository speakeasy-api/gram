import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { WorkloadOrganizationConnectionDetails } from "@gram/client/models/components/workloadorganizationconnectiondetails.js";
import type { ReactNode } from "react";
import {
  AUDIENCE_RULE,
  notReadyWarning,
  unavailableMessage,
  useOrganizationConnection,
} from "./organizationConnection";

function ConnectionValue({
  label,
  value,
  note,
}: {
  label: string;
  value: string;
  note?: ReactNode;
}): JSX.Element {
  return (
    <Stack gap={1}>
      <Text muted small>
        {label}
      </Text>
      <div className="bg-muted flex items-center gap-2 px-3 py-2">
        <code className="min-w-0 flex-1 text-xs break-all">{value}</code>
        <CopyButton
          text={value}
          size="xs"
          tooltip={`Copy ${label.toLowerCase()}`}
          className="shrink-0"
        />
      </div>
      {note && (
        <Text muted small>
          {note}
        </Text>
      )}
    </Stack>
  );
}

function tokenEndpointNote(
  details: WorkloadOrganizationConnectionDetails,
): string {
  if (details.onAuthenticationHost) {
    return "On Gram's authentication host, deliberately a different host from the MCP servers' API host: some platforms require the token endpoint to be separate from the hosts the token is sent to.";
  }
  return "On the platform host, the same host the MCP servers answer on.";
}

function ConnectionValues({
  details,
}: {
  details: WorkloadOrganizationConnectionDetails;
}): JSX.Element {
  const unavailable = unavailableMessage(details);
  if (unavailable !== null) {
    return (
      <InlineEmptyState
        icon="lock"
        heading={unavailable}
        description="There is no organization token endpoint to point this platform at yet."
      />
    );
  }
  const warning = notReadyWarning(details);
  return (
    <Stack gap={4}>
      {warning !== null && (
        <Alert variant="warning" alignTop>
          <div className="text-sm break-words">
            <p className="font-medium">Not ready: exchanges will fail</p>
            <p>{warning}</p>
          </div>
        </Alert>
      )}
      <ConnectionValue
        label="Token endpoint"
        value={details.tokenEndpoint}
        note={tokenEndpointNote(details)}
      />
      <ConnectionValue
        label="Authorization server issuer"
        value={details.issuer}
        note={AUDIENCE_RULE}
      />
      <Text muted small>
        List each MCP server&apos;s host among the platform&apos;s allowed API
        hosts.
      </Text>
    </Stack>
  );
}

/**
 * What to enter in the platform's own console so its workloads can exchange
 * their identity tokens for sessions on the organization's MCP servers.
 */
export function ConnectPlatformSection(): JSX.Element {
  const { data, isPending, isError, refetch } = useOrganizationConnection();

  let body: ReactNode;
  if (isPending) {
    body = <SkeletonTable />;
  } else if (isError) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the connection details"
        description="The request failed. Try again in a moment."
        action={
          <Button size="sm" variant="secondary" onClick={() => void refetch()}>
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  } else {
    body = <ConnectionValues details={data} />;
  }

  return (
    <section className="mt-10" aria-labelledby="connect-platform-title">
      <h2 id="connect-platform-title" className="text-display-xs font-thin">
        Connect this platform
      </h2>
      <Text muted className="mt-1 mb-4">
        Enter these values in the platform&apos;s console.
      </Text>
      {body}
    </section>
  );
}
