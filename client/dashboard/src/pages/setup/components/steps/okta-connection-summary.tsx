import { Check } from "lucide-react";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import { IdentityProviderCapabilities } from "./identity-provider-capabilities";

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
 * which tenant, what it can do there, and which key Okta reads to believe us.
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

      <div className="space-y-1">
        <Text variant="small" muted>
          Speakeasy&apos;s key
        </Text>
        <code className="text-foreground block font-mono text-xs break-all">
          {connection.signingKeyKid}
        </code>
        <Text variant="small" muted>
          Okta fetches the public half of this key from the address you
          installed, every time it authenticates Speakeasy. The private half
          stays in Speakeasy.
        </Text>
      </div>
    </div>
  );
}
