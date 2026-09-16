import { Check } from "lucide-react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { IdentityProviderCapabilities } from "@/components/identity-provider-capabilities";

function formatTimestamp(value: Date): string {
  return value.toLocaleString([], {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * The connection once it has proved itself, shown in place of the exchange:
 * which tenant, and what it can do there.
 */
export function OktaConnectionSummary({
  connection,
}: {
  connection: IdentityProviderConnection;
}): JSX.Element {
  const reads = connection.verifyEvidence?.reads ?? [];

  return (
    <div className="border-border bg-card space-y-5 border p-5">
      <div className="flex flex-wrap items-center gap-3">
        <Text className="font-medium">{connection.tenantIdentifier}</Text>
        <Badge variant="success" background size="sm">
          <Badge.LeftIcon>
            <Check className="h-3 w-3" />
          </Badge.LeftIcon>
          <Badge.Text>Connected</Badge.Text>
        </Badge>
        {connection.lastVerifiedAt ? (
          <Text variant="small" muted>
            Last checked {formatTimestamp(connection.lastVerifiedAt)}
          </Text>
        ) : null}
      </div>

      {connection.clientId ? (
        <Text variant="small" muted>
          Client ID{" "}
          <span className="text-foreground font-mono text-xs">
            {connection.clientId}
          </span>
        </Text>
      ) : null}

      <IdentityProviderCapabilities reads={reads} />
    </div>
  );
}
