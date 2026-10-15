import { Fragment } from "react";

import { Text } from "@/components/ui/Text";
import { useRemoteSessionIssuersByIds } from "@/lib/remote-identity";
import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";

function vendorHost(issuer: string): string {
  try {
    return new URL(issuer).host;
  } catch {
    return issuer;
  }
}

function suffix(provider: EMAProvider): string {
  if (!provider.unavailableReason) return "";
  return provider.unavailableReason === "not_trusted_by_endpoint"
    ? " (not used by this server)"
    : " (can’t serve now)";
}

/** Read-only summary of the vendor's EMA provider order. */
export function EmaProviderOrderLine({
  order,
  speakeasyOnly,
}: {
  order: EMAProviderOrder;
  /** The server's sign-in trusts no external identity provider. */
  speakeasyOnly: boolean;
}): JSX.Element {
  const ids = order.providers.flatMap((provider) =>
    provider.kind === "external" && provider.remoteSessionIssuerId
      ? [provider.remoteSessionIssuerId]
      : [],
  );
  const issuers = useRemoteSessionIssuersByIds(ids);
  const name = (provider: EMAProvider): string => {
    if (provider.kind === "speakeasy") return "Speakeasy";
    const issuer = issuers.items.find(
      (item) => item.id === provider.remoteSessionIssuerId,
    );
    return issuer?.name || "Identity provider";
  };
  const shown = order.providers.filter(
    (provider) =>
      provider.kind === "speakeasy" || !!provider.remoteSessionIssuerId,
  );
  const host = vendorHost(order.vendorIssuer);

  if (order.isDefault && speakeasyOnly) {
    return (
      <Text muted small className="block" data-testid="ema-provider-order">
        Provider order: no order saved yet for {host}.
      </Text>
    );
  }
  return (
    <Text muted small className="block" data-testid="ema-provider-order">
      Tries:{" "}
      {shown.length === 0
        ? "this server’s identity provider"
        : shown.map((provider, index) => (
            <Fragment
              key={`${provider.kind}-${provider.remoteSessionIssuerId ?? index}`}
            >
              {index > 0 && " → "}
              <span className="text-foreground">{name(provider)}</span>
              {suffix(provider)}
            </Fragment>
          ))}
      {order.isDefault ? " (default)" : ""} · applies to every server on {host}
    </Text>
  );
}
