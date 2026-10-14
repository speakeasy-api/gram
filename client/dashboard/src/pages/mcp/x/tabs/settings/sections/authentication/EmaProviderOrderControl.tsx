import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { EMAProviderInput } from "@gram/client/models/components/emaproviderinput.js";
import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";
import { useSetOrganizationRemoteSessionIssuerEMAProvidersMutation } from "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js";

function providerLabel(provider: EMAProvider): string {
  return provider.kind === "speakeasy" ? "Speakeasy" : "Identity provider";
}

/** The saved order with Speakeasy added first or removed; an empty list restores the default. */
function toggledSpeakeasyOrder(
  order: EMAProviderOrder,
  include: boolean,
): EMAProviderInput[] {
  const external: EMAProviderInput[] = order.providers.flatMap((provider) =>
    provider.kind === "external" && provider.remoteSessionIssuerId
      ? [
          {
            kind: "external",
            remoteSessionIssuerId: provider.remoteSessionIssuerId,
          },
        ]
      : [],
  );
  return include ? [{ kind: "speakeasy" }, ...external] : external;
}

/** The vendor's EMA provider order, with a Speakeasy on/off switch for organization admins. */
export function EmaProviderOrderControl({
  order,
  vendorRemoteSessionIssuerId,
  onSaved,
}: {
  order: EMAProviderOrder;
  vendorRemoteSessionIssuerId: string;
  onSaved: () => void;
}): JSX.Element {
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const save = useSetOrganizationRemoteSessionIssuerEMAProvidersMutation({
    onSuccess: () => toast.success("Provider order saved"),
    onError: () => undefined,
    onSettled: onSaved,
  });
  // A default order has no saved Speakeasy entry; its lone external entry
  // without an issuer stands for the endpoint's trust, if any.
  const shown = order.providers.filter(
    (provider) =>
      provider.kind === "speakeasy" || provider.remoteSessionIssuerId,
  );
  const usesSpeakeasy = shown.some((provider) => provider.kind === "speakeasy");
  const setSpeakeasy = (next: boolean) =>
    save.mutate({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId: vendorRemoteSessionIssuerId,
          providers: toggledSpeakeasyOrder(order, next),
        },
      },
    });

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Text small>Vouched for by</Text>
        {shown.length === 0 ? (
          <Text muted small>
            nobody yet
          </Text>
        ) : (
          shown.map((provider, index) => (
            <Badge
              key={`${provider.kind}-${provider.remoteSessionIssuerId ?? index}`}
              variant={provider.unavailableReason ? "warning" : "neutral"}
              size="sm"
            >
              {index + 1}. {providerLabel(provider)}
              {provider.unavailableReason
                ? ` (${provider.unavailableReason.replaceAll("_", " ")})`
                : ""}
            </Badge>
          ))
        )}
        {hasScope("org:admin", organization.id) && (
          <Button
            variant="secondary"
            size="sm"
            disabled={save.isPending}
            onClick={() => setSpeakeasy(!usesSpeakeasy)}
          >
            {usesSpeakeasy ? "Stop using Speakeasy" : "Use Speakeasy sign-in"}
          </Button>
        )}
      </div>
      <Text muted small className="block">
        {usesSpeakeasy
          ? "Identity chaining tries these in order."
          : "Speakeasy vouches only for vendors whose provider order names it."}{" "}
        The order applies to every server using {order.vendorIssuer}.
      </Text>
      <ApiErrorAlert error={save.error} />
    </div>
  );
}
