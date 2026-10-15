import { Link } from "react-router";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { VendorEmaTrustNotice } from "@/components/vendor-ema-trust-notice";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { identityProvidersHref } from "@/pages/org/identity-provider/tabs";
import { useOrgRoutes } from "@/routes";
import type { EMAProvider } from "@gram/client/models/components/emaprovider.js";
import type { EMAProviderOrder } from "@gram/client/models/components/emaproviderorder.js";
import { useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation } from "@gram/client/react-query/addOrganizationRemoteSessionIssuerSpeakeasyEMAProvider.js";

import { useSpeakeasyAppendCheck } from "./useSpeakeasyAppendCheck";

// The API rejects longer orders.
const MAX_EMA_PROVIDERS = 10;

function providerLabel(provider: EMAProvider): string {
  return provider.kind === "speakeasy" ? "Speakeasy" : "Identity provider";
}

/**
 * The vendor's EMA provider order. Organization admins can append Speakeasy
 * for a server whose sign-in trusts no external identity provider; nothing
 * here removes or reorders providers.
 */
export function EmaProviderOrderControl({
  order,
  vendorRemoteSessionIssuerId,
  userSessionIssuerId,
  trustsExternalIdp,
  speakeasyEnabled,
  speakeasyIssuerUrl,
  chainingReady,
  refreshing,
  onSaved,
}: {
  order: EMAProviderOrder;
  vendorRemoteSessionIssuerId: string;
  userSessionIssuerId: string;
  /** The server's sign-in trusts an external identity provider. */
  trustsExternalIdp: boolean;
  /** Speakeasy's issuer is turned on and can sign. */
  speakeasyEnabled: boolean;
  speakeasyIssuerUrl?: string;
  chainingReady: boolean;
  /** The order is being re-read after a change. */
  refreshing: boolean;
  onSaved: () => void;
}): JSX.Element {
  const organization = useOrganization();
  const session = useSession();
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const usesSpeakeasy =
    !order.isDefault &&
    order.providers.some((provider) => provider.kind === "speakeasy");
  const full = order.providers.length >= MAX_EMA_PROVIDERS;
  const admin = hasScope("org:admin", organization.id);
  const supportSession =
    session.organizationOverride || !!session.impersonatorEmail;
  const mayAdd =
    !usesSpeakeasy && !trustsExternalIdp && speakeasyEnabled && !full;
  const canAdd = mayAdd && admin && !supportSession;
  const target = {
    remoteSessionIssuerId: vendorRemoteSessionIssuerId,
    userSessionIssuerId,
  };
  const check = useSpeakeasyAppendCheck(target, canAdd && !refreshing);
  const save =
    useAddOrganizationRemoteSessionIssuerSpeakeasyEMAProviderMutation({
      onSuccess: () => toast.success("Speakeasy added to the provider order"),
      onError: () => undefined,
      onSettled: onSaved,
    });
  const add = () =>
    save.mutate({
      request: {
        addSpeakeasyEMAProviderRequestBody: { ...target, dryRun: false },
      },
    });

  let note: string;
  if (usesSpeakeasy)
    note =
      "Identity chaining tries these in order. Removing or reordering providers isn't available here yet.";
  else if (trustsExternalIdp)
    note =
      "This server's sign-in trusts an external identity provider, so Speakeasy can't be added here.";
  else if (!speakeasyEnabled)
    note =
      "Turn on Speakeasy's issuer in the organization's identity settings before adding it.";
  else if (full)
    note = `This order already has the maximum of ${MAX_EMA_PROVIDERS} providers, so Speakeasy can't be added.`;
  else if (!admin)
    note = "Only organization admins can add Speakeasy to the provider order.";
  else if (supportSession)
    note =
      "The provider order can't be changed from a support or impersonated session.";
  else
    note =
      "Speakeasy vouches only for vendors whose provider order names it. Adding it puts it after any providers already listed.";

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Text small>Provider order</Text>
        {order.isDefault ? (
          <Text muted small>
            none saved, so each server uses its own identity provider
          </Text>
        ) : (
          order.providers.map((provider, index) => (
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
        {canAdd && (
          <Button
            variant="secondary"
            size="sm"
            disabled={
              save.isPending ||
              refreshing ||
              !check.isSuccess ||
              check.isFetching
            }
            onClick={add}
          >
            Add Speakeasy
          </Button>
        )}
      </div>
      <Text muted small className="block">
        {note} The order applies to every server using {order.vendorIssuer}.
      </Text>
      {canAdd && check.isError && (
        <Text muted small className="block">
          {check.error.message}{" "}
          <Link
            className="underline underline-offset-4"
            to={`${orgRoutes.identity.href()}${identityProvidersHref()}`}
          >
            Organization identity settings
          </Link>
        </Text>
      )}
      {((mayAdd && !usesSpeakeasy) || (usesSpeakeasy && !chainingReady)) && (
        <VendorEmaTrustNotice issuerUrl={speakeasyIssuerUrl} speakeasy />
      )}
      <ApiErrorAlert error={save.error} />
    </div>
  );
}
